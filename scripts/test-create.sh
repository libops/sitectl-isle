#!/usr/bin/env bash

set -euo pipefail
set -x

export TERM="${TERM:-dumb}"

FCREPO_STATE="${1:?usage: ./scripts/test-create.sh <fcrepo-on|off> <public|private> [blazegraph-on|off] [cantaloupe|triplet] [disabled|distributed] [bot-mitigation-on|off] [nested|git-root] }"
ISLE_FILE_SYSTEM_URI="${2:?usage: ./scripts/test-create.sh <fcrepo-on|off> <public|private> [blazegraph-on|off] [cantaloupe|triplet] [disabled|distributed] [bot-mitigation-on|off] [nested|git-root] }"
BLAZEGRAPH_STATE="${3:-on}"
IIIF_IMPLEMENTATION="${4:-triplet}"
IIIF_TOPOLOGY="${5:-disabled}"
BOT_MITIGATION_STATE="${6:-off}"
CODEBASE_LAYOUT="${7:-nested}"
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
SITECTL_HOME="${TMP_DIR}/home"
BIN_DIR="${TMP_DIR}/bin"
SITE_DIR="${TMP_DIR}/isle-site-template"
PLUGIN_BIN="${BIN_DIR}/sitectl-isle"
PATH="${BIN_DIR}:${PATH}"
export PATH
mkdir -p "${SITECTL_HOME}"

remove_tmp_dir() {
	if [ ! -d "${TMP_DIR}" ]; then
		return
	fi
	chmod -R u+rwX "${TMP_DIR}" 2>/dev/null || true
	if rm -rf "${TMP_DIR}" 2>/dev/null; then
		return
	fi
	if command -v sudo >/dev/null 2>&1; then
		sudo chown -R "$(id -u):$(id -g)" "${TMP_DIR}" 2>/dev/null || true
		sudo chmod -R u+rwX "${TMP_DIR}" 2>/dev/null || true
	fi
	rm -rf "${TMP_DIR}"
}

cleanup() {
	if [ -d "${SITE_DIR}" ]; then
		HOME="${SITECTL_HOME}" sitectl compose down -v --remove-orphans >/dev/null 2>&1 || true
	fi
	remove_tmp_dir
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
	HOME="${SITECTL_HOME}" sitectl create isle \
		--path "${SITE_DIR}" \
		--type local \
		--checkout-source template \
		--default-context \
		--fcrepo "${FCREPO_STATE}" \
		--blazegraph "${BLAZEGRAPH_STATE}" \
		--iiif "${IIIF_IMPLEMENTATION}" \
		--iiif-topology "${IIIF_TOPOLOGY}" \
		--codebase "${CODEBASE_LAYOUT}" \
		--bot-mitigation "${BOT_MITIGATION_STATE}" \
		--isle-file-system-uri "${ISLE_FILE_SYSTEM_URI}" \
		--setup-only
}

verify_codebase_layout() {
	case "${CODEBASE_LAYOUT}" in
	git-root)
		for path in Dockerfile composer.json composer.lock config/sync web/modules/custom web/themes/custom; do
			if [ ! -e "${SITE_DIR}/${path}" ]; then
				echo "expected git-root codebase path ${path}" >&2
				exit 1
			fi
		done
		if [ -e "${SITE_DIR}/drupal/Dockerfile" ]; then
			echo "drupal/Dockerfile still exists after create --codebase git-root" >&2
			exit 1
		fi
		if ! grep -Eq '^[[:space:]]+context: \.?$' "${SITE_DIR}/docker-compose.yml"; then
			echo "drupal build context was not updated to git root" >&2
			exit 1
		fi
		;;
	nested)
		if [ ! -e "${SITE_DIR}/drupal/Dockerfile" ] || [ ! -e "${SITE_DIR}/drupal/rootfs/var/www/drupal/composer.json" ]; then
			echo "expected upstream nested Drupal codebase layout" >&2
			exit 1
		fi
		;;
	*)
		echo "unexpected codebase layout: ${CODEBASE_LAYOUT}" >&2
		exit 1
		;;
	esac
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

run_healthcheck() {
	HOME="${SITECTL_HOME}" sitectl healthcheck
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

verify_iiif_implementation() {
	if [ "${IIIF_TOPOLOGY}" = "distributed" ]; then
		echo "integration test does not currently assert distributed IIIF topology" >&2
		exit 1
	fi

	case "${IIIF_IMPLEMENTATION}" in
	triplet)
		if ! (
			cd "${SITE_DIR}" &&
				docker compose config --services | grep -qx "triplet"
		); then
			echo "triplet service missing after create --iiif triplet" >&2
			exit 1
		fi
		if (
			cd "${SITE_DIR}" &&
				docker compose config --services | grep -qx "cantaloupe"
		); then
			echo "cantaloupe service still present after create --iiif triplet" >&2
			exit 1
		fi
		if ! grep -Fq "DRUPAL_DEFAULT_CANTALOUPE_URL: \"\${URI_SCHEME}://\${DOMAIN}/iiif/3\"" "${SITE_DIR}/docker-compose.yml"; then
			echo "Drupal IIIF URL was not updated to /iiif/3 for triplet" >&2
			exit 1
		fi
		if [ ! -f "${SITE_DIR}/conf/triplet/config.yaml" ]; then
			echo "Triplet config was not written" >&2
			exit 1
		fi
		;;
	cantaloupe)
		if ! (
			cd "${SITE_DIR}" &&
				docker compose config --services | grep -qx "cantaloupe"
		); then
			echo "cantaloupe service missing after create --iiif cantaloupe" >&2
			exit 1
		fi
		if (
			cd "${SITE_DIR}" &&
				docker compose config --services | grep -qx "triplet"
		); then
			echo "triplet service present after create --iiif cantaloupe" >&2
			exit 1
		fi
		;;
	*)
		echo "unexpected iiif implementation: ${IIIF_IMPLEMENTATION}" >&2
		exit 1
		;;
	esac
}

verify_bot_mitigation_challenge() {
	if [ "${BOT_MITIGATION_STATE}" != "on" ]; then
		return
	fi

	local status
	local body_path="${TMP_DIR}/bot-mitigation-challenge.html"
	for _ in $(seq 1 24); do
		status="$(
			curl \
				--silent \
				--show-error \
				--noproxy "*" \
				--output "${body_path}" \
				--write-out "%{http_code}" \
				--header "X-Forwarded-For: 1.2.3.4" \
				--resolve islandora.io:80:127.0.0.1 \
				http://islandora.io/ || true
		)"
		if [ "${status}" = "429" ]; then
			if ! grep -Fq "Verifying connection" "${body_path}"; then
				echo "bot mitigation returned 429 without the challenge page" >&2
				cat "${body_path}" >&2 || true
				exit 1
			fi
			return
		fi
		sleep 5
	done

	echo "expected bot mitigation to return 429 for X-Forwarded-For: 1.2.3.4, got ${status}" >&2
	cat "${body_path}" >&2 || true
	run_compose_diagnostics
	exit 1
}

main() {
	build_binaries
	create_site
	verify_codebase_layout
	verify_iiif_implementation
	set_assert_target
	run_make_target init
	run_make_target up
	run_healthcheck
	verify_bot_mitigation_challenge
	verify_demo_objects_created

	if [ "${FCREPO_STATE}" = "off" ]; then
		verify_fcrepo_disabled
	fi

	if [ "${BLAZEGRAPH_STATE}" = "off" ]; then
		verify_blazegraph_disabled
	fi
}

main "$@"
