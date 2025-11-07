package server_test

import (
	"fmt"
	"net/http"
	"testing"

	server "github.com/soulteary/apt-proxy/internal/server"
)

func TestIsInternalUrls(t *testing.T) {
	if server.IsInternalUrls("mirrors.tuna.tsinghua.edu.cn/ubuntu/") {
		t.Fatal("test internal url failed")
	}
	if !server.IsInternalUrls(server.INTERNAL_PAGE_HOME) {
		t.Fatal("test internal url failed")
	}
	if !server.IsInternalUrls(server.INTERNAL_PAGE_PING) {
		t.Fatal("test internal url failed")
	}
}

func TestGetInternalResType(t *testing.T) {
	if server.GetInternalResType(server.INTERNAL_PAGE_HOME) != server.TYPE_HOME {
		t.Fatal("test get internal res type failed")
	}

	if server.GetInternalResType(server.INTERNAL_PAGE_PING) != server.TYPE_PING {
		t.Fatal("test get internal res type failed")
	}

	if server.GetInternalResType("/url-not-found") != server.TYPE_NOT_FOUND {
		t.Fatal("test get internal res type failed")
	}
}

func TestRenderInternalUrls(t *testing.T) {
	cacheDir := "./.aptcache"
	res, code := server.RenderInternalUrls(server.INTERNAL_PAGE_PING, cacheDir)
	if code != http.StatusOK {
		t.Fatal("test render internal urls failed")
	}
	if res != "pong" {
		t.Fatal("test render internal urls failed")
	}

	_, code = server.RenderInternalUrls("/url-not-exists", cacheDir)
	if code != http.StatusNotFound {
		t.Fatal("test render internal urls failed")
	}

	res, code = server.RenderInternalUrls(server.INTERNAL_PAGE_HOME, cacheDir)
	fmt.Println(res)
	if !(code == http.StatusOK || code == http.StatusBadGateway) {
		t.Fatal("test render internal urls failed")
	}
	if res == "" {
		t.Fatal("test render internal urls failed")
	}
}
