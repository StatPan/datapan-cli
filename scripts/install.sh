#!/usr/bin/env sh
set -eu

repo="StatPan/datapan-cli"
version="${DATAPAN_VERSION:-latest}"
install_dir="${DATAPAN_INSTALL_DIR:-"$HOME/.datapan/bin"}"
install_dp="${DATAPAN_INSTALL_DP:-0}"

usage() {
  echo "Usage: DATAPAN_VERSION=latest DATAPAN_INSTALL_DIR=\$HOME/.datapan/bin DATAPAN_INSTALL_DP=1 sh scripts/install.sh"
}

if [ "${1:-}" = "--help" ] || [ "${1:-}" = "-h" ]; then
  usage
  exit 0
fi

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need curl
need tar

if [ "$version" = "latest" ]; then
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")"
  version="${latest_url##*/}"
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux) goos="linux" ;;
  darwin) goos="darwin" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

machine="$(uname -m)"
case "$machine" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *) echo "unsupported architecture: $machine" >&2; exit 1 ;;
esac

asset="datapan-cli_${version}_${goos}_${goarch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$version"
tmp="${TMPDIR:-/tmp}/datapan-cli-install-$$"
mkdir -p "$tmp"

cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

curl -fsSL "$base_url/$asset" -o "$tmp/$asset"
curl -fsSL "$base_url/checksums.txt" -o "$tmp/checksums.txt"

expected="$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')"
if [ -z "$expected" ]; then
  echo "checksum for $asset not found" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  need shasum
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
fi

if [ "$expected" != "$actual" ]; then
  echo "checksum mismatch for $asset" >&2
  exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
payload="$tmp/datapan-cli_${version}_${goos}_${goarch}"
mkdir -p "$install_dir"
cp "$payload/datapan" "$install_dir/datapan"
chmod +x "$install_dir/datapan"

if [ "$install_dp" = "1" ]; then
  existing_dp="$(command -v dp 2>/dev/null || true)"
  target_dp="$install_dir/dp"
  if [ -n "$existing_dp" ] && [ "$existing_dp" != "$target_dp" ]; then
    echo "Skipping optional dp alias because another dp command exists at $existing_dp." >&2
  else
    cp "$payload/dp" "$target_dp"
    chmod +x "$target_dp"
  fi
fi

"$install_dir/datapan" version --json
echo "Installed Datapan CLI $version to $install_dir"
echo "Add $install_dir to PATH if datapan is not already available."
