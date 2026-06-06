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
	_ "embed"
	"net/http"
	"strconv"
	"time"
)

//go:embed static/apt-proxy-logo.png
var aptProxyLogoPNG []byte

// logoLastModified is captured at process start so the served asset can
// participate in conditional GET handling without touching the filesystem.
var logoLastModified = time.Now().UTC()

// ServeStaticLogo writes the embedded apt-proxy logo PNG with long-lived
// caching headers. It is intentionally minimal so it can be wired up via any
// router that accepts an http.HandlerFunc.
func ServeStaticLogo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Content-Length", strconv.Itoa(len(aptProxyLogoPNG)))
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("Last-Modified", logoLastModified.Format(http.TimeFormat))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(aptProxyLogoPNG)
}
