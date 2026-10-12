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
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
)

func TestGuardedGeoClientRedirectAndBody(t *testing.T) {
	var destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationHits.Add(1)
		_, _ = w.Write([]byte("https://mirror.example/ubuntu/\n"))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer source.Close()
	u, err := url.Parse(source.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedPorts: []int{port}, AllowCIDRs: []string{"127.0.0.1/32"}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: policy.Transport(&http.Transport{})}
	if _, err := ubuntuMirrorsWithClient(context.Background(), client, source.URL); !errors.Is(err, access.ErrUpstreamDenied) {
		t.Fatalf("geo redirect error=%v", err)
	}
	if destinationHits.Load() != 0 {
		t.Fatal("geo redirect bypassed policy")
	}
	target, err := url.Parse(destination.URL)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, err := strconv.Atoi(target.Port())
	if err != nil {
		t.Fatal(err)
	}
	policy, err = access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedPorts: []int{port, targetPort}, AllowCIDRs: []string{"127.0.0.1/32"}})
	if err != nil {
		t.Fatal(err)
	}
	client = &http.Client{Transport: policy.Transport(&http.Transport{})}
	mirrors, err := ubuntuMirrorsWithClient(context.Background(), client, source.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(mirrors) != 1 || mirrors[0] != "https://mirror.example/ubuntu/" {
		t.Fatalf("mirrors=%v", mirrors)
	}
}
func TestGeoDiscoveryRejectsStatusAndHonorsContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("https://must-not-be-a-candidate/\n"))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	if mirrors, err := ubuntuMirrorsWithClient(context.Background(), server.Client(), server.URL+"/error"); err == nil || len(mirrors) != 0 {
		t.Fatalf("bad status parsed: %v %v", mirrors, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := ubuntuMirrorsWithClient(ctx, server.Client(), server.URL+"/slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("discovery deadline: %v", err)
	}
}
