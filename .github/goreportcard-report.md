# Go Report Card

**Grade: A+** (96.9%)

| Metric | Value |
| ------ | ----- |
| Files | 260 |
| Issues | 69 |

## Checks

| Check | Score |
| ----- | ----- |
| gofmt | 100% |
| go_vet | 100% |
| ineffassign | 100% |
| gocyclo | 73% |
| license | 100% |
| misspell | 100% |

## Issues

### gocyclo

- `internal/config/loader_merge.go`
  - Line 23: cyclomatic complexity 60 for function MergeConfigsWithExplicit
  - Line 174: cyclomatic complexity 48 for function MergeConfigs
- `internal/mirrorhealth/config.go`
  - Line 48: cyclomatic complexity 30 for function (*Config).Validate
- `internal/cli/repository_test.go`
  - Line 19: cyclomatic complexity 24 for function TestRepositoryCommandPrefetchVerifyRepairExportImport
- `internal/distro/apt_sources_test.go`
  - Line 19: cyclomatic complexity 19 for function TestDebStyleSourceCoverage
- `internal/distro/apt_indexes_test.go`
  - Line 30: cyclomatic complexity 16 for function TestContentsPdiffCoverage
- `internal/repository/archive.go`
  - Line 65: cyclomatic complexity 21 for function (*Store).Import
- `internal/storage/s3vfs/roundtrip_test.go`
  - Line 42: cyclomatic complexity 26 for function (*memoryS3).RoundTrip
- `internal/storage/s3vfs/crash_test.go`
  - Line 59: cyclomatic complexity 24 for function TestS3SpillDoesNotSurviveRepeatedProcessKill
- `internal/proxy/transport.go`
  - Line 61: cyclomatic complexity 20 for function (*RetryableTransport).RoundTrip
- `internal/proxy/cache_review_regression_test.go`
  - Line 111: cyclomatic complexity 19 for function TestNotModifiedKeepsDamagedBodyDetectable
- `tests/integration/multi_server_test.go`
  - Line 260: cyclomatic complexity 17 for function TestMultiServerBenchmarkCacheIsIsolated
- `internal/sharedcache/cache_edges_test.go`
  - Line 88: cyclomatic complexity 16 for function TestMarkerValidationAndPersistenceFailure
- `internal/config/loader_validate.go`
  - Line 63: cyclomatic complexity 40 for function ValidateConfig
- `internal/proxy/rewriter.go`
  - Line 513: cyclomatic complexity 21 for function RewriteRequestByMode
- `internal/distro/apt_appstream_test.go`
  - Line 19: cyclomatic complexity 18 for function TestDebStyleAppStreamCoverage
- `internal/mirrorhealth/reselection_test.go`
  - Line 14: cyclomatic complexity 25 for function TestRuntimeFailoverReselectionClearsOnlyCompleteResponses
- `internal/metalink/metalink_test.go`
  - Line 144: cyclomatic complexity 24 for function TestVerifiedFallbackRefreshAndCanonicalIdentity
  - Line 395: cyclomatic complexity 20 for function TestMetadataPrivacyAndArtifactRevalidation
  - Line 478: cyclomatic complexity 19 for function TestConfigurationAndRoutingValidation
- `internal/config/loader.go`
  - Line 129: cyclomatic complexity 22 for function applyDefaultsWithExplicit
- `internal/config/loader_test.go`
  - Line 24: cyclomatic complexity 18 for function TestLoadConfigFile
- `internal/sharedcache/transport_edges_test.go`
  - Line 31: cyclomatic complexity 21 for function TestVerifiedTransportAdmissionFailuresCloseOwnedResources
- `internal/cli/repository.go`
  - Line 20: cyclomatic complexity 31 for function RunRepository
- `internal/repository/repository_test.go`
  - Line 236: cyclomatic complexity 24 for function TestSnapshotExportImportAndArchiveRejection
- `internal/proxy/mirror_logging_test.go`
  - Line 141: cyclomatic complexity 21 for function TestRepositoryLogsAndSpansRedactCredentials
