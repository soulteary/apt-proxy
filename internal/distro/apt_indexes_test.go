package distro

import "testing"

func TestDebStyleIndexCoverage(t *testing.T) {
	for _, path := range []string{"dists/stable/main/binary-amd64/Packages", "dists/stable/main/binary-amd64/Packages.xz", "dists/stable/main/source/Sources.zst", "dists/stable/main/i18n/Translation-zh_CN.xz", "dists/stable/main/Contents-amd64.gz", "dists/stable/main/binary-amd64/Packages.diff/Index", "dists/stable/main/binary-amd64/Packages.diff/2026-10-12-0000.00.gz"} {
		matched := false
		for _, rule := range newDebStyleRules(0) {
			if rule.Pattern.MatchString(path) {
				matched = true
				if rule.CacheControl != "max-age=3600" || !rule.Rewrite {
					t.Fatalf("incorrect policy for %s", path)
				}
				break
			}
		}
		if !matched {
			t.Errorf("missing index rule: %s", path)
		}
	}
	for _, path := range []string{"dists/stable/main/NotPackages.xz", "dists/stable/main/Packages.xz.exe", "dists/stable/main/Contents-amd64.gz.exe", "dists/stable/main/i18n/Translation-zh_CN.xz.exe"} {
		for _, rule := range newDebStyleRules(0) {
			if rule.Pattern.MatchString(path) {
				t.Errorf("unexpected match: %s", path)
			}
		}
	}
}
