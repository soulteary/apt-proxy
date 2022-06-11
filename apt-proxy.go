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
	defaultHost   = "0.0.0.0"
	defaultPort   = "3142"
	defaultDir    = "./.aptcache"
	defaultMirror = "" // "https://mirrors.tuna.tsinghua.edu.cn/ubuntu/"
)

var (
	version  string
	listen   string
	mirror   string
	cacheDir string
	debug    bool
)

func init() {
	var (
		host string
		port string
	)
	flag.StringVar(&host, "host", defaultHost, "the host to bind to")
	flag.StringVar(&port, "port", defaultPort, "the port to bind to")
	listen = host + ":" + port

	flag.StringVar(&mirror, "mirror", defaultMirror, "the mirror for fetching packages")

	flag.StringVar(&cacheDir, "cachedir", defaultDir, "the dir to store cache data in")
	flag.BoolVar(&debug, "debug", false, "whether to output debugging logging")
	flag.Parse()
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
