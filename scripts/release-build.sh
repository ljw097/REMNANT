#!/usr/bin/env bash
# Builds release archives and SHA256SUMS into $DIST (default dist/).
#
#   scripts/release-build.sh v1.2.3 [os/arch ...]     default: linux/amd64 linux/arm64
#
# Builds are reproducible: static (CGO_ENABLED=0), -trimpath, and archives
# with fixed ordering, ownership and timestamps. The web UI is built first
# (needs Node.js and npm) and embedded in remnantd. EXTRA_LDFLAGS is appended
# to the link flags (tests use it to trust a test signing key).
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=${1:?usage: release-build.sh vX.Y.Z [os/arch ...]}
shift
if ! [[ $VERSION =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "release-build: $VERSION is not vX.Y.Z" >&2
  exit 2
fi
PLATFORMS=${*:-linux/amd64 linux/arm64}
COMMIT=$(git rev-parse --short=12 HEAD)
EPOCH=$(git log -1 --format=%ct)
DIST=${DIST:-dist}
PKG=github.com/ljw097/remnant/internal/buildinfo
LDFLAGS="-s -w -X $PKG.Version=$VERSION -X $PKG.Commit=$COMMIT ${EXTRA_LDFLAGS:-}"

# The web UI is embedded in remnantd; build it from the lockfile.
(cd web && npm ci --no-audit --no-fund && npm run build) >&2

rm -rf "$DIST"
mkdir -p "$DIST"
for p in $PLATFORMS; do
  os=${p%/*} arch=${p#*/}
  name=remnant_${VERSION}_${os}_${arch}
  mkdir -p "$DIST/$name"
  for cmd in remnantd remnant-worker; do
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/$name/$cmd" "./cmd/$cmd"
  done
  cp LICENSE README.md "$DIST/$name/"
  tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$EPOCH" -C "$DIST" -cf - "$name" | gzip -n -9 >"$DIST/$name.tar.gz"
  rm -rf "${DIST:?}/$name"
done
(cd "$DIST" && sha256sum -- *.tar.gz >SHA256SUMS)
echo "built $VERSION ($COMMIT) for $PLATFORMS in $DIST/"
