package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/benchmarks"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// Exercise fast callbacks while subsequent initial rewriters are still being
// installed. Run with -race to check that initialization shares the callback lock.
func TestAsyncInitializationPublishesBeforeCallbacks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("package"))
	}))
	defer upstream.Close()
	reg := newTestRegistry()
	for _, d := range reg.GetAll() {
		d.GeoMirrorAPI = ""
		d.Builtin = false
		d.Mirrors = []distro.URLWithAlias{
			{URL: "http://127.0.0.1:1/", Scheme: "http"},
			{URL: upstream.URL + "/", Scheme: "http"},
		}
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		st := newTestState()
		for _, d := range reg.GetAll() {
			st.SetMirror(d.Type, "")
		}
		engine := benchmarks.NewEngine()
		rewriters := CreateNewRewritersAsyncWithEngine(distro.TypeAllDistros, st, reg, engine)
		deadline := time.Now().Add(5 * time.Second)
		for {
			complete := true
			rewriters.Mu.RLock()
			for _, mode := range modesToInit(distro.TypeAllDistros, reg) {
				rw := rewriters.get(mode)
				if rw == nil || rw.mirror == nil {
					t.Errorf("mode %d was not initialized", mode)
				}
				if rw == nil || rw.mirror == nil || rw.mirror.String() != upstream.URL+"/" {
					complete = false
				}
			}
			rewriters.Mu.RUnlock()
			if complete {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("background mirror elections did not complete")
			}
			time.Sleep(time.Millisecond)
		}
	}
}
