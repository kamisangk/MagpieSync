#!/usr/bin/env bash
set -euo pipefail

fpk=$1
root=$(CDPATH= cd -- "$(dirname -- "$fpk")" && pwd)
work=$(mktemp -d "$root/fpk-modes.XXXXXX")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/package" "$work/app"
tar -xzf "$fpk" -C "$work/package"
tar -xzf "$work/package/app.tgz" -C "$work/app"

# Rebuild app.tgz with executable mode only on shipped program binaries.
tar -cf "$work/app.tar" --files-from=/dev/null
while IFS= read -r -d '' path; do
  relative=${path#"$work/app/"}
  case "$relative" in
    bin/magpiesync|bin/ffmpeg/ffmpeg) mode=0755 ;;
    *) mode=0644 ;;
  esac
  tar -rf "$work/app.tar" --mode="$mode" -C "$work/app" "$relative"
done < <(find "$work/app" -mindepth 1 -type f -print0)
while IFS= read -r -d '' path; do
  relative=${path#"$work/app/"}
  tar -rf "$work/app.tar" --mode=0755 --no-recursion -C "$work/app" "$relative"
done < <(find "$work/app" -mindepth 1 -type d -print0)
gzip -n -c "$work/app.tar" > "$work/package/app.tgz"

# fnpack records the compressed application payload's MD5 in manifest.
checksum=$(md5sum "$work/package/app.tgz")
checksum=${checksum%% *}
sed "s/^checksum[[:space:]]*=.*/checksum=$checksum/" "$work/package/manifest" > "$work/manifest"
mv "$work/manifest" "$work/package/manifest"

# Lifecycle handlers must also be executable in the outer FPK archive.
tar -cf "$work/package.tar" --files-from=/dev/null
while IFS= read -r -d '' path; do
  relative=${path#"$work/package/"}
  case "$relative" in
    cmd/*) mode=0755 ;;
    *) mode=0644 ;;
  esac
  tar -rf "$work/package.tar" --mode="$mode" -C "$work/package" "$relative"
done < <(find "$work/package" -mindepth 1 -type f -print0)
while IFS= read -r -d '' path; do
  relative=${path#"$work/package/"}
  tar -rf "$work/package.tar" --mode=0755 --no-recursion -C "$work/package" "$relative"
done < <(find "$work/package" -mindepth 1 -type d -print0)
gzip -n -c "$work/package.tar" > "$work/repaired.fpk"
mv "$work/repaired.fpk" "$fpk"
