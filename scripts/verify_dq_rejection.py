#!/usr/bin/env python3
"""
verify_dq_rejection.py — Confirms a list of Solana token CAs would be
rejected by Layer 1 DQ using the same API calls our probes make.

API calls used (mirrors solana_creator_reputation.go + solana_metadata.go):
  1. GET https://frontend-api-v3.pump.fun/coins/<mint>
     → creator address + metadata_uri
  2. GET https://frontend-api-v3.pump.fun/coins?user=<creator>&limit=50&offset=0
     → prior token count (mirrors SolanaCreatorReputationProbe.fetchFromPumpFun)
  3. GET <metadata_uri>
     → social links JSON (mirrors SolanaMetadataProbe.Probe)

DQ thresholds applied (from config/data_quality.yaml):
  max_creator_prev_token_count: 1   → serial_launcher
  reject_no_social_links: true      → no_social_links (confirmed absence)
  reject_unknown_social_links: true → unknown_social_links (fetch failed)
  reject_unknown_creator_count: true → unknown_creator_count (probe failed)
"""

import json
import sys
import time
import urllib.request
import urllib.error
from typing import Optional

# ── Config (matches config/data_quality.yaml) ─────────────────────────────────
MAX_CREATOR_PREV_TOKEN_COUNT = 1   # reject if prev_count >= this
MAX_BODY_BYTES                = 128 * 1024  # 128 KiB cap (matches probe)
TIMEOUT_S                     = 5

# Blocked website domains (mirrors isBlockedWebsiteDomain in solana_metadata.go)
BLOCKED_WEBSITE_DOMAINS = [
    "dexscreener.com", "birdeye.so", "solscan.io", "raydium.io",
    "jup.ag", "pump.fun", "photon-sol.trycloudflar.com",
    "defined.fi", "dextools.io", "poocoin.app",
]

# Social-media domains blocked as "website" field (mirrors isSocialMediaWebsiteDomain)
SOCIAL_MEDIA_DOMAINS = [
    "twitter.com", "x.com", "t.me", "telegram.me", "telegram.org",
    "discord.com", "discord.gg", "discordapp.com",
    "facebook.com", "fb.com", "instagram.com", "tiktok.com",
    "youtube.com", "youtu.be", "medium.com", "linktr.ee",
    "reddit.com", "bio.link",
]

# Reserved Twitter path segments (mirrors isTwitterProfileURL switch)
RESERVED_TWITTER_PATHS = {
    "i", "search", "intent", "explore", "hashtag", "home",
    "settings", "notifications", "messages", "help",
    "login", "signup", "logout", "about", "privacy", "tos",
}

TWITTER_HOSTS = {"twitter.com", "www.twitter.com", "x.com", "www.x.com"}

TOKENS = [
    "3Ucu4GgLnheah21ZjHBXPQ3cPGjmfThdmC5FsdWAn5o",
    "7a7Kukc9mnsjvt5RwuRTG2WX4kXvhmnBNQRNF7AYpump",
    "7h22A4FTtFcRq1suFr4rB8TXimjBqbd5NfpZDGMipump",
    "8poHAR4szrPjEcTYb72mDWKmqskeAKrZJLjbfPZdpump",
    "ANufBJA3uCaPXKTpPE3AgskuHXrvZg5nkM5DvgiGpump",
]

HEADERS = {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
    "Accept": "application/json",
    "Origin": "https://pump.fun",
    "Referer": "https://pump.fun/",
}

# ── HTTP helper (bounded, mirrors probe's io.LimitReader) ─────────────────────

def fetch_json(url: str, max_bytes: int = MAX_BODY_BYTES) -> Optional[dict]:
    try:
        req = urllib.request.Request(url, headers=HEADERS)
        with urllib.request.urlopen(req, timeout=TIMEOUT_S) as resp:
            raw = resp.read(max_bytes + 1)
            if len(raw) > max_bytes:
                print(f"    ⚠  response exceeded {max_bytes} bytes — truncated")
                raw = raw[:max_bytes]
            return json.loads(raw)
    except urllib.error.HTTPError as e:
        print(f"    ✗  HTTP {e.code} from {url}")
        return None
    except Exception as e:
        print(f"    ✗  fetch error: {e}")
        return None

# ── URL validators (mirrors solana_metadata.go) ───────────────────────────────

