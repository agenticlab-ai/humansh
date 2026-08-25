#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: scripts/release.sh VERSION" >&2
  exit 2
fi
version=$1
if ! printf '%s\n' "$version" | awk '/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$/ { valid = 1 } END { exit !valid }'; then
  echo "invalid semantic release version" >&2
  exit 2
fi
commit=${HUMANSH_COMMIT:-}
build_date=${HUMANSH_BUILD_DATE:-}
if [ -z "$commit" ]; then commit=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown); fi
if [ -z "$build_date" ]; then build_date=$(git show -s --format=%cI HEAD 2>/dev/null || echo unknown); fi
case $commit in *[!0-9A-Za-z._+-]*) echo "invalid release commit" >&2; exit 2 ;; esac
case $build_date in *[!0-9A-Za-z:._+-]*) echo "invalid release build date" >&2; exit 2 ;; esac
dist=${HUMANSH_DIST_DIR:-dist}
mkdir -p "$dist"
temp_dir=
cleanup() {
  if [ -n "$temp_dir" ]; then
    rm -rf "$temp_dir"
    temp_dir=
  fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  os=${target%/*}
  arch=${target#*/}
  name="humansh-$os-$arch"
  temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/humansh-release.XXXXXX")
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/agenticlab-ai/humansh/internal/version.Version=$version -X github.com/agenticlab-ai/humansh/internal/version.Commit=$commit -X github.com/agenticlab-ai/humansh/internal/version.BuildDate=$build_date" \
    -o "$temp_dir/humansh" ./cmd/humansh
  tar -czf "$dist/$name.tar.gz" -C "$temp_dir" humansh
  rm -rf "$temp_dir"
  temp_dir=
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$dist" && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
  else
    (cd "$dist" && shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256")
  fi
done
