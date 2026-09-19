# Iteration 9 — CI/CD: PR, Main, and Release Workflows

## Goal

GitHub Actions enforce the same quality gates as `make all` on every PR and push to
main, and a tag-driven release workflow ships cross-platform binaries.

## Scope

- `.github/workflows/pr.yml` — on `pull_request` targeting `main`:
  - **quality** job (`ubuntu-latest`): checkout, setup-go (version from `go.mod`,
    module cache on), `make fmt-check`, `make lint`, `make test` with coverage
    (upload coverage artifact), `make build`.
  - **security** job: `make security` (gosec, govulncheck, gitleaks — pinned versions,
    not `@latest`).
  - **integration** job: start the Cosmos DB emulator container
    (`mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator` on linux/amd64) as a
    service/step with a health-check wait loop, then `make test-integration`.
    Marked `continue-on-error: false` but isolated so a flaky emulator is visible
    separately from unit failures.
  - **release-check** job: `goreleaser check` + `actionlint` (workflows lint
    themselves).
- `.github/workflows/main.yml` — on push to `main`: same quality + security +
  integration gates, plus `goreleaser release --snapshot --clean` and upload of the
  snapshot archives (linux/darwin × amd64/arm64) as build artifacts.
- `.github/workflows/release.yml` — on tag `v*`:
  - Re-run quality gates, then `goreleaser release --clean` with `GITHUB_TOKEN`:
    archives + checksums + changelog (conventional-ish, from git log groups) →
    GitHub Release.
- `.goreleaser.yml` — builds for `linux`/`darwin`/`windows` × `amd64`/`arm64`,
  `CGO_ENABLED=0`, ldflags injecting `app.Version`/`app.BuildDate` (same variables the
  Makefile uses), archive naming `alchemist_<version>_<os>_<arch>`, `LICENSE` +
  `README.md` in archives.
- Makefile additions: `release-snapshot` (`goreleaser release --snapshot --clean`),
  `actionlint` target; `install-tools` grows pinned goreleaser + actionlint.
- Branch protection expectation documented in README (PR checks required to merge).

**Decided:**

- The quality, security, and integration jobs live once in a reusable
  `.github/workflows/gates.yml` that `pr.yml`, `main.yml`, and `release.yml` call,
  instead of three copies. Checks appear as `gates / quality` and so on.
- No `install-tools` target: goreleaser, actionlint, and gitleaks run through pinned
  `go run pkg@version` like every other tool in the `Makefile`, so workflows call
  `make` targets and the version lives in one place. gitleaks is no longer skipped
  when absent. `release-check` and `release` targets joined `release-snapshot`.
- CI starts the emulator from the same `docker-compose.yml` as `make emulator-up`
  (vnext-preview, HTTP), then `make emulator-wait` polls port 8081.
- `go.mod` moved to Go 1.26.8: govulncheck fails on the standard-library
  vulnerabilities in 1.26.4. goreleaser is pinned at v2.17.1, the last release that
  builds with Go 1.26.
- `LICENSE` (MIT) added so archives can carry it.

## Out of scope

- Homebrew tap / package managers, container images, signing/notarization, SBOMs —
  listed in `.goreleaser.yml` comments as future work.
- Windows CI runners (build cross-compiles for windows; tests run on linux only).

## Steps

1. `.goreleaser.yml` + `make release-snapshot` locally green first (fastest feedback).
2. `pr.yml` quality/security/release-check jobs; verify on a draft PR.
3. Integration job with emulator health-check gating (`curl -k` retry loop on 8081;
   generous timeout — the emulator boots slowly).
4. `main.yml`, then `release.yml`; cut `v0.1.0-rc.1` on a fork/test run before the
   real `v0.1.0`.

## Testing

**Local/static:**
- `actionlint` clean on all four workflow files.
- `goreleaser check` and `make release-snapshot` succeed locally; snapshot binaries for
  darwin/arm64 and linux/amd64 run `--version` correctly (version stamped, not `dev`).

**Live (post-merge):**
- Draft PR shows all four PR jobs; failing lint/test actually blocks.
- Push to main produces downloadable snapshot artifacts.
- Tag `v0.1.0-rc.1` → GitHub Release with 6 archives + checksums + changelog;
  `--version` in a downloaded binary prints the tag.

**Manual checklist:**
- [ ] All PR checks green on the iteration's own PR.
- [ ] Emulator integration job passes in CI (not just locally).
- [ ] rc tag release verified end-to-end, then deleted/superseded by `v0.1.0`.

## Acceptance criteria

- CI runs on cache-warm PRs in under ~5 minutes excluding the integration job.
- Every tool version in workflows is pinned (actions by SHA or major, Go tools by
  version) — no `@latest` anywhere in CI.
- Release is fully reproducible from a tag: no manual steps beyond `git tag && git push --tags`.
