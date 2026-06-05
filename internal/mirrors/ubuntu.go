package mirrors

import (
	"bufio"
	"context"
	"net/http"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// ubuntuGeoLookupTimeout caps how long we wait for the upstream Ubuntu
// mirrors API to respond. Bare http.Get had no timeout, which could stall
// benchmark/startup indefinitely when the API was unreachable.
const ubuntuGeoLookupTimeout = 5 * time.Second

// GetUbuntuMirrorUrlsByGeo fetches the geo-localized mirrors list using a
// background context with a fixed timeout. Prefer GetUbuntuMirrorUrlsByGeoCtx
// when a caller-provided context is available (e.g. inside benchmark flow).
func GetUbuntuMirrorUrlsByGeo() (mirrors []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), ubuntuGeoLookupTimeout)
	defer cancel()
	return GetUbuntuMirrorUrlsByGeoCtx(ctx)
}

// GetUbuntuMirrorUrlsByGeoCtx fetches Ubuntu's mirror list honoring the
// caller-provided context for cancellation/deadline.
func GetUbuntuMirrorUrlsByGeoCtx(ctx context.Context) (mirrors []string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, distro.UbuntuGeoMirrorAPI, nil)
	if err != nil {
		return mirrors, err
	}
	client := &http.Client{Timeout: ubuntuGeoLookupTimeout}
	response, err := client.Do(req)
	if err != nil {
		return mirrors, err
	}
	defer func() { _ = response.Body.Close() }()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		mirrors = append(mirrors, scanner.Text())
	}
	return mirrors, scanner.Err()
}
