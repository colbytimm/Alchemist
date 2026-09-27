# Iteration 24 — Installable packages for macOS, Windows and Linux

<!-- cspell:words nfpm nfpms wixl msitools osslsigncode notarize notarized notarization notarytool quill syft sbom sboms SBOMs spctl lipo msiexec wxs APPDATA noreply keepassxc kwallet pkgbuild upgradecode Authenticode winget danieljoos wincred godbus checksummed Timestamping bindir GOBIN CURDIR anchore archs MSIX Linuxbrew codesign jsign -->

## Goal

Every release ships something a user can install with the tool their platform already
has, and a checksum and a provenance statement for each file. Iteration 9's pipeline
publishes six bare archives. This iteration keeps that pipeline and that one publisher
(goreleaser, run from `make release` on one Linux job), and adds to it:

| Platform | Artifacts |
|---|---|
| macOS | one universal binary (`amd64` + `arm64`) in a `.tar.gz`, signed with a Developer ID and notarized when the secrets exist |
| Windows | `.zip` for `amd64` and `arm64`; an `.msi` for `amd64`; the `.exe` and `.msi` Authenticode-signed when the secrets exist |
| Linux | `.tar.gz`, `.deb`, `.rpm` and `.apk` for `amd64` and `arm64` |
| All | `checksums.txt` over every file, an SPDX SBOM per archive, and a GitHub build provenance attestation per file |

The artifact names and URLs below are a contract. Iteration 25 (Homebrew) and
iteration 26 (winget) consume them without reading this pipeline.

Nothing in the Go code changes. The keychain behavior users meet after installing
(Secret Service on Linux) is already handled by `internal/config` and `cmd/profile.go`;
this iteration documents it and declares it in the Linux packages.

## Current state

Verified against the tree at `dfab0a0`:

- `.goreleaser.yml` has one build (`linux`, `darwin`, `windows` x `amd64`, `arm64`,
  `CGO_ENABLED=0`, ldflags setting `app.Version` and `app.BuildDate`), one archive
  named `{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}` (`.zip` on Windows,
  `.tar.gz` elsewhere) holding the binary, `LICENSE` and `README.md`, and
  `checksums.txt`. A comment lists package managers, signing, notarization and SBOMs
  as deliberately absent.
- goreleaser is pinned at v2.17.1 and run with `go run` from the `Makefile`
  (`release-check`, `release-snapshot`, `release`). This is goreleaser OSS, not Pro.
- `release.yml` runs `gates`, then `make release` on `ubuntu-latest` with
  `contents: write` and `GITHUB_TOKEN`. `main.yml` runs `make release-snapshot` and
  uploads `dist/*.tar.gz`, `dist/*.zip` and `dist/checksums.txt`. `pr.yml` runs
  `make release-check` and `make actionlint`.
- Releases `v0.1.0` and `v0.2.0` exist on GitHub, yet the README still says "There
  are no release builds yet" and its quick start builds from source.
- The keychain is `github.com/zalando/go-keyring` v0.2.8 behind
  `config.SystemKeyring`. On macOS it runs `/usr/bin/security`; on Windows it calls
  Credential Manager (`danieljoos/wincred`); on Linux it speaks the Secret Service API
  over the D-Bus session bus (`godbus/dbus`). All three are pure Go, so
  `CGO_ENABLED=0` binaries keep working and packages need no shared libraries.
- When the keychain cannot be reached, `SecretResolver.Resolve` falls through to
  `ALCHEMIST_<NAME>_KEY`, and `profile add` saves the profile and says to set that
  variable. A Linux machine with no Secret Service already works with the variable.

What goreleaser v2.17.1 OSS offers, read from its source in the module cache:

