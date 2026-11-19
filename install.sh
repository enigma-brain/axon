#!/bin/sh
# Installs axon from a release of the public repository enigma-brain/axon.
#
#   curl -fsSL https://github.com/enigma-brain/axon/releases/latest/download/install.sh | sh
#
# It downloads the archive for this machine and the release's checksums.txt
# with curl, verifies the archive against the checksums, and puts axon in
# ~/.local/bin, replacing any axon already there. With no version given it
# installs the latest full release. AXON_VERSION names another release to
# install, such as 0.1.0-rc.1 or v0.1.0-rc.1.
#
# It needs curl and sha256sum or shasum, no GitHub account, no token and no
# sudo. On any failure it stops before writing to ~/.local/bin.

set -eu

repo=enigma-brain/axon
site=https://github.com/$repo
bin_dir="$HOME/.local/bin"

fail() {
	echo "axon install: $*" >&2
	exit 1
}

# The release has archives for these platforms only.
os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
Darwin/arm64 | Darwin/aarch64) platform=darwin_arm64 ;;
Darwin/x86_64 | Darwin/amd64) platform=darwin_amd64 ;;
Linux/x86_64 | Linux/amd64) platform=linux_amd64 ;;
*) fail "there is no axon release for $os on $arch. axon is released for macOS on Apple silicon, macOS on Intel and Linux on x86-64; on Windows, install it inside WSL." ;;
esac

command -v curl >/dev/null 2>&1 ||
	fail "curl is not installed, and this script needs it to download axon. Install curl, then run this again."

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
	fail "neither sha256sum nor shasum is installed, so the download cannot be verified. Install one of them, then run this again."
fi

tmp=$(mktemp -d)
staged=
trap 'rm -rf "$tmp"; [ -z "$staged" ] || rm -f "$staged"' EXIT
trap 'exit 1' HUP INT TERM

# download <what> <url> <file> fetches url into file or stops, naming what
# could not be downloaded.
download() {
	curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o "$3" "$2" ||
		fail "could not download $1 from $2"
}

# Settle the tag first, so that the checksums and the archive come from one
# release.
if [ -n "${AXON_VERSION:-}" ]; then
	case "$AXON_VERSION" in
	v*) tag=$AXON_VERSION ;;
	*) tag=v$AXON_VERSION ;;
	esac
else
	# The latest release's page redirects to the page of its tag. GitHub
	# never treats a pre-release as the latest.
	no_release="no release of $repo was found. Name one with AXON_VERSION, for example AXON_VERSION=0.1.0-rc.1."
	final=$(curl -fsSL --proto '=https' --tlsv1.2 --retry 3 -o /dev/null -w '%{url_effective}' "$site/releases/latest") ||
		fail "could not look up the latest release at $site/releases/latest; $no_release"
	case "$final" in
	*/releases/tag/?*) tag=${final##*/releases/tag/} ;;
	*) fail "$no_release" ;;
	esac
fi
version=${tag#v}
name=axon_${version}_$platform.tar.gz
base=$site/releases/download/$tag

echo "Downloading axon $version for $platform from $site"
download "checksums.txt for release $tag" "$base/checksums.txt" "$tmp/checksums.txt"
download "$name for release $tag" "$base/$name" "$tmp/$name"

want=$(awk -v n="$name" '$2 == n || $2 == "*" n { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "checksums.txt in release $tag has no checksum for $name."
got=$(sha256 "$tmp/$name")
[ "$got" = "$want" ] ||
	fail "the checksum of $name does not match checksums.txt (expected $want, got $got). Nothing was installed."

mkdir "$tmp/unpacked"
tar -xzf "$tmp/$name" -C "$tmp/unpacked" || fail "could not unpack $name."
[ -f "$tmp/unpacked/axon" ] || fail "$name holds no axon binary."

mkdir -p "$bin_dir" || fail "could not create $bin_dir."
# Copy beside the target and rename over it, so axon is replaced in one step.
staged="$bin_dir/.axon.$$"
{ cp "$tmp/unpacked/axon" "$staged" && chmod 755 "$staged" && mv -f "$staged" "$bin_dir/axon"; } ||
	fail "could not write $bin_dir/axon."
staged=

installed=$("$bin_dir/axon" --version) ||
	fail "$bin_dir/axon was written but does not run. Check that $bin_dir is on a filesystem that allows running programs."
echo "Installed $installed in $bin_dir/axon"

case ":$PATH:" in
*":$bin_dir:"*) ;;
*)
	echo
	echo "$bin_dir is not on your PATH. Add it by putting this line in your shell's startup file (~/.zshrc or ~/.bashrc):"
	echo
	echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
	;;
esac
