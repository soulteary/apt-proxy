package httpcache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gopkg.in/djherbis/stream.v1"
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

type Handler struct {
	Shared    bool
	upstream  http.Handler
	validator *Validator
	cache     Cache
}

func NewHandler(cache Cache, upstream http.Handler) *Handler {
	return &Handler{
		upstream:  upstream,
		cache:     cache,
		validator: &Validator{upstream},
		Shared:    false,
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	cReq, err := newCacheRequest(r)
	if err != nil {
		http.Error(rw, "invalid request: "+err.Error(),
			http.StatusBadRequest)
		return
	}

	if !cReq.isCacheable() {
		debugf("request not cacheable")
		rw.Header().Set(CacheHeader, "SKIP")
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
		debugf("%s %s not in %s cache", r.Method, r.URL.String(), cacheType)
		h.passUpstream(rw, cReq)
		return
	} else {
		debugf("%s %s found in %s cache", r.Method, r.URL.String(), cacheType)
	}

	if h.needsValidation(res, cReq) {
		if cReq.CacheControl.Has("only-if-cached") {
			http.Error(rw, "key was in cache, but required validation",
				http.StatusGatewayTimeout)
			return
		}

		debugf("validating cached response")
		if h.validator.Validate(r, res) {
			debugf("response is valid")
			h.cache.Freshen(res, cReq.Key.String())
		} else {
			debugf("response is changed")
			h.passUpstream(rw, cReq)
			return
		}
	}

	debugf("serving from cache")
	res.Header().Set(CacheHeader, "HIT")
	h.serveResource(res, rw, cReq)

	if err := res.Close(); err != nil {
		errorf("Error closing resource: %s", err.Error())
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
			debugf("using request max-age of %s", reqMaxAge.String())
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
		debugf("using heuristic freshness of %q", hFresh)
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
		debugf("error calculating freshness: %s", err.Error())
		return true
	}

	if r.CacheControl.Has("min-fresh") {
		reqMinFresh, err := r.CacheControl.Duration("min-fresh")
		if err != nil {
			debugf("error parsing request min-fresh: %s", err.Error())
			return true
		}

		if freshness < reqMinFresh {
			debugf("resource is fresh, but won't satisfy min-fresh of %s", reqMinFresh)
			return true
		}
	}

	debugf("resource has a freshness of %s", freshness)

	if freshness <= 0 && r.CacheControl.Has("max-stale") {
		if len(r.CacheControl["max-stale"]) == 0 {
			debugf("resource is stale, but client sent max-stale")
			return false
		} else if maxStale, _ := r.CacheControl.Duration("max-stale"); maxStale >= (freshness * -1) {
			log.Printf("resource is stale, but within allowed max-stale period of %s", maxStale)
			return false
		}
	}

	return freshness <= 0
}

// pipeUpstream makes the request via the upstream handler, the response is not stored or modified
func (h *Handler) pipeUpstream(w http.ResponseWriter, r *cacheRequest) {
	rw := newResponseStreamer(w)
	rdr, err := rw.Stream.NextReader()
	if err != nil {
		debugf("error creating next stream reader: %v", err)
		w.Header().Set(CacheHeader, "SKIP")
		h.upstream.ServeHTTP(w, r.Request)
		return
	}
	defer rdr.Close()

	debugf("piping request upstream")
	go func() {
		h.upstream.ServeHTTP(rw, r.Request)
		rw.Stream.Close()
	}()
	rw.WaitHeaders()

	if r.Method != "HEAD" && !r.isStateChanging() {
		return
	}

	res := rw.Resource()
	defer res.Close()

	if r.Method == "HEAD" {
		h.cache.Freshen(res, r.Key.ForMethod("GET").String())
	} else if res.IsNonErrorStatus() {
		h.invalidateResource(res, r)
	}
}

// passUpstream makes the request via the upstream handler and stores the result
func (h *Handler) passUpstream(w http.ResponseWriter, r *cacheRequest) {
	rw := newResponseStreamer(w)
	rdr, err := rw.Stream.NextReader()
	if err != nil {
		debugf("error creating next stream reader: %v", err)
		w.Header().Set(CacheHeader, "SKIP")
		h.upstream.ServeHTTP(w, r.Request)
		return
	}

	t := Clock()
	debugf("passing request upstream")
	rw.Header().Set(CacheHeader, "MISS")

	go func() {
		h.upstream.ServeHTTP(rw, r.Request)
		rw.Stream.Close()
	}()
	rw.WaitHeaders()
	debugf("upstream responded headers in %s", Clock().Sub(t).String())

	// just the headers!
	res := NewResourceBytes(rw.StatusCode, nil, rw.Header())
	if !h.isCacheable(res, r) {
		rdr.Close()
		debugf("resource is uncacheable")
		rw.Header().Set(CacheHeader, "SKIP")
		return
	}
	b, err := ioutil.ReadAll(rdr)
	rdr.Close()
	if err != nil {
		debugf("error reading stream: %v", err)
		rw.Header().Set(CacheHeader, "SKIP")
		return
	}
	debugf("full upstream response took %s", Clock().Sub(t).String())
	res.ReadSeekCloser = &byteReadSeekCloser{bytes.NewReader(b)}

	if age, err := correctedAge(res.Header(), t, Clock()); err == nil {
		res.Header().Set("Age", strconv.Itoa(int(math.Ceil(age.Seconds()))))
	} else {
		debugf("error calculating corrected age: %s", err.Error())
	}

	rw.Header().Set(ProxyDateHeader, Clock().Format(http.TimeFormat))
	h.storeResource(res, r)
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
		errorf("Error parsing cache-control: %s", err.Error())
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

	debugf("resource is %s old, updating age from %s",
		age.String(), w.Header().Get("Age"))

	w.Header().Set("Age", fmt.Sprintf("%.f", math.Floor(age.Seconds())))
	w.Header().Set("Via", res.Via())

	// hacky handler for non-ok statuses
	if res.Status() != http.StatusOK {
		w.WriteHeader(res.Status())
		io.Copy(w, res)
	} else {
		http.ServeContent(w, req.Request, "", res.LastModified(), res)
	}
}

func (h *Handler) invalidateResource(res *Resource, r *cacheRequest) {
	Writes.Add(1)

	go func() {
		defer Writes.Done()
		debugf("invalidating resource %+v", res)
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
			errorf("storing resources %#v failed with error: %s", keys, err.Error())
		}

		debugf("stored resources %+v in %s", keys, Clock().Sub(t))
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
			debugf("using cached GET request for serving HEAD")
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
		return nil, errors.New("Host header can't be empty")
	}

	return &cacheRequest{
		Request:      r,
		Key:          NewRequestKey(r),
		Time:         Clock(),
		CacheControl: cc,
	}, nil
}

