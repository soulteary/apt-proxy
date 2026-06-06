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
	"context"
	"errors"
	"io"
	"os"

	"github.com/minio/minio-go/v7"
)

// errWriteOnSeek is returned when a caller tries to Seek on an active write
// handle. httpcache-kit's writers go through io.Copy and never seek before
// closing, so the constraint is acceptable.
var errWriteOnSeek = errors.New("s3vfs: seek on writable object is not supported")

// errReadOnWrite is returned when a caller tries to Read from a write-only
// handle. The vfs.WFile interface includes io.Reader for symmetry with local
// files, but write-only flags (O_WRONLY) make Read meaningless.
var errReadOnWrite = errors.New("s3vfs: read on write-only object is not supported")

// s3RFile implements vfs.RFile by delegating Read/Seek/Close to the
// minio.Object returned by GetObject. Because GetObject is lazy, network I/O
// only happens on first read; this is critical for httpcache-kit which often
// opens header files just to call Stat/Read a few bytes.
type s3RFile struct {
	obj *minio.Object
}

func (r *s3RFile) Read(p []byte) (int, error)                { return r.obj.Read(p) }
func (r *s3RFile) Seek(off int64, whence int) (int64, error) { return r.obj.Seek(off, whence) }
func (r *s3RFile) Close() error                              { return r.obj.Close() }
func (r *s3RFile) ReadAt(p []byte, off int64) (int, error)   { return r.obj.ReadAt(p, off) }

// inlineSpillThreshold is the Write-buffer size beyond which we spill to a
// temporary file on disk. It is also the default when S3Config.InlineMaxBytes
// is zero. The value (32 MiB) matches the common APT package size knee point:
// most index files (Release, Packages, etc.) stay below it and never touch
// disk, while large .deb / .rpm payloads spill exactly once.
const inlineSpillThreshold = 32 << 20

// s3WFile implements vfs.WFile. It buffers writes in memory up to inlineMax
// bytes; if the writer keeps going past that threshold it transparently
// spills to a temporary file. Close() finally PUTs the accumulated content
// to S3 in a single request and cleans the temp file up.
type s3WFile struct {
	ctx       context.Context
	client    *minio.Client
	bucket    string
	key       string
	logical   string // path without prefix, used for cache invalidation
	tmpDir    string
	inlineMax int64

	inlineBuf bytes.Buffer
	tmp       *os.File
	spilled   bool
	written   int64
	closed    bool

	onClose func(logical string) // metadata cache invalidation hook
}

// Read on a write-only handle is unsupported. We return an explicit error
// instead of silently succeeding so misuse is caught immediately.
func (w *s3WFile) Read(_ []byte) (int, error) {
	return 0, errReadOnWrite
}

// Seek on a write-only handle is unsupported (see errWriteOnSeek). The vfs
// interface mandates io.Seeker, so we return an error for any seek away from
// the current position. A no-op seek to the current offset is accepted to be
// forgiving with naive callers.
func (w *s3WFile) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekCurrent && off == 0 {
		return w.written, nil
	}
	if whence == io.SeekStart && off == w.written {
		return w.written, nil
	}
	return 0, errWriteOnSeek
}

func (w *s3WFile) Write(p []byte) (int, error) {
	if w.closed {
		return 0, os.ErrClosed
	}

	// Decide whether this Write would cross the inline threshold. We spill
	// before the write so the buffer never exceeds inlineMax in memory.
	if !w.spilled && w.written+int64(len(p)) > w.inlineMax {
		if err := w.spill(); err != nil {
			return 0, err
		}
	}

	if w.spilled {
		n, err := w.tmp.Write(p)
		w.written += int64(n)
		return n, err
	}

	n, err := w.inlineBuf.Write(p)
	w.written += int64(n)
	return n, err
}

// spill drains the in-memory buffer to a freshly-created temp file and
// switches the writer into "spilled" mode. After spill returns, all future
// writes go directly to the temp file.
func (w *s3WFile) spill() error {
	tmp, err := os.CreateTemp(w.tmpDir, "s3vfs-*.bin")
	if err != nil {
		return err
	}
	if w.inlineBuf.Len() > 0 {
		if _, err := tmp.Write(w.inlineBuf.Bytes()); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return err
		}
		w.inlineBuf.Reset()
	}
	w.tmp = tmp
	w.spilled = true
	return nil
}

// Close finalises the upload. For inline writers we PUT the accumulated
// bytes directly from memory. For spilled writers we rewind the temp file
// and stream it to S3, then delete the temp file regardless of the upload
// outcome (the temp file is useless either way: success means the data is
// in S3, failure means the next Store will rewrite it).
func (w *s3WFile) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	defer func() {
		if w.onClose != nil {
			w.onClose(w.logical)
		}
	}()

	opts := minio.PutObjectOptions{
		// Use a generic content-type; the actual HTTP body sent to clients
		// re-derives it from cached headers, so the S3 metadata is purely
		// informative.
		ContentType: "application/octet-stream",
	}

	if w.spilled {
		defer func() {
			_ = w.tmp.Close()
			_ = os.Remove(w.tmp.Name())
		}()
		if _, err := w.tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		_, err := w.client.PutObject(w.ctx, w.bucket, w.key, w.tmp, w.written, opts)
		return err
	}

	body := bytes.NewReader(w.inlineBuf.Bytes())
	_, err := w.client.PutObject(w.ctx, w.bucket, w.key, body, int64(w.inlineBuf.Len()), opts)
	return err
}
