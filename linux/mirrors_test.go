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
	url, res := getLinuxMirrorsAndBenchmarkURL("ubuntu")
	if url != UBUNTU_MIRROR_URLS || res != UBUNTU_BENCHMAKR_URL {
		t.Fatal("Failed to get resource link")
	}
}
