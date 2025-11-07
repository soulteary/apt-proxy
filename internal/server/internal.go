package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/soulteary/apt-proxy/pkg/system"
)

const (
	INTERNAL_PAGE_HOME string = "/"
	INTERNAL_PAGE_PING string = "/_/ping/"
)

const (
	TYPE_NOT_FOUND int = 0
	TYPE_HOME      int = 1
	TYPE_PING      int = 2
)

func IsInternalUrls(url string) bool {
	u := strings.ToLower(url)
	return !(strings.Contains(u, "/ubuntu") || strings.Contains(u, "/debian") || strings.Contains(u, "/centos") || strings.Contains(u, "/alpine"))
}

func GetInternalResType(url string) int {
	if url == INTERNAL_PAGE_HOME {
		return TYPE_HOME
	}

	if url == INTERNAL_PAGE_PING {
		return TYPE_PING
	}

	return TYPE_NOT_FOUND
}

const LABEL_NO_VALID_VALUE = "N/A"

func RenderInternalUrls(url string, cacheDir string) (string, int) {
	switch GetInternalResType(url) {
	case TYPE_HOME:
		cacheSizeLabel := LABEL_NO_VALID_VALUE
		cacheSize, err := system.DirSize(cacheDir)
		if err == nil {
			cacheSizeLabel = system.ByteCountDecimal(cacheSize)
			// } else {
			// return "Get Cache Size Failed", http.StatusBadGateway
		}

		filesNumberLabel := LABEL_NO_VALID_VALUE
		cacheMetaDir := filepath.Join(cacheDir, "header", "v1")
		if _, err := os.Stat(cacheMetaDir); !os.IsNotExist(err) {
			files, err := os.ReadDir(cacheMetaDir)
			if err == nil {
				filesNumberLabel = strconv.Itoa(len(files))
				// } else {
				// return "Get Cache Meta Dir Failed", http.StatusBadGateway
			}
			// } else {
			// return "Get Cache Meta Failed", http.StatusBadGateway
		}

		diskAvailableLabel := LABEL_NO_VALID_VALUE
		available, err := system.DiskAvailable()
		if err == nil {
			diskAvailableLabel = system.ByteCountDecimal(available)
			// } else {
			// return "Get Disk Available Failed", http.StatusBadGateway
		}

		memoryUsageLabel := LABEL_NO_VALID_VALUE
		memoryUsage, goroutine := system.GetMemoryUsageAndGoroutine()
		memoryUsageLabel = system.ByteCountDecimal(memoryUsage)

		return GetBaseTemplate(cacheSizeLabel, filesNumberLabel, diskAvailableLabel, memoryUsageLabel, goroutine), 200
	case TYPE_PING:
		return "pong", http.StatusOK
	}
	return "Not Found", http.StatusNotFound
}
