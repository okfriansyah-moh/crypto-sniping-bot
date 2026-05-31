#!/usr/bin/env python3
"""
inject_test_token.py — End-to-end pipeline validation (Task 19, Production Gate
Hardening Plan).

Injects a synthetic `market_data_event` with all probe-derived quality flags
pre-approved (bypassing L1 DQ) into the PostgreSQL event bus so downstream layers
L2–L10 can be exercised in a replay-worker scope without touching production state.

Usage:
    python scripts/inject_test_token.py \\
        --chain solana \\
        --token <TOKEN_ADDRESS> \\
        [--symbol TOKEN] [--name "Token Name"] \\
        [--creator <CREATOR_WALLET>] [--pool <POOL_ADDRESS>] \\
        [--market raydium-v4] \\
        [--dry-run]

Environment:
    DATABASE_URL  — PostgreSQL DSN (required).
                    Example: postgres://user:pass@localhost:5432/crypto_sniper

Replay isolation:
    The injected event carries event_id = "replay:" + SHA256(chain|token)[:16].
    Production workers filter WHERE event_id NOT LIKE 'replay:%' and therefore
    NEVER consume this event.  Only a replay-mode worker scope that queries
    WHERE event_id LIKE 'replay:%' will process it.

Idempotency:
    event_id is content-addressable (chain + token_address → SHA256 prefix).
    Re-running the script with the same --chain + --token produces no duplicate
    rows: the INSERT uses ON CONFLICT (event_id) DO NOTHING.

After injection:
    Run the replay-worker inspection queries printed at the end of this script's
    output to confirm that downstream layers produced feature_event, edge_event,
    probability_event, etc.

References:
    §7.5  Event bus pattern  (docs/plans/2026-05-29-production-gate-hardening-plan.md)
    §7.13 Replay engine pattern (same document)
"""

import argparse
import hashlib
import json
import os
import struct
import sys
import time
from datetime import datetime, timezone
from typing import Optional

# psycopg2 is imported lazily in _require_psycopg2() so that --dry-run
# works without the package being installed.
try:
    import psycopg2
    import psycopg2.extras
    _PSYCOPG2_AVAILABLE = True
except ImportError:
    psycopg2 = None  # type: ignore[assignment]
    _PSYCOPG2_AVAILABLE = False


def _require_psycopg2() -> None:
    """Abort with a helpful message when psycopg2 is not installed."""
    if not _PSYCOPG2_AVAILABLE:
        print(
            "ERROR: psycopg2 not found.\n"
            "Install with: pip install psycopg2-binary",
            file=sys.stderr,
        )
        sys.exit(1)

# ── Constants ─────────────────────────────────────────────────────────────────

VERSION_ID = "replay-v1"
EVENT_TYPE = "market_data_event"

# Realistic defaults for a known-good Raydium/Solana token that has already
# graduated from the bonding curve and is trading on the open market.
# These values are chosen to pass every active L1 gate while producing a
# strong edge signal for L2–L7 to work with.

DEFAULT_SYMBOL = "TESTGOOD"
DEFAULT_NAME = "TestGood Token"

