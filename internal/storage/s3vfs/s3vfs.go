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

// Package s3vfs implements a vfs.VFS backend backed by an S3-compatible
// object store (AWS S3, MinIO, Ceph RGW, Cloudflare R2, Backblaze B2,
// Aliyun OSS, Tencent COS, ...). It is meant to be plugged into
// httpcache-kit via NewVFSCacheWithConfig so that apt-proxy can serve its
// cached APT/RPM/Alpine packages straight from object storage with no
// changes to the proxy or distro logic.
package s3vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	pathutil "path"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	vfs "github.com/soulteary/vfs-kit"
)

// Config configures the S3 backend. It mirrors the operator-facing
// config.S3Config; New() takes a Config rather than the apt-proxy struct so
// this package stays import-cycle-clean and individually unit-testable.
type Config struct {
	// Endpoint is the host[:port] of the S3-compatible service, e.g.
	// "s3.amazonaws.com", "s3.us-west-2.amazonaws.com",
	// "minio.local:9000", "s3.cn-north-1.qiniucs.com".
	Endpoint string
	// Region is the optional region; required for AWS S3, ignored by most
	// MinIO-flavoured services.
	Region string
	// Bucket is the destination bucket. It must already exist; New()
	// performs a HeadBucket to validate.
	Bucket string
	// Prefix is an optional sub-path inside the bucket, e.g. "apt-proxy/",
	// allowing multiple cache instances to share one bucket. Trailing/leading
	// slashes are normalised away by the package.
	Prefix string
	// AccessKey / SecretKey are the static IAM credentials. Pass empty
	// strings to use anonymous access (rare) or the SDK's default chain.
	AccessKey string
	SecretKey string
	// SessionToken is the optional STS session token (for AssumeRole/IRSA);
	// leave empty for static long-lived credentials.
	SessionToken string
	// UseSSL toggles HTTPS for the S3 endpoint. Defaults to true; explicit
	// false is required only for plain-HTTP MinIO test deployments.
	UseSSL bool
	// UsePathStyle forces path-style URLs (https://endpoint/bucket/key)
	// instead of virtual-hosted-style (https://bucket.endpoint/key). MinIO,
	// Ceph and most third-party services need this set to true.
	UsePathStyle bool
	// InlineMaxBytes is the in-memory write buffer threshold above which a
	// write spills to a temporary file. Zero falls back to inlineSpillThreshold.
	InlineMaxBytes int64
	// TempDir is where spilled writes go. Empty means os.TempDir().
	TempDir string
	// MetaCacheSize controls how many StatObject results are kept in the LRU.
	// Zero falls back to defaultMetaCacheSize.
	MetaCacheSize int
	// MetaCacheTTL bounds how long a cached fileinfo is honored before we
	// re-check S3. Zero means no time-based invalidation; size eviction
	// alone is sufficient for most workloads.
	MetaCacheTTL time.Duration
	// HTTPClient lets tests inject a custom round-tripper. Production callers
	// leave this nil to get minio-go's default transport.
	HTTPClient *http.Client
}

const defaultMetaCacheSize = 8192

// metaEntry caches the result of a StatObject (or a synthetic directory
// fileinfo). storedAt is used for optional TTL-based invalidation.
type metaEntry struct {
	info     os.FileInfo
	storedAt time.Time
}

// S3VFS implements the vfs.VFS interface against an S3-compatible bucket.
type S3VFS struct {
	ctx       context.Context
	client    *minio.Client
	bucket    string
	prefix    string
	tmpDir    string
	inlineMax int64

	metaCache    *lru.Cache[string, metaEntry]
	metaCacheTTL time.Duration
}

