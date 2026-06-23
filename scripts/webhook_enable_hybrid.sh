#!/usr/bin/env bash
# webhook_enable_hybrid.sh — enable hybrid ingestion (pumpfun-amm webhook, rest stream).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ROOT}/.env"
CHAINS_YAML="${ROOT}/shared/config/chains.yaml"
PUMPFUN_AMM_ID="pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"

bash "${ROOT}/scripts/ensure_env.sh"

# shellcheck disable=SC1090
set -a
source "$ENV_FILE"
set +a

set_env_key() {
	local key="$1"
	local value="$2"
	local tmp
	tmp="$(mktemp)"
	if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
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

set_env_key SOLANA_INGESTION_DELIVERY hybrid
set_env_key WEBHOOK_EXPOSURE "${WEBHOOK_EXPOSURE:-none}"

# Re-run ensure_env to generate HELIUS_WEBHOOK_SECRET if needed.
bash "${ROOT}/scripts/ensure_env.sh"

python3 - "$CHAINS_YAML" "$PUMPFUN_AMM_ID" <<'PY'
import re
import sys

path, pump_id = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as f:
    text = f.read()

orig = text

# ingestion.delivery: stream -> hybrid (first occurrence under ingestion block)
text = re.sub(
    r"(^\s*ingestion:\s*\n\s*delivery:\s*)stream\s*$",
    r"\1hybrid",
    text,
    count=1,
    flags=re.MULTILINE,
)

# webhook.enabled: false -> true (under ingestion.webhook)
text = re.sub(
    r"(^\s*webhook:\s*\n(?:\s+.+\n)*?\s*enabled:\s*)false\s*$",
    r"\1true",
    text,
    count=1,
    flags=re.MULTILINE,
)

# Add delivery: webhook under pumpfun-amm program if missing.
block_pat = (
    rf'(\s+- program_id: "{re.escape(pump_id)}"\s*\n'
    rf'\s+family: pumpfun-amm)(\s*\n)'
)
if "delivery: webhook" not in text or not re.search(
    rf'program_id: "{re.escape(pump_id)}".*?delivery: webhook', text, re.DOTALL
):
    def add_delivery(m):
        body = m.group(0)
        if "delivery:" in body:
            return body
        return m.group(1) + "\n      delivery: webhook" + m.group(2)

    text, n = re.subn(block_pat, add_delivery, text, count=1)
    if n == 0:
        print(f"WARN: could not find pumpfun-amm program block for {pump_id}", file=sys.stderr)

if text != orig:
    with open(path, "w", encoding="utf-8") as f:
        f.write(text)
    print(f"Patched {path} for hybrid webhook (pumpfun-amm)")
else:
    print(f"No changes needed in {path} (already hybrid/webhook-ready)")

PY

echo ""
echo "Hybrid webhook ingestion enabled."
echo "  SOLANA_INGESTION_DELIVERY=hybrid"
echo "  chains.yaml: ingestion.webhook.enabled=true, pumpfun-amm delivery=webhook"
echo ""
echo "Next — pick one exposure mode:"
echo "  make webhook-dev          # ngrok (local/staging)"
echo "  make webhook-cloudflare   # Cloudflare Tunnel (static IP, no open ports)"
echo "  make webhook-production   # Caddy + domain on static IP"
echo ""
echo "Then configure Helius dashboard:"
echo "  URL: <your-https-host>/webhooks/helius"
echo "  Authorization header: value of HELIUS_WEBHOOK_SECRET in .env"
echo "  Account filter: ${PUMPFUN_AMM_ID}"
echo ""
echo "Verify: make webhook-verify WEBHOOK_PUBLIC_URL=https://..."
