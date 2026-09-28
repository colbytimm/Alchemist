#!/usr/bin/env bash
# Installs the cask that `make release-snapshot` wrote to dist/ from a throwaway
# tap, checks the binary runs, and removes both. Run from the repository root.
set -euo pipefail

readonly tap=alchemist/local
readonly cask="$tap/alchemist"
readonly dist="$PWD/dist"
readonly generated_cask="$dist/homebrew/Casks/alchemist.rb"
readonly release_url='https://github\.com/colbytimm/Alchemist/releases/download/[^/]*/'

export HOMEBREW_NO_AUTO_UPDATE=1

main() {
	trap clean_up EXIT
	brew tap-new --no-git "$tap"
	local tapped_cask
	tapped_cask="$(brew --repository "$tap")/Casks/alchemist.rb"
	mkdir -p "$(dirname "$tapped_cask")"
	cp "$generated_cask" "$tapped_cask"

	# Not brew style or --strict: they hold casks to homebrew-cask's layout rules,
	# and goreleaser writes this one. Offline: a snapshot's release URLs do not exist.
	brew audit --cask "$cask"

	point_urls_at_dist "$tapped_cask"
	brew install --cask "$cask"
	check_installed_binary "$(cask_version "$tapped_cask")"
}

# The sha256 lines stay valid: they describe these same archives.
point_urls_at_dist() {
	sed -i.bak "s#${release_url}#file://${dist}/#" "$1"
	rm "$1.bak"
}

cask_version() {
	sed -n 's/^  version "\(.*\)"$/\1/p' "$1"
}

check_installed_binary() {
	local binary
	binary="$(brew --prefix)/bin/alchemist"
	"$binary" --version | grep -F "Alchemist v$1 "
	"$binary" --adapter mock --help >/dev/null
}

# Either may not exist yet when an earlier step failed.
clean_up() {
	brew uninstall --cask "$cask" 2>/dev/null || true
	brew untap "$tap" 2>/dev/null || true
}

main
