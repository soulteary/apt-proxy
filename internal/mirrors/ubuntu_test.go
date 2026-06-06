// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
