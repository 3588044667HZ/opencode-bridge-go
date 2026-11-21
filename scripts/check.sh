#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

unformatted="$(gofmt -l .)"
if [[ -n "$unformatted" ]]; then
    echo "Unformatted Go files:" >&2
    echo "$unformatted" >&2
    exit 1
fi

go build ./...
go vet ./...
go test -v ./... -count=1