# Pre-approved quality payload — all probe flags set so L1 passes cleanly.
REALISTIC_QUALITY_FLAGS = {
    # ── Honeypot simulation ───────────────────────────────────────────────
    "honeypot_sim_known": True,
    "buy_sim_success": True,
    "sell_sim_success": True,
    # ── Tax ──────────────────────────────────────────────────────────────
    "tax_known": True,
    "buy_tax_bps": 0,
    "sell_tax_bps": 0,
    "initial_buy_tax_bps": 0,
    "initial_sell_tax_bps": 0,
    "tax_is_dynamic": False,
    "blacklist_function_present": False,
    # ── LP lock ───────────────────────────────────────────────────────────
    "lp_lock_known": True,
    "lp_locked": True,
    "lp_lock_strength": 1.0,
    "lp_lock_days": 365,
    # ── Owner privileges ─────────────────────────────────────────────────
    "owner_privileges_known": True,
    "owner_privileges": [],
    "mint_authority_renounced": True,
    "freeze_authority_renounced": True,
    "solana_authorities_known": True,
    "contract_verified": True,
    "contract_verified_known": True,
    # ── Holder distribution ───────────────────────────────────────────────
    "holder_dist_known": True,
    "top5_holder_pct": 0.12,    # 12% — healthy distribution
    "holder_count": 480,
    # ── Wash trading stats ────────────────────────────────────────────────
    "wash_stats_known": True,
    "tx_count_1m": 52,
    "unique_wallets_1m": 44,
    "wallet_entropy": 3.7,      # high entropy → organic wallets
    "repeat_ratio_1m": 0.04,    # low repeat ratio → not wash-trading
    # ── LP / pool stats ───────────────────────────────────────────────────
    "lp_stats_known": True,
    "liquidity_usd": 55000.0,
    "single_lp_provider_pct": 0.18,
    "lp_churn_detected": False,
    "lp_churn_blocks": 0,
    "pool_age_seconds": 3720,   # ~62 min old; passes min_token_age (900 s default)
    # ── Total supply ──────────────────────────────────────────────────────
    "total_supply_known": True,
    "total_supply": 999_000_000.0,   # < 1B max; passes high_total_supply gate
    # ── Developer reputation ──────────────────────────────────────────────
    "creator_prev_token_count_known": True,
    "creator_prev_token_count": 0,   # first-time creator; passes serial_launcher
    "social_links_known": True,
    "has_social_links": True,
    # ── AI narrative enrichment ───────────────────────────────────────────
    "narrative_known": True,
    "narrative_score": 8.2,
    "scam_probability_score": 0.9,
    "is_copy_paste_desc": False,
    "is_impersonation": False,
    "narrative_type": "meme",
    "narrative_reason": "strong meme narrative",
    # ── Name deduplication ────────────────────────────────────────────────
    "is_name_duplicate": False,
    "is_copycat": False,
    # ── Market-cap and volume (§10) ────────────────────────────────────────
    "market_cap_usd": 520_000.0,
    "volume_usd_5m": 3_400.0,
    "volume_usd_1h": 28_000.0,
    "volume_usd_24h": 115_000.0,
}


# ── Helpers ───────────────────────────────────────────────────────────────────

def sha256_hex(data: str) -> str:
    """Return hex-encoded SHA-256 of the given UTF-8 string."""
    return hashlib.sha256(data.encode("utf-8")).hexdigest()


def content_id(chain: str, token_address: str) -> str:
    """Deterministic 16-char content-addressable ID for (chain, token)."""
    return sha256_hex(chain + "|" + token_address)[:16]


def replay_event_id(chain: str, token_address: str) -> str:
    """Replay-prefixed event_id — production workers filter this out."""
    return "replay:" + content_id(chain, token_address)


def trace_id_for(chain: str, token_address: str) -> str:
    """Deterministic trace_id for this replay injection."""
    return sha256_hex("replay-trace:" + chain + "|" + token_address)[:16]


def logical_order_key_bytes(nanos: int) -> bytes:
    """Big-endian uint64 nanosecond timestamp — matches Go binary.BigEndian.PutUint64."""
    return struct.pack(">Q", nanos)


def partition_key_for(correlation_id: str) -> int:
    """Hash(correlation_id) % 256 — matches Go InsertEvent auto-computation."""
    h = 0
    for c in correlation_id:
        h = h * 31 + ord(c)
    return abs(h) % 256


def now_iso() -> str:
    """Current UTC time in RFC 3339 Nano format."""
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.%f") + "Z"


# ── Payload builder ───────────────────────────────────────────────────────────

