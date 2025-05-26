package mirrors

import (
	"bufio"
	"net/http"

	Define "github.com/apham0001/apt-proxy/define"
)

func GetUbuntuMirrorUrlsByGeo() (mirrors []string, err error) {
	response, err := http.Get(Define.UBUNTU_GEO_MIRROR_API)
	if err != nil {
		return mirrors, err
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		mirrors = append(mirrors, scanner.Text())
	}
	return mirrors, scanner.Err()
}
