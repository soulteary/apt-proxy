package proxy

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soulteary/apt-proxy/internal/system"
)

const (
	InternalPageHome string = "/"
	InternalPagePing string = "/_/ping/"
)

const (
	TypeNotFound int = 0
	TypeHome     int = 1
	TypePing     int = 2
)

func IsInternalUrls(url string) bool {
	u := strings.ToLower(url)
	return !strings.Contains(u, "/ubuntu") && !strings.Contains(u, "/debian") && !strings.Contains(u, "/centos") && !strings.Contains(u, "/alpine")
}

func GetInternalResType(url string) int {
	if url == InternalPageHome {
		return TypeHome
	}

	if url == InternalPagePing {
		return TypePing
	}

	return TypeNotFound
}

const LabelNoValidValue = "N/A"

// homeStatsTTL is the freshness window for the cached home-page stats.
// The home page can be hit by liveness probes / dashboards on tight loops;
// re-walking the cache directory and statting filesystems on every request
// is expensive on a large cache, so we serve a short-lived snapshot.
const homeStatsTTL = 5 * time.Second

type homeStatsSnapshot struct {
	cacheDir         string
	cacheSizeLabel   string
	filesNumberLabel string
	diskAvailable    string
	memoryUsage      string
	goroutine        string
	expiresAt        time.Time
}

var (
	homeStatsMu    sync.Mutex
	homeStatsCache *homeStatsSnapshot
)

func computeHomeStats(cacheDir string) *homeStatsSnapshot {
	cacheSizeLabel := LabelNoValidValue
	if cacheSize, err := system.DirSize(cacheDir); err == nil {
		cacheSizeLabel = system.ByteCountDecimal(cacheSize)
	}

	filesNumberLabel := LabelNoValidValue
	cacheMetaDir := filepath.Join(cacheDir, "header", "v1")
	if _, err := os.Stat(cacheMetaDir); !os.IsNotExist(err) {
		if files, err := os.ReadDir(cacheMetaDir); err == nil {
			filesNumberLabel = strconv.Itoa(len(files))
		}
	}

	diskAvailableLabel := LabelNoValidValue
	// Probe the volume hosting the cache directory. Fallback to the working
	// directory only when the cache directory cannot be statted (e.g. before
	// it has been created).
	probe := cacheDir
	if probe == "" {
		probe = "."
	}
	if available, err := system.DiskAvailable(probe); err == nil {
		diskAvailableLabel = system.ByteCountDecimal(available)
	}

	memoryUsage, goroutine := system.GetMemoryUsageAndGoroutine()
	memoryUsageLabel := system.ByteCountDecimal(memoryUsage)

	return &homeStatsSnapshot{
		cacheDir:         cacheDir,
		cacheSizeLabel:   cacheSizeLabel,
		filesNumberLabel: filesNumberLabel,
		diskAvailable:    diskAvailableLabel,
		memoryUsage:      memoryUsageLabel,
		goroutine:        goroutine,
		expiresAt:        time.Now().Add(homeStatsTTL),
	}
}

func getHomeStats(cacheDir string) *homeStatsSnapshot {
	homeStatsMu.Lock()
	defer homeStatsMu.Unlock()
	if homeStatsCache != nil && homeStatsCache.cacheDir == cacheDir && time.Now().Before(homeStatsCache.expiresAt) {
		return homeStatsCache
	}
	homeStatsCache = computeHomeStats(cacheDir)
	return homeStatsCache
}

func RenderInternalUrls(url string, cacheDir string) (string, int) {
	switch GetInternalResType(url) {
	case TypeHome:
		s := getHomeStats(cacheDir)
		return GetBaseTemplate(s.cacheSizeLabel, s.filesNumberLabel, s.diskAvailable, s.memoryUsage, s.goroutine), 200
	case TypePing:
		return "pong", http.StatusOK
	}
	return "Not Found", http.StatusNotFound
}
