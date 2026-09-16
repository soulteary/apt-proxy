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

import "testing"

// Routing package fetches through a forward proxy is a documented deployment
// option (README, "Reaching Mirrors Through an Upstream Proxy"). Keep the
// resolver wired up so the documentation stays true.
//
// See the note in internal/benchmarks/proxyenv_test.go for why this asserts
// that a resolver exists rather than resolving a URL through it.
func TestUpstreamTransportHonoursProxyEnvironment(t *testing.T) {
	for _, keepAlive := range []bool{true, false} {
		if NewUpstreamTransport(keepAlive).Proxy == nil {
			t.Errorf("NewUpstreamTransport(%v) has no Proxy resolver; HTTP_PROXY/HTTPS_PROXY would be ignored for mirror fetches", keepAlive)
		}
	}
}
