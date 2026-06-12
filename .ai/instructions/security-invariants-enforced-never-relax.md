- **HTTPS only for Jito bundle URLs** — `NewJitoClient` rejects any non-HTTPS URL unless `shadow_mode: true` or the URL is a loopback address (`http://127.` / `http://localhost`) for test servers. Never disable this check in production code.
- **Chain allowlist for DEXScreener** — `CopyTradeProvider` accepts only `ethereum`/`eth`, `bsc`/`bnb`, `solana`/`sol`, `base`. Unknown chains return an error (fail-closed). No passthrough allowed.
- **gRPC auth tokens from env vars only** — `SOLANA_GRPC_TOKEN` is read exclusively via `os.Getenv`. The field `GrpcAuthToken` is intentionally absent from `TransportConfig`, `IngestionTransportConfig`, and `config/chains.yaml`. Never add it back.
- **API keys never in YAML** — all external API keys (`BIRDEYE_API_KEY`, `TWITTER_BEARER_TOKEN`, `COPY_TRADE_WALLETS`, `JITO_BUNDLE_URL`, `JITO_TIP_ACCOUNT`, `GITHUB_COPILOT_TOKEN`, etc.) are read via `os.Getenv` at constructor only. Never log, never config-file.
- **Response bodies are bounded** — Jito HTTP response: 64 KiB cap. DEXScreener copy-trade: 128 KiB cap. Copilot AI response: 4 KiB cap (`max_response_bytes`). Never use `io.ReadAll` without a `LimitReader`.
- **AI enrichment is 1-shot and autonomous** — `GroqClient.Complete()` issues one HTTP request (plus one retry on 429/5xx) and returns immediately. No human approval gate, no interaction loop. All callers are fail-open (`NarrativeKnown=false` / `AIExplanationKnown=false` on any error). The pipeline is never blocked by AI calls.
- **AI model is configurable, never hardcoded** — model priority: `AI_ENRICH_MODEL` env var (highest) → `ai_enrichment.model` in `config/pipeline.yaml` → built-in default `llama-3.3-70b-versatile`. This follows the same pattern as `MODEL_HEAVY="${MODEL_HEAVY:-claude-opus-4.7}"` in `scripts/run_parallel.sh`. Never hardcode a model name in Go source.
- **HTTPS only for AI enrichment endpoint** — `NewGroqClient` rejects any non-HTTPS endpoint at construction (same invariant as Jito). Default endpoint: `https://api.groq.com/openai/v1/chat/completions`.
- **RPC error messages are truncated** — `truncate(msg, 200)` before surfacing in returned errors or logs. Never expose raw RPC error strings of arbitrary length.
- **Mandatory DQ hard-rejects (fail-closed, never relax)** — Layer 1 enforces three mandatory structural hard-rejects that cannot be bypassed by any operational mode, starvation condition, or profit argument:
  1. **Serial launcher dev** — any creator wallet with ≥ `max_creator_prev_token_count` prior launches is REJECTED via `serial_launcher`. When the creator reputation probe fails (`CreatorPrevTokenCountKnown=false`), reject via `unknown_creator_count` (fail-closed). Config: `reject_unknown_creator_count: true`.
  2. **No real social profile / website** — tokens with no profile-level Twitter/X (profile URL, not tweet link) or Telegram and no real project website are REJECTED via `no_social_links`. Websites pointing to DEX scanners, pump.fun pages, or known non-project domains (dexscreener.com, birdeye.so, solscan.io, raydium.io, jup.ag, etc.) are not accepted. When the metadata probe fails (`SocialLinksKnown=false`), reject via `unknown_social_links` (fail-closed). Config: `reject_unknown_social_links: true`.
  3. **Excessive total supply** — tokens with supply > `max_total_supply` (1B canonical) are REJECTED via `high_total_supply`. When the LP probe fails (`TotalSupplyKnown=false`), reject via `unknown_total_supply` (fail-closed). Config: `reject_unknown_total_supply: true`.

  **Never add conditional logic that bypasses these three rejects.** The canonical implementation is in `internal/modules/data_quality/data_quality.go` (`ProcessForMode`) and `internal/modules/probes/solana_metadata.go` (`isSocialProfileURL`, `isTwitterProfileURL`, `isBlockedWebsiteDomain`, `isSocialMediaWebsiteDomain`). See `docs/architecture.md` § 3.1.11 for the canonical specification.

  **Twitter/X profile URL validation rules** (enforced in `isTwitterProfileURL` via `net/url.Parse` — positive validation):
  - Allowed hosts: `twitter.com`, `www.twitter.com`, `x.com`, `www.x.com` only
  - `t.co` short-links are always rejected (redirects, not profiles)
  - Exactly **one** path segment required — multi-segment paths (tweets, internal routes) are rejected
  - Reserved top-level paths rejected: `i`, `search`, `intent`, `explore`, `hashtag`, `home`, `settings`, `notifications`, `messages`, `help`, `login`, `signup`, `logout`, `about`, `privacy`, `tos`
  - Non-standard ports rejected (`parsed.Port() != ""`) — `https://twitter.com:8080/user` is not a real profile
  - `@` in path rejected (`strings.Contains(path, "@")`) — Twitter usernames never contain `@`
  - URLs without scheme (e.g. `twitter.com/user`) are rejected because `parsed.Hostname()` returns `""`

  **Website field social-media blocking** (enforced in `isSocialProfileURL` via `isSocialMediaWebsiteDomain`):
  - A "website" metadata field must point to a real project website, not a social-media platform
  - Blocked domains: `twitter.com`, `x.com`, `t.me`, `telegram.me`, `telegram.org`, `discord.com`, `discord.gg`, `discordapp.com`, `facebook.com`, `fb.com`, `instagram.com`, `tiktok.com`, `youtube.com`, `youtu.be`, `medium.com`, `linktr.ee`, `reddit.com`, `bio.link`
  - DEX-scanner / pump-platform domains are blocked separately via `isBlockedWebsiteDomain`