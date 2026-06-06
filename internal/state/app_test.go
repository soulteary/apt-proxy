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

package state

import (
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
)

func TestNewAppState(t *testing.T) {
	st := NewAppState()
	if st == nil {
		t.Fatal("NewAppState() returned nil")
	}

	if st.Ubuntu == nil {
		t.Error("Ubuntu mirror state is nil")
	}
	if st.UbuntuPorts == nil {
		t.Error("UbuntuPorts mirror state is nil")
	}
	if st.Debian == nil {
		t.Error("Debian mirror state is nil")
	}
	if st.CentOS == nil {
		t.Error("CentOS mirror state is nil")
	}
	if st.Alpine == nil {
		t.Error("Alpine mirror state is nil")
	}
}

func TestAppStateProxyMode(t *testing.T) {
	st := NewAppState()

	if mode := st.GetProxyMode(); mode != 0 {
		t.Errorf("GetProxyMode() = %d, want 0", mode)
	}

	st.SetProxyMode(distro.TypeUbuntu)
	if mode := st.GetProxyMode(); mode != distro.TypeUbuntu {
		t.Errorf("GetProxyMode() = %d, want %d", mode, distro.TypeUbuntu)
	}
}

func TestAppStateSetMirror(t *testing.T) {
	st := NewAppState()

	tests := []struct {
		distType int
		url      string
	}{
		{distro.TypeUbuntu, "https://mirrors.example.com/ubuntu/"},
		{distro.TypeUbuntuPorts, "https://mirrors.example.com/ubuntu-ports/"},
		{distro.TypeDebian, "https://mirrors.example.com/debian/"},
		{distro.TypeCentOS, "https://mirrors.example.com/centos/"},
		{distro.TypeAlpine, "https://mirrors.example.com/alpine/"},
	}

	for _, tt := range tests {
		st.SetMirror(tt.distType, tt.url)
		mirror := st.GetMirror(tt.distType)
		if mirror == nil {
			t.Errorf("GetMirror(%d) returned nil after SetMirror", tt.distType)
			continue
		}
		if mirror.String() != tt.url {
			t.Errorf("GetMirror(%d) = %q, want %q", tt.distType, mirror.String(), tt.url)
		}
	}
}

func TestAppStateSetMirrorUnknownType(t *testing.T) {
	st := NewAppState()
	// Should not panic and should not store anything.
	st.SetMirror(9999, "https://mirrors.example.com/whatever/")
	if got := st.GetMirror(9999); got != nil {
		t.Errorf("GetMirror(9999) = %q, want nil", got.String())
	}
}

func TestAppStateResetAll(t *testing.T) {
	st := NewAppState()
	st.SetMirror(distro.TypeUbuntu, "https://example.com/ubuntu/")
	st.SetMirror(distro.TypeDebian, "https://example.com/debian/")

	st.ResetAll()

	if st.GetMirror(distro.TypeUbuntu) != nil {
		t.Error("Ubuntu mirror should be nil after ResetAll")
	}
	if st.GetMirror(distro.TypeDebian) != nil {
		t.Error("Debian mirror should be nil after ResetAll")
	}
}

func TestAppStateClone(t *testing.T) {
	original := NewAppState()
	original.SetProxyMode(distro.TypeUbuntu)
	original.SetMirror(distro.TypeUbuntu, "https://original.example.com/ubuntu/")

	clone := original.Clone()

	if clone.GetProxyMode() != original.GetProxyMode() {
		t.Errorf("Clone proxy mode = %d, want %d", clone.GetProxyMode(), original.GetProxyMode())
	}

	originalMirror := original.GetMirror(distro.TypeUbuntu)
	cloneMirror := clone.GetMirror(distro.TypeUbuntu)
	if cloneMirror.String() != originalMirror.String() {
		t.Errorf("Clone mirror = %q, want %q", cloneMirror.String(), originalMirror.String())
	}

	clone.SetProxyMode(distro.TypeDebian)
	clone.SetMirror(distro.TypeUbuntu, "https://clone.example.com/ubuntu/")

	if original.GetProxyMode() != distro.TypeUbuntu {
		t.Error("Original proxy mode was modified when clone was changed")
	}
	if original.GetMirror(distro.TypeUbuntu).String() != "https://original.example.com/ubuntu/" {
		t.Error("Original mirror was modified when clone was changed")
	}
}

func TestMirrorStateSetAndGet(t *testing.T) {
	mirror := NewMirrorState(distro.TypeUbuntu)

	if mirror.Get() != nil {
		t.Error("New MirrorState should have nil URL")
	}

	mirror.Set("https://mirrors.example.com/ubuntu/")
	if mirror.Get() == nil {
		t.Fatal("Get() returned nil after Set()")
	}
	if mirror.Get().String() != "https://mirrors.example.com/ubuntu/" {
		t.Errorf("Get() = %q, want %q", mirror.Get().String(), "https://mirrors.example.com/ubuntu/")
	}

	mirror.Set("")
	if mirror.Get() != nil {
		t.Error("Get() should return nil after Set(\"\")")
	}
}

func TestMirrorStateReset(t *testing.T) {
	mirror := NewMirrorState(distro.TypeUbuntu)
	mirror.Set("https://mirrors.example.com/ubuntu/")
	mirror.Reset()

	if mirror.Get() != nil {
		t.Error("Get() should return nil after Reset()")
	}
}

func TestMirrorStateClone(t *testing.T) {
	original := NewMirrorState(distro.TypeUbuntu)
	original.Set("https://original.example.com/ubuntu/")

	clone := original.Clone()

	if clone.Get().String() != original.Get().String() {
		t.Errorf("Clone URL = %q, want %q", clone.Get().String(), original.Get().String())
	}

	clone.Set("https://clone.example.com/ubuntu/")
	if original.Get().String() != "https://original.example.com/ubuntu/" {
		t.Error("Original was modified when clone was changed")
	}
}

func TestConcurrentAccess(t *testing.T) {
	st := NewAppState()
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			st.SetProxyMode(i)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = st.GetProxyMode()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			st.SetMirror(distro.TypeUbuntu, "https://example.com/")
			_ = st.GetMirror(distro.TypeUbuntu)
		}
		done <- true
	}()

	<-done
	<-done
	<-done
}
