# Iteration 26 — winget

<!-- cspell:words LOCALAPPDATA MSIX Unibo winget wingetcreate missingkey notmatch symref -->

## Goal

A Windows user installs, upgrades and removes Alchemist with winget:

```powershell
winget install --id ColbyTimm.Alchemist -e
winget upgrade --id ColbyTimm.Alchemist -e
winget uninstall --id ColbyTimm.Alchemist -e
```

Each stable release tag generates the winget manifests and opens a pull request to
`microsoft/winget-pkgs`, with no manual step on our side. Microsoft's moderators merge
it. CI validates the manifests on a Windows runner before any tag is cut, and installs
from them after a release is published.

No Go code changes. The work is release configuration, two workflow jobs, a secret, and
the user docs.

## What was verified, and where

| Fact | Source |
|---|---|
| The release uses goreleaser v2.17.1, run through `make release` on `ubuntu-latest` with only `GITHUB_TOKEN` | `Makefile`, `.github/workflows/release.yml` |
| Windows builds are `amd64` and `arm64`, archived as `.zip` with `LICENSE` and `README.md`, not wrapped in a folder | `.goreleaser.yml` |
| goreleaser's `winget` pipe accepts only `.zip` archives (as `InstallerType: zip`, `NestedInstallerType: portable`) or bare binaries (`portable`). It never picks up an `.msi` | `internal/pipe/winget/winget.go`, `makeInstaller`, v2.17.1 |
| It writes manifest schema 1.12.0, sets `UpgradeBehavior: uninstallPrevious`, and sets `PortableCommandAlias` to the binary name without `.exe` | same file, `template.go` |
| It skips when two `amd64` zips match, and asks for `ids` to pick one | `errMultipleArchives` |
| With `pull_request.enabled` and no `branch`, it pushes to a per-version branch `{{ .ProjectName }}-{{ .Version }}`, because winget-pkgs rejects a PR that touches more than one version | `internal/client/config.go`, `TemplateRef` |
| It tries to sync the fork with its base before pushing, and only warns if that fails | `doPublish` |
| `skip_upload` is a template. `auto` skips a prerelease. `true` skips always. The check runs before the token is read | `doPublish` |
| `repository.token` must be a single `{{ .Env.X }}` and fails on a missing variable (`missingkey=error`) | `internal/tmpl/tmpl.go`, `ApplySingleEnvOnly` |
| `isEnvSet` is a template function, and is false for a variable set to the empty string | `internal/tmpl/tmpl.go` |
| In a snapshot, goreleaser skips every publisher but still runs the winget pipe, so the manifests land in `dist/winget/` | `cmd/release.go`, `internal/pipeline/pipeline.go`, `template.go` |
| The GitHub Release is published before the winget PR. A failed winget publish lets the other publishers finish, then fails the command | `internal/pipe/publish/publish.go`, `ContinueOnError` |
| `microsoft/winget-pkgs` has `master` as its default branch | `git ls-remote --symref`, 2026-09-27 |
| No `ColbyTimm` package exists in winget-pkgs. `Unibo.Alchemist` does (a WiX installer, `PackageName: Alchemist`, no moniker) | tree of `microsoft/winget-pkgs` at `HEAD`, 2026-09-27 |
| goreleaser zip-portable manifests are accepted: `charmbracelet.gum` 2.0.0 is one, merged with the same schema version | `manifests/c/charmbracelet/gum/2.0.0` in winget-pkgs |
| Config, state and data paths are built from `os.UserHomeDir`, so on Windows they sit under `%USERPROFILE%` | `internal/config/store.go`, `internal/logging/logging.go`, `internal/snapshot/layout.go` |

