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
