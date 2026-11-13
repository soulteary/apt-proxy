// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mirrors

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// geoListServer serves body as the geo mirror list.
func geoListServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// Package requests are sent to whatever discovery returns, so a line that is
// not a plain http(s) mirror URL must never become a candidate.
func TestGeoDiscoveryAcceptsOnlyMirrorURLs(t *testing.T) {
	server := geoListServer(t, strings.Join([]string{
		"http://mirror-a.example/ubuntu/",
		"",
		"  https://mirror-b.example:8443/ubuntu/  ",
		"mirror-c.example/ubuntu/",
		"ftp://mirror-d.example/ubuntu/",
		"file:///etc/passwd",
		"http://user:secret@mirror-e.example/ubuntu/",
		"http://archive.ubuntu.com@mirror-f.example/ubuntu/",
		"http:///ubuntu/",
		"http://:80/ubuntu/",
		"https:mirror-g.example/ubuntu/",
		"//mirror-h.example/ubuntu/",
		"/ubuntu/",
		"http://mirror-i.example/ubuntu/\x7f",
		"<html><body>Sign in to continue</body></html>",
		"http://mirror-j.example/ubuntu/",
	}, "\n"))

	got, err := ubuntuMirrorsWithClient(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"http://mirror-a.example/ubuntu/",
		"https://mirror-b.example:8443/ubuntu/",
		"http://mirror-j.example/ubuntu/",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("discovered mirrors = %q, want %q", got, want)
	}
}

func TestGeoDiscoveryCapsAcceptedMirrors(t *testing.T) {
	var list strings.Builder
	for i := range 2 * maxGeoMirrors {
		// An unusable line between entries: the cap counts accepted mirrors.
		fmt.Fprintf(&list, "not a mirror\nhttp://mirror-%03d.example/ubuntu/\n", i)
	}
	server := geoListServer(t, list.String())

	got, err := ubuntuMirrorsWithClient(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxGeoMirrors {
		t.Fatalf("discovered %d mirrors, want the first %d", len(got), maxGeoMirrors)
	}
	if first, last := "http://mirror-000.example/ubuntu/", fmt.Sprintf("http://mirror-%03d.example/ubuntu/", maxGeoMirrors-1); got[0] != first || got[len(got)-1] != last {
		t.Errorf("discovered %q ... %q, want %q ... %q", got[0], got[len(got)-1], first, last)
	}
}

// A list without a single usable entry, such as a captive portal page served
// with status 200, falls back to the built-in mirrors.
func TestGeoDiscoveryWithoutUsableEntriesFallsBack(t *testing.T) {
	server := geoListServer(t, "<html>\n<body>Sign in to continue</body>\n</html>\n")

	got := GetGeoMirrorUrlsByModeWithLookup(nil, distro.TypeUbuntu, func() ([]string, error) {
		return ubuntuMirrorsWithClient(context.Background(), server.Client(), server.URL)
	})
	if want := builtinMirrorURLs(distro.BuiltinUbuntuMirrors); !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %q, want the built-in mirrors", got)
	}
}

func TestGetUbuntuMirrorUrlsByGeo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mirrors, err := GetUbuntuMirrorUrlsByGeoCtx(ctx)
	if err != nil {
		t.Skipf("upstream Ubuntu mirrors API unavailable: %v", err)
	}
	if len(mirrors) == 0 {
		t.Fatal("get ubuntu get mirrors failed")
	}
}
