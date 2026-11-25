# Go Report Card

**Grade: A+** (96.8%)

| Metric | Value |
| ------ | ----- |
| Files | 283 |
| Issues | 78 |

## Checks

| Check | Score |
| ----- | ----- |
| gofmt | 100% |
| go_vet | 100% |
| ineffassign | 100% |
| gocyclo | 72% |
| license | 100% |
| misspell | 100% |

## Issues

### gocyclo

- `internal/proxy/rewriter.go`
  - Line 614: cyclomatic complexity 21 for function rewriteToMirror
- `internal/config/loader_test.go`
  - Line 24: cyclomatic complexity 18 for function TestLoadConfigFile
- `internal/cli/mirror_health_test.go`
  - Line 16: cyclomatic complexity 18 for function TestProductionRuntimeMirrorFailoverAndStatus
- `internal/proxy/download_guard.go`
  - Line 101: cyclomatic complexity 30 for function (*completeUnknownBodyTransport).RoundTrip
- `internal/proxy/handler.go`
  - Line 243: cyclomatic complexity 27 for function NewPackageStruct
  - Line 624: cyclomatic complexity 16 for function (*PackageStruct).handleExternalURLs
- `internal/repository/index.go`
  - Line 300: cyclomatic complexity 23 for function (*Store).verify
  - Line 122: cyclomatic complexity 16 for function (*Store).selectedIndexes
- `internal/distro/immutable_rules_test.go`
  - Line 22: cyclomatic complexity 20 for function TestDebStyleImmutableArtifactPolicy
- `internal/proxy/immutable_fallback_test.go`
  - Line 33: cyclomatic complexity 38 for function TestImmutableFallbackTransport
- `internal/proxy/transport_edges_test.go`
  - Line 22: cyclomatic complexity 26 for function TestRetryTransportRequestBodyReplayAndFailure
- `internal/metalink/metalink_test.go`
  - Line 144: cyclomatic complexity 24 for function TestVerifiedFallbackRefreshAndCanonicalIdentity
  - Line 395: cyclomatic complexity 20 for function TestMetadataPrivacyAndArtifactRevalidation
  - Line 478: cyclomatic complexity 19 for function TestConfigurationAndRoutingValidation
- `internal/proxy/cache_review_regression_test.go`
  - Line 111: cyclomatic complexity 19 for function TestNotModifiedKeepsDamagedBodyDetectable
- `internal/mirrorhealth/controller_test.go`
  - Line 440: cyclomatic complexity 19 for function TestRuntimeFailoverConcurrentRequestsShareSelection
- `internal/config/loader_validate.go`
  - Line 88: cyclomatic complexity 40 for function ValidateConfig
- `internal/metalink/config.go`
  - Line 97: cyclomatic complexity 34 for function New
- `internal/cli/apt_installers_test.go`
  - Line 33: cyclomatic complexity 30 for function TestProductionInstallerFilesRewriteAndCache
- `internal/config/loader.go`
  - Line 121: cyclomatic complexity 22 for function applyDefaultsWithExplicit
- `internal/benchmarks/mirror_logging_test.go`
  - Line 19: cyclomatic complexity 20 for function TestBenchmarkLogsRedactCredentialsWithoutChangingResults
- `tests/integration/s3_storage_test.go`
  - Line 119: cyclomatic complexity 16 for function TestS3VFSEndToEnd
- `internal/cli/apt_sources_test.go`
  - Line 33: cyclomatic complexity 29 for function TestProductionSourceArtifactsRewriteAndCache
- `internal/storage/s3vfs/roundtrip_test.go`
  - Line 42: cyclomatic complexity 26 for function (*memoryS3).RoundTrip
- `internal/cli/shared_cache_test.go`
  - Line 23: cyclomatic complexity 19 for function TestSharedRepositoryInstancesCoalesceVerifiedContent
- `internal/metalink/edge_cases_test.go`
  - Line 29: cyclomatic complexity 18 for function TestMetadataEndpointSuccessFailureAndExpiry
- `internal/proxy/transport.go`
  - Line 61: cyclomatic complexity 20 for function (*RetryableTransport).RoundTrip
- `internal/proxy/corrupt_cache_test.go`
  - Line 20: cyclomatic complexity 20 for function TestDamagedPackageIsDownloadedAgain
- `internal/mirrorhealth/failover.go`
  - Line 13: cyclomatic complexity 33 for function (*Controller).failover
  - Line 151: cyclomatic complexity 16 for function (*Controller).verify
- `internal/mirrorhealth/reselection_test.go`
  - Line 14: cyclomatic complexity 25 for function TestRuntimeFailoverReselectionClearsOnlyCompleteResponses
