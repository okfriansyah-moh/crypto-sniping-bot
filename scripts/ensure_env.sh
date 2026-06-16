#!/usr/bin/env bash
# ensure_env.sh — create/patch .env for local operator workflow (make start, dashboard-dev).
# Generates DASHBOARD_API_KEY and SNIPER_DB_PASSWORD when missing or still placeholders.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT}/.env"
EXAMPLE="${ROOT}/.env.example"

gen_secret() {
	if command -v openssl >/dev/null 2>&1; then
		openssl rand -hex 32
	else
		# fallback: hex from urandom
		LC_ALL=C tr -dc 'a-f0-9' </dev/urandom | head -c 64
	fi
}

is_placeholder() {
	case "${1:-}" in
	"" | change_me_* | CHANGE_ME* | your_* | YOUR_* ) return 0 ;;
	esac
	return 1
}

set_env_key() {
	local key="$1"
	local value="$2"
	local tmp
	tmp="$(mktemp)"
	if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
		# Replace line (value must not contain newlines)
		awk -v k="$key" -v v="$value" '
			BEGIN { FS=OFS="=" }
			$1 == k { print k, v; next }
			{ print }
		' "$ENV_FILE" >"$tmp"
	else
		cp "$ENV_FILE" "$tmp"
		printf '\n%s=%s\n' "$key" "$value" >>"$tmp"
	fi
	mv "$tmp" "$ENV_FILE"
}

if [[ ! -f "$ENV_FILE" ]]; then
	if [[ ! -f "$EXAMPLE" ]]; then
		echo "ERROR: missing .env and .env.example — cannot bootstrap environment" >&2
		exit 1
	fi
	cp "$EXAMPLE" "$ENV_FILE"
	echo "Created ${ENV_FILE} from .env.example"
fi

# shellcheck disable=SC1090
set -a
# shellcheck source=/dev/null
source "$ENV_FILE"
set +a

patched=0

if is_placeholder "${SNIPER_DB_PASSWORD:-}"; then
	new_pw="$(gen_secret)"
	set_env_key SNIPER_DB_PASSWORD "$new_pw"
	SNIPER_DB_PASSWORD="$new_pw"
	patched=1
	echo "Generated SNIPER_DB_PASSWORD in .env"
fi

if is_placeholder "${DASHBOARD_API_KEY:-}"; then
	new_key="$(gen_secret)"
	set_env_key DASHBOARD_API_KEY "$new_key"
	DASHBOARD_API_KEY="$new_key"
	patched=1
	echo "Generated DASHBOARD_API_KEY in .env"
fi

if [[ -z "${DASHBOARD_ALLOWED_OPERATORS:-}" ]]; then
	set_env_key DASHBOARD_ALLOWED_OPERATORS "local-operator"
	DASHBOARD_ALLOWED_OPERATORS="local-operator"
	patched=1
	echo "Set DASHBOARD_ALLOWED_OPERATORS=local-operator in .env"
fi

if [[ "$patched" -eq 1 ]]; then
	echo "Review ${ENV_FILE} — add RPC keys and wallets before production use."
	set -a
	# shellcheck source=/dev/null
	source "$ENV_FILE"
	set +a
fi

export SNIPER_DB_PASSWORD DASHBOARD_API_KEY DASHBOARD_ALLOWED_OPERATORS