- `internal/metalink/xml.go`
  - Line 59: cyclomatic complexity 72 for function (*Controller).parse
- `internal/mirrorhealth/failover.go`
  - Line 13: cyclomatic complexity 33 for function (*Controller).failover
  - Line 151: cyclomatic complexity 16 for function (*Controller).verify
- `internal/proxy/download_guard.go`
  - Line 59: cyclomatic complexity 30 for function (*completeUnknownBodyTransport).RoundTrip
- `internal/cli/metalink_test.go`
  - Line 23: cyclomatic complexity 30 for function TestMetalinkServerCacheAndRefresh
- `internal/mirrorhealth/snapshots.go`
  - Line 23: cyclomatic complexity 24 for function (*Controller).observe
- `internal/repository/repository.go`
  - Line 69: cyclomatic complexity 23 for function Open
  - Line 375: cyclomatic complexity 22 for function (*Store).Prefetch
  - Line 184: cyclomatic complexity 20 for function validateSelection
  - Line 464: cyclomatic complexity 17 for function (*Store).validateManifest
  - Line 274: cyclomatic complexity 17 for function (*Store).saveObject
- `internal/mirrorhealth/controller.go`
  - Line 122: cyclomatic complexity 20 for function (*Controller).RoundTrip
- `internal/cli/mirror_refresh_failure_test.go`
  - Line 18: cyclomatic complexity 20 for function TestMirrorRefreshAPIFailurePreservesRequests
- `internal/cli/mirror_health_resource_test.go`
  - Line 22: cyclomatic complexity 43 for function TestProductionFailoverSharesAdmissionAndVerifiesCandidateCache
- `internal/cli/shared_status_test.go`
  - Line 171: cyclomatic complexity 31 for function TestSharedHeadFallbackThroughMirror304PreservesAndWithdrawsGet
  - Line 82: cyclomatic complexity 16 for function TestSharedByHash304FreshensExistingVerifiedBody
  - Line 23: cyclomatic complexity 16 for function TestSharedByHashMissingMirrorDoesNotPoisonHealthyMirror
- `internal/proxy/concurrent_cache_test.go`
  - Line 18: cyclomatic complexity 24 for function TestRepositoryConcurrentColdDownloads
- `internal/proxy/cache_policy_test.go`
  - Line 16: cyclomatic complexity 19 for function TestRepositoryCachePolicyEndToEnd
- `internal/cli/mirror_health_test.go`
  - Line 18: cyclomatic complexity 18 for function TestProductionRuntimeMirrorFailoverAndStatus
- `internal/mirrorhealth/health_body_test.go`
  - Line 14: cyclomatic complexity 17 for function TestRuntimeFailoverCountsIncompleteSuccessfulBodies
- `tests/integration/s3_storage_test.go`
  - Line 119: cyclomatic complexity 16 for function TestS3VFSEndToEnd
- `internal/proxy/download_budget_test.go`
  - Line 239: cyclomatic complexity 16 for function TestSpoolDeletionFailureKeepsBudget
- `internal/cli/apt_installers_test.go`
  - Line 33: cyclomatic complexity 30 for function TestProductionInstallerFilesRewriteAndCache
- `internal/proxy/range_cache_test.go`
  - Line 17: cyclomatic complexity 26 for function TestRepositoryRangeCache
- `internal/config/coverage_behavior_test.go`
  - Line 166: cyclomatic complexity 18 for function TestValidateTLSFilesAndBackendFailures
- `internal/storage/verified_cache_test.go`
  - Line 14: cyclomatic complexity 17 for function TestVerifiedCacheRejectsDamagedDiskBodies
- `internal/metalink/config.go`
  - Line 97: cyclomatic complexity 34 for function New
- `internal/cli/apt_appstream_test.go`
  - Line 33: cyclomatic complexity 29 for function TestProductionAppStreamFilesRewriteAndCache
- `internal/cli/daemon.go`
  - Line 195: cyclomatic complexity 25 for function (*Server).initialize
