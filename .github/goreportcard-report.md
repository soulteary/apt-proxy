# Go Report Card

**Grade: A+** (97.0%)

| Metric | Value |
| ------ | ----- |
| Files | 224 |
| Issues | 57 |

## Checks

| Check | Score |
| ----- | ----- |
| gofmt | 100% |
| go_vet | 100% |
| ineffassign | 100% |
| gocyclo | 74% |
| license | 100% |
| misspell | 100% |

## Issues

### gocyclo

- `internal/repository/index.go`
  - Line 289: cyclomatic complexity 18 for function (*Store).verify
- `internal/mirrorhealth/reselection_test.go`
  - Line 14: cyclomatic complexity 25 for function TestRuntimeFailoverReselectionClearsOnlyCompleteResponses
- `internal/cli/daemon_test.go`
  - Line 122: cyclomatic complexity 16 for function TestProductionProxyRouteRewritesAndCaches
- `internal/distro/apt_sources_test.go`
  - Line 19: cyclomatic complexity 19 for function TestDebStyleSourceCoverage
- `internal/cli/shared_status_test.go`
  - Line 171: cyclomatic complexity 31 for function TestSharedHeadFallbackThroughMirror304PreservesAndWithdrawsGet
  - Line 82: cyclomatic complexity 16 for function TestSharedByHash304FreshensExistingVerifiedBody
  - Line 23: cyclomatic complexity 16 for function TestSharedByHashMissingMirrorDoesNotPoisonHealthyMirror
- `internal/proxy/range_cache_test.go`
  - Line 17: cyclomatic complexity 26 for function TestRepositoryRangeCache
- `internal/mirrorhealth/controller.go`
  - Line 122: cyclomatic complexity 20 for function (*Controller).RoundTrip
- `internal/benchmarks/mirror_logging_test.go`
  - Line 19: cyclomatic complexity 20 for function TestBenchmarkLogsRedactCredentialsWithoutChangingResults
- `internal/sharedcache/transport.go`
  - Line 78: cyclomatic complexity 27 for function (*verifiedTransport).RoundTrip
- `internal/proxy/concurrent_cache_test.go`
  - Line 18: cyclomatic complexity 24 for function TestRepositoryConcurrentColdDownloads
- `internal/proxy/mirror_logging_test.go`
  - Line 141: cyclomatic complexity 21 for function TestRepositoryLogsAndSpansRedactCredentials
- `internal/proxy/download_guard.go`
  - Line 59: cyclomatic complexity 30 for function (*completeUnknownBodyTransport).RoundTrip
- `internal/mirrorhealth/controller_test.go`
  - Line 440: cyclomatic complexity 19 for function TestRuntimeFailoverConcurrentRequestsShareSelection
- `internal/config/loader_merge.go`
  - Line 23: cyclomatic complexity 59 for function MergeConfigsWithExplicit
  - Line 171: cyclomatic complexity 47 for function MergeConfigs
- `internal/mirrorhealth/snapshots.go`
  - Line 23: cyclomatic complexity 24 for function (*Controller).observe
- `internal/config/loader_test.go`
  - Line 24: cyclomatic complexity 18 for function TestLoadConfigFile
- `internal/storage/verified_cache_test.go`
  - Line 14: cyclomatic complexity 17 for function TestVerifiedCacheRejectsDamagedDiskBodies
- `internal/cli/apt_appstream_test.go`
  - Line 33: cyclomatic complexity 29 for function TestProductionAppStreamFilesRewriteAndCache
- `internal/proxy/private_repository_test.go`
  - Line 59: cyclomatic complexity 22 for function TestPrivateRepositoryRequestsBypassSharedCache
- `internal/proxy/handler.go`
  - Line 272: cyclomatic complexity 21 for function NewPackageStruct
- `internal/distro/apt_appstream_test.go`
  - Line 19: cyclomatic complexity 18 for function TestDebStyleAppStreamCoverage
- `internal/sharedcache/policy.go`
  - Line 38: cyclomatic complexity 33 for function New
  - Line 170: cyclomatic complexity 19 for function (*Policy).content
- `tests/integration/s3_storage_test.go`
  - Line 119: cyclomatic complexity 16 for function TestS3VFSEndToEnd
- `internal/proxy/rewriter.go`
  - Line 509: cyclomatic complexity 16 for function RewriteRequestByMode
- `internal/cli/repository.go`
  - Line 20: cyclomatic complexity 31 for function RunRepository
- `internal/cli/client_access_test.go`
  - Line 30: cyclomatic complexity 21 for function TestProductionDownloadClientAccess
- `internal/cli/mirror_health_test.go`
  - Line 18: cyclomatic complexity 18 for function TestProductionRuntimeMirrorFailoverAndStatus
- `internal/mirrorhealth/failover.go`
  - Line 13: cyclomatic complexity 33 for function (*Controller).failover
  - Line 151: cyclomatic complexity 16 for function (*Controller).verify
