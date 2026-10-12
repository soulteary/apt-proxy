package cli

import httpcache "github.com/soulteary/httpcache-kit/v4"

// New server caches require private SHA256 metadata by default. An explicit
// opt-out retains legacy performance when the operator accepts that tradeoff.
func applyCacheIntegrity(cache *httpcache.CacheConfig, mode string) {
	cache.Integrity = httpcache.IntegritySHA256
	if mode == "none" {
		cache.Integrity = httpcache.IntegrityDisabled
	}
}
