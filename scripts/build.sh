#!/bin/sh
set -e
cd "$(dirname "$0")/.."
if [ "$(uname)" = Darwin ]; then
	export MACOSX_DEPLOYMENT_TARGET=15.0
	export CGO_CFLAGS="${CGO_CFLAGS:--mmacosx-version-min=15.0}"
	export CGO_LDFLAGS="${CGO_LDFLAGS:--mmacosx-version-min=15.0}"
fi
pnpm --dir frontend build
GOENV=./go.env go build -o ./bin/GrokMcp ./cmd/grokmcp
echo "built bin/GrokMcp (darwin $(go env GOARCH))"
echo "Windows installer: run this script on windows/amd64 with WebView2."
