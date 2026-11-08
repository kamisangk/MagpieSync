#!/usr/bin/env sh
set -eu

platform=${1:-}
case "$platform" in
  x86|x86_64) goarch=amd64; platform=x86 ;;
  arm|arm64|aarch64) goarch=arm64; platform=arm ;;
  *) echo "usage: $0 x86|arm" >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"
pkg="$root/dist/fpk/staging/magpiesync-$platform"
rm -rf "$pkg"
mkdir -p "$pkg/app/bin" "$pkg/app/ui/images" "$pkg/cmd" "$pkg/config" "$pkg/wizard"

CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" \
  go build -trimpath -ldflags '-s -w' -o "$pkg/app/bin/magpiesync" ./cmd/magpiesync
chmod 0755 "$pkg/app/bin/magpiesync"

if [ -n "${FFMPEG_BIN:-}" ]; then
  test -x "$FFMPEG_BIN" || { echo "FFMPEG_BIN must be executable" >&2; exit 1; }
  command -v file >/dev/null 2>&1 || { echo "file is required to verify FFmpeg architecture" >&2; exit 1; }
  ffmpeg_info=$(file -b "$FFMPEG_BIN")
  case "$platform:$ffmpeg_info" in
    x86:*x86-64*|x86:*x86_64*) ;;
    arm:*ARM\ aarch64*|arm:*ARM64*|arm:*aarch64*) ;;
    *) echo "FFmpeg architecture does not match $platform: $ffmpeg_info" >&2; exit 1 ;;
  esac
  mkdir -p "$pkg/app/bin/ffmpeg"
  cp "$FFMPEG_BIN" "$pkg/app/bin/ffmpeg/ffmpeg"
  chmod 0755 "$pkg/app/bin/ffmpeg/ffmpeg"
fi

package_version=${FPK_VERSION:-$(sed -n 's/^version=//p' "$root/packaging/fpk/manifest")}
case "$package_version" in
  ''|*[!0-9A-Za-z.+-]*) echo "invalid FPK_VERSION: $package_version" >&2; exit 2 ;;
esac
sed -e "s/^platform=.*/platform=$platform/" \
  -e "s/^version=.*/version=$package_version/" \
  "$root/packaging/fpk/manifest" > "$pkg/manifest"
cp "$root/packaging/fpk/cmd-main.sh" "$pkg/cmd/main"
cp "$root/packaging/fpk/install_init" "$pkg/cmd/install_init"
cp "$root/packaging/fpk/install_callback" "$pkg/cmd/install_callback"
cp "$root/packaging/fpk/upgrade_init" "$pkg/cmd/upgrade_init"
cp "$root/packaging/fpk/upgrade_callback" "$pkg/cmd/upgrade_callback"
cp "$root/packaging/fpk/uninstall_init" "$pkg/cmd/uninstall_init"
cp "$root/packaging/fpk/uninstall_callback" "$pkg/cmd/uninstall_callback"
chmod 0755 "$pkg/cmd/"*

cat > "$pkg/config/privilege" <<'EOF'
{
  "defaults": {"run-as": "package"},
  "username": "magpiesync",
  "groupname": "magpiesync"
}
EOF
printf '{}\n' > "$pkg/config/resource"
cp "$root/packaging/fpk/icon-64.png" "$pkg/ICON.PNG"
cp "$root/internal/httpapi/web/brand-logo.png" "$pkg/ICON_256.PNG"
cp "$root/packaging/fpk/ui/config" "$pkg/app/ui/config"
cp "$pkg/ICON.PNG" "$pkg/app/ui/images/icon_64.png"
cp "$pkg/ICON_256.PNG" "$pkg/app/ui/images/icon_256.png"

fnpack_bin=${FNPACK_BIN:-fnpack}
if command -v "$fnpack_bin" >/dev/null 2>&1 || [ -x "$fnpack_bin" ]; then
  mkdir -p "$root/dist/fpk/tmp"
  export TEMP="$(cygpath -w "$root/dist/fpk/tmp" 2>/dev/null || printf '%s' "$root/dist/fpk/tmp")"
  export TMP="$TEMP"
  (cd "$pkg" && "$fnpack_bin" build)
  fpk="$pkg/magpiesync.fpk"
  if [ ! -s "$fpk" ]; then
    echo "fnpack did not produce $fpk" >&2
    exit 1
  fi
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) "$root/packaging/fpk/fix-fpk-modes.sh" "$fpk" ;;
  esac
  if ! tar -tvzf "$fpk" | awk '$NF == "cmd/main" { found=1; if (substr($0,1,4) !~ /x/) bad=1 } END { exit !found || bad }'; then
    echo "cmd/main is missing executable permissions in $fpk" >&2
    exit 1
  fi
  if ! tar -xOzf "$fpk" app.tgz | tar -tvzf - | awk '$NF == "bin/magpiesync" { found=1; if (substr($0,1,10) !~ /x/) bad=1 } END { exit !found || bad }'; then
    echo "app/bin/magpiesync is missing executable permissions in $fpk" >&2
    exit 1
  fi
  expected_checksum=$(tar -xOzf "$fpk" manifest | sed -n 's/^checksum[[:space:]]*=[[:space:]]*\([0-9a-f]*\).*/\1/p')
  actual_checksum=$(tar -xOzf "$fpk" app.tgz | md5sum)
  actual_checksum=${actual_checksum%% *}
  if [ "$expected_checksum" != "$actual_checksum" ]; then
    echo "app.tgz checksum does not match manifest in $fpk" >&2
    exit 1
  fi
  echo "fnpack build completed for platform=$platform"
else
  echo "fnpack not found; completed official-layout staging at $pkg"
  exit 1
fi
