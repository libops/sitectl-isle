#!/usr/bin/env bash

set -euo pipefail

SITECTL_PATH="${1:-../sitectl}"
SERVICE_MODULES=()
SITECTL_GOMOD="${SITECTL_PATH}/go.mod"

if [[ ! -f "${SITECTL_GOMOD}" ]]; then
	rm -f go.work
	echo "Skipping go.work; local sitectl checkout not found at ${SITECTL_PATH}"
	exit 0
fi

WORK_USES=(
	"."
	"${SITECTL_PATH}"
)
for module in "${SERVICE_MODULES[@]}"; do
	if [[ -f "${module}/go.mod" ]]; then
		WORK_USES+=("${module}")
	fi
done

GO_LINE="$(grep -E '^go [0-9]+([.][0-9]+)*$' go.mod || true)"
if [[ -z "${GO_LINE}" ]]; then
	echo "Unable to read Go directive from go.mod"
	exit 1
fi
{
	printf '%s\n\n' "${GO_LINE}"
	printf 'use (\n'
	for module in "${WORK_USES[@]}"; do
		printf '    %s\n' "${module}"
	done
	printf ')\n'
} > go.work

echo "Wrote go.work using local sitectl checkout at ${SITECTL_PATH}"
