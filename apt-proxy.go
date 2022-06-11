package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/soulteary/apt-proxy/httpcache"
	"github.com/soulteary/apt-proxy/pkgs/httplog"
	"github.com/soulteary/apt-proxy/proxy"
)

const (
	defaultListen = "0.0.0.0:3142"
	defaultDir    = "./.aptcache"
	defaultMirror = "http://mirrors.tuna.tsinghua.edu.cn/ubuntu/"
)

var (
	version string
	listen  string
	dir     string
	debug   bool
)

func init() {
	flag.StringVar(&listen, "mirror", defaultMirror, "the mirror for fetching packages")
	flag.StringVar(&listen, "listen", defaultListen, "the host and port to bind to")
	flag.StringVar(&dir, "cachedir", defaultDir, "the dir to store cache data in")
	flag.BoolVar(&debug, "debug", false, "whether to output debugging logging")
	flag.Parse()
}

func main() {
	log.Printf("running apt-proxy %s", version)

	if debug {
		httpcache.DebugLogging = true
	}

	cache, err := httpcache.NewDiskCache(dir)
	if err != nil {
		log.Fatal(err)
	}

	ap := proxy.NewAptProxyFromDefaults(defaultMirror)
	ap.Handler = httpcache.NewHandler(cache, ap.Handler)

	logger := httplog.NewResponseLogger(ap.Handler)
	logger.DumpRequests = debug
	logger.DumpResponses = debug
	logger.DumpErrors = debug
	ap.Handler = logger

	log.Printf("proxy listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, ap))
}
