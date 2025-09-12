package server_test

import (
	"fmt"
	"net/http"
	"testing"

	Server "github.com/soulteary/apt-proxy/internal/server"
)

func TestIsInternalUrls(t *testing.T) {
	if Server.IsInternalUrls("mirrors.tuna.tsinghua.edu.cn/ubuntu/") {
		t.Fatal("test internal url failed")
	}
	if !Server.IsInternalUrls(Server.INTERNAL_PAGE_HOME) {
		t.Fatal("test internal url failed")
	}
	if !Server.IsInternalUrls(Server.INTERNAL_PAGE_PING) {
		t.Fatal("test internal url failed")
	}
}

func TestGetInternalResType(t *testing.T) {
	if Server.GetInternalResType(Server.INTERNAL_PAGE_HOME) != Server.TYPE_HOME {
		t.Fatal("test get internal res type failed")
	}

	if Server.GetInternalResType(Server.INTERNAL_PAGE_PING) != Server.TYPE_PING {
		t.Fatal("test get internal res type failed")
	}

	if Server.GetInternalResType("/url-not-found") != Server.TYPE_NOT_FOUND {
		t.Fatal("test get internal res type failed")
	}
}

func TestRenderInternalUrls(t *testing.T) {
	res, code := Server.RenderInternalUrls(Server.INTERNAL_PAGE_PING)
	if code != http.StatusOK {
		t.Fatal("test render internal urls failed")
	}
	if string(res) != "pong" {
		t.Fatal("test render internal urls failed")
	}

	_, code = Server.RenderInternalUrls("/url-not-exists")
	if code != http.StatusNotFound {
		t.Fatal("test render internal urls failed")
	}

	res, code = Server.RenderInternalUrls(Server.INTERNAL_PAGE_HOME)
	fmt.Println(res)
	if !(code == http.StatusOK || code == http.StatusBadGateway) {
		t.Fatal("test render internal urls failed")
	}
	if len(res) == 0 {
		t.Fatal("test render internal urls failed")
	}
}
