#!/usr/bin/env bash

set -euo pipefail

if ! command -v golangci-lint >/dev/null 2>&1; then
	echo "golangci-lint is required for ./scripts/check.sh"
	echo "Install it locally or run the same script in GitHub Actions."
	exit 1
fi

make lint
make test
