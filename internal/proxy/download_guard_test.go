package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestUnknownLengthDownloadTempFileCleanup(t *testing.T) {
	req := httptest.NewRequest("GET", "http://mirror.example/package.deb", nil)
	transport := &completeUnknownBodyTransport{RoundTripper: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("complete"))}, nil
	})}
	res, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := res.Body.(*downloadFile)
	if !ok {
		t.Fatal("unknown body was not spooled")
	}
	path := file.path
	if res.ContentLength != 8 || res.Header.Get("Content-Length") != "8" {
		t.Fatal("completed length not published")
	}
	body, err := io.ReadAll(res.Body)
	if err != nil || string(body) != "complete" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary download retained: %v", err)
	}
}

func TestAbortSafeHandlerDoesNotHideUnexpectedPanics(t *testing.T) {
	for _, value := range []any{http.ErrAbortHandler, "programming error"} {
		func() {
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				h := &abortSafeHandler{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(value) })}
				h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			}()
			if value == http.ErrAbortHandler {
				if recovered != nil {
					t.Fatal("intentional abort escaped")
				}
			} else if recovered != value {
				t.Fatal("unexpected panic swallowed")
			}
		}()
	}
}
