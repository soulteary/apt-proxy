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
	"fmt"
	"strings"
	"sync"
	"testing"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/passthrough"
)

type lockedLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.text.String()
}

// With redirects followed the allowlist no longer bounds upstream fetches.
// The daemon says so at startup, together with the setting that restores the
// bound, and stays silent when the setting is off or nothing is allowlisted.
func TestPassthroughRedirectNotice(t *testing.T) {
	const notice = "set passthrough_follow_redirects to false"
	for _, tc := range []struct {
		name    string
		entries []string
		follow  bool
		want    bool
	}{
		{"followed", []string{"https://pkgs.example.org"}, true, true},
		{"switched off", []string{"https://pkgs.example.org"}, false, false},
		{"nothing allowlisted", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list, err := passthrough.Parse(tc.entries)
			if err != nil {
				t.Fatal(err)
			}
			sink := &lockedLog{}
			s := &Server{
				config: &config.Config{Passthrough: tc.entries, PassthroughFollowRedirects: tc.follow},
				log:    logger.New(logger.Config{Level: logger.InfoLevel, Output: sink, Format: logger.FormatJSON}),
			}

			s.logPassthrough(list)

			if got := strings.Contains(sink.String(), notice); got != tc.want {
				t.Errorf("notice logged = %t, want %t: %s", got, tc.want, sink.String())
			}
		})
	}
}

// The daemon hands the setting to the proxy: the redirect follower is part of
// the upstream transport only when it is on and an origin is allowlisted.
func TestServerPassesRedirectSettingToProxy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []string
		follow  bool
		want    bool
	}{
		{"followed", []string{"https://pkgs.example.org"}, true, true},
		{"switched off", []string{"https://pkgs.example.org"}, false, false},
		{"nothing allowlisted", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := NewServer(&config.Config{
				CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0",
				Mirrors:     config.MirrorConfig{Ubuntu: "http://127.0.0.1:1/ubuntu/"},
				Passthrough: tc.entries, PassthroughFollowRedirects: tc.follow,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = srv.shutdown() })

			transport := fmt.Sprintf("%T", srv.proxy.Transport())
			if got := strings.HasSuffix(transport, ".redirectFollower"); got != tc.want {
				t.Errorf("upstream transport is %s, follower present = %t, want %t", transport, got, tc.want)
			}
		})
	}
}
