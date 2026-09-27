<!-- cspell:words livecheck postflight xattr untap linuxbrew shellenv notarization notarize notarized -->

# Iteration 25 — Homebrew

## Goal

`brew install colbytimm/tap/alchemist` installs Alchemist on macOS and on Linux, and
`brew upgrade` picks up each new release without anyone editing the tap by hand. Every
release tag publishes the tap entry, and CI proves the entry installs a binary that
runs and reports the tagged version.

Iteration 9 ships the GitHub Release: six archives (`linux`, `darwin`, `windows` ×
`amd64`, `arm64`) and `checksums.txt`. Iteration 24 turns those into installable
packages per platform. This iteration adds Homebrew on top of the same archives. It
builds nothing new.

## Prerequisite: a public repository

`colbytimm/Alchemist` is private today (the GitHub API reports `private: true`, 0
stars, 0 forks). Homebrew downloads a cask's `url` with no credentials, so a tap cannot
install from a private repository's release assets. The repository must be public
before step 3 publishes anything. That is the owner's decision, and this plan does not
make it. Until then, steps 1 and 2 still land: the cask is generated and tested in CI
from snapshot archives, which needs no download.

## Decision: a tap now, homebrew-core later

| Option | Verdict |
|---|---|
| Own tap `colbytimm/homebrew-tap`, cask written by goreleaser | Chosen, now |
| Own tap, formula written by goreleaser (`brews`) | Rejected: deprecated |
| `homebrew/core` formula built from source | Later, once notable (below) |
| `homebrew/cask` | Rejected: needs a notarized macOS binary |

- **A tap, because it is the only option open today.** goreleaser writes the tap entry
  on every tag, and the tap needs no review. `homebrew/core` would refuse Alchemist now
  on notability alone.
- **A cask, not a formula.** In goreleaser v2.17.1 (the version the `Makefile` pins),
  `brews` is marked `deprecated=true` in the config schema and its pipe prints a
  deprecation notice "in favor of" the cask pipe (`internal/pipe/brew/brew.go`).
  `homebrew_casks` is the supported path.
- **A cask works on Linux too.** The Cask Cookbook (Homebrew/brew `docs/Cask-Cookbook.md`)
  says `binary` works on either operating system and cross-platform casks put the
  per-OS parts in `on_macos` and `on_linux`. goreleaser's cask template writes exactly
  that: one `on_macos` and one `on_linux` block, each with `on_intel` and `on_arm`,
  `url` and `sha256`.
- **The tap is named `homebrew-tap`**, so users type `colbytimm/tap/alchemist`. One tap
  can later carry other tools without a rename.
- **The docs always use the tap-qualified name.** `brew install alchemist` alone would
  reach `homebrew/core` or `homebrew/cask` first. Neither has an `alchemist` today
  (`Formula/a/alchemist.rb` and `Casks/a/alchemist.rb` both return 404 on
  `raw.githubusercontent.com`), but the qualified name cannot be shadowed later.

### The macOS quarantine

Homebrew applies the quarantine attribute to cask downloads so that Gatekeeper checks
them (Homebrew/brew `docs/Homebrew-Security-and-Supply-Chain.md`). The darwin binaries
are not signed or notarized (iteration 9 lists signing as future work), so Gatekeeper
would refuse to run them on first launch.

The cask removes the attribute in a `postflight` hook, only on macOS. This overrides a
safety, and the plan says so in `.goreleaser.yml`. What stays: the cask pins each
archive's SHA-256, taken from the same build that wrote `checksums.txt`, and the tap
only changes through the release job. If iteration 24 signs and notarizes the darwin
binaries, the hook goes and the quarantine stays.

### homebrew-core, later

`homebrew/core` accepts a formula that builds from source, from an immutable tagged
archive, under a license compatible with the Debian Free Software Guidelines (Homebrew/brew `docs/Acceptable-Formulae.md`).
Alchemist is MIT and builds with `go build`, so it qualifies on those terms. It fails on
notability (Homebrew/brew `docs/Package-Acceptance-Policy.md`):