- `internal/proxy/download_crash_test.go`
  - Line 15: cyclomatic complexity 20 for function TestSpoolDoesNotSurviveProcessKill
- `internal/cli/shared_cache_test.go`
  - Line 23: cyclomatic complexity 19 for function TestSharedRepositoryInstancesCoalesceVerifiedContent
- `internal/storage/s3vfs/http_failure_test.go`
  - Line 29: cyclomatic complexity 18 for function TestS3ConfigurationAndHTTPFailures
- `internal/cli/daemon_test.go`
  - Line 122: cyclomatic complexity 16 for function TestProductionProxyRouteRewritesAndCaches
- `internal/metalink/controller.go`
  - Line 159: cyclomatic complexity 27 for function (*Controller).Handle
  - Line 73: cyclomatic complexity 24 for function parseQuery
  - Line 559: cyclomatic complexity 22 for function (*routingTransport).RoundTrip
  - Line 387: cyclomatic complexity 21 for function (*Controller).read
  - Line 107: cyclomatic complexity 18 for function publicRequest
- `internal/proxy/interrupted_download_test.go`
  - Line 55: cyclomatic complexity 20 for function TestInterruptedUpstreamDownloadRecovers
- `internal/repository/index.go`
  - Line 289: cyclomatic complexity 18 for function (*Store).verify
- `internal/distro/apt_installers_test.go`
  - Line 19: cyclomatic complexity 18 for function TestDebStyleInstallerCoverage
- `internal/mirrorhealth/controller_test.go`
  - Line 440: cyclomatic complexity 19 for function TestRuntimeFailoverConcurrentRequestsShareSelection
- `internal/cli/apt_sources_test.go`
  - Line 33: cyclomatic complexity 29 for function TestProductionSourceArtifactsRewriteAndCache
- `internal/sharedcache/transport.go`
  - Line 78: cyclomatic complexity 27 for function (*verifiedTransport).RoundTrip
- `internal/proxy/transport_edges_test.go`
  - Line 22: cyclomatic complexity 26 for function TestRetryTransportRequestBodyReplayAndFailure
- `internal/proxy/private_repository_test.go`
  - Line 59: cyclomatic complexity 22 for function TestPrivateRepositoryRequestsBypassSharedCache
- `internal/metalink/edge_cases_test.go`
  - Line 29: cyclomatic complexity 18 for function TestMetadataEndpointSuccessFailureAndExpiry
- `internal/cli/lifecycle_coverage_test.go`
  - Line 29: cyclomatic complexity 17 for function TestServeReloadBurstAndCancellation
- `internal/sharedcache/policy.go`
  - Line 38: cyclomatic complexity 33 for function New
  - Line 170: cyclomatic complexity 19 for function (*Policy).content
- `internal/cli/shared_resource_test.go`
  - Line 71: cyclomatic complexity 33 for function TestSharedColdIdentityCoalescesThroughFailoverAndResourceBudget
  - Line 192: cyclomatic complexity 18 for function TestSharedVerificationCancellationReleasesAllResourcesAndMissLock
- `internal/cli/repository_methods_test.go`
  - Line 16: cyclomatic complexity 30 for function TestProductionRepositoryRejectsWritesBeforeMirrorAuthentication
- `internal/proxy/handler.go`
  - Line 276: cyclomatic complexity 23 for function NewPackageStruct
- `internal/benchmarks/mirror_logging_test.go`
  - Line 19: cyclomatic complexity 20 for function TestBenchmarkLogsRedactCredentialsWithoutChangingResults
- `internal/sharedcache/locks_test.go`
  - Line 17: cyclomatic complexity 19 for function TestFileCoordinatorCancellationAndCrashRelease
- `internal/cli/client_access_test.go`
  - Line 30: cyclomatic complexity 21 for function TestProductionDownloadClientAccess
- `internal/proxy/corrupt_cache_test.go`
  - Line 20: cyclomatic complexity 20 for function TestDamagedPackageIsDownloadedAgain

---

_Generated by [Go Report Card](https://github.com/soulteary/goreportcard-action)._
