package system

// https://github.com/soulteary/hosts-blackhole/blob/main/pkg/system/gc.go

import (
	"runtime"
	"strconv"
)

// GetMemoryUsageAndGoroutine returns current Alloc bytes and the goroutine count
// (as a string for direct rendering on the home page).
func GetMemoryUsageAndGoroutine() (uint64, string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc, strconv.Itoa(runtime.NumGoroutine())
}