// New constructs a new S3VFS. It validates connectivity by performing a
// BucketExists call; failing fast at startup is far less surprising than
// crashing on the first cache miss in production.
func New(ctx context.Context, cfg Config) (*S3VFS, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.Endpoint == "" {
		return nil, errors.New("s3vfs: Endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("s3vfs: Bucket is required")
	}

	creds := credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken)
	opts := &minio.Options{
		Creds:  creds,
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	}
	if cfg.UsePathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	if cfg.HTTPClient != nil {
		opts.Transport = cfg.HTTPClient.Transport
	}

	client, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("s3vfs: failed to construct minio client: %w", err)
	}

	// Validate connectivity & access. We use a short-lived child context so
	// a slow DNS / TCP handshake doesn't block startup forever.
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if exists, err := client.BucketExists(checkCtx, cfg.Bucket); err != nil {
		return nil, fmt.Errorf("s3vfs: bucket check for %q failed: %w", cfg.Bucket, err)
	} else if !exists {
		return nil, fmt.Errorf("s3vfs: bucket %q does not exist or is not accessible", cfg.Bucket)
	}

	cacheSize := cfg.MetaCacheSize
	if cacheSize <= 0 {
		cacheSize = defaultMetaCacheSize
	}
	metaCache, err := lru.New[string, metaEntry](cacheSize)
	if err != nil {
		return nil, fmt.Errorf("s3vfs: failed to create meta cache: %w", err)
	}

	inlineMax := cfg.InlineMaxBytes
	if inlineMax <= 0 {
		inlineMax = inlineSpillThreshold
	}

	tmpDir := cfg.TempDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}

	s := &S3VFS{
		ctx:          ctx,
		client:       client,
		bucket:       cfg.Bucket,
		prefix:       normalizePrefix(cfg.Prefix),
		tmpDir:       tmpDir,
		inlineMax:    inlineMax,
		metaCache:    metaCache,
		metaCacheTTL: cfg.MetaCacheTTL,
	}
	return s, nil
}

// Static interface compliance check; placed here so a future signature drift
// in vfs-kit shows up as a build error in this package.
var _ vfs.VFS = (*S3VFS)(nil)

// Bucket returns the configured bucket name. Used by health checks.
func (s *S3VFS) Bucket() string { return s.bucket }

// HealthCheck is a context-aware liveness probe suitable for plugging into
// health-kit. It performs a single HeadBucket round-trip.
func (s *S3VFS) HealthCheck(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("bucket %q is no longer accessible", s.bucket)
	}
	return nil
}

// String implements vfs.VFS. The format mirrors the local-FS String() output
// pattern so debug logs stay uniform.
func (s *S3VFS) String() string {
	return fmt.Sprintf("s3vfs: s3://%s/%s", s.bucket, s.prefix)
}

// Open returns a readable file handle. The minio.Object returned by
// GetObject is lazy: the network request happens on first Read, which lets
// us cheaply construct a handle even if the caller decides to abort.
func (s *S3VFS) Open(p string) (vfs.RFile, error) {
	key := s.toKey(p)
	if key == "" || strings.HasSuffix(key, "/") {
		// Opening "/" or a directory prefix has no meaning on S3.
		return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrInvalid}
	}

	// Ensure the object exists before we hand back a handle. minio-go
	// otherwise defers errors to first Read, which can confuse httpcache-kit's
	// Header() path that opens header files just to inspect them.
	if _, err := s.statKey(key, p); err != nil {
		return nil, err
	}

	obj, err := s.client.GetObject(s.ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, translateError("open", p, err)
	}
	return &s3RFile{obj: obj}, nil
}

// OpenFile honours O_CREATE|O_TRUNC|O_WRONLY (the only flag combination
// httpcache-kit currently passes). Reading via the returned WFile is not
// supported because S3 has no append-or-update semantics that map cleanly
// onto a Go file handle.
func (s *S3VFS) OpenFile(p string, flag int, _ os.FileMode) (vfs.WFile, error) {
	key := s.toKey(p)
	if key == "" || strings.HasSuffix(key, "/") {
		return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrInvalid}
	}

	// Read-only opens funnel through Open(); without this branch a caller
	// with O_RDONLY would silently create an empty object on Close.
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) == 0 {
		rf, err := s.Open(p)
		if err != nil {
			return nil, err
		}
		// Adapt RFile -> WFile by wrapping in a read-only WFile shim.
		return &readOnlyWFile{r: rf}, nil
	}

	w := &s3WFile{
		ctx:       s.ctx,
		client:    s.client,
		bucket:    s.bucket,
		key:       key,
		logical:   p,
		tmpDir:    s.tmpDir,
		inlineMax: s.inlineMax,
		onClose:   s.invalidateMeta,
	}
	return w, nil
}

