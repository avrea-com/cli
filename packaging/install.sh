#!/bin/sh
# Install the native AVR executable. Download and inspect this script before use.
set -eu
version=
bin_dir=${HOME:?HOME is required}/.local/bin
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || exit 2; version=$2; shift 2 ;;
    --bin-dir) [ "$#" -ge 2 ] || exit 2; bin_dir=$2; shift 2 ;;
    -h|--help) echo 'Usage: sh install.sh [--version 0.7.0] [--bin-dir DIRECTORY]'; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done
fail() { echo "avr install: $*" >&2; exit 1; }
case "$bin_dir" in /*) ;; *) fail '--bin-dir must be absolute' ;; esac
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) target=darwin_arm64 ;;
  Linux/x86_64) target=linux_amd64 ;;
  *) fail 'Supported platforms: macOS ARM64 and Linux AMD64. Use PowerShell on Windows AMD64.' ;;
esac
command -v curl >/dev/null || fail 'curl is required'
command -v tar >/dev/null || fail 'tar is required'
if command -v sha256sum >/dev/null; then
  checksum() { sha256sum "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null; then
  checksum() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
  fail 'sha256sum or shasum is required'
fi
base=${AVR_RELEASE_BASE_URL:-https://github.com/avrea-com/cli/releases}
# A loopback fixture may serve test artifacts; remote downloads always use TLS.
case "$base" in
  https://*) protocols='=https' ;;
  http://127.0.0.1:*) protocols='=http' ;;
  *) fail 'Release URL must use HTTPS' ;;
esac
fetch() { curl --fail --silent --show-error --location --connect-timeout 10 --max-time 120 --proto "$protocols" --proto-redir "$protocols" "$1" -o "$2"; }
tmp=$(mktemp -d)
staged=
marker_staged=
cleanup() { rm -f "${staged:-$tmp/unused}" "${marker_staged:-$tmp/unused}"; rm -rf "$tmp"; }
trap cleanup EXIT HUP INT TERM
if [ -z "$version" ]; then
  fetch "$base/latest/download/avr-version.txt" "$tmp/version"
  version=$(cat "$tmp/version")
fi
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$' || fail 'Invalid version'
name=avr_${version}_${target}
archive=$name.tar.gz
fetch "$base/download/v$version/$archive" "$tmp/$archive"
fetch "$base/download/v$version/$archive.sha256" "$tmp/checksum"
actual=$(checksum "$tmp/$archive")
[ "$(cat "$tmp/checksum")" = "$actual  $archive" ] || fail 'Checksum mismatch'
# Extract only the executable to stdout. No archive paths or links are created.
[ "$(tar -tzf "$tmp/$archive" | grep -Fxc "$name/avr")" = 1 ] || fail 'Missing or duplicate executable'
tar -xzOf "$tmp/$archive" "$name/avr" > "$tmp/avr"
[ -s "$tmp/avr" ] || fail 'Empty executable'
mkdir -p "$bin_dir"
marker=$bin_dir/avr-install-method
pending=$bin_dir/avr-install-method.pending
[ ! -L "$bin_dir/avr" ] || fail 'Existing avr is a symlink; update it with its package manager or choose another directory'
for path in "$marker" "$pending"; do
  [ ! -L "$path" ] || fail 'Installation marker must not be a symlink'
  [ ! -e "$path" ] || [ -f "$path" ] || fail 'Installation marker must be a file'
done
if [ -e "$bin_dir/avr" ]; then
  [ -f "$bin_dir/avr" ] || fail 'Destination contains an unmanaged avr; choose another directory'
  current="native $(checksum "$bin_dir/avr")"
  owned=false
  for path in "$marker" "$pending"; do
    if [ -f "$path" ] && [ "$(cat "$path")" = "$current" ]; then owned=true; fi
  done
  # Repair an older interrupted installer only when its binary is exactly the
  # verified version requested, and an installation marker already exists.
  if [ -f "$marker" ] && [ "$current" = "native $(checksum "$tmp/avr")" ]; then owned=true; fi
  [ "$owned" = true ] || fail 'Existing avr belongs to another installation or was changed; choose another directory'
fi
staged=$(mktemp "$bin_dir/.avr-XXXXXXXX")
cat "$tmp/avr" > "$staged"
chmod 755 "$staged"
new_digest=$(checksum "$staged")
marker_staged=$(mktemp "$bin_dir/.avr-marker-XXXXXXXX")
printf 'native %s\n' "$new_digest" > "$marker_staged"
# Keep a recoverable digest before replacing the executable, including on a
# first installation. Every rename stays on the destination filesystem.
mv -f "$marker_staged" "$pending"
marker_staged=
mv -f "$staged" "$bin_dir/avr"
staged=
mv -f "$pending" "$marker"
printf 'Installed avr %s to %s/avr\n' "$version" "$bin_dir"
case ":$PATH:" in *":$bin_dir:"*) ;; *) printf 'Add this directory to PATH: %s\n' "$bin_dir" ;; esac
existing=$(command -v avr || true)
if [ -n "$existing" ] && [ "$existing" != "$bin_dir/avr" ]; then
  printf 'Another avr currently wins on PATH: %s\n' "$existing"
fi
