#!/usr/bin/env bash
# goreleaser's post-build hook for each Windows binary, run from the repository
# root before the archive pipe, so the zip picks up whatever this does to the .exe.
#
# Usage: package.sh <exe> <arch> <version> <is-snapshot>
set -euo pipefail

readonly exe="$1" arch="$2" version="$3" is_snapshot="$4"
# MSI versions are numeric: 0.3.0-rc.1 and 0.2.1-snapshot become 0.3.0 and 0.2.1.
readonly msi_version="${version%%-*}"
readonly wxs=packaging/windows/alchemist.wxs
readonly msi="dist/alchemist_${version}_windows_${arch}.msi"

main() {
	# wixl is not known to build arm64 MSIs; Windows on Arm uses the zip.
	[[ "$arch" == amd64 ]] || return 0
	if ! command -v wixl >/dev/null 2>&1; then
		skip_msi_without_wixl
		return 0
	fi
	build_msi
}

skip_msi_without_wixl() {
	if [[ "$is_snapshot" != true ]]; then
		echo "error: wixl not installed (apt-get install wixl); a release needs the MSI" >&2
		exit 1
	fi
	echo "warning: wixl not installed (apt-get install wixl); skipping the MSI" >&2
}

build_msi() {
	wixl --arch x64 -D Version="$msi_version" -D Binary="$exe" -o "$msi" "$wxs"
	add_path_entry
}

# wixl has no Environment element. These rows are what WiX writes for
# <Environment Name="PATH" Value="[INSTALLDIR]" Action="set" Part="last" System="yes" Permanent="no"/>.
add_path_entry() {
	msibuild "$msi" \
		-q "CREATE TABLE \`Environment\` (\`Environment\` CHAR(72) NOT NULL, \`Name\` CHAR(255) NOT NULL LOCALIZABLE, \`Value\` CHAR(255) LOCALIZABLE, \`Component_\` CHAR(72) NOT NULL PRIMARY KEY \`Environment\`)" \
		-q "INSERT INTO \`Environment\` (\`Environment\`, \`Name\`, \`Value\`, \`Component_\`) VALUES ('PathEntry', '=-*PATH', '[~];[INSTALLDIR]', 'Executable')" \
		-q "INSERT INTO \`InstallExecuteSequence\` (\`Action\`, \`Sequence\`) VALUES ('RemoveEnvironmentStrings', 3300)" \
		-q "INSERT INTO \`InstallExecuteSequence\` (\`Action\`, \`Sequence\`) VALUES ('WriteEnvironmentStrings', 5200)"
}

main
