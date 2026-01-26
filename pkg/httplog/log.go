package httplog

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	logger "github.com/soulteary/logger-kit"
)

const (
	CacheHeader = "X-Cache"
)

type responseWriter struct {
	http.ResponseWriter
	status      int
	size        int
	t           time.Time
	errorOutput bytes.Buffer
}

func (l *responseWriter) Header() http.Header {
	return l.ResponseWriter.Header()
}

func (l *responseWriter) Write(b []byte) (int, error) {
	if l.status == 0 {
		l.status = http.StatusOK
	}
	if isError(l.status) {
		l.errorOutput.Write(b)
	}
	size, err := l.ResponseWriter.Write(b)
	l.size += size
	return size, err
}

func (l *responseWriter) WriteHeader(s int) {
	l.ResponseWriter.WriteHeader(s)
	l.status = s
}

func (l *responseWriter) Status() int {
	return l.status
}

func (l *responseWriter) Size() int {
	return l.size
}

func NewResponseLogger(delegate http.Handler, log *logger.Logger) *ResponseLogger {
	return &ResponseLogger{Handler: delegate, logger: log}
}

type ResponseLogger struct {
	http.Handler
	logger                                  *logger.Logger
	DumpRequests, DumpErrors, DumpResponses bool
}

func (l *ResponseLogger) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if l.DumpRequests {
		b, _ := httputil.DumpRequest(req, false)
		writePrefixString(strings.TrimSpace(string(b)), ">> ", os.Stderr)
	}

	respWr := &responseWriter{ResponseWriter: w, t: time.Now()}
	l.Handler.ServeHTTP(respWr, req)

	if l.DumpResponses {
		buf := &bytes.Buffer{}
		buf.WriteString(fmt.Sprintf("HTTP/1.1 %d %s\r\n",
			respWr.status, http.StatusText(respWr.status),
		))
		respWr.Header().Write(buf)
		writePrefixString(strings.TrimSpace(buf.String()), "<< ", os.Stderr)
	}

	if l.DumpErrors && isError(respWr.status) {
		writePrefixString(respWr.errorOutput.String(), "<< ", os.Stderr)
	}

	l.writeLog(req, respWr)
}

func (l *ResponseLogger) writeLog(req *http.Request, respWr *responseWriter) {
	cacheStatus := respWr.Header().Get(CacheHeader)
	cacheLabel := "SKIP"

	if strings.HasPrefix(cacheStatus, "HIT") {
		cacheLabel = "HIT"
	} else if strings.HasPrefix(cacheStatus, "MISS") {
		cacheLabel = "MISS"
	}

	clientIP := req.RemoteAddr
	if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
		clientIP = clientIP[:colon]
	}

	latency := time.Since(respWr.t)

	// Choose log level based on status code
	var event *logger.Logger
	if respWr.status >= 500 {
		event = l.logger
		event.Error().
			Str("ip", clientIP).
			Str("method", req.Method).
			Str("path", req.URL.String()).
			Str("proto", req.Proto).
			Int("status", respWr.status).
			Int("size", respWr.size).
			Str("cache", cacheLabel).
			Dur("latency", latency).
			Msg("HTTP request")
	} else if respWr.status >= 400 {
		l.logger.Warn().
			Str("ip", clientIP).
			Str("method", req.Method).
			Str("path", req.URL.String()).
			Str("proto", req.Proto).
			Int("status", respWr.status).
			Int("size", respWr.size).
			Str("cache", cacheLabel).
			Dur("latency", latency).
			Msg("HTTP request")
	} else {
		l.logger.Info().
			Str("ip", clientIP).
			Str("method", req.Method).
			Str("path", req.URL.String()).
			Str("proto", req.Proto).
			Int("status", respWr.status).
			Int("size", respWr.size).
			Str("cache", cacheLabel).
			Dur("latency", latency).
			Msg("HTTP request")
	}
}

func isError(code int) bool {
	return code >= 500
}

func writePrefixString(s, prefix string, w io.Writer) {
	os.Stderr.Write([]byte("\n"))
	for _, line := range strings.Split(s, "\r\n") {
		w.Write([]byte(prefix))
		w.Write([]byte(line))
		w.Write([]byte("\n"))
	}
	os.Stderr.Write([]byte("\n"))
}
