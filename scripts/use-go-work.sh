#!/usr/bin/env bash

set -euo pipefail

SITECTL_PATH="${1:-../sitectl}"
SITECTL_GOMOD="${SITECTL_PATH}/go.mod"

if [[ ! -f "${SITECTL_GOMOD}" ]]; then
	rm -f go.work
	echo "Skipping go.work; local sitectl checkout not found at ${SITECTL_PATH}"
	exit 0
fi

cat > go.work <<EOF
$(grep -E "^go (\d|\.)+$" go.mod)

use (
    .
    ${SITECTL_PATH}
)
EOF

echo "Wrote go.work using local sitectl checkout at ${SITECTL_PATH}"
