#!/usr/bin/env bash

set -euo pipefail

SITECTL_PATH="${1:-../}"

cat > go.work <<'EOF'
go 1.25.3

use (
	.
	__SITECTL_PATH__
)
EOF

perl -0pi -e 's#__SITECTL_PATH__#'"${SITECTL_PATH}"'#g' go.work

echo "Wrote go.work using local sitectl checkout at ${SITECTL_PATH}"
