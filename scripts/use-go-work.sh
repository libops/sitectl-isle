#!/usr/bin/env bash

set -euo pipefail

SITECTL_PATH="${1:-../sitectl}"

cat > go.work <<EOF
go 1.25.3

use (
    .
    ${SITECTL_PATH}
)
EOF

echo "Wrote go.work using local sitectl checkout at ${SITECTL_PATH}"
