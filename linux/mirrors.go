package linux

import (
	"bufio"
	"net/http"
	"regexp"
)

type Mirrors struct {
	URLs []string
}

func getGeoMirrors(mirrorListUrl string) (m Mirrors, err error) {
	if len(mirrorListUrl) == 0 {
		m.URLs = DEBIAN_MIRROR_URLS
		return m, nil
	}
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

func getPredefinedConfiguration(osType string) (string, string, *regexp.Regexp) {
	if osType == UBUNTU {
		return UBUNTU_MIRROR_URLS, UBUNTU_BENCHMAKR_URL, UBUNTU_HOST_PATTERN
	} else {
		return "", DEBIAN_BENCHMAKR_URL, DEBIAN_HOST_PATTERN
	}
}
