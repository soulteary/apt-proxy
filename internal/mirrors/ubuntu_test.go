package mirrors

import (
	"context"
	"testing"
	"time"
)

func TestGetUbuntuMirrorUrlsByGeo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mirrors, err := GetUbuntuMirrorUrlsByGeoCtx(ctx)
	if err != nil {
		t.Skipf("upstream Ubuntu mirrors API unavailable: %v", err)
	}
	if len(mirrors) == 0 {
		t.Fatal("get ubuntu get mirrors failed")
	}
}
