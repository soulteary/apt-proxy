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

package system

// https://github.com/soulteary/hosts-blackhole/blob/main/pkg/system/gc.go

import (
	"runtime"
	"strconv"
	"sync"
	"time"
)

// memStatsTTL caches ReadMemStats results so frequent home-page / metrics
// scrapes don't pay the stop-the-world cost on every call.
const memStatsTTL = time.Second

type memSnapshot struct {
	alloc     uint64
	goroutine string
	expiresAt time.Time
}

var (
	memStatsMu    sync.Mutex
	memStatsCache memSnapshot
)

// GetMemoryUsageAndGoroutine returns current Alloc bytes and the goroutine count
// (as a string for direct rendering on the home page). Results are cached for
// memStatsTTL to avoid back-to-back STW pauses under bursty scrapes.
func GetMemoryUsageAndGoroutine() (uint64, string) {
	now := time.Now()

	memStatsMu.Lock()
	if !memStatsCache.expiresAt.IsZero() && now.Before(memStatsCache.expiresAt) {
		alloc, gor := memStatsCache.alloc, memStatsCache.goroutine
		memStatsMu.Unlock()
		return alloc, gor
	}
	memStatsMu.Unlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	gor := strconv.Itoa(runtime.NumGoroutine())

	memStatsMu.Lock()
	memStatsCache = memSnapshot{
		alloc:     m.Alloc,
		goroutine: gor,
		expiresAt: now.Add(memStatsTTL),
	}
	memStatsMu.Unlock()

	return m.Alloc, gor
}
