#!/usr/bin/env bash

set -euo pipefail

DB_HOST="${DB_HOST:-mariadb}"
DB_PORT="${DB_PORT:-3306}"
DB_ROOT_USER="${DB_ROOT_USER:-root}"
DB_CHARACTER_SET="${DB_CHARACTER_SET:-utf8mb4}"
DB_COLLATION="${DB_COLLATION:-utf8mb4_unicode_ci}"
DB_ROOT_PASSWORD_FILE="${DB_ROOT_PASSWORD_FILE:-/run/secrets/DB_ROOT_PASSWORD}"
DB_PASSWORD_FILE="${DB_PASSWORD_FILE:-/run/secrets/DB_PASSWORD}"
readonly DB_HOST DB_PORT DB_ROOT_USER DB_CHARACTER_SET DB_COLLATION
readonly DB_ROOT_PASSWORD_FILE DB_PASSWORD_FILE

: "${DB_NAME:?DB_NAME is required}"
: "${DB_USER:?DB_USER is required}"

validate_identifier() {
  local name="$1"
  local value="$2"
  if [[ ! "${value}" =~ ^[A-Za-z0-9_]+$ ]]; then
    echo "${name} must contain only letters, numbers, and underscores" >&2
    exit 1
  fi
}

read_secret() {
  local name="$1"
  local path="$2"
  local value
  if [ ! -s "${path}" ]; then
    echo "${name} secret is missing or empty at ${path}" >&2
    exit 1
  fi
  value="$(cat -- "${path}")"
  if [ -z "${value}" ] || [[ "${value}" == *$'\n'* ]] || [[ "${value}" == *$'\r'* ]]; then
    echo "${name} must be a non-empty single-line secret" >&2
    exit 1
  fi
  printf '%s' "${value}"
}

escape_option_value() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "${value}"
}

escape_sql_literal() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\'/\'\'}"
  printf '%s' "${value}"
}

validate_identifier DB_ROOT_USER "${DB_ROOT_USER}"
validate_identifier DB_NAME "${DB_NAME}"
validate_identifier DB_USER "${DB_USER}"
validate_identifier DB_CHARACTER_SET "${DB_CHARACTER_SET}"
validate_identifier DB_COLLATION "${DB_COLLATION}"
if [[ ! "${DB_HOST}" =~ ^[A-Za-z0-9._:-]+$ ]]; then
  echo "DB_HOST contains unsupported characters" >&2
  exit 1
fi
if [[ ! "${DB_PORT}" =~ ^[0-9]+$ ]] || [ "${DB_PORT}" -lt 1 ] || [ "${DB_PORT}" -gt 65535 ]; then
  echo "DB_PORT must be an integer from 1 through 65535" >&2
  exit 1
fi

root_password="$(read_secret DB_ROOT_PASSWORD "${DB_ROOT_PASSWORD_FILE}")"
db_password="$(read_secret DB_PASSWORD "${DB_PASSWORD_FILE}")"
root_password_option="$(escape_option_value "${root_password}")"
db_user_sql="$(escape_sql_literal "${DB_USER}")"
db_password_sql="$(escape_sql_literal "${db_password}")"
readonly root_password db_password root_password_option db_user_sql db_password_sql

credentials_dir="$(mktemp -d)"
credentials_file="${credentials_dir}/client.cnf"
cleanup() {
  rm -rf "${credentials_dir}"
}
trap cleanup EXIT
umask 077
cat >"${credentials_file}" <<EOF
[client]
host=${DB_HOST}
port=${DB_PORT}
protocol=tcp
user=${DB_ROOT_USER}
password="${root_password_option}"
EOF
chmod 0600 "${credentials_file}"

database_ready=false
for ((attempt = 1; attempt <= 60; attempt++)); do
  if mariadb --defaults-extra-file="${credentials_file}" --batch --skip-column-names -e 'SELECT 1' >/dev/null 2>&1; then
    database_ready=true
    break
  fi
  sleep 2
done
if [ "${database_ready}" != true ]; then
  echo "MariaDB root access was not ready after 120 seconds" >&2
  exit 1
fi

mariadb --defaults-extra-file="${credentials_file}" <<SQL
CREATE DATABASE IF NOT EXISTS \`${DB_NAME}\` CHARACTER SET ${DB_CHARACTER_SET} COLLATE ${DB_COLLATION};
CREATE USER IF NOT EXISTS '${db_user_sql}'@'%' IDENTIFIED BY '${db_password_sql}';
ALTER USER '${db_user_sql}'@'%' IDENTIFIED BY '${db_password_sql}';
GRANT ALL PRIVILEGES ON \`${DB_NAME}\`.* TO '${db_user_sql}'@'%';
FLUSH PRIVILEGES;
SQL

echo "Database ${DB_NAME} and scoped user ${DB_USER} are ready."