def build_payload(
    chain: str,
    token_address: str,
    event_id: str,
    trace_id: str,
    symbol: str,
    name: str,
    creator: str,
    pool_address: str,
    market: str,
    ingested_at: str,
) -> dict:
    """Build a realistic MarketDataDTO payload that passes all active L1 gates."""
    payload = {
        # ── Traceability ─────────────────────────────────────────────────────
        "event_id": event_id,
        "trace_id": trace_id,
        "correlation_id": trace_id,   # root event — correlation == trace
        "causation_id": "",            # Layer 0 root; empty string (NULL in DB)
        "version_id": VERSION_ID,
        # ── Core market fields ────────────────────────────────────────────────
        "chain": chain,
        "market": market,
        "block_number": 0,
        "block_hash": "",
        "tx_hash": sha256_hex("replay-tx:" + chain + "|" + token_address)[:64],
        "log_index": 0,
        "event_topic": "PairCreated",
        "pool_address": pool_address,
        "token_address": token_address,
        "base_address": "",
        "token0_address": token_address,
        "token1_address": "",
        "amount0_raw": "0",
        "amount1_raw": "0",
        "reserve_base_raw": "0",
        "reserve_token_raw": "0",
        "block_timestamp": ingested_at,
        "ingested_at": ingested_at,
        "rpc_endpoint": "replay",
        "transport": "replay_inject",
        "confirmation_depth": 0,
        "reorged": False,
        "expires_at": "",
        "priority": 5,              # elevated priority so replay workers pick it up first
        "symbol": symbol,
        "name": name,
        "bonding_curve_progress_bps": 0,
        # ── Developer reputation ──────────────────────────────────────────────
        "metadata_uri": "https://arweave.net/replay-test-metadata",
        "creator_address": creator,
        "metadata_description": "Community-driven meme token with strong organic growth.",
        # ── Pre-approved quality flags ────────────────────────────────────────
        **REALISTIC_QUALITY_FLAGS,
    }
    return payload


# ── Database insertion ─────────────────────────────────────────────────────────

def insert_replay_event(
    conn,
    event_id: str,
    payload: dict,
    chain: str,
    dry_run: bool,
) -> None:
    """Insert the replay market_data_event into the event bus."""
    trace_id = payload["trace_id"]
    correlation_id = payload["correlation_id"]
    # causation_id is empty string in payload but must be NULL in the DB (Layer 0 root).
    causation_id_db = None  # maps to SQL NULL — FK is not checked for NULL

    nanos = time.time_ns()
    lkey = logical_order_key_bytes(nanos)
    pkey = partition_key_for(correlation_id)
    created_at = now_iso()

    payload_json = json.dumps(payload, separators=(",", ":"), sort_keys=True)

    q = """
        INSERT INTO events
            (event_id, event_type, payload, trace_id, correlation_id, causation_id,
             version_id, created_at, processed, chain, consumer,
             logical_order_key, partition_key, block_number)
        VALUES (%s, %s, %s::jsonb, %s, %s, %s, %s, %s, FALSE, %s, %s, %s, %s, %s)
        ON CONFLICT (event_id) DO NOTHING
    """

    if dry_run:
        # Build a display-only params list; psycopg2 may not be available.
        display_params = [
            event_id, EVENT_TYPE, f"<{len(payload_json)} chars of JSONB>",
            trace_id, correlation_id, repr(causation_id_db),
            VERSION_ID, created_at, chain, "",
            f"<{len(lkey)} bytes logical_order_key>", pkey, 0,
        ]
        print("\n[DRY-RUN] Would execute:")
        print(q.strip())
        print("\nParams:")
        for i, p in enumerate(display_params, start=1):
            print(f"  ${i}: {p}")
        return

    # Live path — psycopg2 is available (enforced by _require_psycopg2 in main).
    params = (
        event_id,
        EVENT_TYPE,
        payload_json,
        trace_id,
        correlation_id,
        causation_id_db,
        VERSION_ID,
        created_at,
        chain,
        "",                    # consumer — unrouted; replay workers filter by event_id prefix
        psycopg2.Binary(lkey), # noqa: only reached when psycopg2 is not None
        pkey,
        0,                     # block_number — unknown for synthetic events
    )

    with conn.cursor() as cur:
        cur.execute(q, params)
        rows_affected = cur.rowcount

    conn.commit()

    if rows_affected == 0:
        print(
            f"[IDEMPOTENT] Event already exists: {event_id}\n"
            "  No duplicate inserted (ON CONFLICT DO NOTHING)."
        )
    else:
        print(f"[INSERTED] event_id={event_id}  event_type={EVENT_TYPE}")


