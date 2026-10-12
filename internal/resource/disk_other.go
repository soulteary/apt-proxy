//go:build !linux && !darwin

package resource

import "errors"

func availableBytes(string) (int64, error) {
	return 0, errors.New("staging disk headroom checks require Linux or macOS")
}
