#!/bin/sh
set -e
cd "$(dirname "$0")/.."

TARGET="${1:-all}"
VERSION="${VERSION:-${GITHUB_REF_NAME:-}}"
VERSION="${VERSION#v}"
if [ -z "$VERSION" ]; then
	VERSION="0.1.0"
fi

APP_NAME="Grok Supervisor"
BIN_NAME="GrokMcp"
DIST_DIR="./dist"
FRONTEND_OK=0

if [ -z "${CI:-}" ] && [ -f ./go.env ]; then
	export GOENV=./go.env
fi

usage() {
	echo "usage: $0 [all|darwin|windows]" >&2
	exit 2
}

case "$TARGET" in
all|darwin|windows) ;;
*) usage ;;
esac

need_darwin() {
	[ "$TARGET" = "all" ] || [ "$TARGET" = "darwin" ]
}

need_windows() {
	[ "$TARGET" = "all" ] || [ "$TARGET" = "windows" ]
}

if need_darwin && [ "$(uname)" != "Darwin" ]; then
	echo "darwin packages require macOS (hdiutil/codesign)" >&2
	exit 1
fi

build_frontend() {
	if [ "$FRONTEND_OK" = "1" ]; then
		return 0
	fi
	pnpm --dir frontend build
	FRONTEND_OK=1
}

stage_clean() {
	rm -rf "$DIST_DIR"
	mkdir -p "$DIST_DIR"
}

build_darwin() {
	arch="$1"
	outdir="$DIST_DIR/darwin-$arch"
	mkdir -p "$outdir"
	export CGO_ENABLED=1
	export GOOS=darwin
	export GOARCH="$arch"
	export MACOSX_DEPLOYMENT_TARGET=15.0
	export CGO_CFLAGS="${CGO_CFLAGS:--mmacosx-version-min=15.0}"
	export CGO_LDFLAGS="${CGO_LDFLAGS:--mmacosx-version-min=15.0}"
	go build -trimpath -ldflags="-s -w -X grokmcp/internal/version.Version=${VERSION}" -o "$outdir/$BIN_NAME" ./cmd/grokmcp

	appdir="$outdir/$APP_NAME.app"
	rm -rf "$appdir"
	mkdir -p "$appdir/Contents/MacOS" "$appdir/Contents/Resources"
	cp "$outdir/$BIN_NAME" "$appdir/Contents/MacOS/$BIN_NAME"
	sed "s/__VERSION__/${VERSION}/g" build/darwin/Info.plist > "$appdir/Contents/Info.plist"
	cp build/darwin/icons.icns "$appdir/Contents/Resources/icons.icns"
	codesign --force --deep --sign - "$appdir"

	zipfile="$DIST_DIR/${BIN_NAME}-${VERSION}-darwin-${arch}.zip"
	rm -f "$zipfile"
	ditto -c -k --keepParent "$appdir" "$zipfile"
	echo "wrote $zipfile"

	stage="$DIST_DIR/dmg-$arch"
	rm -rf "$stage"
	mkdir -p "$stage"
	cp -R "$appdir" "$stage/"
	ln -s /Applications "$stage/Applications"
	cp "build/darwin/打开说明.txt" "$stage/"
	cp "build/darwin/清除隔离属性.command" "$stage/"
	chmod 755 "$stage/清除隔离属性.command"

	dmg="$DIST_DIR/${BIN_NAME}-${VERSION}-darwin-${arch}.dmg"
	rm -f "$dmg"
	hdiutil create -quiet -volname "$APP_NAME $arch" -srcfolder "$stage" -ov -format UDZO "$dmg"
	rm -rf "$stage"
	echo "wrote $dmg"
}

build_windows() {
	outdir="$DIST_DIR/windows-amd64"
	mkdir -p "$outdir"
	export GOOS=windows
	export GOARCH=amd64
	export CGO_ENABLED=0
	unset MACOSX_DEPLOYMENT_TARGET CGO_CFLAGS CGO_LDFLAGS
	go build -trimpath -ldflags="-s -w -H windowsgui -X grokmcp/internal/version.Version=${VERSION}" -o "$outdir/$BIN_NAME.exe" ./cmd/grokmcp

	zipfile="$DIST_DIR/${BIN_NAME}-${VERSION}-windows-amd64.zip"
	rm -f "$zipfile"
	if command -v zip >/dev/null 2>&1; then
		zip -X -j -q "$zipfile" "$outdir/$BIN_NAME.exe"
	else
		py=python3
		command -v python3 >/dev/null 2>&1 || py=python
		"$py" - "$outdir/$BIN_NAME.exe" "$zipfile" <<'PY'
import sys, zipfile
src, dest = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(dest, "w", compression=zipfile.ZIP_DEFLATED) as zf:
    zf.write(src, arcname="GrokMcp.exe")
PY
	fi
	echo "wrote $zipfile"

	installer="$DIST_DIR/${BIN_NAME}-${VERSION}-windows-amd64.exe"
	rm -f "$installer"
	iscc="${ISCC:-iscc}"
	command -v "$iscc" >/dev/null 2>&1 || {
		echo "Inno Setup compiler not found: $iscc" >&2
		exit 1
	}
	"$iscc" "/DMyAppVersion=$VERSION" "/DSourceDir=$outdir" "/DOutputDir=$DIST_DIR" "build/windows/GrokMcp.iss"
	[ -f "$installer" ] || {
		echo "Inno Setup did not create $installer" >&2
		exit 1
	}
	echo "wrote $installer"
}

stage_clean
build_frontend
if need_darwin; then
	build_darwin amd64
	build_darwin arm64
fi
if need_windows; then
	build_windows
fi
