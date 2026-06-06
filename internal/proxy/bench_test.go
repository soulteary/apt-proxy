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

package proxy

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

// BenchmarkURLRewrite benchmarks URL rewrite performance for different distributions
func BenchmarkURLRewrite(b *testing.B) {
	st := newTestState()
	reg := newTestRegistry()

	rewriters := CreateNewRewriters(distro.TypeAllDistros, st, reg)
	if rewriters == nil {
		b.Fatal("CreateNewRewriters() returned nil")
	}

	testCases := []struct {
		name    string
		mode    int
		urlPath string
		matches bool
	}{
		{"UbuntuMatch", distro.TypeUbuntu, "/ubuntu/dists/jammy/main/binary-amd64/Packages", true},
		{"UbuntuNoMatch", distro.TypeUbuntu, "/other/path", false},
		{"DebianMatch", distro.TypeDebian, "/debian/dists/bullseye/main/binary-amd64/Packages", true},
		{"DebianNoMatch", distro.TypeDebian, "/other/path", false},
		{"CentOSMatch", distro.TypeCentOS, "/centos/7/os/x86_64/Packages/package.rpm", true},
		{"CentOSNoMatch", distro.TypeCentOS, "/other/path", false},
		{"AlpineMatch", distro.TypeAlpine, "/alpine/v3.18/main/x86_64/APKINDEX.tar.gz", true},
		{"AlpineNoMatch", distro.TypeAlpine, "/other/path", false},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			u, err := url.Parse("http://example.com" + tc.urlPath)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for n := 0; n < b.N; n++ {
				req := &http.Request{
					Method: "GET",
					URL:    u,
					Header: make(http.Header),
				}

				RewriteRequestByMode(req, rewriters, tc.mode)

				// Verify rewrite happened if expected
				if tc.matches && req.URL.Host == "" {
					b.Error("Expected URL rewrite but host is empty")
				}
			}
		})
	}
}

// BenchmarkURLRewriteWithQuery benchmarks URL rewrite with query parameters
func BenchmarkURLRewriteWithQuery(b *testing.B) {
	st := newTestState()
	reg := newTestRegistry()

	rewriters := CreateNewRewriters(distro.TypeUbuntu, st, reg)
	if rewriters == nil {
		b.Fatal("CreateNewRewriters() returned nil")
	}

	testURLs := []string{
		"/ubuntu/dists/jammy/main/binary-amd64/Packages",
		"/ubuntu/dists/jammy/main/binary-amd64/Packages?version=1.0",
		"/ubuntu/dists/jammy/main/binary-amd64/Packages?version=1.0&arch=amd64",
		"/ubuntu/dists/jammy/main/binary-amd64/Packages.gz",
		"/ubuntu/dists/jammy/updates/main/binary-amd64/Packages",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		urlPath := testURLs[n%len(testURLs)]
		u, err := url.Parse("http://example.com" + urlPath)
		if err != nil {
			b.Fatal(err)
		}

		req := &http.Request{
			Method: "GET",
			URL:    u,
			Header: make(http.Header),
		}

		RewriteRequestByMode(req, rewriters, distro.TypeUbuntu)
	}
}
