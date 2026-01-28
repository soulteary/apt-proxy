package proxy

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/state"
)

// setupBenchmarkMirrors sets up mock mirrors for benchmarks
func setupBenchmarkMirrors() {
	state.SetUbuntuMirror("http://mirrors.example.com/ubuntu/")
	state.SetUbuntuPortsMirror("http://mirrors.example.com/ubuntu-ports/")
	state.SetDebianMirror("http://mirrors.example.com/debian/")
	state.SetCentOSMirror("http://mirrors.example.com/centos/")
	state.SetAlpineMirror("http://mirrors.example.com/alpine/")
}

// BenchmarkURLRewrite benchmarks URL rewrite performance for different distributions
func BenchmarkURLRewrite(b *testing.B) {
	setupBenchmarkMirrors()
	defer func() {
		state.ResetUbuntuMirror()
		state.ResetUbuntuPortsMirror()
		state.ResetDebianMirror()
		state.ResetCentOSMirror()
		state.ResetAlpineMirror()
	}()

	rewriters := CreateNewRewriters(distro.TYPE_LINUX_ALL_DISTROS)
	if rewriters == nil {
		b.Fatal("CreateNewRewriters() returned nil")
	}

	testCases := []struct {
		name    string
		mode    int
		urlPath string
		matches bool
	}{
		{"UbuntuMatch", distro.TYPE_LINUX_DISTROS_UBUNTU, "/ubuntu/dists/jammy/main/binary-amd64/Packages", true},
		{"UbuntuNoMatch", distro.TYPE_LINUX_DISTROS_UBUNTU, "/other/path", false},
		{"DebianMatch", distro.TYPE_LINUX_DISTROS_DEBIAN, "/debian/dists/bullseye/main/binary-amd64/Packages", true},
		{"DebianNoMatch", distro.TYPE_LINUX_DISTROS_DEBIAN, "/other/path", false},
		{"CentOSMatch", distro.TYPE_LINUX_DISTROS_CENTOS, "/centos/7/os/x86_64/Packages/package.rpm", true},
		{"CentOSNoMatch", distro.TYPE_LINUX_DISTROS_CENTOS, "/other/path", false},
		{"AlpineMatch", distro.TYPE_LINUX_DISTROS_ALPINE, "/alpine/v3.18/main/x86_64/APKINDEX.tar.gz", true},
		{"AlpineNoMatch", distro.TYPE_LINUX_DISTROS_ALPINE, "/other/path", false},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			u, err := url.Parse("http://example.com" + tc.urlPath)
			if err != nil {
				b.Fatal(err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for n := 0; n < b.N; n++ {
				req := &http.Request{
					Method: "GET",
					URL:    u,
					Header: make(http.Header),
				}

				RewriteRequestByMode(req, rewriters, tc.mode)

				// Verify rewrite happened if expected
				if tc.matches && req.URL.Host == "" {
					b.Error("Expected URL rewrite but host is empty")
				}
			}
		})
	}
}

// BenchmarkURLRewriteWithQuery benchmarks URL rewrite with query parameters
func BenchmarkURLRewriteWithQuery(b *testing.B) {
	setupBenchmarkMirrors()
	defer func() {
		state.ResetUbuntuMirror()
		state.ResetUbuntuPortsMirror()
		state.ResetDebianMirror()
		state.ResetCentOSMirror()
		state.ResetAlpineMirror()
	}()

	rewriters := CreateNewRewriters(distro.TYPE_LINUX_DISTROS_UBUNTU)
	if rewriters == nil {
		b.Fatal("CreateNewRewriters() returned nil")
	}

	testURLs := []string{
		"/ubuntu/dists/jammy/main/binary-amd64/Packages",
		"/ubuntu/dists/jammy/main/binary-amd64/Packages?version=1.0",
		"/ubuntu/dists/jammy/main/binary-amd64/Packages?version=1.0&arch=amd64",
		"/ubuntu/dists/jammy/main/binary-amd64/Packages.gz",
		"/ubuntu/dists/jammy/updates/main/binary-amd64/Packages",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		urlPath := testURLs[n%len(testURLs)]
		u, err := url.Parse("http://example.com" + urlPath)
		if err != nil {
			b.Fatal(err)
		}

		req := &http.Request{
			Method: "GET",
			URL:    u,
			Header: make(http.Header),
		}

		RewriteRequestByMode(req, rewriters, distro.TYPE_LINUX_DISTROS_UBUNTU)
	}
}
