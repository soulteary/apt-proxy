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

package cli

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestProductionUpstreamAccess(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _, _ = io.WriteString(w, "release-data") }))
	defer upstream.Close()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		policy *access.UpstreamConfig
		status int
	}{
		{"legacy disabled", nil, 200},
		{"private denied", &access.UpstreamConfig{Enabled: true, AllowedPorts: []int{number}}, 502},
		{"explicit private exception", &access.UpstreamConfig{Enabled: true, AllowedPorts: []int{number}, AllowCIDRs: []string{"127.0.0.1/32"}}, 200},
		{"port denied", &access.UpstreamConfig{Enabled: true, AllowCIDRs: []string{"127.0.0.1/32"}}, 502},
		{"host denied", &access.UpstreamConfig{Enabled: true, AllowedHosts: []string{"mirror.example"}, AllowedPorts: []int{number}, AllowCIDRs: []string{"127.0.0.1/32"}}, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := hits.Load()
			srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}, UpstreamAccess: tc.policy})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = srv.shutdown() })
			resp, err := srv.app.Test(httptest.NewRequest("GET", "/ubuntu/dists/jammy/InRelease", nil))
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d body=%q", resp.StatusCode, body)
			}
			want := before
			if tc.status == 200 {
				want++
			}
			if hits.Load() != want {
				t.Fatalf("upstream hits=%d want=%d", hits.Load(), want)
			}
			if tc.status == 502 {
				if stats := srv.cache.Stats(); stats.ItemCount != 0 {
					t.Fatalf("denied response stored: %+v", stats)
				}
			}
		})
	}
}
func TestNewServerRejectsInvalidUpstreamPolicy(t *testing.T) {
	if _, err := NewServer(&config.Config{UpstreamAccess: &access.UpstreamConfig{AllowedPorts: []int{443}}}); err == nil {
		t.Fatal("disabled policy with restrictions accepted")
	}
}
