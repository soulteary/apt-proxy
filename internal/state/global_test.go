package state

import (
	"testing"

	Define "github.com/soulteary/apt-proxy/define"
)

func TestSetProxyMode(t *testing.T) {
	SetProxyMode(Define.TYPE_LINUX_ALL_DISTROS)
	if GetProxyMode() != Define.TYPE_LINUX_ALL_DISTROS {
		t.Fatal("Test Set/Get ProxyMode Faild")
	}
}
