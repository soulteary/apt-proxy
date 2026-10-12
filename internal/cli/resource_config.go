package cli

import (
	"os"

	"github.com/soulteary/apt-proxy/internal/resource"
)

func (s *Server) buildResourceManager() (*resource.Manager, error) {
	cfg := resource.Config{MaxObjectBytes: resource.DefaultMaxObjectBytes, TotalTempBytes: resource.DefaultTotalTempBytes, MaxActiveDownloads: resource.DefaultMaxActiveDownloads, MinFreeBytes: resource.DefaultMinFreeBytes}
	limits := s.config.Resources
	if limits.MaxObjectMB > 0 {
		cfg.MaxObjectBytes = limits.MaxObjectMB * (1 << 20)
	}
	if limits.TotalTempMB > 0 {
		cfg.TotalTempBytes = limits.TotalTempMB * (1 << 20)
	} else if s.config.Download.TotalSpoolMB > 0 {
		cfg.TotalTempBytes = s.config.Download.TotalSpoolMB * (1 << 20)
	}
	if limits.MaxActiveDownloads > 0 {
		cfg.MaxActiveDownloads = limits.MaxActiveDownloads
	}
	if limits.MinFreeMB > 0 {
		cfg.MinFreeBytes = limits.MinFreeMB * (1 << 20)
	}
	dir := s.downloadTempDir()
	if dir == "" {
		dir = os.TempDir()
	}
	cfg.DiskDirs = []string{dir}
	return resource.New(cfg)
}
