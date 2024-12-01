package system

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func DiskAvailable() (uint64, error) {
	var stat unix.Statfs_t
	wd, err := os.Getwd()
	if err != nil {
		return 0, err
	}

	err = unix.Statfs(wd, &stat)
	if err != nil {
		return 0, err
	}

	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

// uint64 ver https://stackoverflow.com/questions/32482673/how-to-get-directory-total-size
func DirSize(path string) (uint64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return uint64(size), err
}

// https://hakk.dev/docs/golang-convert-file-size-human-readable/
func fileSizeRound(val float64, roundOn float64, places int) float64 {
	var round float64
	pow := math.Pow(10, float64(places))
	digit := pow * val
	_, div := math.Modf(digit)
	if div >= roundOn {
		round = math.Ceil(digit)
	} else {
		round = math.Floor(digit)
	}
	return round / pow
}

// uint64 ver https://programming.guide/go/formatting-byte-size-to-human-readable-format.html
func ByteCountDecimal(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
