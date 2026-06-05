package distro

import "regexp"

var CentosHostPattern = regexp.MustCompile(`/centos/(.+)$`)

const CentosBenchmarkURL = "TIME"

// https://www.centos.org/download/mirrors/ 2022.11.19
// Sites that contain protocol headers, restrict access to resources using that protocol
var CentosOfficialMirrors = []string{
	"mirrors.bfsu.edu.cn/centos/",
	"mirrors.cqu.edu.cn/CentOS/",
	"http://mirrors.neusoft.edu.cn/centos/",
	"mirrors.nju.edu.cn/centos/",
	"mirrors.huaweicloud.com/centos/",
	"mirror.lzu.edu.cn/centos/",
	"http://mirrors.njupt.edu.cn/centos/",
	"mirrors.163.com/centos/",
	"mirrors.bupt.edu.cn/centos/",
	"ftp.sjtu.edu.cn/centos/",
	"mirrors.tuna.tsinghua.edu.cn/centos/",
	"mirrors.ustc.edu.cn/centos/",
}

var CentosCustomMirrors = []string{
	"mirrors.aliyun.com/centos/",
}

var BuiltinCentosMirrors = GenerateBuildInList(CentosOfficialMirrors, CentosCustomMirrors)

var CentosDefaultCacheRules = []Rule{
	{Pattern: regexp.MustCompile(`repomd.xml$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeCentOS},
	{Pattern: regexp.MustCompile(`filelist.gz$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeCentOS},
	{Pattern: regexp.MustCompile(`dir_sizes$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeCentOS},
	{Pattern: regexp.MustCompile(`TIME$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeCentOS},
	{Pattern: regexp.MustCompile(`timestamp.txt$`), CacheControl: `max-age=3600`, Rewrite: true, OS: TypeCentOS},
	{Pattern: regexp.MustCompile(`.*`), CacheControl: `max-age=100000`, Rewrite: true, OS: TypeCentOS},
}
