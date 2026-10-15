#!/usr/bin/env sh
# Build pagebox for every supported platform into dist/ and generate checksums.txt.
set -eu

VERSION=$(cat VERSION 2>/dev/null | tr -d ' \r\n' || echo "0.1.0")
OUT=${OUT:-dist}

LDFLAGS="-s -w -X main.version=$VERSION"

TARGETS="
linux/amd64 linux/arm64 linux/arm linux/386 linux/riscv64
darwin/amd64 darwin/arm64
windows/amd64 windows/arm64 windows/386
freebsd/amd64 freebsd/arm64 openbsd/amd64 openbsd/arm64 netbsd/amd64
"

rm -rf "$OUT"
mkdir -p "$OUT"

echo "Building pagebox v$VERSION across all targets into $OUT/..."

for t in $TARGETS; do
  os=${t%/*}
  arch=${t#*/}
  ext=""
  [ "$os" = "windows" ] && ext=".exe"
  name="pagebox-$VERSION-$os-$arch$ext"
  printf '  %-36s' "$name"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$OUT/$name" ./cmd/pagebox
  printf '%s\n' "$(du -h "$OUT/$name" | cut -f1)"
done

echo "Generating sha256 checksums..."
(
  cd "$OUT"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum pagebox-* > checksums.txt
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 pagebox-* > checksums.txt
  fi
)

echo "✓ Successfully built $(ls -1 "$OUT"/pagebox-* | wc -l | tr -d ' ') binaries in $OUT/ (with checksums.txt)"
