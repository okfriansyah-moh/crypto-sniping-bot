# Production Gate Analysis

> **Historical snapshot** · 2026-05-20 · Findings incorporated into
> [`docs/plans/2026-06-10-profit-restoration-plan.md`](../plans/2026-06-10-profit-restoration-plan.md) Tasks 13–19.
> For current status see [`docs/ops/PROGRESS_REPORT.md`](../ops/PROGRESS_REPORT.md).

**Last updated**: 2026-05-20
**Data window analyzed**: 2026-05-19 14:25–16:25 UTC (2-hour observation window)
**Helius pricing reference**: helius.dev/docs/billing/credits (live-verified)

---

## How to Read This Document

If you are new to this codebase, start from Section 1 and read in order. Each section
answers one specific question. Where there is something to fix, the section ends with a
**step-by-step implementation guide** showing the exact file and change to make.

Think of this document as a "health report + doctor's notes" for the bot. It tells you
what is working, what is wrong, why it is wrong, and exactly how to fix it.

---

## Table of Contents

1. [What Mode Is the Bot Actually Running In?](#section-1)
2. [Why Are Layers 2–10 Showing Zero Activity?](#section-2)
3. [Why Is the Bot Rejecting 100% of Tokens?](#section-3)
4. [CORRECTED: Where Are the Helius Credits Actually Going?](#section-4)
5. [How to Drastically Reduce Credit Costs](#section-5)
6. [Health Dashboard — At a Glance](#section-6)
7. [Prioritized Action Plan](#section-7)
8. [Designer's Perspective — What Would Make This Bot Profitable?](#section-8)
9. [Mode-Aware Serial Launcher Threshold — Design Plan](#section-9)
10. [Market Cap & Volume Filtering via DEXScreener — Design Plan](#section-10)

---

<a name="section-1"></a>

## Section 1 — What Mode Is the Bot Actually Running In?

### Plain English Explanation

The bot has **three operating stages** in its lifecycle:

| Stage              | What it means                | What should be happening                      |
| ------------------ | ---------------------------- | --------------------------------------------- |
| **Pipeline Proof** | The plumbing is being tested | All layers wire up, data flows, but no trades |
| **Shadow Trading** | The bot makes fake trades    | Buy/sell decisions logged but not submitted   |
| **Live Trading**   | Real money, real trades      | Full production operation                     |

**The bot is currently in Pipeline Proof mode.** It has been ingesting tokens, running
quality checks, and logging events — but it has never executed a single trade.

You can confirm this yourself by looking at the logs. You will see thousands of
`data_quality_reject` events and zero `execution_submitted` events.

### Is This a Problem?

No — this is the correct starting point. You should not live-trade with a pipeline you
have never validated end-to-end. The goal right now is to get one token to pass all the
quality gates, trace it through every layer, and confirm the math makes sense before
any real money is involved.

### What Needs to Happen Next

The pipeline needs to move from "proving it runs" to "proving it can find a good trade".
That requires understanding why no token has ever passed the quality check — which is
what Section 3 explains.

---

<a name="section-2"></a>

## Section 2 — Why Are Layers 2–10 Showing Zero Activity?

### The Confusion

When people look at the logs and see nothing happening in layers 2 through 10, they
assume those parts of the system are broken. They are not broken. They are **waiting**.

Here is the exact flow from the 2-hour observation window:

```
Layer 0  (Detection):    2,871 tokens seen arriving
Layer 1  (Quality Gate): 13,553 decisions made → all 13,553 are REJECT
Layers 2–10 (Pipeline):  0 events processed
```

Think of it like a water treatment plant. Layer 1 is the filtration stage. If the filter
rejects every single drop of incoming water, the rest of the plant has nothing to do.
The machinery after the filter is perfectly functional — it just has no input.

The 13,553 decisions being larger than the 2,871 tokens detected is explained by the
**Rescan Worker** (Layer 0.5). This worker re-evaluates tokens at 14 different age
checkpoints (15 minutes old, 30 minutes old, 1 hour old, etc.). Each re-evaluation
generates another quality-check decision. They all still fail the quality gate.

### How to Verify the Workers Are Alive

Run this query against the database to confirm workers are running and responsive:

```sql
SELECT event_type, COUNT(*) as count
FROM events
WHERE created_at > NOW() - INTERVAL '2 hours'
GROUP BY event_type
ORDER BY count DESC
LIMIT 20;
```

You should see `market_data_event`, `data_quality_event`, and `rescan_band_completed`
with non-zero counts. If you see those, the workers are alive — just waiting for their
first approved token.

---

<a name="section-3"></a>

## Section 3 — Why Is the Bot Rejecting 100% of Tokens?

### The Breakdown

Here is where every rejection came from in the 2-hour window:

| Rejection Reason                                                      | Count | %     | Root Cause                              |
| --------------------------------------------------------------------- | ----- | ----- | --------------------------------------- |
| `unknown_creator_count + unknown_social_links + unknown_total_supply` | 9,109 | 67.2% | Rate-limited — quality checks never ran |
| `no_social_links + unknown_creator_count`                             | 1,870 | 13.8% | Checks ran, found no social links       |
| `serial_launcher` (fully confirmed)                                   | 533   | 3.9%  | Confirmed serial token launcher         |
| `duplicate_name`                                                      | 1,350 | 10%   | Same name seen before on this chain     |
| `missing_reserves`                                                    | 171   | 1.3%  | No liquidity in the pool                |
| `ai_copy_paste_desc`                                                  | 172   | 1.3%  | AI detected copy-pasted description     |

There are two separate problems here. Let us look at each one.

---

### Problem A — 71.7% of Tokens Are Rate-Limited Before Any Check Runs

#### What "rate-limited" means

The bot has a probe system that checks each token's quality before deciding to trade it.
Each check costs Helius credits (small amounts — 1-10 credits per call). To avoid
spending too many credits, the bot limits how many tokens it can fully check per minute.

The current limit is set in `shared/config/pipeline.yaml`:

```yaml
# shared/config/pipeline.yaml, around line 506
probes:
  rate_limit_per_min: 30
```

This means the bot can fully check 30 tokens per minute.

#### Why this is a problem

Each token needs 3-4 separate checks (creator reputation, metadata/social links, holder
distribution, and AI narrative). With 30 checks per minute shared across 3-4 probe types,
the effective throughput is about **7-8 tokens fully checked per minute**.

But tokens are arriving at **~24 per minute** (2,871 tokens over 120 minutes).

Result: 71.7% of tokens hit the rate limit → never get their checks run → are rejected
with `unknown_*` codes because the bot does not know enough about them to approve them.
This is called "fail-closed" — when in doubt, reject.

#### The easy part

If you reduce the incoming token volume (by removing low-quality sources like raw
pump.fun — see Section 8), the rate limit stops being a problem without any config
change. At reduced volume the 30/min limit is more than enough.

---

### Problem B — The Pump.fun Factory Problem

#### Why every pump.fun token gets rejected

Pump.fun is a platform where anyone can launch a Solana token in seconds. When you look
at who "created" a pump.fun token, the on-chain creator address always resolves to the
**pump.fun factory program** — not the actual human who created it.

The pump.fun factory has launched tens of thousands of tokens. When the bot queries
"how many prior tokens has this creator launched?", the answer comes back as **49**
(the query limit cap) for every single pump.fun token.

The bot's quality threshold is set in `shared/config/data_quality.yaml`:

```yaml
# shared/config/data_quality.yaml, around line 90
thresholds:
  max_creator_prev_token_count: 1
```

This means: reject any creator who has launched **1 or more** prior tokens.

Every pump.fun token's creator has 49. Every single one is rejected as a `serial_launcher`.
This is **correct behavior** — the pump.fun factory is the ultimate serial launcher.

#### The AI Probe Confirms the Problem

Of the 236 tokens that got AI-scored in the 2-hour window:

- **76.3%** had copy-pasted descriptions (same text as another token)
- **84.7%** had a scam score of 7 or higher (out of 10)
- Average scam score: **7.69** out of 10

The AI is not miscalibrated. The pump.fun bonding-curve token universe is genuinely
this bad. These numbers accurately describe the market the bot is scanning.

#### The Real Problem

The bot is correctly scanning the market. But the market it is scanning is one where the
answer is always "no". This is not a calibration problem — it is a **market selection
problem**. See Section 8 for the solution.

---

<a name="section-4"></a>

## Section 4 — CORRECTED: Where Are the Helius Credits Actually Going?

> **This section corrects a significant error in earlier analysis.** The previous analysis
> claimed `getTransaction` costs 100 Helius credits per call. This is wrong. The Helius
> documentation clearly shows `getTransaction` costs **1 credit**. This changes the entire
> credit burn diagnosis.

### What the Helius Documentation Actually Says

Source: helius.dev/docs/billing/credits (verified 2026-05-20)

| API Method                                           | Credits                           | Notes                              |
| ---------------------------------------------------- | --------------------------------- | ---------------------------------- |
| `getTransaction` (standard Solana RPC)               | **1 credit**                      | Standard historical data call      |
| `getAccountInfo`                                     | **1 credit**                      | Standard RPC call                  |
| `getSignaturesForAddress`                            | **1 credit**                      | Standard RPC call                  |
| `getProgramAccounts`                                 | **10 credits**                    | More expensive standard call       |
| `getAsset` and all DAS API endpoints                 | **10 credits**                    | Digital Asset Standard API         |
| `getTransactionsForAddress` (Helius Enhanced TX API) | **100 credits**                   | Helius-specific enriched history   |
| `logsSubscribe` via WebSocket                        | **2 credits per 0.1 MB received** | Streaming — charged by data volume |
| `transactionSubscribe` via WebSocket                 | **2 credits per 0.1 MB received** | Helius extension — also streaming  |
| Webhooks                                             | **1 credit per event delivered**  | Push notification, not streaming   |
| Webhook management (create/edit/delete)              | **100 credits per operation**     | One-time cost                      |

The "Enhanced Transactions at 100 credits" refers to Helius's proprietary
`getTransactionsForAddress` API. The bot uses the **standard Solana JSON-RPC**
`getTransaction` call — which costs 1 credit.

There is also a **wrong comment** in the codebase that needs fixing:

```yaml
# shared/config/chains.yaml (around line 163 — INCORRECT COMMENT):
# Credit cost is minimal: ≤300 getTransaction calls/day × 100 credits = 900k credits/month
```

This comment says 100 credits per call. The actual cost is **1 credit per call**.
The real cost for 300 calls/day is 300 credits/day — not 900k credits/month.

### The Correct HTTP Credit Math

In the observed 2-hour window, approximately this many HTTP calls were made:

| Method                                    | Calls/day (estimate) | Cost per call | Total credits/day       |
| ----------------------------------------- | -------------------- | ------------- | ----------------------- |
| `getTransaction` (Raydium pool detection) | 21,549               | 1             | **21,549**              |
| `getAccountInfo` (probe system)           | 8,334                | 1             | **8,334**               |
| `getTokenLargestAccounts` (holder probe)  | 556                  | 1             | **556**                 |
| `getSignaturesForAddress` (creator probe) | ~551                 | 1             | **551**                 |
| **Total from HTTP calls**                 |                      |               | **~31,000 credits/day** |

31,000 credits per day from HTTP calls. The Helius Developer plan gives 10 million
credits per month (~333k per day). HTTP calls alone consume less than **10% of the
daily budget**.

### So Where Did the 2,034,157 Credits Actually Go?

The observed billing shows over 2 million credits consumed. 31,000 comes from HTTP.
The remaining ~2 million come from **WebSocket streaming data**.

#### How WebSocket billing works

When you subscribe to a Solana program via WebSocket (`logsSubscribe`), you receive a
notification for **every single transaction** that involves that program — not just the
ones you care about.

Helius charges **2 credits for every 0.1 MB of data received** over the WebSocket
connection. The data flows in whether your code uses it or not.

The bot subscribes to these programs (from `shared/config/chains.yaml`):

```yaml
programs:
  - program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
    family: raydium-v4 # ← the credit killer
  - program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
    family: pumpfun # ← also significant volume
  - program_id: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
    family: pumpfun-amm
  - program_id: "CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK"
    family: raydium-clmm
  - program_id: "whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc"
    family: orca-whirlpool
  - program_id: "LBUZKhRxPF3XUpBCjp4YzTKgLLjLeNox4HgSehp9ZSe"
    family: meteora-dlmm
```

#### The Raydium V4 problem

Raydium V4 is one of the busiest programs on all of Solana. It processes every swap,
every liquidity add, and every pool creation for millions of trades per day. Subscribing
to its logs means receiving all of that traffic — including the 99.9% of it that are
just swaps, not the new pool creations the bot is looking for.

The bot has a filter called `ray_log` that discards swap notifications and only processes
pool-creation events. But this filter runs **after** the data has already been received
and charged.

**Streaming credit estimate:**

| Program               | Estimated transactions/day | Avg notification | MB/day           | Credits/day       |
| --------------------- | -------------------------- | ---------------- | ---------------- | ----------------- |
| Raydium V4            | 20–30 million              | 1–3 KB           | 20,000–90,000 MB | 400k–1.8M         |
| Pump.fun              | 3–5 million                | 0.5–1 KB         | 1,500–5,000 MB   | 30k–100k          |
| CLMM + Orca + Meteora | 5–8 million                | 1–2 KB           | 5,000–16,000 MB  | 100k–320k         |
| **Total estimated**   |                            |                  |                  | **530k–2.2M/day** |

This range directly explains the observed 2,034,157 credits per day. The Raydium V4
subscription — which the bot's filter discards 99.9% of — is responsible for the vast
majority of credit consumption.

### The Math Summary

```
10,000,000 credits/month (Developer plan)
÷ 2,000,000 credits/day (observed burn rate)
= ~5 days of operation per billing cycle
```

Without fixing the streaming cost, the bot exhausts its credits in about 5 days every
month. This is not sustainable.

### One Additional Bug: The DAS API Comment Is Wrong

The DAS probe (`solana_das_asset`) is currently disabled. When it is eventually enabled,
the cost per call is **10 credits** (per Helius docs), not 1 credit. There is no wrong
comment in the code on this specific point, but it is worth knowing before enabling it.

---

<a name="section-5"></a>

## Section 5 — How to Drastically Reduce Credit Costs

There are three practical options. They are ordered from "do this first" to "do this later".

---

### Option 1 (HIGHEST IMPACT) — Disable Raw Pump.fun Subscription

**Why this works**: Pump.fun processes 3–5 million transactions per day. All of them
stream over the WebSocket. The bot's DQ layer correctly rejects 100% of pump.fun tokens
anyway (they all have the factory wallet as creator). Subscribing to pump.fun at all is
paying credits to confirm something that is always true.

**Important note**: The bot already has `pumpfun_decode_from_logs: true` which means
pump.fun tokens **do not call `getTransaction`** — they use the WebSocket log data
directly. But the subscription still streams all pump.fun activity (buys, sells,
creates) and gets charged for every byte.

**Expected savings**: Removing pump.fun subscription cuts ~30k–100k credits per day
from streaming costs alone.

#### Step-by-Step Implementation

**File to edit**: `shared/config/chains.yaml`

**Step 1**: Open the file and find the `programs:` section under `solana:`

```yaml
# Around line 120 in shared/config/chains.yaml
solana:
  ...
  programs:
    - program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
      family: raydium-v4
    - program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
      family: pumpfun             # ← this one
    - program_id: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
      family: pumpfun-amm         # ← keep this one
    ...
```

**Step 2**: Add a `disabled: true` field to the pumpfun entry (do not delete it — the
config system may need the entry to exist):

```yaml
  programs:
    - program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
      family: raydium-v4
    - program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
      family: pumpfun
      disabled: true              # ← add this line
    - program_id: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
      family: pumpfun-amm
    ...
```

**Step 3**: Check if the ingestion code reads a `disabled` flag. Search the codebase:

```bash
grep -rn "disabled" internal/modules/ingestion_solana/ shared/config/chains.yaml
```

If the ingestion code does not respect a `disabled` field, an alternative approach is
to comment out the entry:

```yaml
# pump.fun raw bonding-curve disabled — 100% rejection rate, streaming cost not justified
# - program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
#   family: pumpfun
```

**Step 4**: Restart the bot and verify in logs that `pumpfun` is no longer appearing in
`ingestion_subscribed` events.

**Verification**: After 1 hour of operation, the event count for `pumpfun` family should
be zero in the database:

```sql
SELECT metadata->>'family' as family, COUNT(*) as events
FROM events
WHERE event_type = 'market_data_event'
  AND created_at > NOW() - INTERVAL '1 hour'
GROUP BY family;
```

---

### Option 2 (HIGH IMPACT) — Replace `logsSubscribe` with Webhooks for Pool Detection

**Why this works**: The bot subscribes to Raydium V4 logs to detect new pool creation
events. There are only ~300–500 new Raydium V4 pools per day. But the subscription
delivers millions of swap notifications that the filter discards.

Helius Webhooks work differently: you configure a filter once, and Helius only sends
you the events that match. For new pool creation events, you would receive 300–500
notifications per day at **1 credit each** = 500 credits/day. Compare to the current
~1 million credits/day from streaming all Raydium V4 traffic.

**Savings ratio**: ~2,000x cheaper for the same new-pool detection capability.

**Limitation**: Webhooks have slightly higher latency than streaming (100–500ms delay
for HTTP delivery vs sub-millisecond for WebSocket). For the bot's current setup, this
is irrelevant because `min_token_age_seconds: 900` means tokens must be 15 minutes
old before they can even be approved. Being 0.5 seconds later on detection means nothing.

#### Step-by-Step Implementation

**Phase A — Create the Helius Webhook**

**Step 1**: Log in to the Helius Dashboard at `https://dashboard.helius.dev`

**Step 2**: Navigate to Webhooks → Create New Webhook

**Step 3**: Configure the webhook:

- **Webhook URL**: `https://your-server.com/webhooks/helius` (your bot's HTTP endpoint)
- **Transaction Type**: Select "Program Activity"
- **Program ID**: `675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8` (Raydium V4)
- **Webhook Type**: Raw Transaction (lower latency than Enhanced)

**Step 4**: Save the webhook. Note the webhook ID and secret key.

**Phase B — Add a Webhook Receiver to the Bot**

**Step 5**: Create the webhook handler file. This is a new HTTP handler that accepts
POST requests from Helius and converts them into the same `market_data_event` format
that the existing pipeline already understands.

The handler should live at: `internal/workers/webhook_receiver.go`

The handler receives a Helius webhook payload (a raw Solana transaction), checks if
it contains a `ray_log: Initialize2` instruction (the pool creation marker), and if so,
emits a `market_data_event` into the PostgreSQL event bus — the same event that the
WebSocket ingestion currently emits.

**Step 6**: Add the webhook HTTP endpoint to `cmd/server.go`. This is where all HTTP
routes are registered. Add a route like:

```go
// In cmd/server.go, near other HTTP handler registrations
mux.HandleFunc("/webhooks/helius", webhookReceiver.HandleHelisWebhook)
```

**Phase C — Disable the Raydium V4 WebSocket Subscription**

**Step 7**: Once the webhook is receiving events and they are appearing in the event bus,
disable the Raydium V4 WebSocket subscription from `shared/config/chains.yaml`:

```yaml
- program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
  family: raydium-v4
  disabled: true # using webhooks now — see internal/workers/webhook_receiver.go
```

**Verification**: After switching, check that new Raydium pools still appear in the
database within a few seconds of creation. Monitor the Helius Dashboard credit usage —
it should drop by 90%+ within the first hour.

---

### Option 3 (MEDIUM IMPACT) — Switch to `transactionSubscribe` with Account Filters

**Why this works**: Instead of disabling the WebSocket entirely, you can replace
`logsSubscribe` with the Helius `transactionSubscribe` extension. This method supports
an `accountRequired` filter that tells Helius: "only send me transactions where THIS
specific account is a required participant".

For Raydium V4 pool creation, you can specify the pool program authority account as
`accountRequired`. This filters out all swaps at the source — Helius never sends them —
which means no streaming bytes consumed for unwanted data.

**Cost**: Same 2 credits per 0.1 MB but you only receive 300-500 relevant transactions
per day (~1-2 KB each) instead of 30 million swap notifications. Effectively zero cost.

**Availability**: `transactionSubscribe` with Helius filters requires Developer plan or
above. You already have the Developer plan ($49/month).

#### Step-by-Step Implementation

**Step 1**: Find the WebSocket subscription code for Raydium V4:

```bash
grep -n "logsSubscribe\|programs\|675kPX9" internal/modules/ingestion_solana/ingestion_solana.go | head -20
```

**Step 2**: Change the subscription request from `logsSubscribe` to `transactionSubscribe`
with an `accountInclude` or `accountRequired` filter specifying the Raydium V4 pool
authority or the Initialize2 instruction discriminator account.

The Helius WebSocket endpoint is `wss://mainnet.helius-rpc.com/?api-key=YOUR_KEY`.

The subscription payload changes from:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "logsSubscribe",
  "params": [
    { "mentions": ["675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"] },
    { "commitment": "confirmed" }
  ]
}
```

To:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "transactionSubscribe",
  "params": [
    {
      "accountInclude": ["675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"],
      "accountRequired": ["5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"],
      "failed": false,
      "vote": false
    },
    {
      "commitment": "confirmed",
      "encoding": "base64",
      "maxSupportedTransactionVersion": 0
    }
  ]
}
```

Where `5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1` is the Raydium V4 authority
account (a required signer for pool creation but not for swaps). This effectively
filters to only pool-creation transactions.

---

### Option 4 — Fix the Wrong Comment in `shared/config/chains.yaml`

This is a documentation fix, not a functional fix, but it prevents future confusion.

**File**: `shared/config/chains.yaml`

Find this comment and correct it:

```yaml
# WRONG (current):
# Credit cost is minimal: ≤300 getTransaction calls/day × 100 credits = 900k credits/month worst-case

# CORRECT (should be):
# Credit cost is minimal: ≤300 getTransaction calls/day × 1 credit = ~108k credits/month worst-case
# (getTransaction is 1 credit per Helius docs — the 100 credits figure is for the
# Enhanced Transactions API (getTransactionsForAddress) which this bot does not use)
```

---

<a name="section-6"></a>

## Section 6 — Health Dashboard: At a Glance

| Component                   | Status        | Evidence                                       | Action Needed?                |
| --------------------------- | ------------- | ---------------------------------------------- | ----------------------------- |
| L0 Token Detection          | ✅ Working    | 2,871 tokens in 2h                             | No                            |
| L0.5 Rescan Worker          | ✅ Working    | 1,680 rescan completions                       | No                            |
| L1 DQ Hard Gates            | ✅ Correct    | Serial launcher + no-social rejection working  | No                            |
| L1 DQ Rate Limiter          | ⚠️ Bottleneck | 71.7% of tokens rate-limited                   | Fix via Section 8 changes     |
| L2–L10 Workers              | 🟡 Starved    | Zero events — waiting for first approved token | Fix by changing market target |
| Helius Credit Burn          | ❌ Critical   | ~2M credits/day — plan runs dry in ~5 days     | Fix via Section 5             |
| getTransaction Cost Comment | ⚠️ Wrong      | Says 100 credits, actual is 1 credit           | Fix the comment               |
| DAS Probe                   | ℹ️ Disabled   | Currently off — correct for now                | Enable only after credit fix  |
| AI Narrative Probe          | ✅ Accurate   | 84.7% scam_score ≥ 7 (correct for pump.fun)    | No                            |
| pump.fun-AMM Subscription   | ✅ Configured | pumpfun-amm is in programs list                | Confirm it's receiving events |

---

<a name="section-7"></a>

## Section 7 — Prioritized Action Plan

### Priority 0 — This Week (The Bot Cannot Operate Sustainably Without These)

---

#### P0-A: Fix the Credit Burn (Estimated effort: 2–4 hours)

**The problem**: Raydium V4 WebSocket subscription is consuming ~1–2 million credits per
day from streaming data. The Developer plan (10M credits/month) runs dry in ~5 days.

**The fastest fix**: Implement Option 1 from Section 5 (disable raw pump.fun) and
Option 2 or 3 (replace Raydium V4 logsSubscribe with webhooks or transactionSubscribe).

**How to know it worked**: Open the Helius Dashboard credit usage chart. Within 2 hours
of the change, the per-hour credit consumption rate should drop by 50–90%.

**Reference**: See [Section 5, Option 1](#option-1) and [Option 2](#option-2) for exact steps.

---

#### P0-B: Confirm pump.fun-AMM Events Are Being Received (Estimated effort: 30 minutes)

The config already includes pump.fun-AMM (`pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA`
with family `pumpfun-amm`). But we have not confirmed it is generating events.

**How to check**:

```sql
-- Run this in the database
SELECT
  metadata->>'family' as family,
  COUNT(*) as events_2h
FROM events
WHERE event_type = 'market_data_event'
  AND created_at > NOW() - INTERVAL '2 hours'
GROUP BY family
ORDER BY events_2h DESC;
```

If `pumpfun-amm` appears with a non-zero count, it is working. If it shows zero events,
the subscription may not be connected properly. Graduation tokens (pumpfun-amm events)
should appear at a rate of roughly 1-3 per hour.

---

### Priority 1 — Next Two Weeks (First Token Through the Pipeline)

---

#### P1-A: Implement Real Creator Attribution + OKX-Style "Dev token" Stats (Estimated effort: 1-2 days)

This is the direct fix for the "Pump.fun factory problem".

**Core idea**: stop treating the Pump.fun program/factory as the creator identity.
Track the **real wallet that initiated token creation** and compute serial-launcher
history from that wallet, not from the factory program.

This is exactly how explorer/UIs like OKX can show the "Dev token" panel:

- Dev token: X / Y
- Rug pull %
- Migrated %
- Golden gem %
- Dev address

They are not using a single "factory creator" for all tokens. They are grouping token
history by creator wallet and aggregating outcomes.

##### Why this solves Problem B

Problem B happens because all Pump.fun launches collapse into one identity
(`pump.fun factory`) so every token looks like "serial launcher 49".

When creator identity is switched to the real launch wallet:

- creator A can have 0 prior launches
- creator B can have 2 prior launches
- creator C can have 27 prior launches

Now `serial_launcher` becomes meaningful again because it reflects per-wallet behavior,
not platform-level factory volume.

##### Current code status (good news)

Most of this is already present in your ingestion layer:

- `NormalizePumpFunCreateFromLogs(...)` sets `CreatorAddress = event.User`
  (the real creator wallet from Pump.fun CreateEvent logs).
- `NormalizePumpFunAMMCreatePool(...)` sets `CreatorAddress = event.Creator`
  for graduated pool creation.
- `solana_creator_reputation` probe already queries creator history and marks
  `CreatorPrevTokenCountKnown=true` on success.

This means the architecture is already compatible with the screenshot-style mechanism.

##### Implementation plan

**Goal 1 — Hard guarantee that Pump.fun creator identity is wallet-level, never factory-level**

1. Add a normalization guard in ingestion:
   - If `CreatorAddress` equals known Pump.fun program IDs, mark as invalid creator identity.
   - Prefer event-derived creator (`event.User` / `event.Creator`) when available.
2. Add a DQ reject reason for invalid identity source:
   - `invalid_creator_identity` (fail-closed) when creator resolves to program/factory.
3. Add telemetry:
   - count of events with creator=program ID
   - count of corrected creator identities

**Goal 2 — Add creator profile aggregation (OKX-style Dev token panel backend)**

4. Add a creator profile table keyed by `(chain, creator_address)` storing:
   - `total_tokens`
   - `migrated_tokens`
   - `rug_tokens`
   - `golden_gem_tokens`
   - `last_seen_at`
5. Update profile on token lifecycle outcomes (append-only + idempotent updates).
6. Expose derived percentages:
   - `rug_pull_pct = rug_tokens / total_tokens`
   - `migrated_pct = migrated_tokens / total_tokens`
   - `golden_gem_pct = golden_gem_tokens / total_tokens`

**Goal 3 — Use creator profile in DQ serial-launcher logic**

7. Replace/augment raw count with creator profile count where available.
8. Keep fail-closed behavior when creator profile is unknown:
   - retain `unknown_creator_count` behavior from current DQ policy.
9. Add mode-aware threshold profile for serial launcher:
   - STRICT/BALANCED/EXPLORATION can use different max prior launch limits.

**Goal 4 — Surface the same operator visibility as the screenshot**

10. Add a read endpoint/query path for creator stats used by Telegram/operator UI.
11. Include in diagnostics:

- creator address
- dev token ratio (`wins / total`)
- rug/migrated/golden percentages

12. Add runbook query examples (see below).

##### Suggested SQL validation queries

Use these queries after implementation to verify the mechanism is correct.

```sql
-- 1) Ensure creator identity is wallet-like (not factory/program) for pumpfun events
SELECT
  COUNT(*) FILTER (WHERE payload->>'creator_address' = '6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P') AS creator_is_pumpfun_program,
  COUNT(*) AS total_pumpfun_events
FROM events
WHERE event_type = 'market_data_event'
  AND metadata->>'family' IN ('pumpfun', 'pumpfun-amm')
  AND created_at > NOW() - INTERVAL '24 hours';
```

```sql
-- 2) Top creators by launches (wallet-level identity)
SELECT
  payload->>'creator_address' AS creator,
  COUNT(*) AS launches
FROM events
WHERE event_type = 'market_data_event'
  AND created_at > NOW() - INTERVAL '7 days'
  AND COALESCE(payload->>'creator_address', '') <> ''
GROUP BY payload->>'creator_address'
ORDER BY launches DESC
LIMIT 20;
```

```sql
-- 3) Creator profile output used by "Dev token" style panel
SELECT
  creator_address,
  total_tokens,
  rug_tokens,
  migrated_tokens,
  golden_gem_tokens,
  CASE WHEN total_tokens > 0 THEN (rug_tokens::float / total_tokens) * 100 ELSE 0 END AS rug_pull_pct,
  CASE WHEN total_tokens > 0 THEN (migrated_tokens::float / total_tokens) * 100 ELSE 0 END AS migrated_pct,
  CASE WHEN total_tokens > 0 THEN (golden_gem_tokens::float / total_tokens) * 100 ELSE 0 END AS golden_gem_pct
FROM creator_profiles
ORDER BY total_tokens DESC
LIMIT 20;
```

##### Success criteria

This item is complete only when all conditions are true:

1. Pump.fun tokens no longer collapse to factory program identity.
2. `serial_launcher` is computed from creator wallet history, not program-level aggregate.
3. Operator output can display OKX-like dev stats per creator.
4. At least one token with creator history `< threshold` can pass creator gate while
   high-history wallets still fail as serial launchers.

##### Risk notes

- Wallet rotation/sybil behavior still exists (a scammer can use new wallets).
- This mechanism does not replace other DQ checks (social links, supply, holder distro,
  manipulation detection).
- It fixes identity correctness and improves serial-launcher precision; it is one layer,
  not a full anti-scam guarantee.

---

#### P1-B: Fix the DAS API Cost Comment (Estimated effort: 5 minutes)

**File to edit**: Check if there is any code comment or config comment that claims DAS
costs 1 credit. The actual cost per Helius docs is **10 credits per call**.

The DAS probe is currently disabled, which is correct. Before enabling it, make sure
any cost estimates in comments reflect the real 10-credit cost.

```bash
# Find any incorrect DAS cost references
grep -rn "DAS\|das_asset\|1 credit" config/ internal/app/config/ | grep -v ".git"
```

Update any comments that say "1 credit" for DAS to say "10 credits per Helius docs
(helius.dev/docs/billing/credits)".

---

#### P1-C: Verify End-to-End Pipeline With a Test Token (Estimated effort: 2-4 hours)

Until a real token passes Layer 1, you have no evidence that Layers 2–10 produce
correct outputs. This is the highest-risk unknown in the system.

**What to do**: Identify a known-good token that 10x'd in the past 30 days on Raydium.
Insert it directly into the event bus as a `market_data_event` with all quality flags
pre-set to "known and approved", bypassing the DQ layer. Then observe whether:

- Layer 2 produces a `feature_event` with sensible scores
- Layer 3 produces an `edge_event` (or explains why it didn't)
- Layer 4 produces `probability_event`, `slippage_event`, `latency_event`
- Layer 5 produces a `validated_edge_event` with ACCEPT or explains REJECT
- Layer 6 produces a `selection_event`
- Layer 7 produces an `allocation_event`

If any layer produces no output or errors, that is where a bug exists — and you find
it before any real money is at risk.

**SQL to insert a test event** (adapt the values to a real token):

```sql
INSERT INTO events (event_type, payload, metadata, created_at)
VALUES (
  'market_data_event',
  '{"token_address": "So11111...", "pool_address": "...", ...}',  -- real token fields
  '{"chain": "solana", "family": "pumpfun-amm", "test_mode": true}',
  NOW()
);
```

---

### Priority 2 — Month 1 (Shadow Trading Mode)

---

#### P2-A: Enable Shadow Trading Mode

Once at least one token has successfully traced through Layers 2–10 end-to-end, switch
execution to shadow mode. The bot will generate buy/sell signals but not submit
transactions.

**File to edit**: `shared/config/pipeline.yaml`

```yaml
# shared/config/pipeline.yaml
execution:
  mode: "shadow" # change from "live" to "shadow"
```

Run shadow mode for **minimum 2 weeks** and collect paper-trade P&L. Only proceed to
live trading if the paper-trade expectancy is positive over at least 30 completed trades.

---

#### P2-B: Review `min_token_age_seconds` for the Graduation Market

**File**: `shared/config/data_quality.yaml`

```yaml
thresholds:
  min_token_age_seconds: 900 # 15 minutes
```

For raw pump.fun tokens, 15 minutes is appropriate — you want some history before betting.
For pump.fun graduation tokens (pumpfun-amm), the token has already survived weeks on
the bonding curve before graduation. The 15-minute wait after graduation might be
unnecessarily late for the peak entry window.

During shadow trading, collect data on entry timing versus peak price. If you find that
graduation tokens peak in the first 3-5 minutes after Raydium listing, consider lowering
this to `300` (5 minutes) for the graduation market.

---

<a name="section-8"></a>

## Section 8 — Designer's Perspective: What Would Actually Make This Bot Profitable?

> This section sets architecture aside entirely. It looks only at the data, the market
> dynamics, and the specific numbers observed — and asks: if you were designing this bot
> from scratch to be profitable, what would you change?

---

### The Core Problem in Plain Terms

Imagine you are running a restaurant that only serves the highest-quality, fresh fish.
You bought a net and you are dragging it through the ocean. But 77% of everything you
catch is a type of fish your restaurant will never serve. Your whole supply chain is
designed to test and reject these fish one by one.

The fish are not the problem. The part of the ocean where you are casting the net is.

The bot is well-built. The filters are largely correct. But **the token universe it is
scanning is one where the answer is almost always "no"**. Instead of calibrating filters,
the first change to make is to fish in different water.

---

### Change 1: Stop Scanning Raw Pump.fun Bonding-Curve Tokens

**The evidence**: 2,208 out of 2,871 tokens (77%) in the observation window came from
pump.fun. Every single one was correctly rejected. The system spent credits and probe
budget to arrive at the same conclusion it could have predicted in advance.

**The action**: Disable the pump.fun bonding-curve program subscription in
`shared/config/chains.yaml`. This is already described in Section 5, Option 1.

**The reason this is safe**: The pump.fun-AMM subscription (graduation tokens) is
separate and should remain. What you are removing is the raw bonding-curve feed, not
all pump.fun-related data.

**Expected improvement**:

- Ingestion volume drops from ~24 tokens/min to ~5-7 tokens/min
- Rate-limiting problem largely disappears (30 probes/min is now more than enough)
- Credit savings: significant reduction in streaming data volume
- No loss of signal quality — these tokens were always going to be rejected

---

### Change 2: Focus on Pump.fun Graduation as the Primary Target Market

**What is graduation?** A pump.fun token "graduates" when its bonding curve reaches the
target market cap (~$69,000). At that point, the pump.fun protocol automatically creates
a Raydium AMM pool and moves the liquidity there. The token transitions from being
exclusive to pump.fun to being tradeable on open Raydium markets.

**Why graduation tokens are better**:

1. They have already survived a market test. The bonding curve requires real buyers
   to invest real money. Tokens that graduated had actual community support.
2. They have transaction history. You can see buy/sell patterns, holder distribution,
   and price action from the bonding-curve phase.
3. They come with instant Raydium liquidity. The protocol seeds the pool at graduation.
4. There is a known, repeatable pattern: graduation → Raydium listing → early momentum
   window (first 15-120 minutes) → price discovery.

**How many graduation tokens per day?** Roughly 20-50 tokens graduate per day out of
the thousands launched. This is a manageable volume that the probe system can handle
at 100% coverage without any rate-limit issues.

**Is pumpfun-amm already in the config?** Yes. Looking at `shared/config/chains.yaml`:

```yaml
- program_id: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
  family: pumpfun-amm
```

The infrastructure is ready. The P0-B verification step in Section 7 will confirm whether
it is producing events.

---

### Change 3: Relax the Creator Count Threshold Thoughtfully

**Current setting**: `max_creator_prev_token_count: 1`

**The evidence**: For pump.fun tokens, this correctly rejects them (factory has 49
launches). But for Raydium-native launches and graduation tokens, a developer with
1-3 prior tokens is not a scammer — they are experienced.

**The nuance**: The creator check for pump.fun tokens is handled differently because the
creator address resolves to the pump.fun factory regardless of the threshold. Even at
`max_creator_prev_token_count: 10`, every raw pump.fun token would still be rejected
because the factory has 49 launches. So raising this threshold is safe for non-pump.fun
markets without making pump.fun tokens suddenly pass.

**Revised guidance** (supersedes the "set to 4" recommendation): Do **not** raise the
global threshold to 4. Instead, implement the mode-aware design described in Section 9.
STRICT and BALANCED keep the global threshold of 1 (hard gate). EXPLORATION allows up
to 5 prior launches, and VERY_EXPLORATION allows up to 10, but only when quality gates
pass. The net effect is more permissive in exploration than a flat global raise, while
being equally protective in strict modes. Full design and implementation plan: see
[Section 9](#section-9).

---

### Change 4: Recalibrate the AI Probe for the Graduation Market

**The current data**:

- 84.7% of AI-scored tokens have scam_score ≥ 7
- Average scam_score: 7.69 out of 10

**Why this might be miscalibrated for graduations**: These numbers are accurate for
raw pump.fun bonding-curve tokens, which have copy-pasted descriptions and no real
project behind them. But a graduation token typically has:

- An active Telegram or Discord community (they needed it to raise $69K on the bonding curve)
- Marketing materials (they promoted the token to potential buyers)
- A narrative that was convincing enough to gather real investment

The same AI model that correctly scores raw pump.fun tokens at 7.69 might also score
legitimate graduation tokens unfairly high if it was calibrated on mostly-bad data.

**What to do before trusting the AI gate on graduation tokens**:

1. Find 20 tokens that graduated AND then 5x'd in price afterward (these are available
   via Birdeye or DexScreener historical data)
2. Run the AI probe on them in test mode
3. Check their scam_scores. If genuine 5x tokens are scoring 6-8, the threshold needs
   adjustment for this market segment.
4. If they score 2-4, the existing threshold is fine and no change is needed.

---

### Change 5: The Critical Unknown — What Happens After Layer 1?

This is the most important thing to fix and yet receives the least attention in day-to-day
operations: **Layers 2 through 10 have never run on a real token.**

The feature extraction, edge detection, probability model, capital sizing, and execution
engine have been written and wired together, but never tested with live data. This is a
massive risk. You do not know whether:

- The edge detection scores are calibrated correctly for the graduation market
- The probability model produces sensible win-probability estimates
- The capital allocation produces bet sizes that make economic sense
- The execution engine would successfully submit a transaction at the right time

**What happens if you wait until "everything is perfect" at L1 before looking at L2-L10?**
You could spend months perfecting the quality filter, get a perfect token through, and
then discover that L3 edge detection rejects it because of a miscalibrated threshold.
You wasted all that time optimizing the wrong thing.

**The recommended action**: As described in P1-C (Section 7), manually inject a
known-good historical token into the pipeline, bypass L1, and trace it through all layers.
Find the issues in L2-L10 now, while there is no money at risk.

---

### Change 6: The Path to the First Profitable Trade (Step by Step)

Here is the complete sequence of steps, in order, that leads from the current state to
a real profitable trade.

**Week 1 — Infrastructure**

1. Fix WebSocket streaming cost (Section 5, Option 1 + Option 2 or 3)
2. Disable raw pump.fun subscription
3. Verify pump.fun-AMM graduation events are arriving (P0-B)
4. Fix the wrong `100 credits` comment in chains.yaml

**Week 2 — Pipeline Validation**

5. Implement mode-aware serial launcher thresholds per [Section 9](#section-9)
   (STRICT/BALANCED keep global=1; EXPLORATION gets per-mode=5 with quality gates;
   VERY_EXPLORATION gets per-mode=10 with quality gates — do NOT raise the global value)
6. Monitor DQ logs — wait for the first `data_quality_accept` event
7. If no ACCEPT in 3 days, identify the next most common rejection reason and evaluate
   whether the threshold is justified (use the DQ log breakdown as a guide)
8. Inject a test token directly into L2 (bypass L1) and trace through all layers (P1-C)
9. Fix any issues found in L2-L10 during the test

**Week 3 — Shadow Trading**

10. Enable shadow mode: `execution.mode: "shadow"` in `shared/config/pipeline.yaml`
11. Let the bot run for 2 weeks in shadow mode
12. Monitor paper P&L in the database:

```sql
SELECT
  COUNT(*) as total_shadow_trades,
  AVG(realized_pnl_bps) as avg_pnl_bps,
  SUM(CASE WHEN realized_pnl_bps > 0 THEN 1 ELSE 0 END) as wins,
  SUM(CASE WHEN realized_pnl_bps <= 0 THEN 1 ELSE 0 END) as losses
FROM execution_results
WHERE created_at > NOW() - INTERVAL '14 days';
```

**Weeks 4-5 — Micro-Capital Live Trading**

13. If shadow P&L shows positive expectancy over ≥ 30 trades, switch to live mode
14. Fund the wallet with **micro-capital** ($50-100 maximum) to validate real execution
15. Monitor slippage, fill rates, and actual P&L vs shadow P&L
16. Only scale capital after confirming real execution matches shadow expectations

---

### The Bottom Line: What Would Make This Bot Profitable Today

If you had to summarize in a single paragraph:

> Remove the raw pump.fun subscription immediately. Confirm that graduation tokens
> (pumpfun-amm) are flowing through the pipeline. Implement mode-aware serial launcher
> thresholds (see Section 9) — STRICT/BALANCED keep the hard gate at 1, EXPLORATION
> allows up to 5 with quality guards, VERY_EXPLORATION allows up to 10. When the first
> graduation token gets a DQ ACCEPT, trace it all the way through the pipeline manually.
> Fix whatever is broken in L2-L10. Run shadow mode for 2-4 weeks on graduation tokens.
> If the paper expectancy is positive, start live trading with micro-capital. The
> infrastructure is correct — the market selection and the untested downstream pipeline
> are the two things standing between this bot and its first profitable trade.

---

<a name="section-9"></a>

## Section 9 — Mode-Aware Serial Launcher Threshold: Design Plan

> **Status**: Not yet implemented. This section documents the full design intent and
> implementation plan. Do not begin coding until P1-A (wallet-level creator attribution)
> is complete — this feature depends on correct creator identity to be meaningful.

### Why the Current Single Threshold Falls Short

The current setting `max_creator_prev_token_count: 1` in `shared/config/data_quality.yaml` is
a global threshold applied identically across all four operational modes (STRICT,
BALANCED, EXPLORATION, VERY_EXPLORATION).

This is correct for conservative modes — but it creates a problem in exploration modes.
The entire purpose of EXPLORATION is to widen the lens and find opportunities the bot
would otherwise miss. A serial launcher check that is equally strict in all modes
colludes the exploration mode back to strict behavior, defeating its purpose.

The fix is not to remove the check. It is to make it **mode-aware**: hard gate in
STRICT/BALANCED, conditional allow in EXPLORATION/VERY_EXPLORATION.

---

### The Design Rule

```
STRICT:           serial launcher = HARD REJECT (no exceptions, same as today)
BALANCED:         serial launcher = HARD REJECT (no exceptions, same as today)
EXPLORATION:      serial launcher = CONDITIONAL ALLOW
                    IF all quality gates pass AND fast exit is assured → RISKY_PASS
                    ELSE → SKIP (soft drop, not logged as a rejection)
VERY_EXPLORATION: serial launcher = CONDITIONAL ALLOW (wider count threshold)
                    IF all quality gates pass AND fast exit is assured → RISKY_PASS
                    ELSE → SKIP (soft drop, not logged as a rejection)
```

**"SKIP" versus "REJECT"**:

- `REJECT` blocks the token, logs a rejection reason, and contributes to DQ rejection
  rate statistics that feed the learning engine. It signals "this was bad quality".
- `SKIP` is a soft outcome: the token is silently dropped from the pipeline. No
  rejection reason is logged. This matters because a token that is "risky but tradeable
  under the right conditions" should not pollute the rejection statistics with false
  quality-failure signals.

---

### Quality Gate Conditions (EXPLORATION and VERY_EXPLORATION Only)

When a token's creator count exceeds the global threshold but is within the mode's
per-mode threshold, **all** of the following conditions must be true for the token to
receive a `RISKY_PASS` instead of being silently skipped:

| Condition                                   | EXPLORATION       | VERY_EXPLORATION  | Rationale                                                                  |
| ------------------------------------------- | ----------------- | ----------------- | -------------------------------------------------------------------------- |
| `HasSocialLinks` (verified)                 | `true` required   | `true` required   | No social presence = no community = no one to sustain price action         |
| `SocialLinksKnown` (probe ran successfully) | `true` required   | `true` required   | Cannot verify social presence if probe failed                              |
| `HolderCount`                               | ≥ 50              | ≥ 25              | Minimum distribution to have genuine buyers beyond the creator             |
| Risk score from other detectors             | < 0.40            | < 0.45            | Other risk signals must be low — creator risk is already elevated          |
| Position monitoring available               | kill switch armed | kill switch armed | Must be able to exit fast; if monitoring is degraded, skip unconditionally |

If **any** condition fails, the token is **SKIPPED** (not REJECTED). No rejection reason
is logged. The token is dropped from the pipeline without affecting quality metrics.

If **all** conditions pass, the token proceeds with:

1. Decision = `RISKY_PASS` (not `PASS`) — enters the pipeline with elevated risk flag
2. Flag `serial_launcher_monitored` added to `DataQualityDTO.Flags` — signals to Layers
   7 and 9 that tighter exit management is required
3. Layer 7 (Capital Engine) interprets `RISKY_PASS` as a signal to apply a smaller
   allocation than normal
4. Layer 9 (Position Engine) interprets `serial_launcher_monitored` to apply tighter
   trailing stop and earlier TP1 trigger

---

### Creator Launch Count Thresholds Per Mode

| Mode               | Per-mode max prior launches | When exceeded                              |
| ------------------ | --------------------------- | ------------------------------------------ |
| `STRICT`           | 0 (use global: 1)           | HARD REJECT → logs `serial_launcher`       |
| `BALANCED`         | 0 (use global: 1)           | HARD REJECT → logs `serial_launcher`       |
| `EXPLORATION`      | 5                           | Quality gates checked → RISKY_PASS or SKIP |
| `VERY_EXPLORATION` | 10                          | Quality gates checked → RISKY_PASS or SKIP |

The value `0` means "use the global `thresholds.max_creator_prev_token_count`". STRICT
and BALANCED inherit the global value automatically — no duplication, and a change to
the global threshold is reflected in both modes without separate config edits.

---

### Why Fast Exit Is Non-Negotiable for Serial Launchers

A serial launcher may be a legitimate developer iterating on projects, or may be a
wallet running a pattern of launch → pump → exit. The historical rug rate for wallets
with more than 3 prior launches is materially higher than for first-time launchers.

When exploration mode allows a serial launcher through, the bot is consciously accepting
elevated exit risk. Safety requires:

1. **Position monitoring is running** — Layer 9 (monitoring loop) must be active and
   polling price at every tick. If the monitoring loop is degraded, skip unconditionally.
2. **TP1 triggers earlier** — take first profit at a lower threshold than normal (e.g.,
   25–30% gain) to bank partial gains before any potential exit by the creator.
3. **Trailing stop is tighter** — lock in gains faster once momentum peaks.
4. **Kill switch priority** — if the kill switch fires, all `serial_launcher_monitored`
   positions must close first, before other open positions.

The `serial_launcher_monitored` flag is the mechanism that tells downstream layers to
apply these tighter parameters. If the flag is present, act fast. If the flag is absent,
normal exit parameters apply.

---

### Unknown Creator History in Exploration Modes

When `CreatorPrevTokenCountKnown=false` (probe timed out, API error, or probe disabled):

- **STRICT/BALANCED**: continue to hard-reject via `unknown_creator_count` (fail-closed,
  no change from current behavior)
- **EXPLORATION/VERY_EXPLORATION**: the token automatically fails the quality gate check
  (because `SocialLinksKnown` and `HolderCount` cannot be fully verified in conjunction
  with an unknown creator), so it is **SKIPPED** — not REJECTED

Exploration modes do not log `unknown_creator_count` rejections. They silently skip
tokens whose creator history could not be verified. This keeps the rejection rate
statistics clean and avoids sending false signals to the learning engine.

---

### Relationship to Section 7 P1-A and Section 8 Change 3

**Section 7 P1-A (Goal 9)** mentions "Add mode-aware threshold profile for serial
launcher" as the final goal of the creator attribution work. This section is the full
design for that goal.

**Section 8 Change 3** previously recommended raising the global threshold from 1 to 4.
That recommendation is superseded by this section. Do not raise the global threshold.
Instead, keep the global at 1 and implement per-mode overrides as described here.
The result is stricter in STRICT/BALANCED (stays at 1 not 4) and more permissive in
exploration (5 and 10 respectively) than a flat global raise.

**Sequencing requirement**: P1-A creator attribution (wallet-level identity, not
factory-level) must be complete before this section is implemented. If creator identity
still resolves to the pump.fun factory program at 49 launches, the per-mode thresholds
of 5 and 10 would not change any outcomes for pump.fun tokens (49 > 10 always).

---

### Implementation Plan

Four files need to change. No database migration required.

#### Step 1 — Add fields to `DataQualityModeProfile`

**File**: `internal/app/config/data_quality_runtime_config.go`

Add four new fields to the `DataQualityModeProfile` struct:

```go
type DataQualityModeProfile struct {
    RejectAbove        float64 `yaml:"reject_above"`
    RiskyPassAbove     float64 `yaml:"risky_pass_above"`
    UnknownFactor      float64 `yaml:"unknown_factor"`
    MinTokenAgeSeconds int32   `yaml:"min_token_age_seconds"`

    // MaxCreatorPrevTokenCount overrides the global
    // thresholds.max_creator_prev_token_count for this mode.
    //   0 = use global threshold (STRICT/BALANCED — no change in behavior)
    //  >0 = allow up to this many prior launches, subject to quality gates
    //       (EXPLORATION/VERY_EXPLORATION only)
    MaxCreatorPrevTokenCount int32 `yaml:"max_creator_prev_token_count"`

    // When MaxCreatorPrevTokenCount > 0 and the creator count exceeds the
    // global threshold but is within this mode's threshold, ALL three conditions
    // below must be true for the token to receive RISKY_PASS instead of SKIP.
    SerialLauncherRequiresSocialLinks bool    `yaml:"serial_launcher_requires_social_links"`
    SerialLauncherMaxRiskScore        float64 `yaml:"serial_launcher_max_risk_score"`
    SerialLauncherMinHolderCount      int32   `yaml:"serial_launcher_min_holder_count"`
}
```

#### Step 2 — Update YAML mode profiles

**File**: `shared/config/data_quality.yaml`, `mode_profiles` section

```yaml
mode_profiles:
  strict:
    reject_above: 0.30
    risky_pass_above: 0.15
    unknown_factor: 0.5
    min_token_age_seconds: 0
    max_creator_prev_token_count: 0 # use global (1) — hard gate unchanged
  balanced:
    reject_above: 0.50
    risky_pass_above: 0.25
    unknown_factor: 0.35
    min_token_age_seconds: 0
    max_creator_prev_token_count: 0 # use global (1) — hard gate unchanged
  exploration:
    reject_above: 0.65
    risky_pass_above: 0.35
    unknown_factor: 0.0
    min_token_age_seconds: -1
    max_creator_prev_token_count: 5 # allow up to 5 prior launches WITH quality gates
    serial_launcher_requires_social_links: true
    serial_launcher_max_risk_score: 0.40
    serial_launcher_min_holder_count: 50
  very_exploration:
    reject_above: 0.75
    risky_pass_above: 0.45
    unknown_factor: 0.0
    min_token_age_seconds: -1
    max_creator_prev_token_count: 10 # allow up to 10 prior launches WITH quality gates
    serial_launcher_requires_social_links: true
    serial_launcher_max_risk_score: 0.45
    serial_launcher_min_holder_count: 25
```

#### Step 3 — Replace serial launcher logic in `ProcessForMode()`

**File**: `internal/modules/data_quality/data_quality.go`

The current code has two sequential hard-reject blocks for serial launcher and unknown
creator count. Replace them with mode-aware logic:

```go
// Determine the effective serial launcher threshold for this mode.
// profile.MaxCreatorPrevTokenCount == 0 → use global (STRICT/BALANCED behavior unchanged).
// profile.MaxCreatorPrevTokenCount > 0  → exploration mode; apply quality gate logic.
effectiveMaxCreator := m.runtime.Thresholds.MaxCreatorPrevTokenCount
if profile.MaxCreatorPrevTokenCount > 0 {
    effectiveMaxCreator = profile.MaxCreatorPrevTokenCount
}

if effectiveMaxCreator > 0 {
    if in.CreatorPrevTokenCountKnown &&
        in.CreatorPrevTokenCount >= m.runtime.Thresholds.MaxCreatorPrevTokenCount {
        if profile.MaxCreatorPrevTokenCount == 0 {
            // Hard modes (STRICT/BALANCED): always hard-reject.
            rejectReasons = append(rejectReasons, "serial_launcher")
        } else {
            // Exploration modes: count exceeds global threshold; check quality gates.
            qualityOK := in.SocialLinksKnown && in.HasSocialLinks &&
                in.HolderCount >= profile.SerialLauncherMinHolderCount
            // Note: SerialLauncherMaxRiskScore is checked post-aggregation in
            // the decision phase, because RiskScore is not yet available here.
            if qualityOK {
                // Allowed through with elevated risk flag. RISKY_PASS applies.
                flags = append(flags, "serial_launcher_monitored")
            } else {
                // Quality gates failed → soft skip, not a hard reject.
                flags = append(flags, "serial_launcher_skipped")
                return buildSkipResult(in, flags, profileName) // new helper, see note
            }
        }
    }
    // Fail-closed: unknown creator history.
    if !in.CreatorPrevTokenCountKnown {
        if profile.MaxCreatorPrevTokenCount == 0 {
            // Hard modes: reject on unknown.
            if m.runtime.Thresholds.RejectUnknownCreatorCount {
                rejectReasons = append(rejectReasons, "unknown_creator_count")
            }
        } else {
            // Exploration modes: skip silently (unknown + exploration = don't risk it).
            flags = append(flags, "serial_launcher_skipped")
            return buildSkipResult(in, flags, profileName)
        }
    }
}
```

> **Note on `buildSkipResult`**: A new helper that returns a `DataQualityDTO` with
> `Decision: "SKIP"` and the provided flags. `"SKIP"` must be added as a valid decision
> value alongside `"PASS"`, `"RISKY_PASS"`, and `"REJECT"` in `shared/contracts/data_quality.go`.
> Callers of `ProcessForMode()` must handle `SKIP` by silently dropping the token
> without emitting a rejection event.

#### Step 4 — Update canonical fallback profiles in `decision.go`

**File**: `internal/modules/data_quality/decision.go`

Update the in-code hardcoded fallback map so the new fields have correct defaults even
if the YAML is missing the new keys during a hot reload:

```go
var canonicalProfile = map[string]config.DataQualityModeProfile{
    "STRICT":    {RejectAbove: 0.30, RiskyPassAbove: 0.15, UnknownFactor: 0.5,
                  MaxCreatorPrevTokenCount: 0},
    "BALANCED":  {RejectAbove: 0.50, RiskyPassAbove: 0.25, UnknownFactor: 0.0,
                  MaxCreatorPrevTokenCount: 0},
    "EXPLORATION": {
        RejectAbove: 0.65, RiskyPassAbove: 0.35, UnknownFactor: 0.0,
        MinTokenAgeSeconds: -1, MaxCreatorPrevTokenCount: 5,
        SerialLauncherRequiresSocialLinks: true,
        SerialLauncherMaxRiskScore: 0.40, SerialLauncherMinHolderCount: 50,
    },
    "VERY_EXPLORATION": {
        RejectAbove: 0.75, RiskyPassAbove: 0.45, UnknownFactor: 0.0,
        MinTokenAgeSeconds: -1, MaxCreatorPrevTokenCount: 10,
        SerialLauncherRequiresSocialLinks: true,
        SerialLauncherMaxRiskScore: 0.45, SerialLauncherMinHolderCount: 25,
    },
}
```

---

### Validation Queries

After implementation, use these to verify mode-aware behavior is working:

```sql
-- Serial launcher outcomes by mode
SELECT
  metadata->>'mode'     AS mode,
  payload->>'decision'  AS decision,
  COUNT(*) FILTER (WHERE payload->'flags' ? 'serial_launcher_monitored') AS monitored,
  COUNT(*) FILTER (WHERE payload->'flags' ? 'serial_launcher_skipped')   AS skipped,
  COUNT(*) FILTER (WHERE payload->>'rejection_reasons' LIKE '%serial_launcher%') AS hard_rejected
FROM events
WHERE event_type = 'data_quality_event'
  AND created_at > NOW() - INTERVAL '7 days'
GROUP BY mode, decision
ORDER BY mode, decision;
```

```sql
-- P&L outcomes for serial_launcher_monitored positions (run after live trading)
SELECT
  e.metadata->>'token_address'  AS token,
  r.realized_pnl_bps,
  r.exit_reason
FROM events e
JOIN execution_results r
  ON r.token_address = e.metadata->>'token_address'
WHERE e.event_type = 'data_quality_event'
  AND e.payload->'flags' ? 'serial_launcher_monitored'
  AND e.created_at > NOW() - INTERVAL '30 days'
ORDER BY r.realized_pnl_bps DESC;
```

---

<a name="section-10"></a>

## Section 10 — Market Cap & Volume Filtering via DEXScreener: Design Plan

> **Status**: Not yet implemented. This section documents the data availability finding
> and implementation plan. The data is already in an existing API response — no new
> provider or API key is required.

### Background: What a Token Scanner Shows vs. What This Bot Has

Standard token scanner UIs (e.g., GMGN.ai and similar) offer filter panels for new-launch
sniping. A typical filter set looks like:

| Filter               | Typical value | Status in this bot        | Notes                                                  |
| -------------------- | ------------- | ------------------------- | ------------------------------------------------------ |
| Market cap min       | $3,000        | ❌ Not implemented        | `MarketDataDTO` has no `MarketCapUsd` field            |
| Market cap max       | $20,000       | ❌ Not implemented        | Same field missing                                     |
| Volume min (1h)      | $100          | ❌ Not implemented        | No USD volume fields on DTO                            |
| Social link required | At least 1    | ✅ Implemented (stricter) | Requires verified profile URL, not just any link       |
| Token age min        | 1 hour        | ✅ Implemented            | Set to 15 min; tunable via `min_token_age_seconds`     |
| Timeframe selector   | 1m/5m/30m/1h  | N/A                       | Bot uses fixed rescan bands (15m → 48h); no UI concept |

The two missing filters — market cap range and volume floor — would eliminate a large
fraction of low-quality tokens early in the pipeline, before any probe budget is spent.

---

### Key Finding: The Data Is Already in the API Response

DEXScreener's free public API is already integrated. Every call to
`https://api.dexscreener.com/latest/dex/tokens/{address}` already returns market cap
and volume data in the response body. It is currently discarded.

**In `internal/rpc/price_fetcher.go`, the current parser struct**:

```go
type dexScreenerResponse struct {
    Pairs []struct {
        PriceNative string `json:"priceNative"`
        Liquidity   *struct {
            USD float64 `json:"usd"`
        } `json:"liquidity"`
    } `json:"pairs"`
}
```

This silently discards `marketCap`, `priceUsd`, and the entire `volume` object from
every response.

**What the full response actually contains**:

```json
{
  "pairs": [
    {
      "priceNative": "0.00000042",
      "priceUsd": "0.000067",
      "marketCap": 6700,
      "liquidity": { "usd": 15000 },
      "volume": {
        "m5": 210.5,
        "h1": 4850.0,
        "h6": 12300.0,
        "h24": 18400.0
      }
    }
  ]
}
```

**Cost of parsing these fields**: Zero. Same HTTP call, more fields parsed. No increase
in Helius credits, no new API key, no new external dependency.

---

### Why Helius Cannot Replace DEXScreener for This

Helius is a Solana RPC node provider. It gives access to on-chain state: account balances,
transaction history, program logs, and token account data. It does not compute USD market
caps, aggregate trading volume across time windows, or provide price history. Those are
derived analytics that require an off-chain oracle or aggregator.

DEXScreener is already the correct and only tool needed. No change to the data source.

---

### Three Changes Required for Market Cap / Volume Filters

No database migration is required. Changes are additive — no existing DTO fields are
modified.

#### Change 1 — Expand the DEXScreener response parser

**File**: `internal/rpc/price_fetcher.go`

```go
// Before:
type dexScreenerResponse struct {
    Pairs []struct {
        PriceNative string `json:"priceNative"`
        Liquidity   *struct {
            USD float64 `json:"usd"`
        } `json:"liquidity"`
    } `json:"pairs"`
}

// After:
type dexScreenerResponse struct {
    Pairs []struct {
        PriceNative string  `json:"priceNative"`
        PriceUsd    string  `json:"priceUsd"`
        MarketCap   float64 `json:"marketCap"`
        Liquidity   *struct {
            USD float64 `json:"usd"`
        } `json:"liquidity"`
        Volume *struct {
            M5  float64 `json:"m5"`
            H1  float64 `json:"h1"`
            H6  float64 `json:"h6"`
            H24 float64 `json:"h24"`
        } `json:"volume"`
    } `json:"pairs"`
}
```

The parsed values must then be propagated into `MarketDataDTO` by whichever probe
assembles the DTO before `ProcessForMode()` runs (see note on architecture below).

#### Change 2 — Add fields to `MarketDataDTO`

**File**: `shared/contracts/market_data.go`

Add four new fields alongside the existing `LiquidityUsd`:

```go
// MarketCapUsd is the token's total market capitalisation in USD at the time
// the DEXScreener probe ran. Zero means the data was not available yet
// (token too new, pair not yet indexed). A zero value disables the market
// cap filter so brand-new tokens are not incorrectly rejected.
MarketCapUsd float64

// VolumeUsd5m / VolumeUsd1h / VolumeUsd24h are cumulative USD trading
// volume over each window, sourced from DEXScreener. Zero means not available.
VolumeUsd5m  float64
VolumeUsd1h  float64
VolumeUsd24h float64
```

These fields are additive. All existing DTO consumers are unaffected because they do
not reference fields they do not already use.

#### Change 3 — Add thresholds and structural reject logic

**File**: `shared/config/data_quality.yaml`, add to `thresholds` section:

```yaml
thresholds:
  # Existing fields unchanged ...

  # Market cap range filter (DEXScreener data).
  # Only applied when MarketCapUsd > 0 (i.e., pair is indexed on DEXScreener).
  # Commented out by default — enable and tune after confirming graduation token
  # market cap distribution in shadow mode.
  # min_market_cap_usd: 3000.0    # below $3k = insufficient real capital in the pool
  # max_market_cap_usd: 20000.0   # above $20k = opportunity already discovered

  # Volume floor (DEXScreener data).
  # Only applied when VolumeUsd1h > 0.
  # min_volume_usd_1h: 100.0      # below $100/h = no real trading interest
```

**File**: `internal/modules/data_quality/data_quality.go`, add to structural rejects:

```go
// Optional: market cap range filter.
// Guards on > 0 on the input field ensure brand-new tokens not yet indexed
// by DEXScreener are not incorrectly rejected.
if m.runtime != nil {
    thresholds := m.runtime.Thresholds
    if thresholds.MinMarketCapUsd > 0 && in.MarketCapUsd > 0 {
        if in.MarketCapUsd < thresholds.MinMarketCapUsd {
            rejectReasons = append(rejectReasons, "market_cap_too_low")
        }
    }
    if thresholds.MaxMarketCapUsd > 0 && in.MarketCapUsd > 0 {
        if in.MarketCapUsd > thresholds.MaxMarketCapUsd {
            rejectReasons = append(rejectReasons, "market_cap_too_high")
        }
    }
    if thresholds.MinVolumeUsd1h > 0 && in.VolumeUsd1h > 0 {
        if in.VolumeUsd1h < thresholds.MinVolumeUsd1h {
            rejectReasons = append(rejectReasons, "volume_too_low")
        }
    }
}
```

You also need three new fields in `DataQualityDetectorThresholds` in
`internal/app/config/data_quality_runtime_config.go`:

```go
MinMarketCapUsd float64 `yaml:"min_market_cap_usd"`
MaxMarketCapUsd float64 `yaml:"max_market_cap_usd"`
MinVolumeUsd1h  float64 `yaml:"min_volume_usd_1h"`
```

---

### Where DEXScreener Data Is Fetched During the Pipeline

The current architecture uses DEXScreener in two places:

1. **Position monitoring** (`internal/rpc/price_fetcher.go`): called by Layer 9 to get
   current price for TP/SL evaluation. This is where the parser struct lives.
2. **Token probes** (`internal/modules/probes/`): some probes query DEXScreener for
   liquidity and pair info during Layer 1 DQ evaluation.

Market cap and volume data is most valuable **during Layer 1 DQ** — deciding whether to
even spend probe budget on a token. The cleanest implementation:

- The DEXScreener liquidity probe (already in Layer 1 probes) fetches the response and
  populates `LiquidityUsd` today. Extend it to also populate `MarketCapUsd`,
  `VolumeUsd5m`, `VolumeUsd1h`, `VolumeUsd24h` on the `MarketDataDTO`.
- `ProcessForMode()` then sees these fields already populated when structural reject
  logic runs.
- The position monitoring path (`price_fetcher.go`) can optionally parse these fields
  too for observability, but it is not on the DQ critical path.

---

### Recommended Default Thresholds (Starting Points Only)

These values match the screenshot filter settings and are reasonable starting points.
Do not lock them in before running shadow mode on the graduation token universe:

| Filter               | Starting value | Why / Risk                                                       |
| -------------------- | -------------- | ---------------------------------------------------------------- |
| `min_market_cap_usd` | `3000.0`       | Below $3k = almost no real capital in the pool                   |
| `max_market_cap_usd` | `20000.0`      | Above $20k = entry window has passed for most sniping strategies |
| `min_volume_usd_1h`  | `100.0`        | Below $100/h = no real trading activity                          |

> **Important caveat**: A pump.fun graduation token transitions from bonding curve to
> Raydium at the point the bonding curve reaches its ~$69k market cap target. Immediately
> after graduation, the token's market cap on DEXScreener may be above the $20k max
> filter. Tune `max_market_cap_usd` carefully after observing real graduation market cap
> distributions in shadow mode — do not set it blindly.

---

## Appendix A — Credit Calculator Reference

Use this to estimate monthly credit costs before making changes:

| Activity                                 | Cost            | Monthly Estimate |
| ---------------------------------------- | --------------- | ---------------- |
| getTransaction (current: 21,549/day)     | 1 credit/call   | 645,000/month    |
| getAccountInfo (current: 8,334/day)      | 1 credit/call   | 250,000/month    |
| getTokenLargestAccounts (556/day)        | 1 credit/call   | 17,000/month     |
| DAS getAsset (if enabled at 2,871/day)   | 10 credits/call | 860,000/month    |
| WebSocket: Raydium V4 all traffic        | 2 credits/0.1MB | **1–15M+/month** |
| WebSocket: Graduation-only events        | 2 credits/0.1MB | ~10,000/month    |
| Webhooks: pool creation (500 events/day) | 1 credit/event  | 15,000/month     |
| Pyth SOL/USD price (every 30s)           | 1 credit/call   | 86,400/month     |

**Key insight**: Reducing WebSocket streaming from "all Raydium V4" to "pool creation only"
reduces streaming credits by 99%+ and brings total monthly consumption well within the
10M Developer plan budget.

---

## Appendix B — Key Configuration File Reference

| What to change                          | File                       | Key field                                               |
| --------------------------------------- | -------------------------- | ------------------------------------------------------- |
| Disable/enable program subscriptions    | `shared/config/chains.yaml`       | `solana.programs[].disabled`                            |
| Probe rate limit per minute             | `shared/config/pipeline.yaml`     | `probes.rate_limit_per_min`                             |
| Creator launch count threshold (global) | `shared/config/data_quality.yaml` | `thresholds.max_creator_prev_token_count`               |
| Creator count per mode (exploration)    | `shared/config/data_quality.yaml` | `mode_profiles.<mode>.max_creator_prev_token_count`     |
| Serial launcher quality gate (risk)     | `shared/config/data_quality.yaml` | `mode_profiles.<mode>.serial_launcher_max_risk_score`   |
| Serial launcher quality gate (holders)  | `shared/config/data_quality.yaml` | `mode_profiles.<mode>.serial_launcher_min_holder_count` |
| Minimum token age to qualify            | `shared/config/data_quality.yaml` | `thresholds.min_token_age_seconds`                      |
| Market cap minimum filter               | `shared/config/data_quality.yaml` | `thresholds.min_market_cap_usd`                         |
| Market cap maximum filter               | `shared/config/data_quality.yaml` | `thresholds.max_market_cap_usd`                         |
| Volume floor (1h)                       | `shared/config/data_quality.yaml` | `thresholds.min_volume_usd_1h`                          |
| Helius RPC HTTP rate limit              | `shared/config/chains.yaml`       | `solana.get_transaction_rps`                            |
| Trading mode (live/shadow)              | `shared/config/pipeline.yaml`     | `execution.mode`                                        |
| Max open positions                      | `shared/config/pipeline.yaml`     | `selection.max_open_positions`                          |
| Fixed entry size                        | `shared/config/pipeline.yaml`     | `capital.fixed_entry_size_usd`                          |

---

## Appendix C — Helius Plan Comparison

| Plan         | Price | Credits/month | WebSocket Rate | `transactionSubscribe` | LaserStream gRPC Mainnet |
| ------------ | ----- | ------------- | -------------- | ---------------------- | ------------------------ |
| Free         | $0    | 1M            | Standard only  | ❌ No                  | ❌ No                    |
| Developer    | $49   | 10M           | Enhanced ✅    | ✅ Yes                 | ❌ No                    |
| Business     | $499  | 100M          | Enhanced ✅    | ✅ Yes                 | ✅ Yes                   |
| Professional | $999  | 200M          | Enhanced ✅    | ✅ Yes                 | ✅ Yes + Shred Delivery  |

**Current plan**: Developer ($49/month, 10M credits). Upgrade to Business only if
you need LaserStream gRPC filtering for very low-latency detection — not needed at
the current stage.

---

_This document was created from a 2-hour observation window (2026-05-19 14:25–16:25 UTC),
Helius API documentation (helius.dev, verified 2026-05-20), and analysis of the live
configuration files in this repository._
