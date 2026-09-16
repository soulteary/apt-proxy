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

package benchmarks

import (
	"net/http"
	"testing"
)

// Mirror election must reach the outside world the same way the proxy itself
// does. When a deployment can only reach mirrors through a forward proxy and
// this client ignores HTTP_PROXY, every candidate benchmarks to a timeout and
// apt-proxy elects a mirror it cannot actually fetch from.
//
// The assertion is deliberately "a proxy resolver is installed" rather than
// "this URL resolves to that proxy": net/http reads the proxy environment
// once per process (sync.Once inside ProxyFromEnvironment), so setting the
// variables from a test would only work if this test happened to run first.
func TestBenchmarkClientHonoursProxyEnvironment(t *testing.T) {
	client := newBenchmarkClient()

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("benchmark client transport is %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Error("benchmark transport has no Proxy resolver; HTTP_PROXY/HTTPS_PROXY would be ignored when electing a mirror")
	}
}