# ── Operator runbook ───────────────────────────────────────────────────────────

def print_runbook(event_id: str, trace_id: str, chain: str, token_address: str) -> None:
    """Print SQL inspection queries for validating L2–L10 replay output."""
    print()
    print("=" * 72)
    print("OPERATOR RUNBOOK — Replay-Mode Pipeline Inspection")
    print("=" * 72)
    print()
    print("The test event has been injected.  Use these psql queries to track")
    print("its progress through L2–L10 after your replay-mode workers run.")
    print()
    print("1. Verify the injected market_data_event is present:")
    print(f"""
   SELECT event_id, event_type, created_at, processed
   FROM   events
   WHERE  event_id = '{event_id}';
""")
    print()
    print("2. Confirm production workers CANNOT see this event")
    print("   (event_id must NOT appear in this result set):")
    print("""
   SELECT event_id
   FROM   events
   WHERE  event_type = 'market_data_event'
     AND  event_id   NOT LIKE 'replay:%'
     AND  processed  = FALSE
   ORDER BY created_at DESC
   LIMIT 5;
""")
    print()
    print("3. Check downstream layer outputs by trace_id after replay workers run:")
    print(f"""
   -- L2 Feature extraction
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
     AND  event_type = 'feature_event'
   ORDER BY created_at;

   -- L3 Edge detection
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
     AND  event_type = 'edge_event'
   ORDER BY created_at;

   -- L4 Probability / slippage / latency
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
     AND  event_type IN ('probability_event','slippage_event','latency_event')
   ORDER BY created_at;

   -- L5 Edge validation
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
     AND  event_type = 'validated_edge_event'
   ORDER BY created_at;

   -- L6 Selection engine
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
     AND  event_type = 'selection_event'
   ORDER BY created_at;

   -- L7 Capital engine
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
     AND  event_type = 'allocation_event'
   ORDER BY created_at;
""")
    print()
    print("4. Full trace — all events produced for this injection:")
    print(f"""
   SELECT event_id, event_type, created_at
   FROM   events
   WHERE  trace_id = '{trace_id}'
   ORDER BY created_at;
""")
    print()
    print("5. If any layer produces no output, check the dead-letter queue:")
    print(f"""
   SELECT event_id, reason, error_message, first_failed_at
   FROM   dead_letter_events
   WHERE  trace_id = '{trace_id}';
""")
    print()
    print("NOTE: Replay workers must filter:")
    print("   WHERE event_id LIKE 'replay:%'")
    print("  Production workers must filter:")
    print("   WHERE event_id NOT LIKE 'replay:%'")
    print()
    print(f"Injected token:  chain={chain}  token={token_address}")
    print(f"event_id:        {event_id}")
    print(f"trace_id:        {trace_id}")
    print()
    print("Document any layer that produces no output in docs/PROGRESS_REPORT.md")
    print("Session History and file a new plan for fixes (Task 19 is read-only).")
    print("=" * 72)


# ── CLI ────────────────────────────────────────────────────────────────────────

