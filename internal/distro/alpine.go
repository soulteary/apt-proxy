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
