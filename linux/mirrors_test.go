package linux

import (
	"log"
	"testing"
)

func TestMirrors(t *testing.T) {
	mirrors, err := getGeoMirrors(UBUNTU_MIRROR_URLS)
	if err != nil {
		t.Fatal(err)
	}

	if len(mirrors.URLs) == 0 {
		t.Fatal("No mirrors found")
	}
}

func TestMirrorsBenchmark(t *testing.T) {
	mirrors, err := getGeoMirrors(UBUNTU_MIRROR_URLS)
	if err != nil {
		t.Fatal(err)
	}

	fastest, err := mirrors.Fastest(UBUNTU_BENCHMAKR_URL)
	if err != nil {
		t.Fatal(err)
	}

	log.Printf("Fastest mirror is %s", fastest)
}
