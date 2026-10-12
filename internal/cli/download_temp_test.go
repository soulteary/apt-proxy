package cli

import (
	"testing"

	"github.com/soulteary/apt-proxy/internal/config"
)

func TestDownloadTempDirFollowsBackend(t *testing.T) {
	for _, tc := range []struct{ backend, s3Dir, want string }{
		{"", "/s3-staging", "/cache"},
		{config.StorageBackendDisk, "/s3-staging", "/cache"},
		{config.StorageBackendS3, "/s3-staging", "/s3-staging"},
		{config.StorageBackendS3, "", ""},
	} {
		s := &Server{config: &config.Config{CacheDir: "/cache", Storage: config.StorageConfig{Backend: tc.backend, S3: config.S3Config{TempDir: tc.s3Dir}}}}
		if got := s.downloadTempDir(); got != tc.want {
			t.Errorf("backend %q: temp dir %q, want %q", tc.backend, got, tc.want)
		}
	}
}