- `internal/mirrorhealth/config.go`
  - Line 48: cyclomatic complexity 30 for function (*Config).Validate
- `internal/cli/repository_test.go`
  - Line 19: cyclomatic complexity 24 for function TestRepositoryCommandPrefetchVerifyRepairExportImport
- `internal/cli/mirror_refresh_failure_test.go`
  - Line 18: cyclomatic complexity 20 for function TestMirrorRefreshAPIFailurePreservesRequests
- `internal/proxy/cache_review_regression_test.go`
  - Line 111: cyclomatic complexity 19 for function TestNotModifiedKeepsDamagedBodyDetectable
- `internal/proxy/cache_policy_test.go`
  - Line 16: cyclomatic complexity 19 for function TestRepositoryCachePolicyEndToEnd
- `internal/cli/apt_installers_test.go`
  - Line 33: cyclomatic complexity 30 for function TestProductionInstallerFilesRewriteAndCache
- `internal/storage/s3vfs/roundtrip_test.go`
  - Line 42: cyclomatic complexity 26 for function (*memoryS3).RoundTrip
- `internal/cli/daemon.go`
  - Line 191: cyclomatic complexity 25 for function (*Server).initialize
- `internal/proxy/interrupted_download_test.go`
  - Line 55: cyclomatic complexity 20 for function TestInterruptedUpstreamDownloadRecovers
- `internal/config/loader_validate.go`
  - Line 62: cyclomatic complexity 39 for function ValidateConfig
- `internal/cli/repository_methods_test.go`
  - Line 16: cyclomatic complexity 30 for function TestProductionRepositoryRejectsWritesBeforeMirrorAuthentication
- `internal/storage/s3vfs/crash_test.go`
  - Line 59: cyclomatic complexity 24 for function TestS3SpillDoesNotSurviveRepeatedProcessKill
- `internal/config/loader.go`
  - Line 129: cyclomatic complexity 22 for function applyDefaultsWithExplicit
- `internal/proxy/transport.go`
  - Line 61: cyclomatic complexity 20 for function (*RetryableTransport).RoundTrip
- `tests/integration/multi_server_test.go`
  - Line 260: cyclomatic complexity 17 for function TestMultiServerBenchmarkCacheIsIsolated
- `internal/proxy/download_crash_test.go`
  - Line 15: cyclomatic complexity 20 for function TestSpoolDoesNotSurviveProcessKill
- `internal/cli/shared_cache_test.go`
  - Line 23: cyclomatic complexity 19 for function TestSharedRepositoryInstancesCoalesceVerifiedContent
- `internal/cli/mirror_health_resource_test.go`
  - Line 22: cyclomatic complexity 43 for function TestProductionFailoverSharesAdmissionAndVerifiesCandidateCache
- `internal/proxy/corrupt_cache_test.go`
  - Line 20: cyclomatic complexity 20 for function TestDamagedPackageIsDownloadedAgain
- `internal/distro/apt_installers_test.go`
  - Line 19: cyclomatic complexity 18 for function TestDebStyleInstallerCoverage
- `internal/mirrorhealth/health_body_test.go`
  - Line 14: cyclomatic complexity 17 for function TestRuntimeFailoverCountsIncompleteSuccessfulBodies
- `internal/cli/shared_resource_test.go`
  - Line 71: cyclomatic complexity 33 for function TestSharedColdIdentityCoalescesThroughFailoverAndResourceBudget
  - Line 192: cyclomatic complexity 18 for function TestSharedVerificationCancellationReleasesAllResourcesAndMissLock
- `internal/distro/apt_indexes_test.go`
  - Line 30: cyclomatic complexity 16 for function TestContentsPdiffCoverage
- `internal/repository/archive.go`
  - Line 65: cyclomatic complexity 21 for function (*Store).Import
- `internal/sharedcache/locks_test.go`
  - Line 17: cyclomatic complexity 19 for function TestFileCoordinatorCancellationAndCrashRelease
- `internal/proxy/download_budget_test.go`
  - Line 239: cyclomatic complexity 16 for function TestSpoolDeletionFailureKeepsBudget
- `internal/cli/apt_sources_test.go`
  - Line 33: cyclomatic complexity 29 for function TestProductionSourceArtifactsRewriteAndCache
- `internal/repository/repository_test.go`
  - Line 236: cyclomatic complexity 24 for function TestSnapshotExportImportAndArchiveRejection
- `internal/repository/repository.go`
  - Line 69: cyclomatic complexity 23 for function Open
  - Line 375: cyclomatic complexity 22 for function (*Store).Prefetch
  - Line 184: cyclomatic complexity 20 for function validateSelection
  - Line 464: cyclomatic complexity 17 for function (*Store).validateManifest
  - Line 274: cyclomatic complexity 17 for function (*Store).saveObject

---

_Generated by [Go Report Card](https://github.com/soulteary/goreportcard-action)._