// readOnlyWFile adapts an RFile to the WFile interface for read-only
// OpenFile() calls. Write/Seek-write operations return an error.
type readOnlyWFile struct{ r vfs.RFile }

func (w *readOnlyWFile) Read(p []byte) (int, error)            { return w.r.Read(p) }
func (w *readOnlyWFile) Seek(off int64, w2 int) (int64, error) { return w.r.Seek(off, w2) }
func (w *readOnlyWFile) Close() error                          { return w.r.Close() }
func (w *readOnlyWFile) Write(_ []byte) (int, error)           { return 0, errReadOnWrite }

// Stat returns os.FileInfo for the given path. We treat both real S3 objects
// and synthetic "directories" (anything that has children) as valid Stat
// targets. Results are cached to absorb httpcache-kit's chatty Header()
// access pattern.
func (s *S3VFS) Stat(p string) (os.FileInfo, error) {
	cleaned := pathutil.Clean("/" + p)
	if cleaned == "/" {
		return &s3FileInfo{name: "/", isDir: true, modTime: time.Now()}, nil
	}
	key := s.toKey(p)
	return s.statKey(key, cleaned)
}

// Lstat is identical to Stat for S3: there are no symlinks.
func (s *S3VFS) Lstat(p string) (os.FileInfo, error) { return s.Stat(p) }

// statKey looks up the LRU cache then falls back to StatObject; if no object
// at the key exists it tries to detect a synthetic directory via a one-item
// list under "key/".
func (s *S3VFS) statKey(key, logical string) (os.FileInfo, error) {
	if e, ok := s.metaCache.Get(key); ok {
		if s.metaCacheTTL <= 0 || time.Since(e.storedAt) < s.metaCacheTTL {
			return e.info, nil
		}
		s.metaCache.Remove(key)
	}

	info, err := s.client.StatObject(s.ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		fi := &s3FileInfo{
			name:    pathutil.Base(logical),
			size:    info.Size,
			modTime: info.LastModified,
		}
		s.metaCache.Add(key, metaEntry{info: fi, storedAt: time.Now()})
		return fi, nil
	}

	// If the object doesn't exist at this exact key, it might still be a
	// virtual directory (i.e. there are children under "key/"). httpcache-kit's
	// MkdirAll calls Stat on every ancestor, so we need this fallback to
	// avoid spurious ENOENT errors.
	if isNotFound(err) {
		if s.dirExists(key + "/") {
			fi := &s3FileInfo{name: pathutil.Base(logical), isDir: true, modTime: time.Now()}
			s.metaCache.Add(key, metaEntry{info: fi, storedAt: time.Now()})
			return fi, nil
		}
		return nil, &os.PathError{Op: "stat", Path: logical, Err: os.ErrNotExist}
	}
	return nil, translateError("stat", logical, err)
}

// dirExists returns true if at least one object exists under the supplied
// prefix (which must end with "/"). It uses ListObjects with MaxKeys=1 for
// efficiency.
func (s *S3VFS) dirExists(prefix string) bool {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	ch := s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
		MaxKeys:   1,
	})
	obj, ok := <-ch
	if !ok {
		return false
	}
	return obj.Err == nil
}

