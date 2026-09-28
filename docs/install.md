# Installing

Every release on the [releases page](https://github.com/colbytimm/Alchemist/releases)
has these files. `<version>` is the release without its `v`, such as `0.3.0`.

| Platform | File |
|---|---|
| macOS, Intel and Apple silicon | `alchemist_<version>_darwin_universal.tar.gz` |
| Linux | `alchemist_<version>_linux_<arch>.deb`, `.rpm`, `.apk` or `.tar.gz` |
| Windows | `alchemist_<version>_windows_amd64.msi`, or `alchemist_<version>_windows_<arch>.zip` |

`<arch>` is `amd64` or `arm64`. The commands below set `VERSION` once and use it.

## Homebrew

On macOS and Linux, install from the project's tap:

```sh
brew install colbytimm/tap/alchemist
```

Always use the full name, `colbytimm/tap/alchemist`. A bare `alchemist` could match
a different package in Homebrew's own repositories.

| Task | Command |
|---|---|
| Upgrade | `brew update && brew upgrade alchemist` |
| Uninstall | `brew uninstall --cask alchemist` |
| Remove the tap | `brew untap colbytimm/tap` |

The tap holds stable releases only. A release candidate is on the
[releases page](https://github.com/colbytimm/Alchemist/releases) and not in the tap.

On Linux, you need [Homebrew on Linux](https://docs.brew.sh/Homebrew-on-Linux).
Alchemist installs as a cask, and older Homebrew versions install casks only on
macOS. Run `brew update` first.

On macOS, Homebrew marks each download with the quarantine flag, so Gatekeeper
checks it on first run. Gatekeeper refuses a binary that is not notarized. For such
a release, the cask removes the flag from `alchemist` after it installs. A notarized
release keeps the flag. Either way, the cask checks the SHA-256 of every download.

Uninstalling keeps your settings, history, saved queries and snapshots. The cask has
no zap step, so `brew uninstall --zap` keeps them too.
[Configuration](reference/configuration.md#files) lists where they are.

## macOS

Download the archive, extract the binary, and move it to a directory on your `PATH`:

```sh
VERSION=0.3.0
curl -LO "https://github.com/colbytimm/Alchemist/releases/download/v$VERSION/alchemist_${VERSION}_darwin_universal.tar.gz"
tar -xzf "alchemist_${VERSION}_darwin_universal.tar.gz" alchemist
sudo mv alchemist /usr/local/bin/
```

The binary runs on Intel and Apple silicon Macs.

A release is signed with an Apple Developer ID and notarized once the project has its
certificates. `codesign -dv /usr/local/bin/alchemist` shows whether yours is signed.

A browser download of an unsigned release is quarantined, and macOS refuses to open
it because it cannot check it. To allow it, remove the quarantine flag:

```sh
xattr -d com.apple.quarantine /usr/local/bin/alchemist
```

`curl` does not quarantine what it downloads, so the commands above never meet this
prompt.

## Linux

Download the package for your distribution and architecture, then install it:

| Distribution | Command |
|---|---|
| Debian, Ubuntu | `sudo apt install ./alchemist_${VERSION}_linux_amd64.deb` |
| Fedora, RHEL | `sudo dnf install ./alchemist_${VERSION}_linux_amd64.rpm` |
| Alpine | `sudo apk add --allow-untrusted ./alchemist_${VERSION}_linux_amd64.apk` |
| Any other | extract `alchemist_${VERSION}_linux_amd64.tar.gz` and move `alchemist` to a directory on your `PATH` |

Use `arm64` in place of `amd64` on an ARM machine. For example:

```sh
VERSION=0.3.0
curl -LO "https://github.com/colbytimm/Alchemist/releases/download/v$VERSION/alchemist_${VERSION}_linux_amd64.deb"
sudo apt install "./alchemist_${VERSION}_linux_amd64.deb"
```

The packages install `/usr/bin/alchemist` and need no other package. They are not
GPG-signed, so `apk` needs `--allow-untrusted`. See [verify a download](#verify-a-download)
for checking a file.

### The keychain on Linux

Alchemist stores keys through the Secret Service API. Any provider works:

- GNOME Keyring
- KeePassXC, with Secret Service integration turned on
- KWallet 5.97 or later

The provider needs a D-Bus session bus. A desktop session has one. An SSH session
often does not, and neither do servers and containers. Without one, `profile add`
saves the profile and tells you to set `ALCHEMIST_<NAME>_KEY` instead. See
[keys](data/profiles.md#keys).

The `.deb` and `.rpm` suggest `gnome-keyring` but do not install it.

## Windows

Download `alchemist_<version>_windows_amd64.msi` and run it. It installs
`C:\Program Files\Alchemist\alchemist.exe` and adds that folder to the system `PATH`.
Open a new terminal, then run `alchemist`.

To install without prompts, from an administrator terminal:

```powershell
msiexec /i alchemist_0.3.0_windows_amd64.msi /qn
```

Installing a newer MSI replaces the older version.

A release is signed once the project has a code signing certificate. If yours is not,
SmartScreen shows "Windows protected your PC". Select More info, then Run anyway.

The zip holds `alchemist.exe` and needs no installer. Extract it to a folder on your
`PATH`. On Windows on Arm, use `alchemist_<version>_windows_arm64.zip`, or the MSI,
which runs under emulation.

## Verify a download

`checksums.txt` lists the SHA-256 of every file in the release. Download it next to
your files and check them:

```sh
sha256sum -c checksums.txt --ignore-missing
```

On macOS, use `shasum -a 256 -c checksums.txt --ignore-missing`. On Windows,
`Get-FileHash <file>` prints the hash to compare.

Every file also has a build provenance attestation. It proves the file was built by
this repository's release workflow. Check it with the [GitHub CLI](https://cli.github.com):

```sh
gh attestation verify alchemist_0.3.0_linux_amd64.deb --repo colbytimm/Alchemist
```

Each archive has an SPDX software bill of materials next to it, named
`<archive>.sbom.json`. It lists the Go modules built into the binary.

## Uninstall

| Installed from | Command |
|---|---|
| Homebrew | `brew uninstall --cask alchemist` |
| macOS archive | `sudo rm /usr/local/bin/alchemist` |
| `.deb` | `sudo apt remove alchemist` |
| `.rpm` | `sudo dnf remove alchemist` |
| `.apk` | `sudo apk del alchemist` |
| MSI | remove Alchemist in Settings, Apps, or run `msiexec /x alchemist_<version>_windows_amd64.msi` |
| zip | delete `alchemist.exe` |

Uninstalling keeps your settings in `~/.config/alchemist`, your history and snapshots
in `~/.local/state/alchemist` and `~/.local/share/alchemist`, and your keys in the
keychain. To delete a key, run `alchemist profile remove <name>` before you uninstall.

## Build from source

You need Go 1.26 or newer:

```sh
git clone https://github.com/colbytimm/Alchemist.git
cd Alchemist
make build
```

The binary is `bin/alchemist`. Move it to a directory on your `PATH` to run it as
`alchemist`.
