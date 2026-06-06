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

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPExtractor_NoTrustedProxies(t *testing.T) {
	e := NewClientIPExtractor(nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 198.51.100.5")

	if got := e.ClientIP(req); got != "203.0.113.7" {
		t.Fatalf("expected RemoteAddr host when no trusted proxies, got %q", got)
	}
}

func TestClientIPExtractor_TrustedHonoursXFF(t *testing.T) {
	e := NewClientIPExtractor([]string{"10.0.0.0/8"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.42, 10.0.0.1")

	if got := e.ClientIP(req); got != "198.51.100.42" {
		t.Fatalf("expected left-most XFF IP, got %q", got)
	}
}

func TestClientIPExtractor_RejectsMalformedXFF(t *testing.T) {
	e := NewClientIPExtractor([]string{"10.0.0.0/8"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:443"
	// Embedded whitespace in the first token must NOT be trusted.
	req.Header.Set("X-Forwarded-For", "1.2.3.4 attacker, 10.0.0.1")

	if got := e.ClientIP(req); got != "10.1.2.3" {
		t.Fatalf("expected fallback to RemoteAddr host on malformed XFF, got %q", got)
	}
}

func TestClientIPExtractor_IgnoresInvalidCIDR(t *testing.T) {
	e := NewClientIPExtractor([]string{"not-a-cidr", "", "10.0.0.0/8"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")

	if got := e.ClientIP(req); got != "203.0.113.99" {
		t.Fatalf("trusted proxy CIDR should still apply, got %q", got)
	}
}