What could not be verified from here is listed under [Open questions](#open-questions).

## Package identity

| Manifest field | Value | Why |
|---|---|---|
| `PackageIdentifier` | `ColbyTimm.Alchemist` | Publisher dot product, the winget convention. Set explicitly: goreleaser's default would be `ColbyTimm.alchemist` from `project_name` |
| `PackageName` | `Alchemist` | |
| `Moniker` | `alchemist` | goreleaser sets it from `name` |
| `Publisher`, `Author` | `Colby Timm` | matches `LICENSE` |
| `License` | `MIT` | |
| Command | `alchemist` | from `PortableCommandAlias` |

`Unibo.Alchemist`, an unrelated simulator, also has `PackageName: Alchemist`. So
`winget install alchemist` may match two packages and ask. The docs always give
`--id ColbyTimm.Alchemist -e`, which is unambiguous.

## Installer type: the portable zip

winget installs the zip goreleaser already builds. The `.msi` from iteration 24 stays
a direct download.

- goreleaser generates manifests only for zips and bare binaries. An `.msi` manifest
  would need `wingetcreate` or hand-written YAML, a Windows job in the release, and
  the MSI's `ProductCode` in `AppsAndFeaturesEntries` so winget can match installs.
- A portable install puts `alchemist` on the PATH through winget's own links folder.
  Nothing in the zip has to do it.
- `UpgradeBehavior: uninstallPrevious` gives a clean upgrade: winget removes the old
  files and installs the new ones. It tracks the portable install itself, so upgrade
  and uninstall need no installer logic of ours.
- An unsigned MSI draws more scrutiny in winget-pkgs validation and at install time
  than an unsigned executable run from a terminal.

The manifest lists `x64` and `arm64`. There is no `386` build, and none is added.

A user who installs both the MSI and the winget package gets two copies on the PATH.
The README says to pick one.

## Publishing on each release tag

goreleaser's `winget` pipe, not `wingetcreate`. It runs in the existing `make release`
on Linux, reads the checksums goreleaser already computed, and needs no new tool or
runner in the release path. `wingetcreate` is a Windows-only .NET tool whose `update`
command needs a package that already exists.

`.goreleaser.yml` gains:

```yaml
winget:
  - name: alchemist
    package_identifier: ColbyTimm.Alchemist
    package_name: Alchemist
    publisher: Colby Timm
    publisher_url: https://github.com/colbytimm
    author: Colby Timm
    copyright: Copyright (c) 2026 Colby Timm
    short_description: A terminal IDE for Azure Cosmos DB.
    description: >-
      A keyboard-driven terminal IDE for Azure Cosmos DB (NoSQL API). Browse
      databases, write SQL, and page through results in one screen.
    homepage: https://github.com/colbytimm/Alchemist
    license: MIT
    license_url: https://github.com/colbytimm/Alchemist/blob/main/LICENSE
    release_notes_url: https://github.com/colbytimm/Alchemist/releases/tag/{{ .Tag }}
    tags: [azure, cosmos-db, database, sql, terminal, tui]
    skip_upload: '{{ if isEnvSet "WINGET_GITHUB_TOKEN" }}auto{{ else }}true{{ end }}'
    repository:
      owner: colbytimm
      name: winget-pkgs
      token: "{{ .Env.WINGET_GITHUB_TOKEN }}"
      pull_request:
        enabled: true
        base:
          owner: microsoft
          name: winget-pkgs
          branch: master
```

- `release_notes_url` and no `release_notes`: the changelog can be long, and the
  release page already holds it.
- If iteration 24 gives Windows more than one zip per architecture, add
  `ids: [<id of the plain archive>]`. Otherwise goreleaser skips with
  `found multiple archives for the same platform`.
- If iteration 24 wraps archive contents in a folder, nothing changes here: goreleaser
  reads the wrap folder into `RelativeFilePath`.
- The header comment's "other package managers" future-work line loses winget.

The fork `colbytimm/winget-pkgs` is created once, by hand, before the first tag.

## The token

The default `GITHUB_TOKEN` can write only to this repository. Pushing to the fork and
opening a PR on `microsoft/winget-pkgs` needs a token of the fork's owner.

| Item | Value |
|---|---|
| Secret | `WINGET_GITHUB_TOKEN`, a repository secret of `colbytimm/Alchemist` |
| Kind | classic personal access token of `colbytimm`, scope `public_repo`, with an expiry |
| Used by | the `release` job only, as an env variable on `make release` |

A classic token, because a fine-grained one is limited to one resource owner's
repositories, and the PR is opened on Microsoft's. Whether a fine-grained token can
do this now is an open question; switch if it can.

What happens without it:

| Run | Result |
|---|---|
| Fork of Alchemist, or no secret set | `skip_upload` is `true`. goreleaser logs `winget.skip_upload is set` and the release succeeds |
| Prerelease tag (`v0.3.0-rc.1`) | `auto` skips. Manifests are still built and checked on Windows |
| Snapshot (`main.yml`, `make release-snapshot`) | publishing is skipped. Manifests land in `dist/winget/` |
| Expired or revoked token | the GitHub Release is already up. The winget step fails and the job goes red |

To recover from a failed winget step, rotate the secret, then open the PR by hand from
the `winget-manifests` artifact of that run: copy its folder into a branch of the fork
and open the PR, or run `wingetcreate submit <folder>` on Windows. Re-running the job
is not the fix, because goreleaser would try to upload release assets that exist.

## Validation

Three layers, cheapest first.

1. **`make release-check`** (`goreleaser check`, already in `pr.yml`) catches a
   malformed `winget` block on every PR.
2. **`main.yml`, job `winget-validate`** on `windows-latest`, after `snapshot`. The
   snapshot job uploads `dist/winget/**` as the artifact `winget-manifests`. This job
   runs `winget validate --manifest <folder>`. It checks schema and field rules only:
   the snapshot's installer URLs point at a release that does not exist.
3. **`release.yml`, job `winget-install`** on `windows-latest`, after `release`. The
   release job uploads `dist/winget/**`. This job enables local manifests, validates,
   installs from the folder, runs the installed `alchemist --version` and checks it
   prints the tag, then uninstalls. The URLs are real now, so this is the same check
   winget-pkgs runs, one step earlier. It runs for prereleases too, which makes an rc
   tag the full rehearsal.

```yaml
winget-install:
  needs: release
  runs-on: windows-latest
  permissions:
    contents: read
  steps:
    - uses: actions/download-artifact@v6
      with:
        name: winget-manifests
        path: winget
    - shell: pwsh
      run: |
        $manifest = (Get-ChildItem winget -Recurse -Filter *.installer.yaml).DirectoryName
        winget settings --enable LocalManifestFiles
        winget validate --manifest $manifest
        winget install --manifest $manifest --accept-source-agreements --accept-package-agreements --disable-interactivity
        $version = & "$env:LOCALAPPDATA\Microsoft\WinGet\Links\alchemist.exe" --version
        if ($version -notmatch [regex]::Escape("${{ github.ref_name }}")) { throw "installed $version, tagged ${{ github.ref_name }}" }
        winget uninstall --id ColbyTimm.Alchemist -e --disable-interactivity
```

The job calls the binary by its link path because a step does not see a PATH change
made during the job. The winget PR is already open when this job runs, so it reports a
problem rather than preventing one. Gating the PR on it would split goreleaser's
publish in two, which costs more than it saves.

These are the first Windows runners in CI. They run `winget` directly, not `make`:
the commands exist only on Windows and no developer runs them on Linux or macOS.

After the PR is open, winget-pkgs runs its own pipeline: manifest validation, URL and
hash checks, a Defender scan, and an install in a sandbox. The first version of a new
package also waits for a human moderator, which can take from hours to days. The PR
author must accept the Microsoft CLA once. Later versions are expected to go faster,
but that is Microsoft's process, not ours.

## SmartScreen and signing

The binaries stay unsigned. Signing is out of scope.

- winget downloads the zip itself and the user runs `alchemist` from a terminal. We
  expect no SmartScreen prompt on that path. The manual checklist confirms it.
- A zip downloaded from the Releases page in a browser carries the mark of the web.
  Double-clicking the extracted `alchemist.exe` can show "Windows protected your PC".
  winget avoids that, which is one more reason to lead with it.
- Defender can flag an unsigned Go binary as a false positive. winget-pkgs scans every
  installer, and a hit stops the PR. The remedy is a false-positive submission to
  Microsoft, then a re-run of the PR's checks.
- Signing later changes the binary, and so the zip and its hash. It must happen before
  archiving and checksums. The manifest shape does not change.

## Documentation

The docs change in the same PR as the release config. They describe `winget` as the
Windows install, and building from source as the option for everyone else until
iteration 24's other packages land.

| File | Change |
|---|---|
| `README.md` | Replace "Alchemist is in early development. There are no release builds yet." with one sentence saying releases are on the Releases page. In Quick start, add an `### Install` subsection first: a table of platform and command, with Windows as `winget install --id ColbyTimm.Alchemist -e` and a "From source" row holding the current `make build` steps. Below it, one line each for upgrade and uninstall, one sentence to open a new terminal after installing, one sentence to install either with winget or from the MSI and not both, and one sentence that uninstalling keeps profiles, keys, history and snapshots. Commands after the install use `alchemist`, with one sentence that a source build is `./bin/alchemist`. The emulator section says it needs a clone of the repository. Features does not change: it lists what the app does, not how it ships. If iteration 24 has already added an install section, add the Windows row to it instead |
| `docs/README.md` | The first line points to the install instructions: "Start with the [quick start](../README.md#quick-start), which covers installing." |
| `docs/reference/configuration.md` | Under Files, add: "On Windows, `~` is your user folder, `%USERPROFILE%`." and "Uninstalling Alchemist leaves these files and the keychain entries in place." |
| `docs/reference/cli.md` | No change: no flag or command is added |
| `docs/reference/keys.md`, `docs/reference/statements.md` | No change |
| `docs/using/*.md`, `docs/language/*.md`, `docs/data/*.md` | No change. `docs/data/profiles.md` already names Credential Manager as the Windows keychain |

No new page. The install text fits in the README.

Style for every change: short plain sentences, one idea per sentence, active voice,
tables for reference facts, sentence-case headings, no em dashes, no bold lead-ins, no
marketing words. Match the rewritten README and `docs/`.

`cspell.config.yaml` gains `winget` and any other new word `make spell` reports, under
the heading that fits it.

## Scope

- `.goreleaser.yml`: the `winget` block above; the future-work comment.
- `.github/workflows/release.yml`: `WINGET_GITHUB_TOKEN` on the `make release` step;
  an `actions/upload-artifact@v6` step for `dist/winget/**` named `winget-manifests`;
  the `winget-install` job.
- `.github/workflows/main.yml`: the same upload in `snapshot`; the `winget-validate`
  job.
- `Makefile`: the `release` help line reads `publish a GitHub Release for the current
  tag (needs GITHUB_TOKEN; WINGET_GITHUB_TOKEN also opens the winget PR)`.
- `cspell.config.yaml`: new words.
- `README.md`, `docs/README.md`, `docs/reference/configuration.md`: as above.
- `docs/plan/00-overview.md`: row 26 in the iterations table.
- Repository settings, by hand: the fork `colbytimm/winget-pkgs` and the secret.
- No Go file.

## Out of scope

- An MSI or MSIX manifest in winget. Revisit if the portable install proves wrong.
- Code signing and notarization, for Windows or any platform.
- Scoop, Chocolatey, and a `386` build.
- Windows unit test runs. Tests still run on Linux only.
- Publishing from forks of Alchemist under their own winget identifier.
- Automated follow-up of the winget-pkgs PR (labels, moderator comments). A person
  watches it.

## Relationship to other iterations

- **9, CI and release.** Extends its pipeline: same `make release`, same gates, one
  new secret, and the first Windows runners, which 9 left out of scope. A release is
  still reproducible from a tag. Moderation is the only step outside our control.
- **24, installable packages.** Soft dependency, planned in parallel. This iteration
  uses the Windows zip and its checksum, which exist today and which 24 keeps. The MSI
  stays a direct download. If 24 adds a second Windows zip, set `ids`. If 24 lands
  first, its README install section gains the Windows row.
- **6, config and profiles.** No change. The Windows paths it already uses are now
  documented.

## Steps

1. Fork `microsoft/winget-pkgs` to `colbytimm/winget-pkgs`. Create the classic token
   and store it as `WINGET_GITHUB_TOKEN`.
2. Add the `winget` block. `make release-check` passes. `make release-snapshot` writes
   three files to `dist/winget/manifests/c/ColbyTimm/Alchemist/<version>/`. Read the
   installer file: `x64` and `arm64`, `RelativeFilePath: alchemist.exe`,
   `PortableCommandAlias: alchemist`, URLs on `colbytimm/Alchemist`.
3. `main.yml`: upload the manifests, add `winget-validate`. `make actionlint` passes.
   Merge and watch the job on `main`. If `winget` is missing on the runner, install it
   in the job before validating (see Open questions).
4. `release.yml`: the token env, the upload, `winget-install`. `make actionlint`
   passes.
5. Update the docs in the same PR: `README.md`, `docs/README.md`,
   `docs/reference/configuration.md` as listed under Documentation, `cspell.config.yaml`,
   and row 26 in `docs/plan/00-overview.md`. `make spell` passes and every link
   resolves.
6. Push an rc tag. The GitHub prerelease appears, no winget PR opens, and
   `winget-install` passes on the real URLs.
7. Push the next stable tag. The PR opens on `microsoft/winget-pkgs`. Accept the CLA,
   answer moderators, and walk the manual checklist once it merges.

## Testing

**Unit:** none. No Go code changes. `make lint` and `make test` still pass.

**Static:**
- `make release-check` and `make actionlint` pass.
- `make release-snapshot` produces the three manifest files with the fields listed in
  step 2.
- Without `WINGET_GITHUB_TOKEN`, `goreleaser release --clean --skip=validate` in a
  scratch fork logs `winget.skip_upload is set` and exits 0.

**CI:**
- `winget-validate` passes on `main`.
- `winget-install` passes for the rc tag and the stable tag.

**Manual checklist** (Windows 11 x64, and Windows on arm64 if at hand):
- [ ] `winget search --id ColbyTimm.Alchemist` finds the package once the PR merges.
- [ ] `winget install --id ColbyTimm.Alchemist -e` installs with no prompt beyond
      the source agreement.
- [ ] In a new terminal, `alchemist --version` prints the tag.
- [ ] `alchemist --adapter mock` opens the app. `q` quits.
- [ ] No SmartScreen prompt on the first run from the terminal.
- [ ] `alchemist profile add` stores a key in Credential Manager, and
      `%USERPROFILE%\.config\alchemist\config.toml` holds the profile.
- [ ] After the next release, `winget upgrade --id ColbyTimm.Alchemist -e` moves to
      it, and only one copy remains under `%LOCALAPPDATA%\Microsoft\WinGet\Packages`.
- [ ] `winget uninstall --id ColbyTimm.Alchemist -e` removes the command. The config
      file and the Credential Manager entry remain.
- [ ] An rc tag opened no winget PR. A release with the secret removed stayed green.
- [ ] The README install steps work as written on a clean machine.

## Acceptance criteria

- `winget install --id ColbyTimm.Alchemist -e` installs the latest stable release on
  Windows x64 and arm64, and `alchemist` works from a new terminal.
- `winget upgrade` installs a newer release over the old one, and `winget uninstall`
  removes it, leaving user data in place.
- Every stable tag opens exactly one PR to `microsoft/winget-pkgs` for exactly one
  version, from `colbytimm/winget-pkgs`, with no manual step on our side.
- A prerelease tag, a snapshot, or a run without `WINGET_GITHUB_TOKEN` opens no PR and
  does not fail the job.
- Manifests are validated on Windows on every push to `main` and installed on Windows
  after every tag.
- Every tool and action in the new jobs is pinned, as in iteration 9.
- The docs describe the shipped behavior, every link in the changed docs resolves,
  and `make spell` passes.
- `make lint` and `make test` pass.

## Open questions

- **winget on `windows-latest`.** Whether the GitHub-hosted Windows image ships winget
  and lets `winget settings --enable LocalManifestFiles` run was not verified. If not,
  the job installs it first, for example with `Repair-WinGetPackageManager` from the
  `Microsoft.WinGet.Client` PowerShell module. Settle on the first run of step 3.
- **Portable install paths.** The link folder
  `%LOCALAPPDATA%\Microsoft\WinGet\Links` and the packages folder were not verified
  here. The `winget-install` job and the checklist confirm them.
- **Fine-grained token.** Whether a fine-grained token can open a PR on a repository
  of another owner. If it can, use it with access to the fork only.
- **Snapshot versions.** Whether `winget validate` accepts a `PackageVersion` like
  `0.2.1-snapshot`. If it does not, `winget-validate` rewrites that one field before
  validating, or the snapshot `version_template` changes.
- **`actions/download-artifact@v6`.** Assumed to exist at the same major as the
  `upload-artifact@v6` the repo uses. `actionlint` does not check that.
- **winget-pkgs process.** The CLA prompt, the moderation delay, and whether later
  versions from the same submitter merge without a moderator are from memory of the
  repository's practice, not checked today.
- **Commit author.** goreleaser's default author is its bot. Whether winget-pkgs or
  the CLA check wants the token owner as commit author was not verified. Set
  `commit_author` if the first PR is flagged.