| Rule | Threshold | Alchemist now |
|---|---|---|
| Submitted by someone else | 30 forks, 30 watchers or 75 stars | 0 stars, 0 forks, private |
| Submitted by the owner | 90 forks, 90 watchers or 225 stars | as above |
| Repository age | at least 30 days | created 2024-01-30 |

When a threshold is met, a separate iteration writes the formula
(`depends_on "go" => :build`, the same ldflags as `.goreleaser.yml`, a `test do` block
that runs `alchemist --version`) and opens the pull request against `homebrew/core`.
That iteration also retires the tap cask. The retirement mechanism is an open question
below. Nothing in this iteration blocks it.

## What happens on each tag

| Tag | GitHub Release | Tap updated | Post-release check |
|---|---|---|---|
| `v1.2.3`, token set | yes | yes, one commit to `Casks/alchemist.rb` | installs from the tap on macOS and Linux |
| `v1.2.3-rc.1`, token set | yes | no (`skip_upload: auto` skips prereleases) | skipped |
| any tag, no token (a fork) | yes | no, skipped with a log line | skipped |
| push to `main` (snapshot) | no | no (goreleaser skips every publisher) | installs the snapshot cask from local archives |

The skip is decided by one template in `.goreleaser.yml`:

```yaml
skip_upload: '{{ if isEnvSet "HOMEBREW_TAP_TOKEN" }}auto{{ else }}true{{ end }}'
```

`isEnvSet` is true only for a set, non-empty variable (`internal/tmpl/tmpl.go`), and a
fork's missing secret arrives as an empty string. `skip_upload` is templated before the
publish step reads it, and the token is read only after the skip check, so a missing
token never reaches the template engine as `{{ .Env.HOMEBREW_TAP_TOKEN }}`.

## The token

The release job's `GITHUB_TOKEN` can write only to `colbytimm/Alchemist`. The tap is
another repository, so it needs its own token.

| | |
|---|---|
| Secret | `HOMEBREW_TAP_TOKEN`, a repository secret on `colbytimm/Alchemist` |
| Kind | fine-grained personal access token |
| Repository access | `colbytimm/homebrew-tap` only |
| Permissions | Contents: read and write (Metadata: read is implied) |
| Expiry | one year; the owner rotates it and a calendar reminder is theirs to set |
| Used by | the `release` job only, as an environment variable of `make release` |

- **A personal access token, not a GitHub App.** An App needs its own registration, a
  private key secret and a token-minting step, for one repository with one writer. The
  token is the smaller moving part.
- **An expired or wrong token fails loudly.** The cask pipe is continue-on-error in
  goreleaser, so the GitHub Release is still published. goreleaser then exits non-zero
  and the `release` job goes red. The generated cask is uploaded as a workflow
  artifact on every run (`if: always()`), so the owner can commit it to the tap by hand
  after rotating the token.

## The generated cask

goreleaser writes `dist/homebrew/Casks/alchemist.rb` from this entry. The shape below is
from goreleaser v2.17.1's `internal/pipe/cask/templates/cask.rb`:

```ruby
cask "alchemist" do
  version "1.2.3"
  on_macos do
    on_intel do
      sha256 "…"
      url "https://github.com/colbytimm/Alchemist/releases/download/v1.2.3/alchemist_1.2.3_darwin_amd64.tar.gz"
    end
    on_arm do … end
  end
  on_linux do
    on_intel do … end
    on_arm do … end
  end
  name "alchemist"
  desc "Keyboard-driven terminal IDE for Azure Cosmos DB"
  homepage "https://github.com/colbytimm/Alchemist"
  livecheck do
    skip "Auto-generated on release."
  end
  binary "alchemist"
  postflight do
    if OS.mac?
      system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/alchemist"]
    end
  end
end
```