- `internal/repository/archive.go`
  - Line 65: cyclomatic complexity 21 for function (*Store).Import
- `internal/cli/immutable_cache_test.go`
  - Line 34: cyclomatic complexity 17 for function TestProductionImmutableArtifactCaching
- `internal/metalink/xml.go`
  - Line 59: cyclomatic complexity 72 for function (*Controller).parse
- `internal/config/loader_merge.go`
  - Line 23: cyclomatic complexity 63 for function MergeConfigsWithExplicit
  - Line 186: cyclomatic complexity 51 for function MergeConfigs
- `internal/cli/shared_resource_test.go`
  - Line 70: cyclomatic complexity 33 for function TestSharedColdIdentityCoalescesThroughFailoverAndResourceBudget
  - Line 191: cyclomatic complexity 18 for function TestSharedVerificationCancellationReleasesAllResourcesAndMissLock
- `internal/cli/shared_status_test.go`
  - Line 169: cyclomatic complexity 31 for function TestSharedHeadFallbackThroughMirror304PreservesAndWithdrawsGet
  - Line 80: cyclomatic complexity 16 for function TestSharedByHash304FreshensExistingVerifiedBody
  - Line 21: cyclomatic complexity 16 for function TestSharedByHashMissingMirrorDoesNotPoisonHealthyMirror
- `internal/sharedcache/transport_edges_test.go`
  - Line 31: cyclomatic complexity 21 for function TestVerifiedTransportAdmissionFailuresCloseOwnedResources
- `internal/distro/apt_sources_test.go`
  - Line 19: cyclomatic complexity 19 for function TestDebStyleSourceCoverage
- `internal/storage/verified_cache_test.go`
  - Line 14: cyclomatic complexity 17 for function TestVerifiedCacheRejectsDamagedDiskBodies
- `internal/sharedcache/transport.go`
  - Line 78: cyclomatic complexity 27 for function (*verifiedTransport).RoundTrip
- `internal/proxy/range_cache_test.go`
  - Line 17: cyclomatic complexity 26 for function TestRepositoryRangeCache
- `internal/proxy/cache_policy_test.go`
  - Line 16: cyclomatic complexity 19 for function TestRepositoryCachePolicyEndToEnd
- `internal/metalink/controller.go`
  - Line 159: cyclomatic complexity 27 for function (*Controller).Handle
  - Line 73: cyclomatic complexity 24 for function parseQuery
  - Line 559: cyclomatic complexity 22 for function (*routingTransport).RoundTrip
  - Line 387: cyclomatic complexity 21 for function (*Controller).read
  - Line 107: cyclomatic complexity 18 for function publicRequest
- `internal/storage/s3vfs/crash_test.go`
  - Line 59: cyclomatic complexity 24 for function TestS3SpillDoesNotSurviveRepeatedProcessKill
- `internal/proxy/download_crash_test.go`
  - Line 15: cyclomatic complexity 20 for function TestSpoolDoesNotSurviveProcessKill
- `internal/sharedcache/locks_test.go`
  - Line 17: cyclomatic complexity 19 for function TestFileCoordinatorCancellationAndCrashRelease
- `internal/storage/s3vfs/http_failure_test.go`
  - Line 29: cyclomatic complexity 18 for function TestS3ConfigurationAndHTTPFailures
- `internal/distro/apt_installers_test.go`
  - Line 19: cyclomatic complexity 18 for function TestDebStyleInstallerCoverage
- `tests/integration/multi_server_test.go`
  - Line 260: cyclomatic complexity 17 for function TestMultiServerBenchmarkCacheIsIsolated
- `internal/cli/lifecycle_coverage_test.go`
  - Line 28: cyclomatic complexity 16 for function TestServeReloadBurstAndCancellation
- `internal/cli/daemon.go`
  - Line 190: cyclomatic complexity 25 for function (*Server).initialize
- `internal/repository/repository_test.go`
  - Line 498: cyclomatic complexity 31 for function TestOfficialArchiveLayoutSnapshotOffline
  - Line 310: cyclomatic complexity 24 for function TestSnapshotExportImportAndArchiveRejection
  - Line 579: cyclomatic complexity 16 for function TestOfficialArchiveLayoutExportImportAndRepair
- `internal/repository/repository.go`
  - Line 375: cyclomatic complexity 25 for function (*Store).Prefetch
  - Line 69: cyclomatic complexity 23 for function Open
  - Line 184: cyclomatic complexity 20 for function validateSelection
  - Line 478: cyclomatic complexity 17 for function (*Store).validateManifest
  - Line 274: cyclomatic complexity 17 for function (*Store).saveObject
- `internal/proxy/interrupted_download_test.go`
  - Line 55: cyclomatic complexity 22 for function TestInterruptedUpstreamDownloadRecovers
