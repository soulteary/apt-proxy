# Go report workflow

The PR job runs both owned Actions with `commit: false` and released CLIs
(`go-test-report-action v1.1.2`, `goreportcard-action v1.1.3`, pinned wrapper SHAs).
It replaces the primary Linux Go test/coverage invocation and reuses its profile
for existing coverage artifacts/gates. Other OS matrices, release verification,
integration, benchmarks and fuzz workflows retain their distinct checks.

Reports run on pushed tags and `workflow_dispatch`. They do not generate bot
commits on branch pushes. A separate job may commit the five stable reports in
one report commit, only if the tested commit still equals the default branch
head. Older release tags and non-default manual refs retain downloadable reports
without overwriting the current branch. PRs never enter that write job. Protected
branches can reject writeback; no PAT, bypass, or force-push is used.

Test directory: `.`. Packages: `./...`.
Race detector: `true`. Additional args: `-short -count=1`.
Coverage floor: `80%` (zero records a baseline).
Per-package floor: `0%`.
Quality-score floor: `0%`.
Raw profiles/test logs are artifacts, never committed.

Published releases with a CI skip annotation use the release-event fallback. Ordinary releases use the tag-push report run; the release gate skips duplicate tests. Stable artifacts containing only reports under `.github` are downloaded into that directory explicitly; mixed report/document artifacts retain the repository hierarchy.

Published releases and automated release dispatches reuse only completed successful tag-push report runs for the tested SHA that still have an unexpired stable report artifact. An artifact from an earlier attempt of the same run is valid after a partial retry (`artifact_attempt <= run_attempt`). Active, failed, cancelled, missing, or expired runs do not suppress fresh reports. API errors fail the gate rather than silently assume evidence exists.

The producing reports job outputs its artifact name. Writeback uses that output even when only failed jobs are rerun; it never substitutes the consuming attempt number. Five report files restore the `.github` artifact root; grantseal's mixed report/document artifact retains repository hierarchy.

Report concurrency includes the source ref and unique run ID, so pending reports cannot replace each other. Writeback concurrency includes the tested SHA, so historical releases cannot replace the current source's pending writer. Only the unchanged default-branch source can publish; identical-source writers serialize and a successful report commit makes later writers stale. Artifacts remain available when writeback is skipped or blocked by branch protection.

Cleanup runs even after preceding steps fail. Stable and raw evidence is uploaded after gate failures, and failed report jobs cannot publish repository changes. Report commits have no CI skip annotation: the normal workflow token prevents recursive push triggers. Production Actions and CLI versions remain pinned to formal releases; PR report calls remain read-only.

Successful token-published releases explicitly dispatch the current default-branch report workflow for their released tag, since token-generated release events do not start another workflow. Branch snapshots and PR release checks do not dispatch report publishers. Historical tags can also be reported manually from the default-branch workflow using `source_ref`.
