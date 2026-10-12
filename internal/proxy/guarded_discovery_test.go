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
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

func TestGuardedDiscoveryConstructionAndRefresh(t *testing.T) {
	var hits atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(200) }))
	defer source.Close()
	u, err := url.Parse(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	reg := distro.NewBuiltinRegistry()
	original, ok := reg.GetByType(distro.TypeUbuntu)
	if !ok {
		t.Fatal("missing Ubuntu")
	}
	custom := *original
	custom.Builtin = false
	custom.Mirrors = []distro.URLWithAlias{{URL: source.URL + "/ubuntu/", Scheme: "http"}}
	custom.BenchmarkURL = "probe"
	if err := reg.Register(&custom); err != nil {
		t.Fatal(err)
	}
	ps, err := NewPackageStruct(Options{State: state.NewAppState(), Registry: reg, Mode: distro.TypeUbuntu, CacheDir: t.TempDir(), UpstreamAccess: &access.UpstreamConfig{Enabled: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatal("startup benchmark bypassed policy")
	}
	ps.RefreshMirrors()
	if hits.Load() != 0 {
		t.Fatal("refresh benchmark bypassed policy")
	}
	if _, err := ps.BenchmarkEngine().GetTheFastestMirrorWithCache(distro.TypeUbuntu, []string{source.URL}, "/probe"); !errors.Is(err, access.ErrUpstreamDenied) {
		t.Fatalf("production engine not guarded: %v", err)
	}
	if _, ok := ps.BenchmarkEngine().Cache().GetCachedResult(distro.TypeUbuntu); ok {
		t.Fatal("denied mirror cached")
	}
}
