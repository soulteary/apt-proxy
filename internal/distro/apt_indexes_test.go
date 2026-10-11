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

func TestContentsPdiffCoverage(t *testing.T) {
	cfg, err := NewLoader("../../config/distributions.yaml").Load()
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	for i := range cfg.Distributions {
		if err := reg.LoadFromConfig(&cfg.Distributions[i]); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{DistroUbuntu, DistroUbuntuPorts, DistroDebian} {
		dist, ok := reg.GetByID(id)
		if !ok {
			t.Fatalf("missing distribution: %s", id)
		}
		for source, rules := range map[string][]Rule{"builtin": newDebStyleRules(dist.Type), "yaml": dist.CacheRules} {
			t.Run(id+"/"+source, func(t *testing.T) {
				for _, target := range []string{"Contents-amd64", "Contents-all", "Contents-udeb-amd64"} {
					for _, suffix := range []string{"Index", "2026-10-12-0000.00.gz", "2026-10-12-0000.00.xz", "Index.exe", "2026-10-12-0000.00.gz.exe"} {
						path := "dists/stable/main/" + target + ".diff/" + suffix
						matched := false
						for _, rule := range rules {
							if rule.Pattern.MatchString(path) {
								matched = true
								if rule.CacheControl != "max-age=3600" || !rule.Rewrite {
									t.Errorf("incorrect policy: %s", path)
								}
							}
						}
						want := suffix == "Index" || suffix == "2026-10-12-0000.00.gz" || suffix == "2026-10-12-0000.00.xz"
						if matched != want {
							t.Errorf("match %s = %v, want %v", path, matched, want)
						}
					}
				}
			})
		}
	}
}
