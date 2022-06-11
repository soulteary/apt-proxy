package linux

import (
	"testing"
)

func TestGetGeoMirrors(t *testing.T) {
	mirrors, err := getGeoMirrors(UBUNTU_MIRROR_URLS)
	if err != nil {
		t.Fatal(err)
	}

	if len(mirrors.URLs) == 0 {
		t.Fatal("No mirrors found")
	}
}

func TestGetMirrorsUrlAndBenchmarkUrl(t *testing.T) {
	url, res, pattern := getPredefinedConfiguration(UBUNTU)
	if url != UBUNTU_MIRROR_URLS || res != UBUNTU_BENCHMAKR_URL {
		t.Fatal("Failed to get resource link")
	}

	if !pattern.MatchString("http://archive.ubuntu.com/ubuntu/InRelease") {
		t.Fatal("Failed to verify domain name rules")
	}
}
