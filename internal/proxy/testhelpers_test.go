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
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

// newTestState constructs a fresh AppState pre-populated with mock
// mirrors, so tests don't have to set / reset shared globals. Several
// _test.go files in this package consume it; keeping it here (rather
// than alongside one specific test) makes the dependency obvious.
func newTestState() *state.AppState {
	st := state.NewAppState()
	st.SetMirror(distro.TypeUbuntu, "http://mirrors.example.com/ubuntu/")
	st.SetMirror(distro.TypeUbuntuPorts, "http://mirrors.example.com/ubuntu-ports/")
	st.SetMirror(distro.TypeDebian, "http://mirrors.example.com/debian/")
	st.SetMirror(distro.TypeCentOS, "http://mirrors.example.com/centos/")
	st.SetMirror(distro.TypeAlpine, "http://mirrors.example.com/alpine/")
	return st
}

// newTestRegistry returns a registry seeded with the built-in distributions.
func newTestRegistry() *distro.Registry {
	return distro.NewBuiltinRegistry()
}
