package linux

import (
	"bufio"
	"net/http"
)

type Mirrors struct {
	URLs []string
}

func getGeoMirrors(mirrorListUrl string) (m Mirrors, err error) {
	response, err := http.Get(mirrorListUrl)
	if err != nil {
		return
	}

	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	m.URLs = []string{}

	for scanner.Scan() {
		m.URLs = append(m.URLs, scanner.Text())
	}

	return m, scanner.Err()
}

func getLinuxMirrorsAndBenchmarkURL(osType string) (string, string) {
	if osType == "ubuntu" {
		return UBUNTU_MIRROR_URLS, UBUNTU_BENCHMAKR_URL
	} else {
		return ALPINE_MIRROR_URLS, ALPINE_BENCHMAKR_URL
	}
}
