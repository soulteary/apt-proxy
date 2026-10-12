package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
)

func TestRuntimeMirrorHealthConcurrentRefresh(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "metadata") }))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "metadata") }))
	defer secondary.Close()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, primary.URL+"/ubuntu/")
	ps, err := NewPackageStruct(Options{State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu,
		MirrorHealth: &mirrorhealth.Config{Enabled: true, Repositories: []mirrorhealth.Repository{{ID: "ubuntu", Mirrors: []string{primary.URL + "/ubuntu/", secondary.URL + "/ubuntu/"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				rec := httptest.NewRecorder()
				ps.ServeHTTP(rec, httptest.NewRequest("GET", "/ubuntu/dists/jammy/InRelease", nil))
				if rec.Code != 200 || rec.Body.String() != "metadata" {
					t.Errorf("request while refreshing: %d %q", rec.Code, rec.Body.String())
					return
				}
				_ = ps.MirrorHealthStatus()
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if err := ps.RefreshMirrors(); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
