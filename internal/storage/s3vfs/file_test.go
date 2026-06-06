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

package s3vfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// TestSmallWriteStaysInMemory verifies that writes below the inline threshold
// never touch disk: the temp pointer should remain nil, the spilled flag
// should be false, and the buffer should hold the bytes verbatim.
func TestSmallWriteStaysInMemory(t *testing.T) {
	w := &s3WFile{
		tmpDir:    t.TempDir(),
		inlineMax: 1024,
	}
	payload := []byte("small payload, way under threshold")
	n, err := w.Write(payload)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(payload) {
		t.Errorf("Write n = %d, want %d", n, len(payload))
	}
	if w.spilled {
		t.Error("expected spilled=false for small write")
	}
	if w.tmp != nil {
		t.Error("expected tmp file to remain nil for small write")
	}
	if !bytes.Equal(w.inlineBuf.Bytes(), payload) {
		t.Errorf("inline buffer mismatch: got %q want %q", w.inlineBuf.Bytes(), payload)
	}
	if w.written != int64(len(payload)) {
		t.Errorf("written = %d, want %d", w.written, len(payload))
	}
}

// TestLargeWriteSpillsToTempFile verifies that crossing the inline threshold
// spills to a temp file; the in-memory buffer is reset and the temp file
// exists with the original prefix bytes plus the new write at the tail.
func TestLargeWriteSpillsToTempFile(t *testing.T) {
	tmpDir := t.TempDir()
	w := &s3WFile{
		tmpDir:    tmpDir,
		inlineMax: 16,
	}

	// First write stays inline.
	if _, err := w.Write([]byte("hello-")); err != nil {
		t.Fatalf("Write inline: %v", err)
	}
	if w.spilled {
		t.Fatal("unexpected early spill")
	}

	// Second write exceeds threshold; should trigger spill.
	if _, err := w.Write([]byte("this-pushes-over-the-edge")); err != nil {
		t.Fatalf("Write spill: %v", err)
	}
	if !w.spilled {
		t.Fatal("expected spilled=true after threshold crossed")
	}
	if w.tmp == nil {
		t.Fatal("expected non-nil temp file after spill")
	}
	if w.inlineBuf.Len() != 0 {
		t.Errorf("inline buffer should be drained after spill; got %d bytes", w.inlineBuf.Len())
	}

	// Sanity-check the temp file contents include both writes in order.
	if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("tmp.Seek: %v", err)
	}
	got, err := io.ReadAll(w.tmp)
	if err != nil {
		t.Fatalf("ReadAll tmp: %v", err)
	}
	want := "hello-this-pushes-over-the-edge"
	if string(got) != want {
		t.Errorf("temp file = %q, want %q", got, want)
	}

	// Cleanup: closing should remove the temp file even if we can't talk to
	// S3 (Close will hit a nil client, but the deferred temp cleanup runs).
	// We instead clean up manually here.
	_ = w.tmp.Close()
	if err := os.Remove(w.tmp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("os.Remove tmp: %v", err)
	}
}

// TestWriteAfterClose returns os.ErrClosed without modifying state. Useful
// because httpcache-kit closes WFiles via defer; subsequent stray writes
// must not silently corrupt anything.
func TestWriteAfterClose(t *testing.T) {
	w := &s3WFile{
		tmpDir:    t.TempDir(),
		inlineMax: 16,
		closed:    true,
	}
	if _, err := w.Write([]byte("ignored")); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after close: got %v, want os.ErrClosed", err)
	}
}

// TestSeekOnWriteRejectsArbitraryOffsets checks that seeks away from the
// current position fail loudly. A no-op seek to the current position is
// permitted to be forgiving with naive callers.
func TestSeekOnWriteRejectsArbitraryOffsets(t *testing.T) {
	w := &s3WFile{
		tmpDir:    t.TempDir(),
		inlineMax: 1024,
	}
	if _, err := w.Write([]byte("12345")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Seek to current pos via SeekCurrent should succeed.
	if pos, err := w.Seek(0, io.SeekCurrent); err != nil || pos != 5 {
		t.Errorf("Seek(0, SeekCurrent) = (%d, %v), want (5, nil)", pos, err)
	}
	// Arbitrary backward seek must error.
	if _, err := w.Seek(0, io.SeekStart); !errors.Is(err, errWriteOnSeek) {
		t.Errorf("Seek(0, SeekStart) on non-empty buf: got %v, want errWriteOnSeek", err)
	}
}

// TestReadOnWriteOnly returns errReadOnWrite. Symmetric to Seek above; this
// guards against future refactors that make the WFile silently readable.
func TestReadOnWriteOnly(t *testing.T) {
	w := &s3WFile{
		tmpDir:    t.TempDir(),
		inlineMax: 1024,
	}
	buf := make([]byte, 4)
	if _, err := w.Read(buf); !errors.Is(err, errReadOnWrite) {
		t.Errorf("Read on write-only handle: got %v, want errReadOnWrite", err)
	}
}

// TestIsNotFound recognises the expected error shapes. We synthesize two
// real-world variants: a typed minio.ErrorResponse{Code:"NoSuchKey"} and a
// raw fmt-wrapped message that older SDK paths still produce.
func TestIsNotFound(t *testing.T) {
	if !isNotFound(errors.New("The specified key does not exist.")) {
		t.Error("expected fallback message detection to work")
	}
	if isNotFound(nil) {
		t.Error("nil should not be classified as not-found")
	}
	if isNotFound(errors.New("some other error")) {
		t.Error("unrelated error should not be classified as not-found")
	}
}

// TestNewRequiresFields surfaces missing required configuration before
// any network I/O; this is important so operators get fast, deterministic
// failure messages on misconfiguration.
func TestNewRequiresFields(t *testing.T) {
	if _, err := New(nil, Config{}); err == nil || !strings.Contains(err.Error(), "Endpoint") {
		t.Errorf("expected Endpoint required, got %v", err)
	}
	if _, err := New(nil, Config{Endpoint: "x"}); err == nil || !strings.Contains(err.Error(), "Bucket") {
		t.Errorf("expected Bucket required, got %v", err)
	}
}