- **No `zap` stanza.** `~/.config/alchemist` holds saved queries and
  `~/.local/share/alchemist` holds snapshots. Both are the user's work, and `brew
  uninstall --zap` is too easy to type to delete them. The docs list the directories
  instead.
- **No shell completions.** `cmd/root.go` sets `CompletionOptions.DisableDefaultCmd`, so
  there is no `alchemist completion` to generate them from.
- **No `license`.** goreleaser v2.17.1's config source marks the cask `license` field as
  having no effect.
- **Livecheck is skipped on purpose.** The template hard-codes the skip, because the
  release job itself updates the tap. There is nothing for livecheck to find, and CI
  does not run it.
- **The archives stay as iteration 9 names them.** The binary sits at the root of each
  archive, so `binary "alchemist"` needs no path. If iteration 24 adds a second archive
  per platform, the cask sets `ids` to the plain `tar.gz` archive's build ID.

## Testing the cask in CI

A new `make brew-check` target runs `test/homebrew/check.sh` against `dist/` after
`make release-snapshot`. It runs on a developer's Mac or Linux machine with Homebrew,
and in CI:

1. Create a throwaway tap: `brew tap-new --no-git alchemist/local`.
2. Copy `dist/homebrew/Casks/alchemist.rb` into it unchanged.
3. `brew style` and `brew audit --cask --strict alchemist/local/alchemist`. Offline:
   the snapshot's release URLs do not exist.
4. Rewrite each `https://github.com/colbytimm/Alchemist/releases/download/<tag>/` to
   `file://$PWD/dist/`. The `sha256` lines stay valid, since they describe these
   archives.
5. `brew install --cask alchemist/local/alchemist`. Check that `alchemist --version`
   prints the snapshot version and that `alchemist --adapter mock --help` exits 0.
6. `brew uninstall --cask alchemist/local/alchemist` and `brew untap alchemist/local`,
   also on failure (`trap`).

`HOMEBREW_NO_AUTO_UPDATE=1` keeps each run from updating Homebrew itself.

After a real release, the `brew-verify` job installs from the published tap with
`brew install colbytimm/tap/alchemist`. It checks `alchemist --version` against the
tag and runs `brew audit --cask --strict --online colbytimm/tap/alchemist`, which
fetches the real URLs.

## Scope

- `.goreleaser.yml`:
  - a `homebrew_casks` entry: `repository` `colbytimm/homebrew-tap`, `token` from
    `HOMEBREW_TAP_TOKEN`, the `skip_upload` template, `description`, `homepage`,
    `commit_msg_template: "alchemist {{ .Tag }}"`, and the macOS `postflight` hook with
    a comment saying why the quarantine is removed;
  - the "future work" comment loses "Homebrew tap".
- `.github/workflows/release.yml`:
  - `release` job: `HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}` beside
    `GITHUB_TOKEN`;
  - a step that writes `tap-published=true` to `$GITHUB_OUTPUT` when the token is set
    and the tag has no `-`, exposed as a job output;
  - an `if: always()` upload of `dist/homebrew/Casks/alchemist.rb`;
  - new `brew-verify` job: `needs: release`, `if: needs.release.outputs.tap-published == 'true'`,
    matrix `macos-latest` and `ubuntu-latest`, as described above.
- `.github/workflows/main.yml`:
  - the `snapshot` job adds `dist/homebrew/Casks/alchemist.rb` to the uploaded
    `snapshot-archives`;
  - new `brew` job: `needs: snapshot`, matrix `macos-latest` and `ubuntu-latest`,
    downloads `snapshot-archives` into `dist/`, runs `make brew-check`.
- `Makefile`: the `brew-check` target, added to `.PHONY` and `make help`. The `release`
  help line says `HOMEBREW_TAP_TOKEN` is optional.
- `test/homebrew/check.sh`: the six steps above, `set -euo pipefail`, no arguments.
- `colbytimm/homebrew-tap`: a new public repository with a `README.md` (the install
  line) and an empty `Casks/`. It is created once, by hand, by the owner.
- `docs/plan/00-overview.md`: row 25 in the iteration table.
- `docs/plan/09-ci-release.md`: its "Out of scope" line for the Homebrew tap points here.
- The user docs listed under "Documentation".

No Go code changes. `internal/tui`, the adapters and `cmd/` are untouched.

## Documentation

