#!/usr/bin/env bash
# Unit tests and static checks. Requires only a Go toolchain.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "### gofmt"
unformatted=$(gofmt -s -l .)
if [ -n "$unformatted" ]; then
	echo "not gofmt-clean:" >&2
	echo "$unformatted" >&2
	exit 1
fi

echo "### go vet"
go vet ./...

echo "### go test"
go test -race -cover ./...
