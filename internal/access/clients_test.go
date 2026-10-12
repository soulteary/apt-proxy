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

package access

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		allow, deny []string
		remote      string
		want        bool
	}{
		{"default", nil, nil, "invalid", true},
		{"IPv4", []string{"192.0.2.0/24"}, nil, "192.0.2.42:4000", true},
		{"outside", []string{"192.0.2.0/24"}, nil, "198.51.100.1:4000", false},
		{"deny wins", []string{"192.0.2.0/24"}, []string{"192.0.2.42/32"}, "192.0.2.42:4000", false},
		{"deny only allows others", nil, []string{"192.0.2.0/24"}, "198.51.100.1:4000", true},
		{"IPv6", []string{"2001:db8::/32"}, nil, "[2001:db8::42]:4000", true},
		{"mapped IPv4", []string{"192.0.2.0/24"}, nil, "[::ffff:192.0.2.42]:4000", true},
		{"zoned IPv6", []string{"fe80::/10"}, nil, "[fe80::1%en0]:4000", true},
		{"malformed peer", nil, []string{"192.0.2.0/24"}, "not-an-ip", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewClients(tc.allow, tc.deny)
			if err != nil {
				t.Fatal(err)
			}
			if got := p.Allowed(tc.remote); got != tc.want {
				t.Fatalf("Allowed=%v, want %v", got, tc.want)
			}
		})
	}
}
func TestClientPolicyRejectsInvalidRules(t *testing.T) {
	for _, cidr := range []string{"", "localhost", "192.0.2.1", "192.0.2.0/33", " ::1/128", "::ffff:192.0.2.0/120"} {
		for _, deny := range []bool{false, true} {
			allow, block := []string{cidr}, []string(nil)
			if deny {
				allow, block = nil, allow
			}
			if _, err := NewClients(allow, block); err == nil {
				t.Fatalf("accepted %q", cidr)
			}
		}
	}
}
func TestForwardedHeadersCannotGrantAccess(t *testing.T) {
	p, err := NewClients([]string{"192.0.2.0/24"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := p.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "http://proxy/ubuntu/pool/main/p.deb", nil)
	r.RemoteAddr = "198.51.100.1:4000"
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	r.Header.Set("Forwarded", "for=192.0.2.1")
	r.Header.Set("X-Real-IP", "192.0.2.1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 || calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d calls=%d headers=%v", w.Code, calls, w.Header())
	}
}
