package httpcache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	logger "github.com/soulteary/logger-kit"
)

const (
	CacheHeader     = "X-Cache"
	ProxyDateHeader = "Proxy-Date"
)

var Writes sync.WaitGroup

var storeable = map[int]bool{
	http.StatusOK:                   true,
	http.StatusFound:                true,
	http.StatusNonAuthoritativeInfo: true,
	http.StatusMultipleChoices:      true,
	http.StatusMovedPermanently:     true,
	http.StatusGone:                 true,
	http.StatusNotFound:             true,
}

var cacheableByDefault = map[int]bool{
	http.StatusOK:                   true,
	http.StatusFound:                true,
	http.StatusNotModified:          true,
	http.StatusNonAuthoritativeInfo: true,
	http.StatusMultipleChoices:      true,
	http.StatusMovedPermanently:     true,
	http.StatusGone:                 true,
	http.StatusPartialContent:       true,
}

// HandlerOptions holds optional configuration for NewHandlerWithOptions.
// Logger injected here is used by the handler for all debug/error logging;
// if nil, the package-level logger (see SetLogger) is used.
type HandlerOptions struct {
	Logger *logger.Logger
}

type Handler struct {
	Shared    bool
	upstream  http.Handler
	validator *Validator
	cache     Cache
	metrics   *CacheMetrics
	log       *logger.Logger
}

// NewHandler returns a cache handler with default options (package-level logger).
func NewHandler(cache Cache, upstream http.Handler) *Handler {
	return NewHandlerWithOptions(cache, upstream, nil)
}

// NewHandlerWithOptions returns a cache handler with the given options.
// If opts.Logger is set, it is used for handler logging; otherwise the package-level logger is used.
func NewHandlerWithOptions(cache Cache, upstream http.Handler, opts *HandlerOptions) *Handler {
	h := &Handler{
		upstream:  upstream,
		cache:     cache,
		validator: &Validator{upstream},
		Shared:    false,
		metrics:   DefaultMetrics,
	}
	if opts != nil && opts.Logger != nil {
		h.log = opts.Logger
	}
	return h
}

func (h *Handler) logRef() *logger.Logger {
	if h.log != nil {
		return h.log
	}
	return cacheLogger
}

func (h *Handler) debugf(format string, args ...interface{}) {
	if DebugLogging {
		h.logRef().Debug().Msgf(format, args...)
	}
}

func (h *Handler) errorf(format string, args ...interface{}) {
	h.logRef().Error().Msgf(format, args...)
}