// ReadDir lists the immediate children of the given vfs directory using a
// delimited ListObjects. Both real objects and CommonPrefixes (synthetic
// subdirectories) are returned.
func (s *S3VFS) ReadDir(p string) ([]os.FileInfo, error) {
	listPrefix := s.dirPrefix(p)

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	results := make([]os.FileInfo, 0, 16)
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    listPrefix,
		Recursive: false,
	}) {
		if obj.Err != nil {
			return nil, translateError("readdir", p, obj.Err)
		}
		if obj.Key == "" {
			continue
		}
		// CommonPrefixes are returned as objects whose key ends with "/" and
		// has zero size. Treat them as directories.
		isDir := strings.HasSuffix(obj.Key, "/")
		name := baseName(obj.Key)
		if name == "" {
			continue
		}
		results = append(results, &s3FileInfo{
			name:    name,
			size:    obj.Size,
			modTime: obj.LastModified,
			isDir:   isDir,
		})
	}
	return results, nil
}

// Mkdir is a no-op on S3: there are no directories per se, just key prefixes.
// Returning nil keeps vfs.MkdirAll happy.
func (s *S3VFS) Mkdir(_ string, _ os.FileMode) error { return nil }

// Remove deletes a single object by exact key. If the path looks like a
// directory (ends with "/" or is identifiable as a virtual dir), we
// recursively delete all keys under it. This lets httpcache-kit's Purge()
// do its job correctly.
func (s *S3VFS) Remove(p string) error {
	key := s.toKey(p)
	if key == "" {
		return &os.PathError{Op: "remove", Path: p, Err: os.ErrInvalid}
	}

	if strings.HasSuffix(p, "/") || strings.HasSuffix(key, "/") {
		return s.removeAll(strings.TrimRight(key, "/") + "/")
	}

	err := s.client.RemoveObject(s.ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return &os.PathError{Op: "remove", Path: p, Err: os.ErrNotExist}
		}
		return translateError("remove", p, err)
	}
	s.metaCache.Remove(key)
	return nil
}

// removeAll deletes every object under prefix using the bulk-remove channel
// API. We capture errors from the result channel and surface the first one;
// the rest is logged-and-dropped because the operation is best-effort.
func (s *S3VFS) removeAll(prefix string) error {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	objCh := make(chan minio.ObjectInfo)
	go func() {
		defer close(objCh)
		for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		}) {
			if obj.Err != nil {
				continue
			}
			objCh <- obj
		}
	}()

	var firstErr error
	for rerr := range s.client.RemoveObjects(ctx, s.bucket, objCh, minio.RemoveObjectsOptions{}) {
		if rerr.Err != nil && firstErr == nil {
			firstErr = rerr.Err
		}
	}

	// Purge the affected entries from the meta cache. We can't enumerate the
	// keys cheaply (LRU has no prefix scan), so we just drop everything; the
	// next access will repopulate.
	s.metaCache.Purge()
	return firstErr
}

// invalidateMeta is called from s3WFile.Close() to drop a stale fileinfo for
// the freshly-written key. Without this, an immediate Stat after a Store()
// could return ErrNotExist from the cache.
func (s *S3VFS) invalidateMeta(logicalPath string) {
	s.metaCache.Remove(s.toKey(logicalPath))
}

// translateError wraps an arbitrary error in *os.PathError so vfs.IsNotExist
// (which calls os.IsNotExist) and other stdlib helpers behave correctly.
func translateError(op, p string, err error) error {
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return &os.PathError{Op: op, Path: p, Err: os.ErrNotExist}
	}
	if errors.Is(err, io.EOF) {
		return &os.PathError{Op: op, Path: p, Err: io.EOF}
	}
	return &os.PathError{Op: op, Path: p, Err: err}
}

// isNotFound recognises the various ways the minio SDK signals
// NoSuchKey/NoSuchBucket. It avoids a hard dependency on minio-internal
// error types by inspecting the typed ErrorResponse.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		switch resp.Code {
		case "NoSuchKey", "NoSuchBucket", "NotFound":
			return true
		}
		if resp.StatusCode == http.StatusNotFound {
			return true
		}
	}
	// Fallback: minio sometimes returns wrapped fmt.Errorf strings on
	// non-HEAD code paths, so we look at the message as a last resort.
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") || strings.Contains(msg, "key does not exist")
}
