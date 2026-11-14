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

package proxy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/logsafe"
	"github.com/soulteary/apt-proxy/internal/passthrough"
)

// An allowlisted origin may answer with a redirect instead of the file:
// pkgs.k8s.io sends every package request to a CDN host. A client cannot act
// on that answer through apt-proxy. With apt-proxy as its HTTP proxy it asks
// for the new location, which is refused unless that host is allowlisted as
// well, or it opens a CONNECT tunnel, which apt-proxy does not provide. With
// the HTTPS/// marker it leaves the proxy and fetches the location directly.
// In no case is the file cached.
//
// redirectFollower follows these redirects inside apt-proxy. The cache stores
// the final response under the URL the client asked for.

// maxRedirectHops bounds a redirect chain. An archive CDN needs one or two.
const maxRedirectHops = 5

// redirectDrainLimit is how much of a redirect's body is read before the
// body is closed, so that the connection can be reused.
const redirectDrainLimit = 2 << 10

// allowlistHint ends the reason for a refusal that an allowlist entry for the
// target would lift.
const allowlistHint = "; add the host to passthrough to follow the redirect"

// redirectFollower is the RoundTripper that follows the redirects of
// passthrough requests. Every other request goes to next unchanged.
//
// A redirect that is not followed is returned to the client as received, as
// if the follower were absent. That happens when the target is refused, when
// the chain is longer than maxRedirectHops, and when the Location cannot be
// used.
type redirectFollower struct {
	// next is the regular upstream chain. It carries the client's request
	// and every hop to an allowlisted origin.
	next http.RoundTripper
	// public carries the hops to hosts outside the allowlist. It connects to
	// public addresses only, so a redirect cannot reach the loopback
	// interface or a private network.
	public http.RoundTripper
	// allowlist holds the origins the operator named.
	allowlist *passthrough.List
	// policy is the operator's upstream access policy, nil when disabled.
	policy *access.Upstream
	// egressProxy reports the forward proxy next uses for a request. public
	// connects directly: a forward proxy resolves the name itself, and the
	// address it then connects to cannot be checked. Nil when next connects
	// directly as well.
	egressProxy func(*http.Request) (*url.URL, error)
	log         *logger.Logger
}

// newRedirectFollower wraps next. base is the transport next is built on, or
// nil when the caller supplied its own transport.
func newRedirectFollower(next http.RoundTripper, base *http.Transport, policy *access.Upstream, allowlist *passthrough.List, log *logger.Logger) *redirectFollower {
	follower := &redirectFollower{next: next, public: next, allowlist: allowlist, policy: policy, log: log}
	if base == nil {
		// A supplied transport stands in for the network, address checks
		// included.
		return follower
	}
	follower.public = NewRetryableTransport(access.PublicOnly().Transport(base))
	if policy == nil {
		follower.egressProxy = base.Proxy
	}
	return follower
}

// RoundTrip implements http.RoundTripper.
func (f *redirectFollower) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := f.next.RoundTrip(req)
	if err != nil || !followsRedirects(req) {
		return res, err
	}

	current := req
	for hops := 0; ; hops++ {
		target := redirectTarget(res, current.URL)
		if target == nil {
			break
		}
		if hops == maxRedirectHops {
			f.handBack(res, req, current, target, fmt.Sprintf("the chain is longer than %d redirects", maxRedirectHops))
			break
		}
		hop, transport, reason := f.hopRequest(req, current, target)
		if reason != "" {
			f.handBack(res, req, current, target, reason)
			break
		}
		followed, err := transport.RoundTrip(hop)
		if errors.Is(err, access.ErrUpstreamDenied) {
			// The redirect is still held, so the client can be given it.
			reason := logsafe.Error(err).Error()
			if _, listed := f.allowlist.Match(target.Host); !listed {
				reason += allowlistHint
			}
			f.handBack(res, req, current, target, reason)
			break
		}
		discardRedirectBody(res)
		if err != nil {
			return nil, err
		}
		f.log.Debug().
			Str("origin", req.URL.Host).
			Str("location", logsafe.URL(target.String())).
			Int("status", followed.StatusCode).
			Msg("followed a redirect from an allowlisted origin")
		res, current = followed, hop
	}

	if current != req {
		// The response answers req, and the layers above judge it by req:
		// whether the client sent credentials decides whether the response
		// may be shared.
		res.Request = req
	}
	return res, nil
}

// followsRedirects reports whether req is a passthrough fetch. Distribution
// requests are fetched from a mirror apt-proxy selected and keep their
// redirects.
func followsRedirects(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	rule, _ := req.Context().Value(cacheRuleKey{}).(*distro.Rule)
	return rule == passthroughRule
}

// redirectTarget returns the absolute URL that res redirects to. It returns
// nil when res is not a redirect, or names no http or https location.
func redirectTarget(res *http.Response, base *url.URL) *url.URL {
	switch res.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return nil
	}
	location := res.Header.Get("Location")
	if location == "" {
		return nil
	}
	target, err := base.Parse(location)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil
	}
	target.Fragment, target.RawFragment = "", ""
	return target
}

// hopRequest builds the request for target and selects the transport that may
// carry it. A non-empty reason means the redirect is not followed.
func (f *redirectFollower) hopRequest(original, current *http.Request, target *url.URL) (hop *http.Request, transport http.RoundTripper, reason string) {
	rule, listed := f.allowlist.Match(target.Host)
	switch {
	case listed:
		// A hop to an allowlisted origin is made the way a client request for
		// that origin is; see matchPassthrough.
		if rule.ForceHTTPS && target.Scheme == "http" {
			target.Scheme = "https"
			if rule.Port == "" {
				target.Host = stripDefaultPort(target.Host)
			}
		}
		transport = f.next
	case current.URL.Scheme == "https" && target.Scheme == "http":
		// APT refuses this downgrade when it follows a redirect itself.
		return nil, nil, "the target leaves https for http" + allowlistHint
	default:
		transport = f.public
	}

	hop = current.Clone(current.Context())
	hop.URL = target
	hop.Host = target.Host
	hop.RequestURI = ""
	if !sameHost(original.URL.Host, target.Host) {
		// The client's credentials were issued for the origin it named. Once
		// removed they stay removed: later hops are built from this one.
		hop.Header.Del("Authorization")
		hop.Header.Del("Cookie")
	}
	if listed {
		return hop, transport, ""
	}

	if f.policy != nil {
		if err := f.policy.CheckURL(target); err != nil {
			return nil, nil, "upstream_access does not allow the target: " + err.Error()
		}
	}
	if f.egressProxy != nil {
		if via, err := f.egressProxy(hop); err != nil || via != nil {
			return nil, nil, "the target is reached through a forward proxy, where its address cannot be checked" + allowlistHint
		}
	}
	return hop, transport, ""
}

// handBack leaves res, the redirect that is not followed, for the client and
// records why.
func (f *redirectFollower) handBack(res *http.Response, original, current *http.Request, target *url.URL, reason string) {
	if current != original {
		// The client resolves a relative Location against the URL it asked
		// for, not against the hop that answered.
		res.Header.Set("Location", target.String())
	}
	f.log.Warn().
		Str("origin", original.URL.Host).
		Str("location", logsafe.URL(target.String())).
		Str("reason", reason).
		Msg("redirect from an allowlisted origin was not followed and is passed to the client")
}

// discardRedirectBody closes the body of a redirect that was followed.
func discardRedirectBody(res *http.Response) {
	if res.Body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, res.Body, redirectDrainLimit)
	_ = res.Body.Close()
}