Docs style for every change: short plain sentences, one idea per sentence, active
voice, tables for reference facts, sentence-case headings, no em dashes, no bold
lead-ins, no marketing words. Match the README and `docs/` as they are now.

| File | Change |
|---|---|
| `README.md` | Replace "There are no release builds yet." (stale: v0.1.0 and v0.2.0 are released) with nothing, keeping "Alchemist is in early development." |
| `README.md`, Features | One bullet: installs with Homebrew on macOS and Linux. |
| `README.md`, Quick start | A new first subsection, "Install". Homebrew comes first for macOS and Linux: `brew install colbytimm/tap/alchemist`. Then iteration 24's packages, then the existing build from source. |
| `README.md`, Quick start | `./bin/alchemist` becomes `alchemist` in every command. One sentence says a source build runs as `./bin/alchemist`. The emulator subsection says its `make` targets run in a clone of the repository. |
| `README.md`, Documentation table | A row for the install page. |
| `docs/install.md` (new) | Install, upgrade (`brew upgrade`), uninstall. Uninstalling leaves config, history, saved queries and snapshots, with a link to the files table. The macOS quarantine removal and why. Linux needs Homebrew on Linux and a Homebrew recent enough for casks. Release candidates are not in the tap. |
| `docs/README.md` | A first section, "Installing", linking `install.md`. |
| `docs/reference/configuration.md`, Files | One sentence: uninstalling Alchemist, with Homebrew or otherwise, leaves these files in place. |
| `docs/reference/cli.md` | No change: no command or flag is added. |
| `docs/reference/keys.md` | No change. |
| `docs/using/*.md`, `docs/language/*.md`, `docs/data/*.md` | No change. None names `./bin/alchemist` or an install method (checked with `grep`). |

`docs/install.md` is new because upgrade, uninstall, the quarantine and the files left
behind would crowd the quick start. The quick start keeps only the install line. If
iteration 24 lands first with its own install page, this iteration adds a "Homebrew"
section to that page instead of creating `docs/install.md`.

## Out of scope

- A `homebrew/core` formula. It waits for notability, as described above.
- `homebrew/cask`. It needs a notarized binary.
- Signing and notarizing the darwin binaries. Iteration 24 or later.
- Shell completions. They need `alchemist completion`, which `cmd/root.go` turns off.
- CI for the tap repository itself (`brew test-bot`). The Alchemist release workflow
  tests the one cask the tap holds.
- A `zap` stanza, for the reason given above.
- Windows package managers. Iteration 24.

## Relationship to other iterations

- **9, CI and release.** The cask is one more goreleaser publisher in the same
  `make release`. The release still needs nothing beyond `git tag && git push --tags`
  once the secret exists. The `brew` job follows 9's pattern: workflows call `make`
  targets, and tool versions stay pinned in the `Makefile`.
- **24, installable packages.** Parallel. This iteration reads the per-platform
  `tar.gz` archives and checksums that 24 keeps. If 24 changes archive names or wraps
  the binary in a directory, goreleaser's cask template follows (it handles
  `wrap_in_directory`) and `test/homebrew/check.sh` catches a break. If 24 adds a
  universal darwin binary, goreleaser's cask pipe already prefers it. If 24 notarizes,
  the `postflight` hook is removed.

## Steps

1. **Spike on a runner.** On a branch, add the `homebrew_casks` entry and run
   `make release-check` and `make release-snapshot`. Run `test/homebrew/check.sh` by
   hand on `macos-latest` and `ubuntu-latest`. Confirm these before anything else:
   - Homebrew on the Ubuntu runner installs a cask with `on_linux`;
   - `brew audit --cask --strict` accepts goreleaser's output;
   - `file://` URLs install.
   Record any surprise in "Implementation notes" and resolve it here, not later.
2. **CI for the snapshot.** Add `make brew-check` and the `main.yml` `brew` job.
   `make actionlint` must pass.
3. **Create the tap and the token.** The owner creates `colbytimm/homebrew-tap` and the
   fine-grained token, then adds the `HOMEBREW_TAP_TOKEN` secret. This needs the public
   repository from the prerequisite.