// SetMetrics sets the metrics instance for the handler
func (h *Handler) SetMetrics(m *CacheMetrics) {
	h.metrics = m
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	cReq, err := newCacheRequest(r)
	if err != nil {
		http.Error(rw, "invalid request: "+err.Error(),
			http.StatusBadRequest)
		return
	}

	if !cReq.isCacheable() {
		h.debugf("request not cacheable")
		rw.Header().Set(CacheHeader, "SKIP")
		if h.metrics != nil {
			h.metrics.RecordCacheSkip()
		}
		h.pipeUpstream(rw, cReq)
		return
	}

	res, err := h.lookup(cReq)
	if err != nil && err != ErrNotFoundInCache {
		http.Error(rw, "lookup error: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	cacheType := "private"
	if h.Shared {
		cacheType = "shared"
	}

	if err == ErrNotFoundInCache {
		if cReq.CacheControl.Has("only-if-cached") {
			http.Error(rw, "key not in cache",
				http.StatusGatewayTimeout)
			return
		}
		h.debugf("%s %s not in %s cache", r.Method, r.URL.String(), cacheType)
		if h.metrics != nil {
			h.metrics.RecordCacheMiss(r.Method)
		}
		h.passUpstream(rw, cReq)
		return
	} else {
		h.debugf("%s %s found in %s cache", r.Method, r.URL.String(), cacheType)
	}

	if h.needsValidation(res, cReq) {
		if cReq.CacheControl.Has("only-if-cached") {
			http.Error(rw, "key was in cache, but required validation",
				http.StatusGatewayTimeout)
			return
		}

		h.debugf("validating cached response")
		if h.validator.Validate(r, res) {
			h.debugf("response is valid")
			_ = h.cache.Freshen(res, cReq.Key.String())
		} else {
			h.debugf("response is changed")
			h.passUpstream(rw, cReq)
			return
		}
	}

	h.debugf("serving from cache")
	res.Header().Set(CacheHeader, "HIT")
	if h.metrics != nil {
		h.metrics.RecordCacheHit(r.Method)
	}
	h.serveResource(res, rw, cReq)

	if err := res.Close(); err != nil {
		h.errorf("Error closing resource: %s", err.Error())
	}
}

// freshness returns the duration that a requested resource will be fresh for
func (h *Handler) freshness(res *Resource, r *cacheRequest) (time.Duration, error) {
	maxAge, err := res.MaxAge(h.Shared)
	if err != nil {
		return time.Duration(0), err
	}

	if r.CacheControl.Has("max-age") {
		reqMaxAge, err := r.CacheControl.Duration("max-age")
		if err != nil {
			return time.Duration(0), err
		}

		if reqMaxAge < maxAge {
			h.debugf("using request max-age of %s", reqMaxAge.String())
			maxAge = reqMaxAge
		}
	}

	age, err := res.Age()
	if err != nil {
		return time.Duration(0), err
	}

	if res.IsStale() {
		return time.Duration(0), nil
	}

	if hFresh := res.HeuristicFreshness(); hFresh > maxAge {
		h.debugf("using heuristic freshness of %q", hFresh)
		maxAge = hFresh
	}

	return maxAge - age, nil
}

func (h *Handler) needsValidation(res *Resource, r *cacheRequest) bool {
	if res.MustValidate(h.Shared) {
		return true
	}

	freshness, err := h.freshness(res, r)
	if err != nil {
		h.debugf("error calculating freshness: %s", err.Error())
		return true
	}

	if r.CacheControl.Has("min-fresh") {
		reqMinFresh, err := r.CacheControl.Duration("min-fresh")
		if err != nil {
			h.debugf("error parsing request min-fresh: %s", err.Error())
			return true
		}

		if freshness < reqMinFresh {
			h.debugf("resource is fresh, but won't satisfy min-fresh of %s", reqMinFresh)
			return true
		}
	}

	h.debugf("resource has a freshness of %s", freshness)

	if freshness <= 0 && r.CacheControl.Has("max-stale") {
		if len(r.CacheControl["max-stale"]) == 0 {
			h.debugf("resource is stale, but client sent max-stale")
			return false
		} else if maxStale, _ := r.CacheControl.Duration("max-stale"); maxStale >= (freshness * -1) {
			h.debugf("resource is stale, but within allowed max-stale period of %s", maxStale)
			return false
		}
	}

	return freshness <= 0
}

// pipeUpstream makes the request via the upstream handler, the response is not stored or modified
func (h *Handler) pipeUpstream(w http.ResponseWriter, r *cacheRequest) {
	rw, err := newResponseStreamer(w)
	if err != nil {
		h.debugf("error creating response streamer: %v", err)
		w.Header().Set(CacheHeader, "SKIP")
		h.upstream.ServeHTTP(w, r.Request)
		return
	}
	rdr, err := rw.NextReader()
	if err != nil {
		h.debugf("error creating next stream reader: %v", err)
		w.Header().Set(CacheHeader, "SKIP")
		h.upstream.ServeHTTP(w, r.Request)
		return
	}
	defer func() { _ = rdr.Close() }()

	h.debugf("piping request upstream")
	go func() {
		h.upstream.ServeHTTP(rw, r.Request)
		_ = rw.Close()
	}()
	rw.WaitHeaders()

	if r.Method != "HEAD" && !r.isStateChanging() {
		return
	}

	res := rw.Resource()
	defer func() { _ = res.Close() }()

	if r.Method == "HEAD" {
		_ = h.cache.Freshen(res, r.Key.ForMethod("GET").String())
	} else if res.IsNonErrorStatus() {
		h.invalidateResource(res, r)
	}
}

// passUpstream makes the request via the upstream handler and stores the result
// It uses streaming to avoid loading the entire response into memory for large files.
func (h *Handler) passUpstream(w http.ResponseWriter, r *cacheRequest) {
	rw, err := newResponseStreamer(w)
	if err != nil {
		h.debugf("error creating response streamer: %v", err)
		w.Header().Set(CacheHeader, "SKIP")
		h.upstream.ServeHTTP(w, r.Request)
		return
	}
	rdr, err := rw.NextReader()
	if err != nil {
		h.debugf("error creating next stream reader: %v", err)
		w.Header().Set(CacheHeader, "SKIP")
		h.upstream.ServeHTTP(w, r.Request)
		return
	}

	t := Clock()
	h.debugf("passing request upstream")
	rw.Header().Set(CacheHeader, "MISS")

	go func() {
		h.upstream.ServeHTTP(rw, r.Request)
		_ = rw.Close()
	}()
	rw.WaitHeaders()
	h.debugf("upstream responded headers in %s", Clock().Sub(t).String())

	// just the headers!
	res := NewResourceBytes(rw.StatusCode, nil, rw.Header())
	if !h.isCacheable(res, r) {
		h.debugf("resource is uncacheable")
		rw.Header().Set(CacheHeader, "SKIP")
		// Drain body so upstream goroutine can finish and client receives the response
		_, _ = io.Copy(io.Discard, rdr)
		_ = rdr.Close()
		return
	}

	// Create temporary file to store body for caching (supports multiple reads for multiple keys)
	tmpFile, err := os.CreateTemp("", "httpcache-*.tmp")
	if err != nil {
		h.debugf("error creating temp file: %v, falling back to in-memory", err)
		// Fallback to in-memory for small files
		b, err := io.ReadAll(rdr)
		_ = rdr.Close()
		if err != nil {
			h.debugf("error reading stream: %v", err)
			rw.Header().Set(CacheHeader, "SKIP")
			return
		}
		res.ReadSeekCloser = &byteReadSeekCloser{bytes.NewReader(b)}
		h.finishPassUpstream(res, r, rw, t)
		return
	}
	tmpPath := tmpFile.Name()

	// Since responseStreamer.Write already writes to both pipeWriter (for pipeReader) and ResponseWriter (client),
	// the client is already receiving data. We read from pipeReader and write to temp file for caching.
	// This allows us to cache without blocking the client response.
	_, err = io.Copy(tmpFile, rdr)
	if err != nil {
		h.debugf("error copying to temp file: %v", err)
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		_ = rdr.Close()
		rw.Header().Set(CacheHeader, "SKIP")
		return
	}

	// Close temp file for writing
	if err := tmpFile.Close(); err != nil {
		h.debugf("error closing temp file: %v", err)
		_ = os.Remove(tmpPath)
		_ = rdr.Close()
		rw.Header().Set(CacheHeader, "SKIP")
		return
	}
	_ = rdr.Close()

	upstreamDuration := Clock().Sub(t)
	h.debugf("full upstream response took %s", upstreamDuration.String())

	// Create Resource from temp file for caching
	tmpFileReader, err := os.Open(tmpPath)
	if err != nil {
		h.debugf("error reopening temp file for caching: %v", err)
		_ = os.Remove(tmpPath)
		rw.Header().Set(CacheHeader, "SKIP")
		return
	}

	// Create a ReadSeekCloser from temp file
	// The temp file will be cleaned up after caching is complete
	res.ReadSeekCloser = &tempFileReadSeekCloser{file: tmpFileReader, path: tmpPath}

	// Record upstream duration metric
	if h.metrics != nil {
		h.metrics.RecordUpstreamDuration(r.Method, rw.StatusCode, upstreamDuration.Seconds())
	}

	if age, err := correctedAge(res.Header(), t, Clock()); err == nil {
		res.Header().Set("Age", strconv.Itoa(int(math.Ceil(age.Seconds()))))
	} else {
		h.debugf("error calculating corrected age: %s", err.Error())
	}

	rw.Header().Set(ProxyDateHeader, Clock().Format(http.TimeFormat))

	// Store resource in background - errors won't affect client response
	h.storeResource(res, r)
}

// finishPassUpstream completes the passUpstream flow for in-memory fallback
func (h *Handler) finishPassUpstream(res *Resource, r *cacheRequest, rw *responseStreamer, t time.Time) {
	upstreamDuration := Clock().Sub(t)
	h.debugf("full upstream response took %s", upstreamDuration.String())

	// Record upstream duration metric
	if h.metrics != nil {
		h.metrics.RecordUpstreamDuration(r.Method, rw.StatusCode, upstreamDuration.Seconds())
	}

	if age, err := correctedAge(res.Header(), t, Clock()); err == nil {
		res.Header().Set("Age", strconv.Itoa(int(math.Ceil(age.Seconds()))))
	} else {
		h.debugf("error calculating corrected age: %s", err.Error())
	}

	rw.Header().Set(ProxyDateHeader, Clock().Format(http.TimeFormat))
	h.storeResource(res, r)
}

// tempFileReadSeekCloser implements ReadSeekCloser for temporary files
type tempFileReadSeekCloser struct {
	file *os.File
	path string
}

func (t *tempFileReadSeekCloser) Read(p []byte) (n int, err error) {
	return t.file.Read(p)
}

func (t *tempFileReadSeekCloser) Seek(offset int64, whence int) (int64, error) {
	return t.file.Seek(offset, whence)
}

func (t *tempFileReadSeekCloser) Close() error {
	var err error
	if t.file != nil {
		err = t.file.Close()
	}
	// Clean up temp file after use
	if t.path != "" {
		if removeErr := os.Remove(t.path); removeErr != nil && err == nil {
			err = removeErr
		}
	}
	return err
}

// correctedAge adjusts the age of a resource for clock skew and travel time
// https://httpwg.github.io/specs/rfc7234.html#rfc.section.4.2.3
func correctedAge(h http.Header, reqTime, respTime time.Time) (time.Duration, error) {
	date, err := timeHeader("Date", h)
	if err != nil {
		return time.Duration(0), err
	}

	apparentAge := respTime.Sub(date)
	if apparentAge < 0 {
		apparentAge = 0
	}

	respDelay := respTime.Sub(reqTime)
	ageSeconds, err := intHeader("Age", h)
	if err != nil {
		return time.Duration(0), err
	}
	age := time.Second * time.Duration(ageSeconds)
	correctedAge := age + respDelay

	if apparentAge > correctedAge {
		correctedAge = apparentAge
	}

	residentTime := Clock().Sub(respTime)
	currentAge := correctedAge + residentTime

	return currentAge, nil
}

func (h *Handler) isCacheable(res *Resource, r *cacheRequest) bool {
	cc, err := res.cacheControl()
	if err != nil {
		h.errorf("Error parsing cache-control: %s", err.Error())
		return false
	}

	if cc.Has("no-cache") || cc.Has("no-store") {
		return false
	}

	if cc.Has("private") && len(cc["private"]) == 0 && h.Shared {
		return false
	}

	if _, ok := storeable[res.Status()]; !ok {
		return false
	}

	if r.Header.Get("Authorization") != "" && h.Shared {
		return false
	}

	if res.Header().Get("Authorization") != "" && h.Shared &&
		!cc.Has("must-revalidate") && !cc.Has("s-maxage") {
		return false
	}

	if res.HasExplicitExpiration() {
		return true
	}

	if _, ok := cacheableByDefault[res.Status()]; !ok && !cc.Has("public") {
		return false
	}

	if res.HasValidators() {
		return true
	} else if res.HeuristicFreshness() > 0 {
		return true
	}

	return false
}

func (h *Handler) serveResource(res *Resource, w http.ResponseWriter, req *cacheRequest) {
	for key, headers := range res.Header() {
		for _, header := range headers {
			w.Header().Add(key, header)
		}
	}

	age, err := res.Age()
	if err != nil {
		http.Error(w, "Error calculating age: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	// http://httpwg.github.io/specs/rfc7234.html#warn.113
	if age > (time.Hour*24) && res.HeuristicFreshness() > (time.Hour*24) {
		w.Header().Add("Warning", `113 - "Heuristic Expiration"`)
	}

	// http://httpwg.github.io/specs/rfc7234.html#warn.110
	freshness, err := h.freshness(res, req)
	if err != nil || freshness <= 0 {
		w.Header().Add("Warning", `110 - "Response is Stale"`)
	}

	h.debugf("resource is %s old, updating age from %s",
		age.String(), w.Header().Get("Age"))

	w.Header().Set("Age", fmt.Sprintf("%.f", math.Floor(age.Seconds())))
	w.Header().Set("Via", res.Via())

	// hacky handler for non-ok statuses
	if res.Status() != http.StatusOK {
		w.WriteHeader(res.Status())
		_, _ = io.Copy(w, res)
	} else {
		http.ServeContent(w, req.Request, "", res.LastModified(), res)
	}
}

func (h *Handler) invalidateResource(res *Resource, r *cacheRequest) {
	Writes.Add(1)

	go func() {
		defer Writes.Done()
		h.debugf("invalidating resource %+v", res)
	}()
}

func (h *Handler) storeResource(res *Resource, r *cacheRequest) {
	Writes.Add(1)

	go func() {
		defer Writes.Done()
		t := Clock()
		keys := []string{r.Key.String()}
		headers := res.Header()

		if h.Shared {
			res.RemovePrivateHeaders()
		}

		// store a secondary vary version
		if vary := headers.Get("Vary"); vary != "" {
			keys = append(keys, r.Key.Vary(vary, r.Request).String())
		}

		if err := h.cache.Store(res, keys...); err != nil {
			h.errorf("storing resources %#v failed with error: %s", keys, err.Error())
			if h.metrics != nil {
				h.metrics.RecordStoreOperation(false)
			}
		} else {
			if h.metrics != nil {
				h.metrics.RecordStoreOperation(true)
			}
		}

		h.debugf("stored resources %+v in %s", keys, Clock().Sub(t))
	}()
}

// lookupResource finds the best matching Resource for the
// request, or nil and ErrNotFoundInCache if none is found
func (h *Handler) lookup(req *cacheRequest) (*Resource, error) {
	res, err := h.cache.Retrieve(req.Key.String())

	// HEAD requests can possibly be served from GET
	if err == ErrNotFoundInCache && req.Method == "HEAD" {
		res, err = h.cache.Retrieve(req.Key.ForMethod("GET").String())
		if err != nil {
			return nil, err
		}

		if res.HasExplicitExpiration() && req.isCacheable() {
			h.debugf("using cached GET request for serving HEAD")
			return res, nil
		} else {
			return nil, ErrNotFoundInCache
		}
	} else if err != nil {
		return res, err
	}

	// Secondary lookup for Vary
	if vary := res.Header().Get("Vary"); vary != "" {
		res, err = h.cache.Retrieve(req.Key.Vary(vary, req.Request).String())
		if err != nil {
			return res, err
		}
	}

	return res, nil
}

type cacheRequest struct {
	*http.Request
	Key          Key
	Time         time.Time
	CacheControl CacheControl
}

func newCacheRequest(r *http.Request) (*cacheRequest, error) {
	cc, err := ParseCacheControl(r.Header.Get("Cache-Control"))
	if err != nil {
		return nil, err
	}

	if r.Proto == "HTTP/1.1" && r.Host == "" {
		return nil, errors.New("host header can't be empty")
	}

	return &cacheRequest{
		Request:      r,
		Key:          NewRequestKey(r),
		Time:         Clock(),
		CacheControl: cc,
	}, nil
}

func (r *cacheRequest) isStateChanging() bool {
	return r.Method != "POST" && r.Method != "PUT" && r.Method != "DELETE"
}

func (r *cacheRequest) isCacheable() bool {
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}

	if r.Header.Get("If-Match") != "" ||
		r.Header.Get("If-Unmodified-Since") != "" ||
		r.Header.Get("If-Range") != "" {
		return false
	}

	if maxAge, ok := r.CacheControl.Get("max-age"); ok && maxAge == "0" {
		return false
	}

	if r.CacheControl.Has("no-store") || r.CacheControl.Has("no-cache") {
		return false
	}

	return true
}

func newResponseStreamer(w http.ResponseWriter) (*responseStreamer, error) {
	pr, pw := io.Pipe()
	return &responseStreamer{
		ResponseWriter: w,
		pipeReader:     pr,
		pipeWriter:     pw,
		C:              make(chan struct{}),
	}, nil
}

type responseStreamer struct {
	StatusCode int
	http.ResponseWriter
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	// C is closed by WriteHeader to signal the headers' writing. headerOnce ensures it is closed at most once.
	C          chan struct{}
	headerOnce sync.Once
}

// WaitHeaders returns when WriteHeader has been called (i.e. rw.C is closed).
func (rw *responseStreamer) WaitHeaders() {
	for range rw.C {
	}
}

// WriteHeader implements http.ResponseWriter. Safe if called more than once; only the first call closes C and writes the status.
func (rw *responseStreamer) WriteHeader(status int) {
	rw.headerOnce.Do(func() { close(rw.C) })
	rw.StatusCode = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseStreamer) Write(b []byte) (int, error) {
	return io.MultiWriter(rw.pipeWriter, rw.ResponseWriter).Write(b)
}

func (rw *responseStreamer) Close() error {
	return rw.pipeWriter.Close()
}

// NextReader returns a reader for the response body (reads from the same pipe that upstream writes to).
func (rw *responseStreamer) NextReader() (io.ReadCloser, error) {
	return rw.pipeReader, nil
}

// Resource returns a copy of the responseStreamer as a Resource object
func (rw *responseStreamer) Resource() *Resource {
	b, err := io.ReadAll(rw.pipeReader)
	if err != nil {
		return &Resource{
			header:         rw.Header(),
			statusCode:     rw.StatusCode,
			ReadSeekCloser: errReadSeekCloser{err},
		}
	}
	return NewResourceBytes(rw.StatusCode, b, rw.Header())
}

type errReadSeekCloser struct {
	err error
}

func (e errReadSeekCloser) Error() string {
	return e.err.Error()
}
func (e errReadSeekCloser) Close() error                       { return e.err }
func (e errReadSeekCloser) Read(_ []byte) (int, error)         { return 0, e.err }
func (e errReadSeekCloser) Seek(_ int64, _ int) (int64, error) { return 0, e.err }