def _is_twitter_profile_url(raw: str) -> bool:
    """Mirrors isTwitterProfileURL — positive validation via url parsing."""
    import urllib.parse
    raw = raw.strip()
    if not raw:
        return False
    try:
        p = urllib.parse.urlparse(raw)
    except Exception:
        return False
    host = p.hostname or ""
    if host == "t.co":
        return False
    if host not in TWITTER_HOSTS:
        return False
    # non-standard port check (BLOCKER-1 fix)
    if p.port is not None:
        return False
    path = p.path.strip("/")
    if not path:
        return False
    # must be exactly one segment
    if "/" in path:
        return False
    # reserved paths
    if path.lower() in RESERVED_TWITTER_PATHS:
        return False
    # @ in path check (BLOCKER-2 fix)
    if "@" in path:
        return False
    return True

def _is_blocked_website(raw: str) -> bool:
    lower = raw.lower()
    for d in BLOCKED_WEBSITE_DOMAINS:
        if d in lower:
            return True
    return False

def _is_social_media_website(raw: str) -> bool:
    lower = raw.lower()
    for d in SOCIAL_MEDIA_DOMAINS:
        if d in lower:
            return True
    return False

def is_social_profile_url(social_type: str, raw_url: str) -> bool:
    """Mirrors isSocialProfileURL in solana_metadata.go."""
    u = raw_url.strip()
    if not u:
        return False
    t = social_type.lower()
    if t in ("twitter", "x"):
        return _is_twitter_profile_url(u)
    if _is_blocked_website(u):
        return False
    if t == "website" and _is_social_media_website(u):
        return False
    return True

def check_social_links(meta: dict) -> tuple[bool, list[str]]:
    """
    Mirrors parseSocialLinks — returns (has_valid_link, [details]).
    Checks top-level keys, 'extensions', and 'links' objects.
    """
    details = []
    found = False

    def check_pair(stype: str, val: str):
        nonlocal found
        if not val:
            return
        valid = is_social_profile_url(stype, val)
        mark = "✓" if valid else "✗"
        details.append(f"      [{mark}] {stype}: {val!r}")
        if valid:
            found = True

    # 1. Top-level keys
    for key in ("twitter", "telegram", "website"):
        check_pair(key, meta.get(key, "") or "")

    # 2. extensions object
    ext = meta.get("extensions") or {}
    if isinstance(ext, dict):
        for key in ("twitter", "telegram", "website"):
            check_pair(key, ext.get(key, "") or "")

    # 3. links object (catch-all)
    links = meta.get("links") or {}
    if isinstance(links, dict):
        for k, v in links.items():
            if isinstance(v, str) and v:
                check_pair(k, v)

    return found, details

# ── Creator probe (mirrors SolanaCreatorReputationProbe.fetchFromPumpFun) ──────

def fetch_creator_history(creator: str) -> tuple[Optional[int], str]:
    """Returns (prev_token_count, status_detail)."""
    url = f"https://frontend-api-v3.pump.fun/coins?user={creator}&limit=50&offset=0"
    data = fetch_json(url)
    if data is None:
        return None, "API error — CreatorPrevTokenCountKnown=false (fail-closed)"
    if not isinstance(data, list):
        return None, f"unexpected response type: {type(data).__name__}"
    # API returns ALL tokens including the current one; subtract 1 (matches probe logic)
    total = len(data)
    prev = max(0, total - 1)
    return prev, f"pump.fun returned {total} total tokens (prev={prev})"

# ── Main verification loop ────────────────────────────────────────────────────

