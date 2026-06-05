package distro

import "regexp"

var AlpineHostPattern = regexp.MustCompile(`/alpine/(.+)$`)

const AlpineBenchmarkURL = "MIRRORS.txt"

// https://mirrors.alpinelinux.org/ 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var AlpineOfficialMirrors = []string{
	"mirrors.tuna.tsinghua.edu.cn/alpine/",
	"mirrors.ustc.edu.cn/alpine/",
	"mirrors.nju.edu.cn/alpine/",
	"mirrors.sjtug.sjtu.edu.cn/alpine/",
	"mirrors.aliyun.com/alpine/",
}

var AlpineCustomMirrors = []string{}

var BuiltinAlpineMirrors = GenerateBuildInList(AlpineOfficialMirrors, AlpineCustomMirrors)

var AlpineDefaultCacheRules = []Rule{
	{Pattern: regexp.MustCompile(`APKINDEX.tar.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeAlpine},
	{Pattern: regexp.MustCompile(`tar.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeAlpine},
	{Pattern: regexp.MustCompile(`apk$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeAlpine},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=100000`, Rewrite: true, OS: TypeAlpine},
}
