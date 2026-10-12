package proxy

import (
	"net/http"

	"github.com/soulteary/apt-proxy/internal/mirrorhealth"
)

type mirrorHealthKey struct{}
type mirrorHealthScope struct {
	id    string
	epoch uint64
}
type mirrorHealthTransport struct {
	controller *mirrorhealth.Controller
	base       http.RoundTripper
}

func (t *mirrorHealthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	scope, ok := r.Context().Value(mirrorHealthKey{}).(mirrorHealthScope)
	if !ok {
		return t.base.RoundTrip(r)
	}
	return t.controller.RoundTrip(r, scope.id, scope.epoch)
}

// MirrorHealthStatus exposes public, process-local decisions without credentials.
func (ap *PackageStruct) MirrorHealthStatus() mirrorhealth.State {
	if ap == nil {
		return (*mirrorhealth.Controller)(nil).State()
	}
	return ap.mirrorHealth.State()
}
