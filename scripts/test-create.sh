#!/usr/bin/env bash

set -euo pipefail
set -x

FCREPO_STATE="${1:?usage: ./scripts/test-create.sh <fcrepo-on|off> <public|private> [blazegraph-on|off] }"
ISLE_FILE_SYSTEM_URI="${2:?usage: ./scripts/test-create.sh <fcrepo-on|off> <public|private> [blazegraph-on|off] }"
BLAZEGRAPH_STATE="${3:-on}"
GIT_REMOTE_URL="${GIT_REMOTE_URL:-}"
SITECTL_CONTEXT="${SITECTL_CONTEXT:-integration-test}"

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." &>/dev/null && pwd)"
if [ -n "${SITECTL_TMP_PARENT:-}" ]; then
	TMP_PARENT="${SITECTL_TMP_PARENT}"
elif [ -n "${GITHUB_WORKSPACE:-}" ]; then
	TMP_PARENT="${GITHUB_WORKSPACE}"
else
	TMP_PARENT="${HOME}/.tmp"
fi
mkdir -p "${TMP_PARENT}"
TMP_DIR="$(mktemp -d "${TMP_PARENT%/}/sitectl-isle-test.XXXXXX")"
SITE_DIR="${TMP_DIR}/isle-site-template"
PLUGIN_BIN="${TMP_DIR}/sitectl-isle"

cleanup() {
	if [ -d "${SITE_DIR}" ]; then
		(
			cd "${SITE_DIR}" &&
				docker compose down -v --remove-orphans >/dev/null 2>&1 || true
		)
	fi
	rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

(
	cd "${REPO_ROOT}" &&
		go build -o "${PLUGIN_BIN}" .
)

"${PLUGIN_BIN}" create \
	--path "${SITE_DIR}" \
	--context "${SITECTL_CONTEXT}" \
	--fcrepo "${FCREPO_STATE}" \
	--blazegraph "${BLAZEGRAPH_STATE}" \
	--isle-file-system-uri "${ISLE_FILE_SYSTEM_URI}" \
	--git-remote-url "${GIT_REMOTE_URL}"

count_files() {
	local service="$1"
	local target="$2"
	(
		cd "${SITE_DIR}" &&
			docker compose exec -T "${service}" sh -lc "find ${target} -type f 2>/dev/null | wc -l"
	)
}

wait_for_growth() {
	local service="$1"
	local target="$2"
	local before="$3"
	for _ in $(seq 1 24); do
		sleep 10
		local after
		after="$(count_files "${service}" "${target}" | tr -d '[:space:]')"
		if [ "${after:-0}" -gt "${before:-0}" ]; then
			return 0
		fi
	done

	return 1
}

drupal_sql_query() {
	local query="$1"
	(
		cd "${SITE_DIR}" &&
			docker compose exec -T drupal sh -lc "drush --root=/var/www/drupal sql:query --extra=--skip-column-names \"${query}\""
	)
}

if [ "${FCREPO_STATE}" = "on" ]; then
	ASSERT_SERVICE="fcrepo"
	ASSERT_PATH="/data"
else
	ASSERT_SERVICE="drupal"
	case "${ISLE_FILE_SYSTEM_URI}" in
	public)
		ASSERT_PATH="/var/www/drupal/web/sites/default/files"
		;;
	private)
		ASSERT_PATH="/var/www/drupal/private"
		;;
	*)
		echo "unexpected isle-file-system-uri: ${ISLE_FILE_SYSTEM_URI}" >&2
		exit 1
		;;
	esac
fi

(
	cd "${SITE_DIR}" &&
		make up
)

BEFORE_COUNT="$(count_files "${ASSERT_SERVICE}" "${ASSERT_PATH}" | tr -d '[:space:]')"

(
	cd "${SITE_DIR}" &&
		make demo-objects
)

if ! wait_for_growth "${ASSERT_SERVICE}" "${ASSERT_PATH}" "${BEFORE_COUNT}"; then
	echo "expected ingested content to appear in ${ASSERT_SERVICE}:${ASSERT_PATH}" >&2
	exit 1
fi

if [ "${FCREPO_STATE}" = "off" ]; then
	if (
		cd "${SITE_DIR}" &&
			docker compose config --services | grep -qx "fcrepo"
	); then
		echo "fcrepo service still present after create --fcrepo off" >&2
		exit 1
	fi

	FEDORA_COUNT="$(drupal_sql_query "SELECT COUNT(*) FROM file_managed WHERE uri LIKE 'fedora%';" | tr -d '[:space:]')"
	if [ "${FEDORA_COUNT:-0}" != "0" ]; then
		echo "expected no fedora-backed file_managed URIs when fcrepo is off, got ${FEDORA_COUNT}" >&2
		exit 1
	fi
fi

if [ "${BLAZEGRAPH_STATE}" = "off" ]; then
	if (
		cd "${SITE_DIR}" &&
			docker compose config --services | grep -qx "blazegraph"
	); then
		echo "blazegraph service still present after create --blazegraph off" >&2
		exit 1
	fi
fi
