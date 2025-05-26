package server_test

import (
	"strings"
	"testing"

	Server "github.com/apham0001/apt-proxy/internal/server"
)

func TestGetBaseTemplate(t *testing.T) {
	tpl := Server.GetBaseTemplate("11000", "11001", "11002", "11003", "11004")

	if !strings.Contains(tpl, "<strong>11000</strong>") {
		t.Fatal("test get base template failed")
	}

	if !strings.Contains(tpl, "<strong>11001</strong>") {
		t.Fatal("test get base template failed")
	}

	if !strings.Contains(tpl, "<strong>11002</strong>") {
		t.Fatal("test get base template failed")
	}

	if !strings.Contains(tpl, "<strong>11003</strong>") {
		t.Fatal("test get base template failed")
	}

	if !strings.Contains(tpl, "<strong>11004</strong>") {
		t.Fatal("test get base template failed")
	}
}
