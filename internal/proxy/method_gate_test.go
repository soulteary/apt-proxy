package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepositoryMethodGate(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		if rejectRepositoryMethod(w, httptest.NewRequest(method, "/ubuntu/pool/test.deb", nil)) || w.Body.Len() != 0 || len(w.Header()) != 0 {
			t.Fatalf("read method %s was rejected or its response modified", method)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace, "CUSTOM"} {
		t.Run(method, func(t *testing.T) {
			// No routing/logger state exists: rejection must happen before either
			// is used and before the downstream cache/upstream handler is invoked.
			ps := &PackageStruct{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("rejected method reached cache/upstream") })}
			w := httptest.NewRecorder()
			ps.ServeHTTP(w, httptest.NewRequest(method, "/ubuntu/pool/test.deb", nil))
			if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, HEAD" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("method %s: status=%d headers=%v", method, w.Code, w.Header())
			}
		})
	}
}
