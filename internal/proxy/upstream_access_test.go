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
	"fmt"
	"net/http"
	"testing"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

func TestUpstreamPolicyRejectsTransportOverride(t *testing.T) {
	_, err := NewPackageStruct(Options{State: state.NewAppState(), Registry: distro.NewBuiltinRegistry(), TransportOverride: http.DefaultTransport, UpstreamAccess: &access.UpstreamConfig{Enabled: true}})
	if err == nil {
		t.Fatal("custom transport bypassed active upstream policy")
	}
}

type deniedUpstreamFixture struct{ calls int }

func (f *deniedUpstreamFixture) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return nil, fmt.Errorf("wrapped: %w", access.ErrUpstreamDenied)
}
func TestUpstreamPolicyDenialsAreNotRetried(t *testing.T) {
	fixture := &deniedUpstreamFixture{}
	req, err := http.NewRequest("GET", "https://mirror.example/file", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewRetryableTransport(fixture).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(err, access.ErrUpstreamDenied) || fixture.calls != 1 {
		t.Fatalf("error=%v attempts=%d", err, fixture.calls)
	}
}