def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="inject_test_token.py",
        description=(
            "Inject a synthetic market_data_event (replay-prefixed) into the "
            "event bus for end-to-end L2–L10 pipeline validation."
        ),
    )
    parser.add_argument(
        "--chain",
        required=True,
        help="Chain identifier, e.g. 'solana', 'eth', 'bsc'.",
    )
    parser.add_argument(
        "--token",
        required=True,
        help="Token address (mint address on Solana, 0x-address on EVM).",
    )
    parser.add_argument(
        "--symbol",
        default=DEFAULT_SYMBOL,
        help=f"Token symbol (default: {DEFAULT_SYMBOL}).",
    )
    parser.add_argument(
        "--name",
        default=DEFAULT_NAME,
        help=f"Token name (default: {DEFAULT_NAME!r}).",
    )
    parser.add_argument(
        "--creator",
        default="",
        help="Creator wallet address. Defaults to a deterministic replay value.",
    )
    parser.add_argument(
        "--pool",
        default="",
        help="Pool/pair address. Defaults to a deterministic replay value.",
    )
    parser.add_argument(
        "--market",
        default="raydium-v4",
        help="Market identifier (default: raydium-v4).",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print the SQL and payload without writing to the database.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    chain = args.chain.strip().lower()
    token_address = args.token.strip()

    if not chain:
        print("ERROR: --chain must not be empty.", file=sys.stderr)
        return 1
    if not token_address:
        print("ERROR: --token must not be empty.", file=sys.stderr)
        return 1

    # Default creator and pool to deterministic replay placeholders when not supplied.
    creator = args.creator.strip() or sha256_hex("replay-creator:" + chain + "|" + token_address)[:44]
    pool_address = args.pool.strip() or sha256_hex("replay-pool:" + chain + "|" + token_address)[:44]

    # Compute content-addressable IDs.
    evt_id = replay_event_id(chain, token_address)
    t_id = trace_id_for(chain, token_address)
    ingested_at = now_iso()

    payload = build_payload(
        chain=chain,
        token_address=token_address,
        event_id=evt_id,
        trace_id=t_id,
        symbol=args.symbol,
        name=args.name,
        creator=creator,
        pool_address=pool_address,
        market=args.market,
        ingested_at=ingested_at,
    )

    print(f"[INFO] Preparing replay event:")
    print(f"  chain          = {chain}")
    print(f"  token          = {token_address}")
    print(f"  event_id       = {evt_id}")
    print(f"  trace_id       = {t_id}")
    print(f"  transport      = replay_inject")
    print(f"  market_cap_usd = {REALISTIC_QUALITY_FLAGS['market_cap_usd']:,.0f}")
    print(f"  volume_1h_usd  = {REALISTIC_QUALITY_FLAGS['volume_usd_1h']:,.0f}")

    if args.dry_run:
        print("\n[INFO] Payload (pretty-printed):")
        print(json.dumps(payload, indent=2))

        # Dry-run: connect-less path — psycopg2 not required.
        insert_replay_event(conn=None, event_id=evt_id, payload=payload, chain=chain, dry_run=True)
        print_runbook(evt_id, t_id, chain, token_address)
        return 0

    # Live path — psycopg2 is required.
    _require_psycopg2()

    # Read DATABASE_URL from environment — never hardcode credentials.
    database_url = os.environ.get("DATABASE_URL", "")
    if not database_url:
        print(
            "ERROR: DATABASE_URL environment variable is not set.\n"
            "Export it before running:\n"
            "  export DATABASE_URL=postgres://user:pass@localhost:5432/crypto_sniper",
            file=sys.stderr,
        )
        return 1

    try:
        conn = psycopg2.connect(dsn=database_url)
    except psycopg2.OperationalError as exc:
        print(f"ERROR: Cannot connect to database: {exc}", file=sys.stderr)
        return 1

    try:
        insert_replay_event(conn=conn, event_id=evt_id, payload=payload, chain=chain, dry_run=False)
    except psycopg2.Error as exc:
        print(f"ERROR: Database write failed: {exc}", file=sys.stderr)
        conn.rollback()
        return 1
    finally:
        conn.close()

    print_runbook(evt_id, t_id, chain, token_address)
    return 0


if __name__ == "__main__":
    sys.exit(main())