func (r *cacheRequest) isStateChanging() bool {
	if !(r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE") {
		return true
	}

	return false
}

func (r *cacheRequest) isCacheable() bool {
	if !(r.Method == "GET" || r.Method == "HEAD") {
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

func newResponseStreamer(w http.ResponseWriter) *responseStreamer {
	strm, err := stream.NewStream("responseBuffer", stream.NewMemFS())
	if err != nil {
		panic(err)
	}
	return &responseStreamer{
		ResponseWriter: w,
		Stream:         strm,
		C:              make(chan struct{}),
	}
}

type responseStreamer struct {
	StatusCode int
	http.ResponseWriter
	*stream.Stream
	// C will be closed by WriteHeader to signal the headers' writing.
	C chan struct{}
}

// WaitHeaders returns iff and when WriteHeader has been called.
func (rw *responseStreamer) WaitHeaders() {
	for range rw.C {
	}
}

func (rw *responseStreamer) WriteHeader(status int) {
	defer close(rw.C)
	rw.StatusCode = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseStreamer) Write(b []byte) (int, error) {
	rw.Stream.Write(b)
	return rw.ResponseWriter.Write(b)
}
func (rw *responseStreamer) Close() error {
	return rw.Stream.Close()
}

// Resource returns a copy of the responseStreamer as a Resource object
func (rw *responseStreamer) Resource() *Resource {
	r, err := rw.Stream.NextReader()
	if err == nil {
		b, err := ioutil.ReadAll(r)
		r.Close()
		if err == nil {
			return NewResourceBytes(rw.StatusCode, b, rw.Header())
		}
	}
	return &Resource{
		header:         rw.Header(),
		statusCode:     rw.StatusCode,
		ReadSeekCloser: errReadSeekCloser{err},
	}
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