def verify_token(ca: str) -> dict:
    print(f"\n{'─'*70}")
    print(f"  TOKEN: {ca}")
    print(f"{'─'*70}")

    result = {
        "ca": ca,
        "reject_reasons": [],
        "creator": None,
        "prev_token_count": None,
        "has_social_links": None,
        "social_links_known": False,
        "name": None,
        "symbol": None,
    }

    # ── Step 1: fetch coin info from pump.fun ──────────────────────────────
    print(f"  [1] GET https://frontend-api-v3.pump.fun/coins/{ca[:16]}...")
    coin_url = f"https://frontend-api-v3.pump.fun/coins/{ca}"
    coin = fetch_json(coin_url)
    if coin is None:
        print("      ✗  coin info unavailable — cannot continue")
        result["reject_reasons"].append("probe_error")
        return result

    name   = coin.get("name", "?")
    symbol = coin.get("symbol", "?")
    creator = coin.get("creator", "") or ""
    meta_uri = coin.get("metadata_uri", "") or coin.get("uri", "") or ""

    result["name"]    = name
    result["symbol"]  = symbol
    result["creator"] = creator

    print(f"      name={name!r}  symbol={symbol!r}")
    print(f"      creator={creator}")
    print(f"      metadata_uri={meta_uri!r}")

    # ── Step 2: creator reputation (mirrors SolanaCreatorReputationProbe) ──
    print(f"\n  [2] Creator history (pump.fun creator API):")
    if not creator:
        print("      ✗  creator address missing — unknown_creator_count → REJECT")
        result["reject_reasons"].append("unknown_creator_count")
    else:
        prev_count, detail = fetch_creator_history(creator)
        print(f"      {detail}")
        if prev_count is None:
            print(f"      → CreatorPrevTokenCountKnown=false → unknown_creator_count → REJECT")
            result["reject_reasons"].append("unknown_creator_count")
        else:
            result["prev_token_count"] = prev_count
            if prev_count >= MAX_CREATOR_PREV_TOKEN_COUNT:
                print(f"      → prev_count={prev_count} >= threshold={MAX_CREATOR_PREV_TOKEN_COUNT} → serial_launcher → REJECT")
                result["reject_reasons"].append("serial_launcher")
            else:
                print(f"      → prev_count={prev_count} < threshold={MAX_CREATOR_PREV_TOKEN_COUNT} → OK")
        time.sleep(0.3)  # be polite to the API

    # ── Step 3: metadata / social links (mirrors SolanaMetadataProbe) ───────
    print(f"\n  [3] Social links (metadata probe):")
    # Also check top-level fields directly from the pump.fun coin response
    # (pump.fun API includes twitter/telegram/website at top level)
    has_valid, details = check_social_links(coin)

    if meta_uri and meta_uri != coin.get("image", ""):
        print(f"      Fetching off-chain metadata: {meta_uri[:80]}...")
        meta = fetch_json(meta_uri, max_bytes=64 * 1024)
        if meta and isinstance(meta, dict):
            has_valid_meta, details_meta = check_social_links(meta)
            # merge — any valid link across both sources is enough
            details = details + [d for d in details_meta if d not in details]
            has_valid = has_valid or has_valid_meta
            result["social_links_known"] = True
        else:
            print("      ✗  metadata fetch failed")
            if not has_valid:  # pump.fun inline fields are our only source
                result["social_links_known"] = False
    else:
        # No separate metadata URI; pump.fun coin JSON is the only source
        result["social_links_known"] = True

    if details:
        print("      Social fields found:")
        for d in details:
            print(d)
    else:
        print("      (no social fields in response)")

    result["has_social_links"] = has_valid

    if not result["social_links_known"]:
        print(f"      → SocialLinksKnown=false → unknown_social_links → REJECT")
        result["reject_reasons"].append("unknown_social_links")
    elif not has_valid:
        print(f"      → HasSocialLinks=false (no valid profile) → no_social_links → REJECT")
        result["reject_reasons"].append("no_social_links")
    else:
        print(f"      → HasSocialLinks=true → OK")

    return result

def main():
    print("=" * 70)
    print("  DQ Rejection Verification — codebase mechanism mirror")
    print(f"  Thresholds: max_creator_prev_token_count={MAX_CREATOR_PREV_TOKEN_COUNT}")
    print(f"              reject_no_social_links=true")
    print(f"              reject_unknown_creator_count=true")
    print("=" * 70)

    results = []
    for ca in TOKENS:
        r = verify_token(ca)
        results.append(r)
        time.sleep(0.5)

    # ── Summary ────────────────────────────────────────────────────────────
    print(f"\n\n{'=' * 70}")
    print("  SUMMARY")
    print(f"{'=' * 70}")
    print(f"  {'TOKEN':<48}  {'NAME':<12}  {'PREV':<6}  {'SOCIALS':<8}  VERDICT")
    print(f"  {'-'*48}  {'-'*12}  {'-'*6}  {'-'*8}  {'─'*20}")
    all_rejected = True
    for r in results:
        prev  = str(r["prev_token_count"]) if r["prev_token_count"] is not None else "?"
        soc   = "yes" if r["has_social_links"] else ("?" if r["has_social_links"] is None else "no")
        name  = (r["name"] or "?")[:12]
        verdict = "REJECT ✓" if r["reject_reasons"] else "PASS ✗ (BUG)"
        if not r["reject_reasons"]:
            all_rejected = False
        print(f"  {r['ca']:<48}  {name:<12}  {prev:<6}  {soc:<8}  {verdict}")
        print(f"    reasons: {', '.join(r['reject_reasons']) or '(none)'}")

    print(f"\n{'=' * 70}")
    if all_rejected:
        print("  ✅  ALL 5 TOKENS CONFIRMED REJECTED BY DQ LAYER")
    else:
        print("  ❌  WARNING: ONE OR MORE TOKENS NOT REJECTED — review required")
    print(f"{'=' * 70}\n")

    return 0 if all_rejected else 1

if __name__ == "__main__":
    sys.exit(main())