- `internal/repository/edge_cases_test.go`
  - Line 392: cyclomatic complexity 21 for function TestRepositoryClosureWithAbsentIndexVariants
- `internal/cli/repository_test.go`
  - Line 19: cyclomatic complexity 24 for function TestRepositoryCommandPrefetchVerifyRepairExportImport
- `internal/cli/mirror_refresh_failure_test.go`
  - Line 18: cyclomatic complexity 24 for function TestMirrorRefreshAPIFailurePreservesRequests
- `internal/proxy/private_repository_test.go`
  - Line 59: cyclomatic complexity 22 for function TestPrivateRepositoryRequestsBypassSharedCache
- `internal/mirrorhealth/controller.go`
  - Line 122: cyclomatic complexity 20 for function (*Controller).RoundTrip
- `internal/config/coverage_behavior_test.go`
  - Line 161: cyclomatic complexity 18 for function TestValidateTLSFilesAndBackendFailures
- `internal/mirrorhealth/health_body_test.go`
  - Line 14: cyclomatic complexity 17 for function TestRuntimeFailoverCountsIncompleteSuccessfulBodies
- `internal/proxy/download_budget_test.go`
  - Line 239: cyclomatic complexity 16 for function TestSpoolDeletionFailureKeepsBudget
- `internal/distro/apt_indexes_test.go`
  - Line 30: cyclomatic complexity 16 for function TestContentsPdiffCoverage
- `internal/mirrorhealth/config.go`
  - Line 48: cyclomatic complexity 30 for function (*Config).Validate
- `internal/proxy/immutable_cache_test.go`
  - Line 173: cyclomatic complexity 24 for function TestExpiredEntryDuringUpstreamFailure
- `internal/mirrorhealth/snapshots.go`
  - Line 23: cyclomatic complexity 24 for function (*Controller).observe
- `internal/proxy/mirror_logging_test.go`
  - Line 162: cyclomatic complexity 21 for function TestRepositoryLogsAndSpansRedactCredentials
- `internal/cli/client_access_test.go`
  - Line 30: cyclomatic complexity 21 for function TestProductionDownloadClientAccess
- `internal/distro/apt_appstream_test.go`
  - Line 19: cyclomatic complexity 18 for function TestDebStyleAppStreamCoverage
- `internal/proxy/mode_set_test.go`
  - Line 141: cyclomatic complexity 17 for function TestModeSetServesExactlyTheEnabledDistributions
  - Line 411: cyclomatic complexity 16 for function TestRefreshMirrorsKeepsAndReplacesTheEnabledSet
- `internal/cli/mode_set_test.go`
  - Line 292: cyclomatic complexity 17 for function TestReloadWithVanishedModeIDKeepsPreviousRouting
- `internal/sharedcache/policy.go`
  - Line 38: cyclomatic complexity 33 for function New
  - Line 170: cyclomatic complexity 19 for function (*Policy).content
- `internal/cli/repository.go`
  - Line 20: cyclomatic complexity 31 for function RunRepository
- `internal/cli/apt_appstream_test.go`
  - Line 33: cyclomatic complexity 29 for function TestProductionAppStreamFilesRewriteAndCache
- `internal/proxy/concurrent_cache_test.go`
  - Line 18: cyclomatic complexity 24 for function TestRepositoryConcurrentColdDownloads
- `internal/proxy/refresh_failure_test.go`
  - Line 56: cyclomatic complexity 22 for function TestFailedMirrorElectionKeepsPreviousMirror
  - Line 132: cyclomatic complexity 18 for function TestFailedMirrorElectionWithoutPreviousMirrorRetainsRouting
- `internal/cli/mirror_health_resource_test.go`
  - Line 21: cyclomatic complexity 43 for function TestProductionFailoverSharesAdmissionAndVerifiesCandidateCache
- `internal/cli/repository_methods_test.go`
  - Line 16: cyclomatic complexity 30 for function TestProductionRepositoryRejectsWritesBeforeMirrorAuthentication
- `internal/cli/metalink_test.go`
  - Line 23: cyclomatic complexity 30 for function TestMetalinkServerCacheAndRefresh
- `internal/proxy/official_mirror_test.go`
  - Line 469: cyclomatic complexity 17 for function TestRefreshPrefersPreviousMirrorOverOfficialMirror
- `internal/sharedcache/cache_edges_test.go`
  - Line 88: cyclomatic complexity 16 for function TestMarkerValidationAndPersistenceFailure
- `internal/cli/daemon_test.go`
  - Line 123: cyclomatic complexity 16 for function TestProductionProxyRouteRewritesAndCaches

---

_Generated by [Go Report Card](https://github.com/soulteary/goreportcard-action)._