| Need | goreleaser OSS | Used here |
|---|---|---|
| macOS universal binary | `universal_binaries` (the artifact's `Goarch` is `all`) | yes |
| macOS sign and notarize | `notarize.macos`, through quill, on Linux; skipped only by `enabled`, not by `--snapshot` | yes |
| macOS `.pkg` or `.dmg` | none (Pro only) | no, see Out of scope |
| `.msi` | none (Pro only) | built by a post-build hook with `wixl` |
| `.deb`, `.rpm`, `.apk` | `nfpms` | yes |
| SBOM | `sboms`, runs `syft` | yes |
| Extra files in the release and in `checksums.txt` | `release.extra_files`, `checksum.extra_files` | yes, for the `.msi` |
| winget manifests | `winget` pipe, reads `.zip` archives as portable installers | iteration 26 |
| Homebrew | `homebrew_casks` (cask pipe) and `brews` | iteration 25 |

Pipe order matters and was checked in `internal/pipeline/pipeline.go`: build (with its
post hooks), universal binary, binary signing, notarize, then archive, nfpm, sbom,
checksums, release. So a Windows `.exe` signed in its post hook and a notarized macOS
binary are what land in the archives and packages.

## Released artifacts

Base URL: `https://github.com/colbytimm/Alchemist/releases/download/v<version>/`.
`<version>` is the tag without its `v` (`0.3.0`).

| File | Contents |
|---|---|
| `alchemist_<version>_darwin_universal.tar.gz` | `alchemist`, `LICENSE`, `README.md` |
| `alchemist_<version>_linux_amd64.tar.gz` | same |
| `alchemist_<version>_linux_arm64.tar.gz` | same |
| `alchemist_<version>_windows_amd64.zip` | `alchemist.exe`, `LICENSE`, `README.md` |
| `alchemist_<version>_windows_arm64.zip` | same |
| `alchemist_<version>_windows_amd64.msi` | installs `alchemist.exe` and adds it to `PATH` |
| `alchemist_<version>_linux_amd64.deb`, `..._arm64.deb` | `/usr/bin/alchemist` |
| `alchemist_<version>_linux_amd64.rpm`, `..._arm64.rpm` | same |
| `alchemist_<version>_linux_amd64.apk`, `..._arm64.apk` | same |
| `<archive>.sbom.json` | SPDX JSON for each archive above |
| `checksums.txt` | SHA-256 of every file above, `sha256sum` format |

Rules that 25 and 26 rely on:

- Every archive has the binary at its root, not in a folder.
- There is exactly one archive per OS and architecture, so goreleaser's `winget` pipe
  (which refuses several archives per platform) and its cask pipe pick them without
  filters. The `.msi` is an extra file, not an archive, and does not count.
- Tags with a prerelease part (`v0.3.0-rc.1`) publish as GitHub prereleases
  (`release.prerelease: auto`), so `releases/latest` always names a final release.
- The names change once, in this iteration: the two darwin archives become one
  `darwin_universal`. `v0.1.0` and `v0.2.0` keep their old names.

## macOS

One universal binary, because Homebrew and a manual download then need one URL and
nobody has to know their architecture. `universal_binaries` with `replace: true`
drops the two single-architecture darwin binaries, and the archive name template maps
`Goarch` `all` to `universal`:

```yaml
name_template: >-
  {{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ if eq .Arch "all" }}universal{{ else }}{{ .Arch }}{{ end }}
```

Signing and notarization use goreleaser's `notarize.macos`, which runs quill on the
Linux release runner, so no macOS runner is added to the release path:

```yaml
notarize:
  macos:
    - enabled: '{{ and (not .IsSnapshot) (isEnvSet "MACOS_SIGN_P12") }}'
      sign:
        certificate: "{{ .Env.MACOS_SIGN_P12 }}"
        password: "{{ .Env.MACOS_SIGN_PASSWORD }}"
      notarize:
        issuer_id: "{{ .Env.MACOS_NOTARY_ISSUER_ID }}"
        key_id: "{{ .Env.MACOS_NOTARY_KEY_ID }}"
        key: "{{ .Env.MACOS_NOTARY_KEY }}"
        wait: true
        timeout: 20m
```

- `not .IsSnapshot` is required. `--snapshot` does not skip the notarize pipe, and a
  snapshot must never spend a notarization or depend on secrets.
- `wait: true` makes an `Invalid` or `Rejected` verdict fail the release. A timeout
  only logs in v2.17.1, so the manual checklist checks the verdict with `spctl`.
- A bare Mach-O binary cannot have a ticket stapled. Gatekeeper checks the ticket
  online on first run. That is enough for a Homebrew cask and for a browser download.
- No entitlements file. The binary does not JIT or load plugins, and quill signs with
  the hardened runtime that notarization requires.

The `go-keyring` backend on macOS runs `/usr/bin/security`, so the login keychain
prompts once per profile and needs nothing from the package.

## Windows

The `.zip` stays the primary Windows artifact: winget (iteration 26) installs it as a
portable package. The `.msi` serves people who install by double-click and
administrators who deploy MSIs.

goreleaser OSS cannot build an MSI. The build is split into two ids so Windows gets a
post-build hook:

```yaml
builds:
  - id: alchemist
    goos: [linux, darwin]
    # env, goarch, ldflags, mod_timestamp as today
  - id: alchemist-windows
    goos: [windows]
    # same env, goarch, ldflags, mod_timestamp
    hooks:
      post:
        - cmd: packaging/windows/package.sh "{{ .Path }}" "{{ .Arch }}" "{{ .Version }}" "{{ .Major }}.{{ .Minor }}.{{ .Patch }}" "{{ .IsSnapshot }}"
          output: true
```

The shared fields live once in a YAML anchor. `packaging/windows/package.sh` does
three things, each a small function:

1. `sign_file` on the `.exe`, in place, when signing is enabled (below). The archive
   pipe runs later, so the `.zip` carries the signed `.exe`.
2. For `amd64` only, `build_msi`: `wixl` (from `msitools`) builds
   `dist/alchemist_<version>_windows_amd64.msi` from `packaging/windows/alchemist.wxs`.
3. `sign_file` on the `.msi`.

`release.extra_files` and `checksum.extra_files` both take the glob `dist/*.msi`,
which goreleaser resolves after the build, so the MSI is published and checksummed
like any archive.

`alchemist.wxs`:

- A fixed `UpgradeCode` GUID, generated once and never changed, and a `MajorUpgrade`
  element, so installing a newer MSI replaces the older one.
- `Version` is `Major.Minor.Patch`. MSI versions are numeric, so `0.3.0-rc.1` and
  `0.3.0` share a version; the upgrade allows same-version upgrades so the final
  replaces the rc.
- Per-machine install to `C:\Program Files\Alchemist\alchemist.exe`, and the install
  folder appended to the system `PATH`, removed again on uninstall.
- No shortcuts, no services, no custom actions. It is a terminal program.

Why `wixl` in the Linux job and not WiX on a Windows runner: it keeps one job, one
publisher and one `checksums.txt`. A Windows job would have to upload into a release
goreleaser already published and rewrite its checksums. `wixl` supports a subset of
WiX 3, and `arm64` MSIs are not among what it is known to build, hence `amd64` only.
Windows on Arm runs the `amd64` MSI under emulation, or uses the `arm64` zip.

Authenticode signing uses `osslsigncode` with a PFX, because it signs both PE files
and MSIs on Linux and installs from Ubuntu's archive. Timestamping uses
`http://timestamp.digicert.com` so signatures outlive the certificate.

On Windows the keychain is Credential Manager, which every desktop and server edition
has. Nothing to declare.

## Linux

`nfpms` builds `.deb`, `.rpm` and `.apk` from the `alchemist` build's Linux binaries:

```yaml
nfpms:
  - id: packages
    ids: [alchemist]
    package_name: alchemist
    file_name_template: "{{ .PackageName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats: [deb, rpm, apk]
    vendor: Colby Timm
    maintainer: Colby Timm <colbytimm@users.noreply.github.com>
    homepage: https://github.com/colbytimm/Alchemist
    description: A keyboard-driven terminal IDE for Azure Cosmos DB.
    license: MIT
    section: utils
    bindir: /usr/bin
    suggests: [gnome-keyring]
    contents:
      - src: LICENSE
        dst: /usr/share/doc/alchemist/copyright
        packager: deb
      - src: LICENSE
        dst: /usr/share/licenses/alchemist/LICENSE
        packager: rpm
      - src: LICENSE
        dst: /usr/share/licenses/alchemist/LICENSE
        packager: apk
      - src: README.md
        dst: /usr/share/doc/alchemist/README.md
```

goreleaser appends the extension, so the names match the table above and keep the Go
architecture names (`amd64`, not `x86_64`) across all three formats, which is what 25
and 26 find easiest to template.

The Secret Service dependency is a suggestion, not a requirement or a
recommendation. `apt` installs recommendations by default, and a desktop keyring
daemon is wrong on the servers and containers where `ALCHEMIST_<NAME>_KEY` is the
answer. Any Secret Service provider works: GNOME Keyring, KeePassXC, or KWallet 5.97
and later. It also needs a D-Bus session bus, which an SSH session often lacks; the
docs say so. `apk` has no suggestions field, so Alpine relies on the docs.

The packages are not GPG-signed. A signature is only checked against a repository key
the user has trusted, and there is no repository yet. `checksums.txt` and the
attestation cover integrity. `apk add` needs `--allow-untrusted` for an unsigned local
file; the docs say so.

## Checksums, SBOMs and provenance

- `checksums.txt` stays SHA-256. goreleaser already covers every uploadable artifact,
  which now includes packages and SBOMs; `checksum.extra_files` adds the MSI.
- `sboms` with the default `artifacts: archive` writes one SPDX JSON per archive.
  `syft` is pinned in the `Makefile` as `SYFT_VERSION` and installed with
  `GOBIN=$(CURDIR)/bin/tools go install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)`
  by a `syft` target that `release` and `release-snapshot` depend on, and both run
  goreleaser with `bin/tools` first on `PATH`. This keeps the repo's rule: every tool
  pinned in the `Makefile`, nothing from `@latest`.
- Provenance: after `make release`, `release.yml` runs
  `actions/attest-build-provenance` over `dist/*.tar.gz`, `dist/*.zip`, `dist/*.msi`,
  `dist/*.deb`, `dist/*.rpm`, `dist/*.apk` and `dist/checksums.txt`. The job gains
  `id-token: write` and `attestations: write`. Users verify with
  `gh attestation verify <file> --repo colbytimm/Alchemist`. cosign is not added;
  the attestation already gives a keyless, verifiable signature tied to the workflow.
- Builds get `mod_timestamp: "{{ .CommitTimestamp }}"` so the same tag yields the same
  binary bytes (signing aside).

## Secrets and forks

| Secret | Holds | Used by |
|---|---|---|
| `MACOS_SIGN_P12` | base64 of the Developer ID Application certificate and key (`.p12`) | notarize.sign |
| `MACOS_SIGN_PASSWORD` | the `.p12` password | notarize.sign |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect API issuer ID | notarize |
| `MACOS_NOTARY_KEY_ID` | App Store Connect API key ID | notarize |
| `MACOS_NOTARY_KEY` | base64 of the API key (`.p8`) | notarize |
| `WINDOWS_SIGN_PFX` | base64 of the code signing certificate and key (`.pfx`) | `package.sh` |
| `WINDOWS_SIGN_PASSWORD` | the `.pfx` password | `package.sh` |

Only the `release` job in `release.yml` receives them, as step `env`. `pr.yml`,
`main.yml` and `packages.yml` never do.

When they are absent:

| Situation | Result |
|---|---|
| Snapshot (`make release-snapshot`, PR, main) | always unsigned, even with the secrets set: every signing switch includes `not .IsSnapshot` |
| Tag in a fork, or upstream before certificates exist | release succeeds, unsigned; macOS warns on a browser download, Windows SmartScreen warns |
| Tag upstream with repository variable `REQUIRE_SIGNED_RELEASES` set to `true` | a first step in the `release` job fails the run unless all seven secrets are set, before anything is built |

The guard is opt-in by variable, not keyed on the repository name, so the upstream
can release before it buys certificates and a fork never trips it. `package.sh`
signs when `WINDOWS_SIGN_PFX` is set and the snapshot flag is `false`; it decodes the
PFX into a `mktemp` file and removes it on exit with a `trap`.

`package.sh` needs `wixl` for a release and fails without it. In a snapshot it warns
and skips the MSI, like `make spell` without `npx`, so `make release-snapshot` still
works on a laptop without `msitools`.

## CI

A new reusable workflow, `.github/workflows/packages.yml`, builds a snapshot and
installs each artifact on the platform it is for:

| Job | Runner | Checks |
|---|---|---|
| `build` | `ubuntu-latest` | `apt-get install msitools`, `make release-snapshot`, `sha256sum -c dist/checksums.txt`, upload `dist/` artifacts as `packages` |
| `linux` (matrix) | `ubuntu-latest` in containers `debian`, `fedora`, `alpine`; `ubuntu-24.04-arm` for the `arm64` `.deb` | install the package with `apt-get install ./…`, `dnf install ./…`, `apk add --allow-untrusted ./…`; `alchemist --version` prints the snapshot version; remove the package and check `/usr/bin/alchemist` is gone |
| `macos` | `macos-latest` | extract the universal archive; `lipo -archs` prints `x86_64 arm64`; `alchemist --version` |
| `windows` | `windows-latest` | `msiexec /i … /qn`, then `alchemist --version` from a fresh shell whose `PATH` is read from the registry; `msiexec /x … /qn` removes the file and the `PATH` entry; the `arm64` zip is unpacked but not run |

Container images and runner labels are pinned to a major tag (`debian:12`-style), as
the repo pins actions by major.

- `pr.yml` calls it as job `packages`, in parallel with `gates`, so the PR time budget
  of iteration 9 grows only by the slowest of these jobs.
- `main.yml` replaces its `snapshot` job with it; the `packages` artifact replaces
  `snapshot-archives`.
- `release.yml` runs `release` after both `gates` and `packages`, so a tag never
  publishes what did not install. The `release` job installs `msitools` and
  `osslsigncode` with `apt-get` before `make release`.

## Scope

- `.goreleaser.yml`: builds split into `alchemist` and `alchemist-windows` with a
  shared anchor and `mod_timestamp`; `universal_binaries`; archive name template;
  `notarize`; `nfpms`; `sboms`; `checksum.extra_files`; `release.extra_files`,
  `release.prerelease: auto`, and a `release.footer` that links `docs/install.md`. The
  "future work" comment shrinks to container images.
- `packaging/windows/alchemist.wxs`, `packaging/windows/package.sh` (new).
- `Makefile`: `SYFT_VERSION`, a `syft` target, `release` and `release-snapshot` depend
  on it and put `bin/tools` on `PATH`; their `##` help lines name `wixl` and
  `osslsigncode`. `clean` already removes `bin/`.
- `.github/workflows/packages.yml` (new); `pr.yml`, `main.yml`, `release.yml` as in CI.
- `cspell.config.yaml`: the new words (`nfpm`, `wixl`, `msitools`, `osslsigncode`,
  `notarize`, `syft`, and the others this file lists at its top).
- `docs/plan/00-overview.md`: row 24; `packaging/` in the architecture tree.
- `docs/plan/09-ci-release.md`: its Out of scope line on signing and SBOMs points here.
- The user docs listed under Documentation.
- No `.go` file changes. `cmd/`, `app/`, `internal/` and `go.mod` are untouched.

## Out of scope

- A macOS `.pkg` or `.dmg`. goreleaser OSS builds neither; `pkgbuild` needs a macOS
  runner and a second certificate (Developer ID Installer). The Homebrew cask in 25
  gives macOS a one-command install.
- An `arm64` MSI, until `wixl` is shown to build one.
- MSIX. nfpm in v2.17.1 marks it experimental, and an unsigned MSIX cannot be
  installed at all.
- Package repositories (apt, yum, apk) and package signing, which only matter with a
  repository.
- Chocolatey, Scoop, Snap, Flatpak, AUR, Nix, container images.
- Azure Trusted Signing for Windows (see Open questions).
- Shell completions and man pages. The root command disables cobra's completion
  command; turning it on is a CLI change of its own.
- Self-update.

## Relationship to other iterations

- **6, config and profiles.** The keychain backends it chose are why packages need
  no library dependencies, and why Linux suggests a Secret Service provider. Its
  environment-variable fallback is the documented answer for headless Linux.
- **9, CI and release.** Extended, not replaced: same pinned goreleaser, same `make`
  targets, same single publishing job. The `snapshot` job becomes `packages.yml`.
- **25, Homebrew.** Consumes `alchemist_<version>_darwin_universal.tar.gz` (and the
  Linux archives, if it covers Linuxbrew) from the Released artifacts table. A cask
  keeps macOS quarantine on the file, so it needs the binary signed and notarized, or
  a quarantine-removing step that 25 must justify.
- **26, winget.** Consumes the two Windows zips through goreleaser's `winget` pipe,
  which writes them as `zip` installers with a nested `portable` binary. It may also
  list the `amd64` MSI as a second installer type. The MSI's `UpgradeCode` in
  `alchemist.wxs` is the product code winget tracks; it never changes.
- All other iterations: none.

## Steps

1. goreleaser, Linux and macOS: universal binary, archive names, `nfpms`,
   `prerelease: auto`, `mod_timestamp`. `make release-check` and
   `make release-snapshot` green; the `dist/` file names match the Released artifacts
   table exactly.
2. Windows: split builds, `package.sh` (MSI only, no signing yet), `alchemist.wxs`,
   extra files. The snapshot contains the MSI and `checksums.txt` lists it.
3. SBOMs: `SYFT_VERSION`, the `syft` target, `sboms`. One `.sbom.json` per archive,
   listed in `checksums.txt`.
4. `packages.yml`, wired into `pr.yml` and `main.yml`. Every install job green on a
   draft PR. `make actionlint` clean.
5. Signing: `notarize` block, `sign_file` in `package.sh`, the
   `REQUIRE_SIGNED_RELEASES` guard, secrets passed only to the `release` job. Tag
   `v0.3.0-rc.1` on a fork: an unsigned prerelease with every artifact.
6. Provenance: `attest-build-provenance` and the job permissions in `release.yml`.
7. Documentation, in the same change as the steps above: every file under
   Documentation, `cspell.config.yaml`, and the two plan documents. `make spell` passes
   and every relative link resolves.
8. Upstream: add the secrets that exist, tag `v0.3.0-rc.1`, walk the manual
   checklist, then tag `v0.3.0` and mark row 24 Done in `00-overview.md`.

## Documentation

The README and `docs/` were just rewritten in one style. Match it: short plain
sentences, one idea per sentence, active voice, tables for reference facts,
sentence-case headings, no em dashes, no bold lead-ins, no marketing words.

| File | Change |
|---|---|
| `README.md` | Delete "There are no release builds yet." Keep the `## Quick start` heading, whose anchor `docs/README.md` links, and open it with `### Install`: one table, one row per platform, with the one command or file to use (`tar` for macOS, the `.deb`/`.rpm`/`.apk` for Linux, the `.msi` or `.zip` for Windows) and a link to `docs/install.md` for the rest. Every `./bin/alchemist` becomes `alchemist`. The emulator section says it needs a clone of the repository, since `make emulator-up` does. Features gains no line: installing is not a feature. |
| `docs/install.md` (new) | Too long for the README, so its own page. Sections: `## macOS` (download, extract, move to a `PATH` directory, the signed and notarized status, what the Gatekeeper prompt means if a release is unsigned); `## Linux` (a table of distro to command, `--allow-untrusted` for `apk`, the Secret Service note with the three providers and the SSH session bus caveat, pointing at `ALCHEMIST_<NAME>_KEY`); `## Windows` (MSI and zip, where the MSI installs, the SmartScreen warning when unsigned, `arm64` uses the zip); `## Verify a download` (`sha256sum -c checksums.txt --ignore-missing`, `gh attestation verify`, the SBOM files); `## Uninstall` (per platform, and that `~/.config/alchemist` and the keychain entries stay); `## Build from source` (the current README text, Go 1.26 or newer, `make build`). |
| `docs/README.md` | First line links both the quick start and `install.md`. |
| `docs/data/profiles.md` | Under `## Keys`, after "On machines without a keychain…": one sentence that on Linux the keychain is any Secret Service provider on a D-Bus session bus, linking `../install.md#linux`. |
| `docs/reference/configuration.md` | Under `## Files`: one sentence that on Windows `~` is `%USERPROFILE%` (the code uses the home directory on every OS, not `%APPDATA%`). |
| `docs/reference/cli.md` | Unchanged. No flag or command is added; `--version` is already listed. |
| `docs/reference/keys.md` | Unchanged. No key binding is added. |
| `docs/using/*.md`, `docs/language/*.md`, `docs/data/catalog.md`, `docs/data/cloning.md`, `docs/data/snapshots.md` | Unchanged. None mentions `./bin/alchemist` or building (checked with `grep`). The implementer greps again before finishing. |

## Testing

**Unit:** none new. No Go code changes. `make lint` and `make test` still pass,
because iteration 9's definition of done requires them for any change.

**Static:**

- `make release-check` accepts the new `.goreleaser.yml`.
- `make actionlint` is clean on the four existing workflows and `packages.yml`.
- `shellcheck packaging/windows/package.sh` is clean (actionlint already runs
  shellcheck on `run:` blocks; the script is checked by hand once).

**Integration (CI, `packages.yml`):** the install matrix in the CI section. It is the
test suite for this iteration and runs on every PR.

**Manual checklist** (on `v0.3.0-rc.1`, upstream, with secrets):

- [ ] The release lists every file in the Released artifacts table and nothing else
      besides goreleaser's source archive, if enabled.
- [ ] `sha256sum -c checksums.txt` passes for every downloaded file.
- [ ] `gh attestation verify` passes for the universal archive, the MSI and one `.deb`.
- [ ] macOS, downloaded with a browser (so it is quarantined): the binary runs with
      no Gatekeeper prompt; `codesign -dv --verbose=4` names the Developer ID;
      `spctl -a -vvv -t install alchemist` says `Notarized Developer ID`.
- [ ] macOS: the first `alchemist profile add` prompts for the login keychain once
      and then connects.
- [ ] Windows: the MSI's Properties show a valid signature; install shows no
      "unknown publisher"; `alchemist --version` works in a new terminal; installing
      `v0.3.0` later upgrades in place; uninstall removes the file and the `PATH`
      entry.
- [ ] Windows: `profile add` stores the key in Credential Manager.
- [ ] Ubuntu desktop: install the `.deb`; `profile add` stores the key in GNOME
      Keyring.
- [ ] Ubuntu over SSH with no session bus: `profile add` saves the profile and says
      to set `ALCHEMIST_<NAME>_KEY`; with it set, `alchemist <name>` connects.
- [ ] Fedora `.rpm` and Alpine `.apk` install and run `--version`.
- [ ] A fork's rc tag: unsigned prerelease, every artifact present.
- [ ] With `REQUIRE_SIGNED_RELEASES=true` and one secret removed, the release job
      fails before building.
- [ ] Every command in `docs/install.md` run as written, on its platform.

## Acceptance criteria

- One tag push produces every artifact in the Released artifacts table, with those
  exact names, plus SBOMs, `checksums.txt` and an attestation per file, with no manual
  step.
- The macOS binary is universal. With the secrets set it is signed and notarized; the
  Windows `.exe` inside the zip and the MSI is signed, and so is the MSI.
- Without the secrets, forks and snapshots build every artifact unsigned and
  succeed. Snapshots are unsigned even when the secrets exist.
- With `REQUIRE_SIGNED_RELEASES` set, a release with any secret missing fails before
  it publishes.
- Every package installs, runs `--version` with the tag's version, and uninstalls
  cleanly, in CI, on every PR.
- Linux packages declare no hard dependency and suggest a Secret Service provider;
  the docs say what to do without one.
- Every tool is pinned: goreleaser and syft in the `Makefile`, actions and container
  images by major. `msitools` and `osslsigncode` come from the runner's Ubuntu archive.
- The docs describe the shipped behavior, the README no longer builds from source as
  its only install path, every doc link resolves, and `make spell` passes.
- `make lint` and `make test` pass. No Go file changed.

## Open questions

- `wixl` support for `Environment` (the `PATH` entry) and for same-version major
  upgrades was not verified here; the network was not used. If either fails, the
  fallback is a `windows-latest` job running WiX v5 that uploads the MSI to a draft
  release, which costs the single-publisher design; decide in step 2.
- goreleaser's docs describe `certificate` and `key` in `notarize` as a path or base64
  contents. quill was not in the module cache to confirm it; step 5 confirms with a
  throwaway certificate.
- `syft` and `actions/attest-build-provenance` versions: pick the current release of
  each in step 3 and step 6; neither could be looked up here.
- The package maintainer address: `colbytimm@users.noreply.github.com` is a
  placeholder until the owner chooses one.
- Azure Trusted Signing is cheaper than a PFX certificate but is not a PFX and needs
  a different signer (`jsign` or Microsoft's action). Revisit if a PFX is not bought.