4. **Release wiring.** Add the token env, the `tap-published` output, the cask upload
   and `brew-verify` to `release.yml`.
5. **Rehearse.** Tag `vX.Y.Z-rc.1`. The release is published and the tap is not
   touched. Then tag the real `vX.Y.Z`. The tap gets one commit, and `brew-verify`
   passes on both runners.
6. **Docs, in the same pull request as steps 2 and 4.** Update every file in the
   "Documentation" table. Update `docs/plan/00-overview.md` and
   `docs/plan/09-ci-release.md`. Run `make spell` and check every link in the changed
   docs.

## Testing

**Static, on every PR (existing `release-check` job):**

- `make release-check` (`goreleaser check`) accepts the `homebrew_casks` entry with no
  deprecation notice.
- `make actionlint` is clean on `main.yml` and `release.yml`.
- `make spell` passes on the new script, the plan and the docs.

**Integration, on every push to `main`:**

- The `brew` job passes on `macos-latest` and `ubuntu-latest`: style, strict offline
  audit, install from local archives, `--version` matches the snapshot, uninstall.
- A deliberate break fails the job, tried once on a branch: a wrong `binary` name in
  `.goreleaser.yml`.

**Live, on a release tag:**

- `brew-verify` passes on both runners against the published tap.
- A fork's tag run (no secret) publishes its own release and logs the cask skip. The
  job stays green.

**Manual checklist:**

- [ ] On an Apple Silicon Mac: `brew install colbytimm/tap/alchemist`, then
      `alchemist --version` prints the tag with no Gatekeeper dialog.
- [ ] On an Intel Mac, or under Rosetta with `arch -x86_64 brew`: the same.
- [ ] On Linux (Ubuntu, Homebrew on Linux): the same, and `alchemist --adapter mock`
      opens the app.
- [ ] After the next tag: `brew update && brew upgrade` moves to it.
- [ ] `brew uninstall --cask alchemist` leaves `~/.config/alchemist` and
      `~/.local/share/alchemist` in place.
- [ ] A `-rc` tag leaves `Casks/alchemist.rb` in the tap unchanged.
- [ ] The tap's commit history shows only release-job commits.
- [ ] Every link in `README.md`, `docs/README.md` and `docs/install.md` resolves.

## Acceptance criteria

- `brew install colbytimm/tap/alchemist` installs the latest stable release on macOS
  (arm64, amd64) and Linux (arm64, amd64), and `alchemist --version` prints its tag.
- A stable tag updates the tap with no manual step. A prerelease tag, a snapshot and a
  run without `HOMEBREW_TAP_TOKEN` never touch it, and the last still releases.
- The snapshot cask is styled, audited, installed and run on macOS and Linux on every
  push to `main`.
- No `brews` entry and no goreleaser deprecation notice.
- The docs describe the shipped behavior: the install line, upgrade, uninstall and the
  files left behind. Every doc link resolves, and `make spell` passes.
- `make all` still passes. No Go code changed.

## Open questions

- **Public repository.** The tap cannot work until `colbytimm/Alchemist` is public.
  Homebrew has, as far as this plan can verify, no supported way to download private
  release assets. The owner decides.
- **Linux cask support by Homebrew version.** Homebrew's docs say casks support Linux,
  but this plan could not find the first Homebrew release that installs a `binary`
  cask on Linux. Step 1 checks the runner. `docs/install.md` says "run `brew update`
  first" rather than naming a version.
- **Strict audit of goreleaser's cask.** Not verified offline. If `brew audit --strict`
  rejects part of the template (the livecheck skip, the `postflight` hook), step 1
  drops `--strict` for that check alone and records why.
- **Quarantine removal on current macOS.** The `xattr` hook is the approach
  goreleaser's template comments point to (goreleaser issue 5958). The manual
  checklist confirms it on a real Mac, since a CI runner's Gatekeeper settings may not
  match a user's.
- **Retiring the tap when a core formula lands.** `tap_migrations.json` moves formulae
  between taps. Whether it moves a tap cask to a core formula is unverified. The later
  core iteration answers it.
