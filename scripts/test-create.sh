#!/usr/bin/env bash

set -euo pipefail
set -x

export TERM="${TERM:-dumb}"

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
BIN_DIR="${TMP_DIR}/bin"
SITE_DIR="${TMP_DIR}/isle-site-template"
PLUGIN_BIN="${BIN_DIR}/sitectl-isle"
PATH="${BIN_DIR}:${PATH}"
export PATH

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

run_compose_diagnostics() {
	(
		cd "${SITE_DIR}" &&
			docker compose config || true
	)
	(
		cd "${SITE_DIR}" &&
			docker compose config --services || true
	)
	(
		cd "${SITE_DIR}" &&
			docker compose ps -a || true
	)
	(
		cd "${SITE_DIR}" &&
			docker compose run --rm init || true
	)
}

build_binaries() {
	mkdir -p "${BIN_DIR}"
	(
		cd "${REPO_ROOT}" &&
			go build -o "${PLUGIN_BIN}" .
	)
}

create_site() {
	sitectl create isle \
		--path "${SITE_DIR}" \
		--context "${SITECTL_CONTEXT}" \
		--type local \
		--checkout-source template \
		--fcrepo "${FCREPO_STATE}" \
		--blazegraph "${BLAZEGRAPH_STATE}" \
		--isle-file-system-uri "${ISLE_FILE_SYSTEM_URI}" \
		--setup-only
}

set_assert_target() {
	if [ "${FCREPO_STATE}" = "on" ]; then
		ASSERT_SERVICE="fcrepo"
		ASSERT_PATH="/data"
		return
	fi

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
}

run_make_target() {
	local target="$1"
	if ! (
		cd "${SITE_DIR}" &&
			make "${target}"
	); then
		run_compose_diagnostics
		exit 1
	fi
}

verify_demo_objects_created() {
	local before_count
	before_count="$(count_files "${ASSERT_SERVICE}" "${ASSERT_PATH}" | tr -d '[:space:]')"

	(
		cd "${SITE_DIR}" &&
			make demo-objects
	)

	if ! wait_for_growth "${ASSERT_SERVICE}" "${ASSERT_PATH}" "${before_count}"; then
		echo "expected ingested content to appear in ${ASSERT_SERVICE}:${ASSERT_PATH}" >&2
		exit 1
	fi
}

verify_fcrepo_disabled() {
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
}

verify_blazegraph_disabled() {
	if (
		cd "${SITE_DIR}" &&
			docker compose config --services | grep -qx "blazegraph"
	); then
		echo "blazegraph service still present after create --blazegraph off" >&2
		exit 1
	fi
}

main() {
	build_binaries
	create_site
	set_assert_target
	run_make_target init
	run_make_target up
	verify_demo_objects_created

	if [ "${FCREPO_STATE}" = "off" ]; then
		verify_fcrepo_disabled
	fi

	if [ "${BLAZEGRAPH_STATE}" = "off" ]; then
		verify_blazegraph_disabled
	fi
}

main "$@"
