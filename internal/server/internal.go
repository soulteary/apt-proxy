package server

import (
	"embed"
	"fmt"
	"net/http"
	_url "net/url"
	"os"
	"strconv"
	"strings"

	"github.com/soulteary/apt-proxy/pkg/system"
)

const (
	INTERNAL_PAGE_HOME   string = "/"
	INTERNAL_PAGE_PING   string = "/_/ping/"
	INTERNAL_PAGE_ASSETS string = "/assets"
)

const (
	TYPE_NOT_FOUND int = iota
	TYPE_HOME
	TYPE_PING
	TYPE_ASSETS
)

//go:embed assets/*
var f embed.FS

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

	if strings.HasPrefix(url, INTERNAL_PAGE_ASSETS) {
		return TYPE_ASSETS
	}

	return TYPE_NOT_FOUND
}

// TODO: use configuration
const CACHE_META_DIR = "./.aptcache/header/v1"
const LABEL_NO_VALID_VALUE = "N/A"

func RenderInternalUrls(url string) ([]byte, int) {
	switch GetInternalResType(url) {
	case TYPE_HOME:
		cacheSizeLabel := LABEL_NO_VALID_VALUE
		// TODO: use configuration
		cacheSize, err := system.DirSize("./.aptcache")
		if err == nil {
			cacheSizeLabel = system.ByteCountDecimal(cacheSize)
			// } else {
			// return "Get Cache Size Failed", http.StatusBadGateway
		}

		filesNumberLabel := LABEL_NO_VALID_VALUE
		if _, err := os.Stat(CACHE_META_DIR); !os.IsNotExist(err) {
			files, err := os.ReadDir(CACHE_META_DIR)
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

		return []byte(GetBaseTemplate(cacheSizeLabel, filesNumberLabel, diskAvailableLabel, memoryUsageLabel, goroutine)), http.StatusOK
	case TYPE_PING:
		return []byte("pong"), http.StatusOK
	case TYPE_ASSETS:
		// [FIXME] logging with fmt.Println is ugly
		u, err := _url.Parse(url)
		if err != nil {
			fmt.Printf("error parsing url %s : %s", url, err.Error())
			return nil, http.StatusInternalServerError
		}
		f, err := f.ReadFile(u.Path[1:])
		if err != nil {
			fmt.Printf("error reading url %s : %s", url, err.Error())
			return nil, http.StatusNotFound
		}
		return f, http.StatusOK
	}
	return []byte("Not Found"), http.StatusNotFound
}
