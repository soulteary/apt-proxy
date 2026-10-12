//go:build !unix

package sharedcache

import (
	"context"
	"fmt"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	"time"
)

type FileCoordinator struct{}
type fileLease struct{}

func NewFileCoordinator(string) (*FileCoordinator, error) {
	return nil, fmt.Errorf("shared disk coordination requires Unix kernel file locks")
}
func (*FileCoordinator) Acquire(context.Context, string) (httpcache.MissLease, error) {
	return nil, fmt.Errorf("shared disk coordination unsupported")
}
func (*FileCoordinator) lock(context.Context, string) (*fileLease, error) {
	return nil, fmt.Errorf("shared disk coordination unsupported")
}
func (*fileLease) Close() error { return nil }

func (*FileCoordinator) readMarker(string) (time.Time, bool, error) {
	return time.Time{}, false, fmt.Errorf("shared disk coordination unsupported")
}
func (*FileCoordinator) writeMarker(string, time.Time) error {
	return fmt.Errorf("shared disk coordination unsupported")
}
