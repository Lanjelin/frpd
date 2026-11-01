#!/usr/bin/env bash
set -euo pipefail

readonly binary_name="frpd"
readonly output_dir="${OUTPUT_DIR:-dist}"
readonly common_targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
)
# Override with VERSION=...; defaults to `git describe`, or "dev" outside git.
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
readonly version

usage() {
  cat <<'EOF'
Usage: ./build.sh [--all | GOOS/GOARCH ...]

Builds static frpd binaries into dist/ by default.

Without arguments, builds for the current Go host target. Pass one or more
GOOS/GOARCH targets to cross-compile. --all builds the common release targets:
  linux/amd64  linux/arm64  darwin/amd64  darwin/arm64  windows/amd64

Version is taken from VERSION, else `git describe --tags --always --dirty`.

Examples:
  ./build.sh
  ./build.sh linux/amd64 linux/arm64
  ./build.sh --all
  OUTPUT_DIR=out ./build.sh darwin/arm64
  VERSION=v1.2.3 ./build.sh --all
EOF
}

if [[ ${1:-} == "--help" || ${1:-} == "-h" ]]; then
  usage
  exit 0
fi

if [[ ${1:-} == "--all" ]]; then
  if [[ $# -ne 1 ]]; then
    printf '%s\n' "--all cannot be combined with explicit targets" >&2
    exit 2
  fi
  targets=("${common_targets[@]}")
elif [[ $# -eq 0 ]]; then
  targets=("$(go env GOOS)/$(go env GOARCH)")
else
  targets=("$@")
fi

mkdir -p "$output_dir"

for target in "${targets[@]}"; do
  if [[ ! $target =~ ^[a-z0-9]+/[a-z0-9_]+$ ]]; then
    printf 'invalid target: %s (expected GOOS/GOARCH)\n' "$target" >&2
    exit 2
  fi

  goos=${target%/*}
  goarch=${target#*/}
  extension=""
  if [[ $goos == "windows" ]]; then
    extension=".exe"
  fi

  mkdir -p "$output_dir/${goos}_${goarch}"
  output="$output_dir/${goos}_${goarch}/${binary_name}${extension}"
  printf 'building %s -> %s\n' "$target" "$output"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w -X main.version=${version}" -o "$output" ./cmd/frpd
done
