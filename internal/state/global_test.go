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
	state := NewAppState()
	if state == nil {
		t.Fatal("NewAppState() returned nil")
	}

	if state.Ubuntu == nil {
		t.Error("Ubuntu mirror state is nil")
	}
	if state.UbuntuPorts == nil {
		t.Error("UbuntuPorts mirror state is nil")
	}
	if state.Debian == nil {
		t.Error("Debian mirror state is nil")
	}
	if state.CentOS == nil {
		t.Error("CentOS mirror state is nil")
	}
	if state.Alpine == nil {
		t.Error("Alpine mirror state is nil")
	}
}

func TestAppStateProxyMode(t *testing.T) {
	state := NewAppState()

	// Default mode should be 0
	if mode := state.GetProxyMode(); mode != 0 {
		t.Errorf("GetProxyMode() = %d, want 0", mode)
	}

	// Set and get mode
	state.SetProxyMode(distro.TypeUbuntu)
	if mode := state.GetProxyMode(); mode != distro.TypeUbuntu {
		t.Errorf("GetProxyMode() = %d, want %d", mode, distro.TypeUbuntu)
	}
}

func TestAppStateSetMirror(t *testing.T) {
	state := NewAppState()

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
		state.SetMirror(tt.distType, tt.url)
		mirror := state.GetMirror(tt.distType)
		if mirror == nil {
			t.Errorf("GetMirror(%d) returned nil after SetMirror", tt.distType)
			continue
		}
		if mirror.String() != tt.url {
			t.Errorf("GetMirror(%d) = %q, want %q", tt.distType, mirror.String(), tt.url)
		}
	}
}

func TestAppStateResetAll(t *testing.T) {
	state := NewAppState()

	// Set all mirrors
	state.SetMirror(distro.TypeUbuntu, "https://example.com/ubuntu/")
	state.SetMirror(distro.TypeDebian, "https://example.com/debian/")

	// Reset all
	state.ResetAll()

	// Verify all are nil
	if state.GetMirror(distro.TypeUbuntu) != nil {
		t.Error("Ubuntu mirror should be nil after ResetAll")
	}
	if state.GetMirror(distro.TypeDebian) != nil {
		t.Error("Debian mirror should be nil after ResetAll")
	}
}

func TestAppStateClone(t *testing.T) {
	original := NewAppState()
	original.SetProxyMode(distro.TypeUbuntu)
	original.SetMirror(distro.TypeUbuntu, "https://original.example.com/ubuntu/")

	clone := original.Clone()

	// Verify clone has same values
	if clone.GetProxyMode() != original.GetProxyMode() {
		t.Errorf("Clone proxy mode = %d, want %d", clone.GetProxyMode(), original.GetProxyMode())
	}

	originalMirror := original.GetMirror(distro.TypeUbuntu)
	cloneMirror := clone.GetMirror(distro.TypeUbuntu)
	if cloneMirror.String() != originalMirror.String() {
		t.Errorf("Clone mirror = %q, want %q", cloneMirror.String(), originalMirror.String())
	}

	// Modify clone and verify original is unchanged
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

	// Initially nil
	if mirror.Get() != nil {
		t.Error("New MirrorState should have nil URL")
	}

	// Set valid URL
	mirror.Set("https://mirrors.example.com/ubuntu/")
	if mirror.Get() == nil {
		t.Fatal("Get() returned nil after Set()")
	}
	if mirror.Get().String() != "https://mirrors.example.com/ubuntu/" {
		t.Errorf("Get() = %q, want %q", mirror.Get().String(), "https://mirrors.example.com/ubuntu/")
	}

	// Set empty string resets to nil
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

	// Verify clone has same value
	if clone.Get().String() != original.Get().String() {
		t.Errorf("Clone URL = %q, want %q", clone.Get().String(), original.Get().String())
	}

	// Modify clone and verify original is unchanged
	clone.Set("https://clone.example.com/ubuntu/")
	if original.Get().String() != "https://original.example.com/ubuntu/" {
		t.Error("Original was modified when clone was changed")
	}
}

func TestBackwardCompatibility(t *testing.T) {
	// Test global functions work as expected

	// Reset state first
	ResetUbuntuMirror()
	ResetDebianMirror()

	// Test SetProxyMode and GetProxyMode
	SetProxyMode(distro.TypeDebian)
	if mode := GetProxyMode(); mode != distro.TypeDebian {
		t.Errorf("GetProxyMode() = %d, want %d", mode, distro.TypeDebian)
	}

	// Test mirror convenience functions
	SetUbuntuMirror("https://mirrors.example.com/ubuntu/")
	mirror := GetUbuntuMirror()
	if mirror == nil {
		t.Fatal("GetUbuntuMirror() returned nil")
	}
	if mirror.String() != "https://mirrors.example.com/ubuntu/" {
		t.Errorf("GetUbuntuMirror() = %q, want %q", mirror.String(), "https://mirrors.example.com/ubuntu/")
	}

	ResetUbuntuMirror()
	if GetUbuntuMirror() != nil {
		t.Error("GetUbuntuMirror() should return nil after reset")
	}
}

func TestGlobalSingleton(t *testing.T) {
	// Test that Global() returns the same instance
	state1 := Global()
	state2 := Global()

	if state1 != state2 {
		t.Error("Global() should return the same instance")
	}
}

func TestSetGlobal(t *testing.T) {
	// Save original
	original := Global()

	// Create custom state
	custom := NewAppState()
	custom.SetProxyMode(99)

	// Replace global
	SetGlobal(custom)

	// Verify global was replaced
	if GetProxyMode() != 99 {
		t.Errorf("GetProxyMode() = %d, want 99 after SetGlobal", GetProxyMode())
	}

	// Restore original
	SetGlobal(original)
}

func TestConcurrentAccess(t *testing.T) {
	state := NewAppState()
	done := make(chan bool)

	// Concurrent writes
	go func() {
		for i := 0; i < 100; i++ {
			state.SetProxyMode(i)
		}
		done <- true
	}()

	// Concurrent reads
	go func() {
		for i := 0; i < 100; i++ {
			_ = state.GetProxyMode()
		}
		done <- true
	}()

	// Concurrent mirror operations
	go func() {
		for i := 0; i < 100; i++ {
			state.SetMirror(distro.TypeUbuntu, "https://example.com/")
			_ = state.GetMirror(distro.TypeUbuntu)
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done
}
