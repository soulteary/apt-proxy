package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/soulteary/apt-proxy/httpcache"
	"github.com/soulteary/apt-proxy/linux"
	"github.com/soulteary/apt-proxy/pkgs/httplog"
	"github.com/soulteary/apt-proxy/proxy"
)

const (
	DEFAULT_HOST      = "0.0.0.0"
	DEFAULT_PORT      = "3142"
	DEFAULT_CACHE_DIR = "./.aptcache"
	DEFAULT_MIRROR    = "" // "https://mirrors.tuna.tsinghua.edu.cn/ubuntu/"
	DEFAULT_TYPE      = linux.UBUNTU
	DEFAULT_DEBUG     = false
)

var (
	version  string
	listen   string
	mirror   string
	types    string
	cacheDir string
	debug    bool
)

func init() {
	var (
		host string
		port string
	)
	flag.StringVar(&host, "host", DEFAULT_HOST, "the host to bind to")
	flag.StringVar(&port, "port", DEFAULT_PORT, "the port to bind to")
	flag.BoolVar(&debug, "debug", DEFAULT_DEBUG, "whether to output debugging logging")
	flag.StringVar(&mirror, "mirror", DEFAULT_MIRROR, "the mirror for fetching packages")
	flag.StringVar(&types, "type", DEFAULT_TYPE, "select the type of system to cache: ubuntu/debian")
	flag.StringVar(&cacheDir, "cachedir", DEFAULT_CACHE_DIR, "the dir to store cache data in")
	flag.Parse()

	if types != linux.UBUNTU && types != linux.DEBIAN {
		types = linux.UBUNTU
	}

	listen = host + ":" + port
}

func main() {

	log.Printf("running apt-proxy %s", version)

	if debug {
		log.Printf("enable debug: true")
		httpcache.DebugLogging = true
	}

	cache, err := httpcache.NewDiskCache(cacheDir)
	if err != nil {
		log.Fatal(err)
	}

	ap := proxy.NewAptProxyFromDefaults(mirror, types)
	ap.Handler = httpcache.NewHandler(cache, ap.Handler)

	logger := httplog.NewResponseLogger(ap.Handler)
	logger.DumpRequests = debug
	logger.DumpResponses = debug
	logger.DumpErrors = debug
	ap.Handler = logger

	log.Printf("proxy listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, ap))
}
