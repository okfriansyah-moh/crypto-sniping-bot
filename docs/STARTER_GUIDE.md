# Starter Guide

> Complete beginner-to-production playbook for running the crypto-sniping-bot on your machine,
> in Docker, and on a VPS or cloud server. Written for macOS, Linux, and Windows.
> Includes every environment variable, every API key you need, and exactly how to obtain them.

---

> **Who is this guide for?** Someone who has never run a Go application or a trading bot before.
> Every step is explained in plain language. Do not skip sections — they build on each other.

---

## Table of Contents

1. [Understanding the Stack](#1-understanding-the-stack)
2. [Prerequisites — Install Required Software](#2-prerequisites--install-required-software)
   - [macOS](#21-macos)
   - [Linux (Ubuntu/Debian)](#22-linux-ubuntudebian)
   - [Windows](#23-windows)
3. [Obtaining All Required Credentials](#3-obtaining-all-required-credentials)
   - [EVM RPC Endpoints (Infura, Alchemy, QuickNode)](#31-blockchain-rpc-endpoints)
   - [Telegram Bot Token](#32-telegram-bot-token)
   - [EVM Wallet Private Key](#33-crypto-wallet-private-key)
   - [Solana RPC Endpoints (Phase 7)](#34-solana-rpc-endpoints-phase-7)
   - [Solana Wallet Keypair (Phase 7)](#35-solana-wallet-keypair-phase-7)
   - [BirdEye API Key (Phase 9 — optional)](#36-birdeye-api-key-phase-9--optional)
   - [Jito Bundle Credentials (Phase 11 — optional)](#37-jito-bundle-credentials-phase-11--optional)
   - [Solana gRPC Credentials (Phase 11 — optional)](#38-solana-grpc-transport-credentials-phase-11--optional)
   - [Copy-Trade Alpha Wallets (Phase 9 — optional)](#39-copy-trade-alpha-wallets-phase-9--optional)
4. [Clone the Repository](#4-clone-the-repository)
5. [Configure the Application](#5-configure-the-application)
   - [Create .env File](#51-create-env-file--all-environment-variables)
   - [Edit YAML Config Files](#52-edit-yaml-config-files)
6. [Database Setup](#6-database-setup)
7. [Run the Bot — Local](#7-run-the-bot--local)
8. [Run with Docker](#8-run-with-docker)
9. [Run on VPS or Cloud](#9-run-on-vps-or-cloud)
   - [Provider Comparison for Jakarta, Indonesia](#91-provider-comparison-for-jakarta-indonesia)
   - [Setup on a VPS Step-by-Step](#92-setup-on-a-vps-step-by-step)
10. [Monitoring & Health Check](#10-monitoring--health-check)
11. [Common Errors and Fixes](#11-common-errors-and-fixes)
12. [Reference — All Environment Variables](#12-reference--all-environment-variables)
13. [Phase 7 – 11 Features Overview](#13-phase-7--11-features-overview)
14. [Scenario Testing: Local Docker → VPS → Mainnet](#14-scenario-testing-local-docker--vps--mainnet)
15. [Optional Add-ons — Skip Until Profitable](#15-optional-add-ons--skip-until-profitable)

---

## 1. Understanding the Stack

Before touching any code, understand what this bot actually is and what it needs to run.

### What the bot does

This is a **DEX (Decentralized Exchange) sniping bot**. It monitors blockchain events in real time,
detects new token launches on Uniswap (Ethereum) and PancakeSwap (BSC), scores them through an
11-layer analysis pipeline, and executes buy/sell trades automatically.

### What the bot is made of

| Component            | Technology                                        | Why                                         |
| -------------------- | ------------------------------------------------- | ------------------------------------------- |
| Application language | **Go 1.25**                                       | Fast, compiled, low memory usage            |
| Database             | **PostgreSQL**                                    | Stores all pipeline state, events, trades   |
| EVM blockchain       | **RPC endpoints** (Infura, Alchemy, or QuickNode) | ETH/BSC: read data, submit EVM transactions |
| Solana blockchain    | **Solana RPC endpoints** (Helius, Triton, QN)     | SOL: Raydium/PumpFun sniping (Phase 7)      |
| Notifications        | **Telegram Bot API**                              | Sends alerts and accepts operator commands  |
| EVM wallet           | **Ethereum private key**                          | Signs EVM transactions (ETH/BSC)            |
| Solana wallet        | **Ed25519 keypair JSON file**                     | Signs Solana transactions (Phase 7)         |

### Minimum hardware requirements

| Environment         | CPU     | RAM  | Disk      | Network       |
| ------------------- | ------- | ---- | --------- | ------------- |
| Local dev / testing | 2 cores | 4 GB | 10 GB     | Any broadband |
| Production (local)  | 4 cores | 8 GB | 20 GB     | 50 Mbps+      |
| VPS (recommended)   | 2 vCPUs | 4 GB | 40 GB SSD | Datacenter    |

---

## 2. Prerequisites — Install Required Software

You need three things installed: **Go**, **PostgreSQL**, and **Git**.
Follow the section for your operating system.

---

### 2.1 macOS

#### Step 1 — Install Homebrew (package manager for macOS)

Open **Terminal** (press `Cmd + Space`, type "Terminal", press Enter) and run:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

After it finishes, close and reopen Terminal. Verify:

```bash
brew --version
# Should print: Homebrew 4.x.x
```

#### Step 2 — Install Go

```bash
brew install go
```

Verify:

```bash
go version
# Should print: go version go1.25.x darwin/amd64  (or arm64 for M1/M2/M3 Macs)
```

#### Step 3 — Install PostgreSQL

```bash
brew install postgresql@16
```

Start PostgreSQL and set it to start automatically on login:

```bash
brew services start postgresql@16
```

Add PostgreSQL tools to your PATH (so you can run `psql` from anywhere):

```bash
echo 'export PATH="/opt/homebrew/opt/postgresql@16/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

> **Note for Intel Macs:** Replace `/opt/homebrew` with `/usr/local` in the path above.

Verify PostgreSQL is running:

```bash
psql --version
# Should print: psql (PostgreSQL) 16.x
```

#### Step 4 — Install Git

```bash
brew install git
```

```bash
git --version
# Should print: git version 2.x.x
```

---

### 2.2 Linux (Ubuntu / Debian)

Open a terminal. All commands below require `sudo`.

#### Step 1 — Update system packages

```bash
sudo apt update && sudo apt upgrade -y
```

#### Step 2 — Install Go

```bash
# Download Go 1.25 (adjust URL if a newer version is available at https://go.dev/dl/)
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz

# Remove any existing Go installation and extract the new one
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz

# Add Go to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc
```

Verify:

```bash
go version
# Should print: go version go1.25.0 linux/amd64
```

#### Step 3 — Install PostgreSQL

```bash
sudo apt install -y postgresql postgresql-contrib
```

Start PostgreSQL and enable it to start on boot:

```bash
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

Verify:

```bash
psql --version
# Should print: psql (PostgreSQL) 16.x
```

#### Step 4 — Install Git

```bash
sudo apt install -y git
git --version
```

---

### 2.3 Windows

> **Recommended approach for Windows:** Use **WSL 2** (Windows Subsystem for Linux). This gives
> you a real Linux environment inside Windows and avoids many Windows-specific compatibility issues.
> The instructions below cover both native Windows and WSL 2.

#### Option A — WSL 2 (Recommended for Windows users)

1. Open **PowerShell as Administrator** and run:
   ```powershell
   wsl --install
   ```
2. Restart your computer when prompted.
3. After restart, Ubuntu will install automatically. Set a username and password.
4. Inside the Ubuntu terminal, follow the **Linux (Ubuntu/Debian)** instructions above exactly.

#### Option B — Native Windows

**Install Go:**

1. Go to [https://go.dev/dl/](https://go.dev/dl/)
2. Download the Windows installer (`.msi` file) for Go 1.25
3. Run the installer — accept all defaults
4. Open a new Command Prompt and verify:
   ```cmd
   go version
   ```

**Install PostgreSQL:**

1. Go to [https://www.postgresql.org/download/windows/](https://www.postgresql.org/download/windows/)
2. Download the installer for PostgreSQL 16
3. Run the installer:
   - Set a password for the `postgres` user — **remember this password**
   - Keep the default port `5432`
   - Keep all components selected
4. After install, PostgreSQL starts automatically as a Windows service.
5. Open **pgAdmin 4** (installed alongside PostgreSQL) to manage your database visually.

**Install Git:**

1. Go to [https://git-scm.com/download/win](https://git-scm.com/download/win)
2. Download and run the installer — accept all defaults
3. Open **Git Bash** (not Command Prompt) for all subsequent commands

> **For Windows users:** Use **Git Bash** for all commands in this guide unless stated otherwise.

---

## 3. Obtaining All Required Credentials

You need up to nine types of credentials:

1. **EVM RPC endpoint URLs** — to read from and write to Ethereum and BSC
2. **Telegram Bot Token + Chat ID** — to receive alerts and send commands
3. **EVM Wallet private key** — to sign ETH/BSC trades
4. **Solana RPC endpoint URLs** — to read from and write to Solana (Phase 7, required only if running Solana chain)
5. **Solana Wallet keypair JSON file** — to sign Solana trades (Phase 7, required only if running Solana chain)
6. **BirdEye API key** — enriches dev reputation and token metadata DQ checks (Phase 9, optional)
7. **Jito bundle URL + tip account** — Solana MEV protection via Jito block-engine (Phase 11, optional)
8. **Solana gRPC endpoint + auth token** — ultra-low-latency Solana ingestion via Yellowstone gRPC (Phase 11, optional)
9. **Copy-trade alpha wallet addresses** — smart wallet tracking for copy-trade DQ signal (Phase 9, optional)

> **Minimum setup:** To run the bot with only Ethereum or BSC, you only need credentials 1–3.
> Solana credentials (4–5) are only required when you enable Solana in `config/chains.yaml`.
> Credentials 6–9 are optional enrichments — the bot runs without them at reduced DQ coverage.

---

### 3.1 EVM Blockchain RPC Endpoints (Ethereum & BSC)

An RPC (Remote Procedure Call) endpoint is a URL that lets your bot talk to the Ethereum or BSC
blockchain. You cannot run the bot without at least one RPC endpoint.

You need **two types** per chain:

- **HTTP endpoint** — for querying data (used for most operations)
- **WebSocket endpoint** — for real-time event subscriptions (used for detecting new pool launches)

#### Option A — Infura (Beginner Friendly, Free Tier Available)

Infura is one of the oldest and most reliable Ethereum node providers. It has a generous free tier.

**How to sign up and get your API key:**

1. Go to [https://infura.io](https://infura.io) and click **Sign Up**
2. Enter your email and create a password. Verify your email.
3. After logging in, click **Create New API Key**
4. Select **Web3 API** as the network
5. Give it a name (e.g., "sniper-bot") and click **Create**
6. You will see your **API Key** (a 32-character string like `a1b2c3d4e5f6...`)

**Your endpoints will be:**

```
# Ethereum Mainnet HTTP
ETH_RPC_1=https://mainnet.infura.io/v3/YOUR_API_KEY

# Ethereum Mainnet WebSocket
ETH_WS_1=wss://mainnet.infura.io/ws/v3/YOUR_API_KEY
```

> **Infura Free Tier Limits:** 100,000 requests/day. For active trading, you will likely exceed
> this — upgrade to the "Developer" plan ($50/month) or use Alchemy as a fallback.

> **Important:** Infura does **not** have a Southeast Asia server. All requests from Jakarta will
> route to Infura's US/EU servers, adding ~170–200ms of network latency. Use QuickNode's
> Singapore endpoint for lower latency (see Option C below).

---

#### Option B — Alchemy (Better Free Tier, Recommended as Backup)

Alchemy offers 300 million "compute units" free per month, which is more generous than Infura.

**How to sign up and get your API key:**

1. Go to [https://www.alchemy.com](https://www.alchemy.com) and click **Get started for free**
2. Sign up with email or Google account
3. After logging in, click **+ Create new app** in the top right
4. Fill in:
   - Name: `sniper-bot`
   - Chain: **Ethereum** (or BSC)
   - Network: **Ethereum Mainnet**
5. Click **Create app**
6. Click your new app, then **API Key** to see your key
7. Copy the **HTTPS** and **WebSockets** URLs

**Your endpoints will be:**

```
# Ethereum Mainnet HTTP
ETH_RPC_2=https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# Ethereum Mainnet WebSocket
# ETH_WS_1=wss://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY
```

> **For BSC:** Alchemy does not support BSC. Use public BSC endpoints for BSC (see below).

---

#### Option C — QuickNode (Best Performance from Southeast Asia, Recommended)

QuickNode has a **Singapore server**. If you are trading from Jakarta or running your bot on a
Singapore VPS, using QuickNode gives you the lowest latency (~20ms to Singapore vs ~170ms to US).

**How to sign up:**

1. Go to [https://www.quicknode.com](https://www.quicknode.com) and click **Get Started for Free**
2. Sign up with email
3. Click **Create Endpoint**
4. Select:
   - Chain: **Ethereum**
   - Network: **Mainnet**
   - Plan: Free (8 million credits/month) or paid
5. Under **Advanced settings**, select **Asia Pacific (Singapore)** as the closest region
6. Click **Create Endpoint**
7. You will see your HTTP and WSS URLs

**Your endpoints will be:**

```
ETH_RPC_1=https://XXXX.quiknode.pro/YOUR_KEY/
ETH_WS_1=wss://XXXX.quiknode.pro/YOUR_KEY/
```

> **QuickNode Free Tier:** 8 million credits/month (approximately 80,000–800,000 requests depending
> on method). Sufficient for testing. For production, the $49/month Starter plan is recommended.

---

#### Option D — Free Public BSC Endpoints (No Signup Required)

For **Binance Smart Chain (BSC)**, Binance provides free public endpoints:

```
BSC_RPC_1=https://bsc-dataseed1.binance.org
BSC_RPC_2=https://bsc-dataseed2.binance.org
BSC_WS_1=wss://bsc-ws-node.nariox.org
```

> **Warning:** Public BSC endpoints are shared and can be slow or rate-limited during high traffic.
> For production, use a paid QuickNode or GetBlock BSC endpoint.

---

#### RPC Provider Summary for Jakarta/Indonesia Users

| Provider   | Free Allowance   | SEA Server?  | HTTP RTT from JKT | HTTP RTT SGP VPS | Recommended Use                   |
| ---------- | ---------------- | ------------ | ----------------- | ---------------- | --------------------------------- |
| Infura     | 100K req/day     | ❌ US/EU     | ~175–200ms        | ~170–185ms       | Backup/testing only               |
| Alchemy    | 300M CU/month    | ❌ US/EU     | ~175–200ms        | ~170–185ms       | Backup/testing only               |
| QuickNode  | 8M credits/month | ✅ Singapore | ~20–40ms          | ~1–5ms           | **Primary — best latency**        |
| Ankr       | 500 req/sec free | ✅ Singapore | ~20–40ms          | ~2–8ms           | Alternative to QuickNode          |
| Public BSC | Unlimited        | ✅ Various   | ~25–60ms          | ~5–20ms          | BSC only — rate-limited, not prod |

##### Per-Operation Credit Cost (ETH — Uniswap v2/v3, BSC — PancakeSwap)

The bot's ingestion and execution modules perform these operations per pipeline cycle:

| Operation                                  | When                  | QuickNode Credits | Alchemy CU  | Infura Req |
| ------------------------------------------ | --------------------- | ----------------- | ----------- | ---------- |
| `eth_subscribe` (WebSocket, keep-alive)    | Once (persistent WS)  | 0                 | 0           | 0          |
| New pair event notify (WebSocket push)     | Per new pair detected | ~2                | ~10 CU      | 1          |
| `eth_getTransactionReceipt` (parse pair)   | Per new pair          | ~5                | ~15 CU      | 1          |
| `eth_call` (token checks, honeypot sim)    | Per pair, 3–5 calls   | ~3 each           | ~26 CU ea   | 1 each     |
| `eth_gasPrice` / `eth_maxFeePerGas`        | Per trade attempt     | ~2                | ~10 CU      | 1          |
| `eth_sendRawTransaction` (swap submission) | Per trade             | ~5                | ~250 CU     | 1          |
| `eth_getTransactionReceipt` (confirmation) | 2–3 polls per trade   | ~5 each           | ~15 CU ea   | 1 each     |
| **Total per completed trade (ETH/BSC)**    |                       | **~35–45**        | **~380 CU** | **~10**    |
| **Total per detected pair (no trade)**     |                       | **~12–20**        | **~95 CU**  | **~6**     |

##### Free Tier Projection — ETH (Uniswap v2) and BSC (PancakeSwap)

Assumptions: Uniswap v2 creates ~50–150 new pairs/day; PancakeSwap v2 ~80–200/day.
DQ filter rejection rate: ~95% (aggressive Layer 1). Trade attempts: ~5–10/day canary.

| Scenario                      | QuickNode/month   | Alchemy/month      | Infura/month  |
| ----------------------------- | ----------------- | ------------------ | ------------- |
| Idle (WS connected, 0 events) | ~0                | ~0                 | ~0            |
| **Canary — 8 trades/day ETH** | **~42,000**       | **~375,000 CU**    | **~2,400**    |
| Active — 30 trades/day ETH    | ~140,000          | ~1.3M CU           | ~9,000        |
| Aggressive — 80 trades/day    | ~350,000          | ~3.3M CU           | ~24,000       |
| **Free tier capacity**        | **8,000,000**     | **300,000,000 CU** | **3,000,000** |
| **Headroom at canary volume** | **190×**          | **800×**           | **1,250×**    |
| **Limit reached at**          | ~1,800 trades/day | ~63,000 trades/day | ~2,400/day    |

Free tier runway at canary volume:

```
QuickNode  8M credits/month:
  Canary  (8 trades/day):   █░░░░░░░░░  0.5% used → 190× headroom
  Active  (30 trades/day):  ██░░░░░░░░  1.7% used →  57× headroom
  Aggr.   (80 trades/day):  ████░░░░░░  4.4% used →  23× headroom
  Limit   (~1800/day):      ██████████ 100% used  →  upgrade needed

Alchemy  300M CU/month:
  Canary  (8 trades/day):   ░░░░░░░░░░  0.1% used → 800× headroom
  Active  (30 trades/day):  ░░░░░░░░░░  0.4% used → 231× headroom
  Aggr.   (80 trades/day):  █░░░░░░░░░  1.1% used →  91× headroom
  Limit   (~63000/day):     ██████████ 100% used  →  upgrade needed

Infura  3M req/month:
  Canary  (8 trades/day):   ░░░░░░░░░░  0.08% used → 1250× headroom
  Active  (30 trades/day):  ░░░░░░░░░░  0.3%  used →  333× headroom
  Aggr.   (80 trades/day):  █░░░░░░░░░  0.8%  used →  125× headroom
  Limit   (~2400/day):      ██████████ 100%  used  →  upgrade needed
```

> **Key finding:** All three providers have **massive headroom** at canary volume for ETH/BSC.
> The deciding factor is **latency, not credits** — and QuickNode Singapore wins on latency.

**Recommended setup for Jakarta users:**

- Primary ETH: QuickNode Singapore — lowest latency, 8M credits/month
- Fallback ETH: Alchemy — 300M CU/month backup, automatically tried on QuickNode failure
- Primary BSC: QuickNode Singapore BSC — same Singapore PoP
- Fallback BSC: Public Binance endpoints — free, acceptable for low-frequency BSC canary

> **Cost to upgrade when ready:** QuickNode $49/month Starter (50M credits). At 1,800+ trades/day
> you are already profitable — the upgrade pays for itself in well under one trading day.

---

### 3.2 Telegram Bot Token

The bot sends trading alerts and accepts operator commands via Telegram. You need a bot token
(a secret string that identifies your bot) and a chat ID (identifies where to send messages).

**Step 1 — Create a Telegram Bot**

1. Open Telegram on your phone or desktop
2. Search for **@BotFather** (the official Telegram bot creation service)
3. Start a chat with BotFather and send: `/newbot`
4. It will ask for a **name** — this is the display name (e.g., "My Sniper Bot")
5. It will ask for a **username** — must end in "bot" (e.g., "mysniper_bot")
6. BotFather will reply with your **Bot Token** — it looks like:
   ```
   1234567890:AABBCCDDEEFFaabbccddeeff1234567890
   ```
   **Copy and save this token** — treat it like a password.

**Step 2 — Get Your Chat ID**

Your Chat ID is needed so the bot knows where to send messages (your private chat, or a group).

For a **private chat with your bot**:

1. Send any message to your new bot on Telegram
2. Open this URL in your browser (replace `YOUR_TOKEN` with your actual token):
   ```
   https://api.telegram.org/botYOUR_TOKEN/getUpdates
   ```
3. Look for `"chat":{"id":` in the JSON response — the number after `id` is your chat ID.
   It looks like: `123456789`

For a **group** (to share with multiple people):

1. Add your bot to a Telegram group
2. Send a message in the group mentioning your bot
3. Open the same `getUpdates` URL
4. The chat ID for groups is a **negative number** like `-1001234567890`

**Step 3 — Note your credentials**

```
SNIPER_TELEGRAM_BOT_TOKEN=1234567890:AABBCCDDEEFFaabbccddeeff1234567890
SNIPER_TELEGRAM_CHAT_ID=123456789
```

> **Note:** The Telegram dispatcher is fully operational in the codebase
> (`internal/telegram/`). It receives all notifications through the event bus and supports the
> full operator command set — see **Section 13.3** for the complete 21-command reference including
> read-only queries, mode switching, and destructive commands.
> Set these values now — they take effect immediately.

---

### 3.3 EVM Crypto Wallet Private Key

The bot needs a wallet to sign and broadcast transactions on-chain. This wallet will hold real
cryptocurrency (ETH or BNB) to pay for gas fees and buy tokens.

> ⚠️ **SECURITY WARNING:** Your private key gives **complete control** over all funds in that
> wallet. If anyone sees your private key, they can steal everything instantly. Never share it.
> Never put it in a file that gets committed to Git. Use environment variables only.

#### Option A — Create a New Wallet with MetaMask (Recommended for Beginners)

1. Install the MetaMask browser extension from [https://metamask.io](https://metamask.io)
2. Click **Get Started** → **Create a Wallet**
3. Set a strong password
4. **Carefully save your 12-word Secret Recovery Phrase** — write it on paper and store safely
5. After wallet creation, click the three dots menu (⋮) → **Account details**
6. Click **Export Private Key** and enter your MetaMask password
7. Copy the 64-character hex string (no `0x` prefix needed) — this is your `SNIPER_WALLET_KEY`

Your wallet address (starts with `0x`) is your `SNIPER_WALLET_ADDRESS`.

#### Option B — Create Multiple Sharded Wallets (Recommended for Production)

The bot supports wallet sharding (4 wallets by default) to submit transactions in parallel,
preventing nonce conflicts. Create 4 separate MetaMask wallets and use:

```
SNIPER_WALLET_0_ADDRESS=0xWallet0Address
SNIPER_WALLET_0_KEY=privateKey0WithoutHexPrefix

SNIPER_WALLET_1_ADDRESS=0xWallet1Address
SNIPER_WALLET_1_KEY=privateKey1WithoutHexPrefix

SNIPER_WALLET_2_ADDRESS=0xWallet2Address
SNIPER_WALLET_2_KEY=privateKey2WithoutHexPrefix

SNIPER_WALLET_3_ADDRESS=0xWallet3Address
SNIPER_WALLET_3_KEY=privateKey3WithoutHexPrefix
```

#### How much ETH/BNB do you need?

| Purpose                 | Minimum                       | Recommended          |
| ----------------------- | ----------------------------- | -------------------- |
| Local testing (dry run) | 0                             | 0                    |
| Live trading (ETH)      | 0.05 ETH (~$175 at $3500/ETH) | 0.2–0.5 ETH          |
| Live trading (BSC)      | 0.1 BNB (~$70 at $700/BNB)    | 0.5–1 BNB            |
| Gas reserve per wallet  | 0.01 ETH or 0.05 BNB          | Always maintain this |

> **Where to buy ETH/BNB in Indonesia?** Registered Indonesian exchanges: **Indodax** (indodax.com),
> **Tokocrypto** (tokocrypto.com), **Pintu** (pintu.co.id). Indodax and Tokocrypto both support
> ETH and BNB trading pairs against IDR (Indonesian Rupiah).

---

### 3.4 Solana RPC Endpoints (Phase 7)

> **Skip this section if you are not enabling Solana trading.** Solana support is controlled by
> `config/chains.yaml`. If you leave the Solana block blank or don’t set the env vars, the
> bot will simply not ingest Solana markets.

Solana RPC endpoints work differently from EVM endpoints:

- **HTTP RPC** — used for transaction submission and slot queries
- **WebSocket** — used for real-time program log subscription (new pool detection on Raydium/PumpFun)

You need both. The bot monitors **Raydium v4** and **PumpFun** programs on Solana mainnet.

#### Option A — Helius (Best Free Tier for Solana, Recommended)

Helius is the most popular Solana RPC provider with a generous free tier and Asia nodes.

**How to sign up:**

1. Go to [https://www.helius.dev](https://www.helius.dev) and click **Sign Up Free**
2. Sign up with email or GitHub
3. After logging in, go to **API Keys** in the dashboard
4. You will see a default API key — click **Copy**
5. Your Helius endpoints will be:

```
# Solana HTTP RPC
SOLANA_RPC_HTTP_1=https://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY

# Solana WebSocket
SOLANA_WS_1=wss://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY
```

**Helius Free Tier Limits:** 1 million credits/month. One transaction fetch uses ~5 credits.
Sufficient for testing. For production, the Developer plan ($49/month) gives 10M credits.

> **Latency note for Jakarta users:** Helius does not have a Southeast Asia server as of 2026.
> All requests route to US-East (~180ms from Jakarta). Use QuickNode Solana Singapore (Option B)
> for lower latency.

---

#### Option B — QuickNode Solana (Best Latency from Jakarta)

QuickNode has a Singapore Solana endpoint, which is the lowest-latency option from Jakarta.

**How to get an endpoint:**

1. Go to [https://www.quicknode.com](https://www.quicknode.com) and sign in
2. Click **Create Endpoint**
3. Select **Solana** → **Mainnet Beta**
4. Under **Advanced settings**, select **Asia Pacific (Singapore)**
5. Click **Create Endpoint**
6. Copy the **HTTP Provider** and **WSS Provider** URLs

```
SOLANA_RPC_HTTP_1=https://YOUR-ENDPOINT.solana-mainnet.quiknode.pro/YOUR_KEY/
SOLANA_WS_1=wss://YOUR-ENDPOINT.solana-mainnet.quiknode.pro/YOUR_KEY/
```

---

#### Option C — Triton One (Professional Grade)

Triton One is favored by high-frequency trading bots for its ultra-low latency and high throughput.

1. Go to [https://triton.one](https://triton.one) and contact sales or sign up for an RPC node
2. They provide dedicated endpoints — no shared rate limits
3. Cost starts around $100–$300/month for a shared node

> **Best for:** Production bots doing high-frequency sniping where every millisecond counts.

---

#### Option D — Free Public Solana Endpoints (Testing Only)

```
# Public Solana mainnet (rate-limited, not for production)
SOLANA_RPC_HTTP_1=https://api.mainnet-beta.solana.com
SOLANA_WS_1=wss://api.mainnet-beta.solana.com
```

> **Warning:** Public endpoints are heavily rate-limited (100 req/sec). They will throttle or
> disconnect your bot under load. Use only for initial setup testing.

---

#### Solana RPC Provider Comparison for Jakarta Users

| Provider   | Free Allowance        | SEA Server?  | HTTP RTT from JKT | HTTP RTT SGP VPS | Notes                                 |
| ---------- | --------------------- | ------------ | ----------------- | ---------------- | ------------------------------------- |
| Helius     | 1M credits/month      | ❌ US-East   | ~170–185ms        | ~175–180ms       | Best Solana docs + DAS APIs           |
| QuickNode  | 10M credits/month     | ✅ Singapore | ~18–25ms          | ~1–5ms           | **Best latency from Jakarta/SGP VPS** |
| Triton One | Paid only (~$100+/mo) | ✅ Yes       | Varies            | ~1–10ms          | Dedicated, HFT production             |
| Public API | Rate-limited free     | ❌ US        | ~170ms+           | ~175ms+          | Testing setup only, 100 req/s cap     |

> **Latency matters more on Solana than on EVM.** A Solana block is ~400ms. The ~150ms
> round-trip difference between QuickNode Singapore and Helius US-East means Helius delivers
> your `sendTransaction` roughly **half a block later** — often the difference between getting
> filled at the open price and chasing a token already up 20%.

##### Per-Operation Credit Cost (Solana — Raydium v4 + PumpFun)

Your `ingestion_solana` module uses `logsSubscribe` WebSocket; `execution_solana` signs and
submits swaps. These are the operations per pipeline cycle:

| Operation                                        | When                  | Helius Credits | QuickNode Credits |
| ------------------------------------------------ | --------------------- | -------------- | ----------------- |
| `logsSubscribe` WebSocket (keep-alive)           | Once (persistent WS)  | 0              | 0                 |
| New pool log notification (WebSocket push)       | Per new pool detected | ~1             | ~2                |
| `getTransaction` (parse pool details)            | Per new pool          | ~5             | ~10               |
| `getLatestBlockhash` (before each swap)          | Per trade attempt     | ~1             | ~2                |
| `sendTransaction` (swap submission)              | Per trade             | ~10            | ~25               |
| `sendTransaction` retry (0–2 retries)            | Per stuck/dropped tx  | ~10 each       | ~25 each          |
| `getSignatureStatuses` poll (confirmation)       | 2–3 polls per trade   | ~5 each        | ~10 each          |
| **Total per completed trade**                    |                       | **~36–41**     | **~72–82**        |
| **Total per detected pool (rejected, no trade)** |                       | **~6**         | **~12**           |

##### Free Tier Projection Analytics — Solana (PumpFun + Raydium v4)

Assumptions: PumpFun + Raydium combined ~300 new pools/day (realistic mainnet average).
DQ filter rejection rate: ~95%. Trade attempts: ~15/day at canary volume.

| Scenario                                  | Helius/month  | QuickNode/month |
| ----------------------------------------- | ------------- | --------------- |
| Idle (WS connected, 0 pools)              | ~0            | ~0              |
| **Canary — 15 trades/day, 300 pools/day** | **~69,750**   | **~139,500**    |
| Active — 50 trades/day, 500 pools/day     | ~142,500      | ~285,000        |
| Aggressive — 100 trades/day, 800/day      | ~354,000      | ~708,000        |
| **Free tier total capacity**              | **1,000,000** | **10,000,000**  |
| **Headroom at canary volume**             | **14×**       | **72×**         |
| **Free limit reached at ~N trades/day**   | **~800**      | **~3,700**      |

Free tier runway visualization:

```
Helius  1M credits/month:
  Canary  (15 trades/day):  ██░░░░░░░░   7% used →  14× headroom
  Active  (50 trades/day):  █████░░░░░  14% used →   7× headroom
  Aggr.  (100 trades/day):  ███████░░░  35% used →   3× headroom
  Limit  (~800 trades/day): ██████████ 100% used →  upgrade needed

QuickNode  10M credits/month:
  Canary  (15 trades/day):  █░░░░░░░░░   1.4% used →  72× headroom
  Active  (50 trades/day):  ██░░░░░░░░   2.9% used →  35× headroom
  Aggr.  (100 trades/day):  ████░░░░░░   7.1% used →  14× headroom
  Limit (~3700 trades/day): ██████████ 100%  used →  upgrade needed
```

##### Head-to-Head Decision Matrix

| Factor                          | Helius      | QuickNode  | Winner        |
| ------------------------------- | ----------- | ---------- | ------------- |
| Latency from Jakarta (home)     | ~170ms      | ~20ms      | **QuickNode** |
| Latency from Singapore VPS      | ~178ms      | ~3ms       | **QuickNode** |
| Free credits/month              | 1M          | 10M        | **QuickNode** |
| Free tier headroom (canary vol) | 14×         | 72×        | **QuickNode** |
| Rate limit (free, req/s)        | ~10         | ~25        | **QuickNode** |
| Solana docs + DAS APIs          | ⭐⭐⭐⭐⭐  | ⭐⭐⭐⭐   | **Helius**    |
| PumpFun-native event parsing    | ✅ Built-in | Manual     | **Helius**    |
| Jito bundle support             | Paid only   | Paid only  | Tie           |
| WebSocket stability             | ⭐⭐⭐⭐⭐  | ⭐⭐⭐⭐⭐ | Tie           |
| Price to upgrade                | $49/mo      | $49/mo     | Tie           |

**Recommended setup for Jakarta users (use both):**

```yaml
# config/chains.yaml — Solana RPC block
solana:
  rpc:
    - url: "${SOLANA_RPC_HTTP_1}" # QuickNode Singapore — primary HTTP
      priority: 1
      kind: http
      region: ap-southeast-1
    - url: "${SOLANA_RPC_HTTP_2}" # Helius US-East — fallback HTTP
      priority: 2
      kind: http
      region: us-east
    - url: "${SOLANA_WS_1}" # QuickNode Singapore WebSocket
      priority: 1
      kind: ws
      region: ap-southeast-1
```

Set in `.env`:

```bash
SOLANA_RPC_HTTP_1=https://YOUR-ENDPOINT.solana-mainnet.quiknode.pro/YOUR_KEY/
SOLANA_RPC_HTTP_2=https://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY
SOLANA_WS_1=wss://YOUR-ENDPOINT.solana-mainnet.quiknode.pro/YOUR_KEY/
```

The circuit breaker in `internal/rpc/` handles automatic failover to Helius if QuickNode has
an outage. Combined free allowance: ~11M credits/month before you pay anything.

> **When to upgrade:** When consistently above ~100 trades/day and positive expectancy is proven
> by 50+ trades. Both providers charge $49/month for the next tier — one profitable trade covers it.

---

### 3.5 Solana Wallet Keypair (Phase 7)

> **Skip this section if you are not enabling Solana trading.**

Solana wallets are different from Ethereum wallets. Instead of a 64-char hex private key, Solana
uses a **keypair JSON file** — a JSON array of 64 bytes (seed + public key). This is the standard
format output by the `solana-keygen` CLI tool.

> ⚠️ **SECURITY WARNING:** Your keypair JSON file gives complete control over all funds in that
> wallet. Treat it like a private key — never commit it to Git, never share it.
> Store it in a secure location (e.g., `/etc/sniper/keys/` with permissions 600).

#### Step 1 — Install the Solana CLI

The easiest way to generate Solana keypairs is with the official Solana CLI.

**macOS / Linux:**

```bash
# Install the Solana CLI
sh -c "$(curl -sSfL https://release.solana.com/v1.18.0/install)"

# Add to PATH
export PATH="$HOME/.local/share/solana/install/active_release/bin:$PATH"
echo 'export PATH="$HOME/.local/share/solana/install/active_release/bin:$PATH"' >> ~/.zshrc  # macOS
echo 'export PATH="$HOME/.local/share/solana/install/active_release/bin:$PATH"' >> ~/.bashrc # Linux

# Verify
solana --version
```

**Windows (WSL2):**

Follow the Linux instructions inside your WSL2 Ubuntu terminal.

**Windows (native, Git Bash):**

```powershell
# In PowerShell (as Administrator):
curl https://release.solana.com/v1.18.0/solana-install-init-x86_64-pc-windows-msvc.exe --output C:\solana-install-init.exe
C:\solana-install-init.exe v1.18.0
```

#### Step 2 — Generate a new keypair

```bash
# Create directory for keys (secure it)
mkdir -p ~/.config/sniper/keys
chmod 700 ~/.config/sniper/keys

# Generate a new keypair JSON file
solana-keygen new --outfile ~/.config/sniper/keys/solana-wallet-1.json

# The tool will ask you to set a passphrase (press Enter to skip, or set one)
# It prints your public key (wallet address) at the end
# Example output:
#   pubkey: 9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM
```

**Your wallet address** is the `pubkey` printed. **Fund this address with SOL** for gas fees.

#### Step 3 — Get your wallet's public address

```bash
solana-keygen pubkey ~/.config/sniper/keys/solana-wallet-1.json
# Prints: 9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM  (your actual address will differ)
```

#### Step 4 — Set the env var to the file path

Unlike EVM wallets (where you pass the raw key), Solana wallets use the **file path**:

```bash
# In your .env file:
SOLANA_WALLET_KEY_1=/home/your_username/.config/sniper/keys/solana-wallet-1.json
```

For a VPS or Docker, use the absolute path on the server:

```bash
# VPS example:
SOLANA_WALLET_KEY_1=/etc/sniper/keys/solana-wallet-1.json
```

#### How much SOL do you need?

| Purpose                 | Minimum          | Recommended     |
| ----------------------- | ---------------- | --------------- |
| Local testing (dry run) | 0                | 0               |
| Live trading on Solana  | 0.1 SOL (~$15)   | 0.5–1 SOL       |
| Gas reserve per wallet  | 0.01 SOL minimum | Always maintain |

> **Where to buy SOL in Indonesia?** Registered exchanges: **Indodax**, **Tokocrypto**, **Pintu**.
> All three support SOL/IDR trading pairs. Transfer SOL from exchange to your wallet address.

---

### 3.6 BirdEye API Key (Phase 9 — optional)

> **Skip if:** You don't need dev reputation checks or token holder concentration data.
> Without this key, the `dev_reputation` and token metadata DQ providers are disabled — the bot
> still works, but misses serial-launcher and no-social-links risk signals.

BirdEye is a Solana/EVM token analytics platform. The bot uses its API for:

- **Developer reputation** — detect serial launcher wallets with prior rug history
- **Token metadata** — verify social link presence and holder concentration
- **Holder distribution** — surface top-holder concentration risk

**Important:** BirdEye has two separate systems. The regular `birdeye.so` profile page does
**not** have API key management. API keys are managed through the separate
**BDS (Birdeye Data Services)** dashboard at `bds.birdeye.so`.

**How to get your API key:**

1. Go to [https://bds.birdeye.so/auth/sign-up](https://bds.birdeye.so/auth/sign-up) and create an account
   (if you already have a `birdeye.so` account, the same email/password works — they share one account)
2. Log in at [https://bds.birdeye.so/auth/sign-in](https://bds.birdeye.so/auth/sign-in)
3. In the BDS Dashboard, click the **Security** tab in the left sidebar
4. Click **Generate Key** and give it a name (e.g., "sniper-bot")
5. Copy the generated key immediately — it is only shown once

**Set it in your `.env` file:**

```bash
BIRDEYE_API_KEY=your_birdeye_api_key_here
```

> **Cost:** BirdEye uses a **Compute Unit (CU)** pricing model, not simple requests-per-minute.
> The free starter package includes a limited CU budget per month. The codebase has
> **no built-in rate limiter** for BirdEye — when CUs are exhausted or rate-limited (HTTP 429),
> the provider returns `Degraded: true` and the pipeline continues without the score. In quiet
> markets this is fine. In busy markets (Solana bull runs, 100+ launches/minute), BirdEye
> degrades silently — it becomes a no-op. Keep `shadow_mode: true` in `config/data_quality.yaml`
> while validating, and check your CU usage in the BDS Dashboard before disabling shadow mode.

> **Security:** This key is read via `os.Getenv("BIRDEYE_API_KEY")` at startup — it is never
> written to any YAML file, never logged, and never committed to Git.

---

### 3.7 Jito Bundle Credentials (Phase 11 — optional)

> **Skip if:** You are not trading on Solana, or you are in shadow mode.
> Jito bundle submission is enabled via `execution.solana.jito.enabled: true` in `config/execution.yaml`.
> By default `enabled: false` and `shadow_mode: true` — no bundles are submitted until you explicitly enable it.

Jito is a Solana MEV infrastructure that lets you submit transactions as **bundles** to Jito
block-engine validators, providing priority inclusion and MEV protection. Use this when:

- You are competing for new PumpFun/Raydium launches at high frequency
- You want to tip validators for guaranteed inclusion in a specific slot

The bot needs two values:

| Variable           | What it is                                                   |
| ------------------ | ------------------------------------------------------------ |
| `JITO_BUNDLE_URL`  | The Jito block-engine bundle submission HTTPS endpoint       |
| `JITO_TIP_ACCOUNT` | A Jito tip account address (one per region — sends lamports) |

**How to get these:**

1. Go to [https://jito.network](https://jito.network) — Jito infrastructure is permissionless;
   no account or API key is required. The bundle endpoint URLs are publicly documented.

2. Choose the closest regional endpoint:

   | Region         | Bundle URL                                                           |
   | -------------- | -------------------------------------------------------------------- |
   | US East        | `https://mainnet.block-engine.jito.labs.io/api/v1/bundles`           |
   | US West        | `https://dallas.mainnet.block-engine.jito.labs.io/api/v1/bundles`    |
   | EU (Amsterdam) | `https://amsterdam.mainnet.block-engine.jito.labs.io/api/v1/bundles` |
   | Asia (Tokyo)   | `https://tokyo.mainnet.block-engine.jito.labs.io/api/v1/bundles`     |

   > **For Jakarta/Singapore VPS:** Use the Tokyo endpoint for lowest latency.

3. Pick a Jito tip account (any of these work — rotate if you want):

   ```
   96gYZGLnJYVFmbjzopPSU6QiEV5fGqZNyN9nmNhvrZU5
   HFqU5x63VTqvQss8hp11i4wVV8bD44PvwucfZ2bU7gRe
   Cw8CFyM9FkoMi7K7Crf6HNQqf4uEMzpKw6QNghXLvLkY
   ADaUMid9yfUytqMBgopwjb2DTLSokTSzL1sTaC4pr870
   ```

**Set in your `.env` file:**

```bash
# For Singapore VPS users — use Tokyo as closest Jito relay
JITO_BUNDLE_URL=https://tokyo.mainnet.block-engine.jito.labs.io/api/v1/bundles
JITO_TIP_ACCOUNT=96gYZGLnJYVFmbjzopPSU6QiEV5fGqZNyN9nmNhvrZU5
```

> **Security:** The bot enforces HTTPS-only for `JITO_BUNDLE_URL` — any non-HTTPS URL is
> rejected at startup (except loopback addresses used in test servers). Never use HTTP.
> The response body is capped at 64 KiB to prevent unbounded reads.

> **Cost:** Jito charges no subscription fee. You pay a **tip in lamports** per bundle
> (configured via `execution.solana.jito.tip_lamports: 1000` in `config/execution.yaml`).
> At 1000 lamports per bundle, this is $0.000001 per trade — effectively free.

---

### 3.8 Solana gRPC Transport Credentials (Phase 11 — optional)

> **Skip if:** You are using the default WebSocket transport (the default `mode: rpc` in
> `config/chains.yaml`). gRPC is only needed for ultra-low-latency sniping where WebSocket
> latency (~50ms per event) is a bottleneck.

The Phase 11 hybrid transport supports **Yellowstone gRPC** (Geyser plugin streaming), which
delivers new slot and transaction notifications faster than WebSocket subscription. Two providers
support this:

| Provider   | gRPC Support           | Cost        | Notes                            |
| ---------- | ---------------------- | ----------- | -------------------------------- |
| Triton One | ✅ Native Yellowstone  | $100+/month | Dedicated node; lowest latency   |
| Helius     | ✅ Geyser via Business | $499+/month | Business tier required           |
| QuickNode  | ❌ Not yet             | —           | WebSocket only on standard plans |

**How to get gRPC access:**

**Triton One:**

1. Contact [https://triton.one](https://triton.one) — email or Discord
2. Request a Yellowstone gRPC node
3. They provide: `endpoint: your-node.rpcpool.com:443` and a bearer token

**Helius:**

1. Log in at [https://www.helius.dev](https://www.helius.dev)
2. Upgrade to the **Business** tier
3. Go to **Geyser Plugin** → **gRPC endpoint** in dashboard

**Set in your `.env` file:**

```bash
# The endpoint overrides the value in config/chains.yaml — only needed if using gRPC mode
SOLANA_GRPC_ENDPOINT=your-node.rpcpool.com:443

# Auth token — NEVER put this in YAML — env var only
SOLANA_GRPC_TOKEN=your_bearer_token_here
```

**Enable gRPC in config:**

```yaml
# config/chains.yaml
solana:
  transport:
    mode: grpc # Change from "rpc" to "grpc"
    grpc_endpoint: "" # Leave blank — SOLANA_GRPC_ENDPOINT env var takes precedence
    # grpc_auth_token is loaded from env var SOLANA_GRPC_TOKEN (never put in YAML)
    fallback_on_error: true # Auto-fall back to WebSocket if gRPC fails
    fallback_error_n: 5
```

> **Security:** `SOLANA_GRPC_TOKEN` is intentionally absent from all YAML config structs in the
> codebase — it can only be provided via environment variable. The field `GrpcAuthToken` was
> deliberately removed from `TransportConfig` to prevent accidental YAML serialization.

---

### 3.9 Copy-Trade Alpha Wallets (Phase 9 — optional)

> **Skip if:** You don't have alpha wallet addresses to track. This provider adds a copy-trade
> signal to the DQ layer — if a known smart wallet recently bought the same token, it gets a
> positive signal. Without wallet addresses, the provider runs in degraded mode (returns no-match).

Copy-trade tracking requires no external account or API key. You simply provide a list of wallet
addresses that you consider "alpha" — wallets with a proven track record of buying tokens early
that later pumped significantly.

**How to find alpha wallets:**

Common approaches:

- **Cielo Finance** ([https://app.cielo.finance](https://app.cielo.finance)) — browse public
  wallet leaderboards and see top-performing Solana wallets
- **Birdeye Wallet** — look at "Top Traders" on tokens that pumped
- **Dune Analytics** — query on-chain data for wallets with consistent early entries
- **GMGN** ([https://gmgn.ai](https://gmgn.ai)) — Solana wallet analyzer, shows winrate/ROI

**Set in your `.env` file:**

```bash
# Comma-separated list — no spaces around commas
# EVM wallets: 0x-prefixed addresses
# Solana wallets: base58 addresses
COPY_TRADE_WALLETS=0xAlphaWallet1,0xAlphaWallet2,SolanaWalletBase58Address
```

> **How it works in the pipeline:** When the Data Quality Engine evaluates a token, the
> `CopyTradeProvider` checks whether any wallet in `COPY_TRADE_WALLETS` has recently bought
> that token (via DEXScreener's wallet transaction history). If a match is found, the token
> receives a `copy_trade_alpha_match` flag and a reduced risk score — bypassing some conservative
> DQ thresholds. If no wallets are configured, the provider logs a warning at startup and returns
> a neutral `copy_trade_no_wallets` flag for every token (no rejection, no boost).

> **Chain support:** Only `ethereum`/`eth`, `bsc`/`bnb`, `solana`/`sol`, and `base` are accepted.
> Unknown chain identifiers are rejected (fail-closed) — no passthrough.

> **Security:** Wallet addresses are never logged at INFO level. If you rotate wallets, just
> update `COPY_TRADE_WALLETS` and restart the bot — no database changes required.

---

## 4. Clone the Repository

Open Terminal (macOS/Linux) or Git Bash (Windows) and run:

```bash
# Navigate to where you want the project
cd ~  # or wherever you prefer, e.g., cd ~/projects

# Clone the repository
git clone https://github.com/YOUR_USERNAME/crypto-sniping-bot.git

# Enter the project directory
cd crypto-sniping-bot
```

Verify the structure looks correct:

```bash
ls
# You should see: Makefile  README.md  cmd/  config/  contracts/  database/  docs/  ...
```

---

## 5. Configure the Application

### 5.1 Create .env File — All Environment Variables

The bot reads sensitive configuration from environment variables (never from files that could
accidentally be committed to Git). Create a file named `.env` in the project root:

```bash
# In the project root directory:
cp .env.example .env 2>/dev/null || touch .env
```

> **Note:** `.env` is already in `.gitignore` — it will never be committed to Git.

Open `.env` in a text editor and fill in all values:

```bash
# =============================================================================
# DATABASE CONFIGURATION
# =============================================================================

# Required: PostgreSQL password for the 'sniper' user
SNIPER_DB_PASSWORD=your_secure_password_here

# Optional overrides (defaults shown — change only if needed)
# SNIPER_DB_HOST=localhost
# SNIPER_DB_NAME=sniper
# SNIPER_DB_USER=sniper
# SNIPER_DB_SSL_MODE=disable

# =============================================================================
# BLOCKCHAIN RPC ENDPOINTS — Ethereum (required if using ETH chain)
# =============================================================================

# Primary ETH HTTP RPC (use QuickNode Singapore for best latency from Jakarta)
ETH_RPC_1=https://YOUR_QUICKNODE_ENDPOINT.quiknode.pro/YOUR_KEY/

# Fallback ETH HTTP RPC
ETH_RPC_2=https://eth-mainnet.g.alchemy.com/v2/YOUR_ALCHEMY_KEY

# ETH WebSocket (required for real-time pool event subscription)
ETH_WS_1=wss://YOUR_QUICKNODE_ENDPOINT.quiknode.pro/YOUR_KEY/

# =============================================================================
# BLOCKCHAIN RPC ENDPOINTS — BSC / BNB Chain (required if using BSC chain)
# =============================================================================

BSC_RPC_1=https://bsc-dataseed1.binance.org
BSC_RPC_2=https://bsc-dataseed2.binance.org
BSC_WS_1=wss://bsc-ws-node.nariox.org

# =============================================================================
# WALLET CONFIGURATION
# =============================================================================

# Single wallet (for testing / getting started)
SNIPER_WALLET_ADDRESS=0xYourWalletAddressHere
SNIPER_WALLET_KEY=yourPrivateKeyHere64CharHexNoPrefixNoQuotes

# Multi-wallet shards (for production — uncomment and fill all 4)
# SNIPER_WALLET_0_ADDRESS=0xShard0WalletAddress
# SNIPER_WALLET_0_KEY=shard0PrivateKey
# SNIPER_WALLET_1_ADDRESS=0xShard1WalletAddress
# SNIPER_WALLET_1_KEY=shard1PrivateKey
# SNIPER_WALLET_2_ADDRESS=0xShard2WalletAddress
# SNIPER_WALLET_2_KEY=shard2PrivateKey
# SNIPER_WALLET_3_ADDRESS=0xShard3WalletAddress
# SNIPER_WALLET_3_KEY=shard3PrivateKey

# =============================================================================
# BLOCKCHAIN RPC ENDPOINTS — Solana (required only if using Solana chain)
# See Section 3.4 for how to obtain these from Helius or QuickNode Solana
# =============================================================================

# Solana HTTP RPC endpoint (for transaction submission and slot queries)
SOLANA_RPC_HTTP_1=https://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY

# Solana HTTP RPC fallback endpoint (optional but recommended)
SOLANA_RPC_HTTP_2=https://YOUR-ENDPOINT.solana-mainnet.quiknode.pro/YOUR_KEY/

# Solana WebSocket endpoint (for real-time Raydium/PumpFun log subscription)
SOLANA_WS_1=wss://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY

# =============================================================================
# SOLANA WALLET CONFIGURATION (required only if using Solana chain)
# This is a FILE PATH to a JSON keypair file, NOT a raw private key.
# See Section 3.5 for how to generate this file with solana-keygen.
# =============================================================================

# Path to the Solana keypair JSON file (64-byte array generated by solana-keygen)
SOLANA_WALLET_KEY_1=/home/your_username/.config/sniper/keys/solana-wallet-1.json

# =============================================================================
# SOLANA GRPC TRANSPORT (optional — Phase 11, for Triton One / Helius Business)
# Provides ~10-20ms lower latency than WebSocket. Leave unset to use WebSocket.
# SOLANA_GRPC_TOKEN is read from env only — NEVER put it in YAML.
# =============================================================================

# SOLANA_GRPC_ENDPOINT=your-triton-endpoint.rpcpool.com:443
# SOLANA_GRPC_TOKEN=your_grpc_auth_token_here

# =============================================================================
# JITO BUNDLE SUBMISSION (optional — Solana MEV protection)
# Loaded from env only — NEVER put these in YAML or config files.
# =============================================================================

# JITO_BUNDLE_URL=https://mainnet.block-engine.jito.labs.io/api/v1/bundles
# JITO_TIP_ACCOUNT=96gYZGLnJYVFmbjzopPSU6QiEV5fGqZNyN9nmNhvrZU5

# =============================================================================
# OPTIONAL DATA QUALITY ENRICHMENT
# These providers improve scam detection. Bot works without them (reduced coverage).
# =============================================================================

# BirdEye API key — enriches dev reputation and token metadata checks
# Get yours at https://birdeye.so/profile/api-key
# BIRDEYE_API_KEY=your_birdeye_api_key_here

# Copy-trade alpha wallets — comma-separated smart wallet addresses to track
# COPY_TRADE_WALLETS=0xAlphaWallet1,0xAlphaWallet2

# =============================================================================
# TELEGRAM (fully operational — see Section 3.2 for setup instructions)
# =============================================================================

SNIPER_TELEGRAM_BOT_TOKEN=1234567890:YourTelegramBotTokenHere
SNIPER_TELEGRAM_CHAT_ID=123456789

# Optional: comma-separated Telegram user IDs allowed to use operator commands.
# If unset, all users who can message the bot can send commands (less secure).
# SNIPER_TELEGRAM_ALLOWED_USERS=123456789,987654321

# =============================================================================
# SERVER
# =============================================================================

# HTTP server port (health check endpoint at /health)
PORT=8080

# Log verbosity: debug | info | warn | error
LOG_LEVEL=info
```

**Loading the .env file:** The bot does not automatically load `.env` files — you need to export
the variables into your shell. Do this every time you open a new terminal, or add it to your shell
profile:

```bash
# Load .env into current shell session (macOS/Linux)
export $(grep -v '^#' .env | grep -v '^$' | xargs)
```

Or use a helper (add to `~/.zshrc` or `~/.bashrc`):

```bash
# Add this function to your ~/.zshrc or ~/.bashrc
function loadenv() {
  export $(grep -v '^#' .env | grep -v '^$' | xargs)
  echo "Environment loaded from .env"
}
```

Then just run `loadenv` each time.

---

### 5.2 Edit YAML Config Files

The YAML files in `config/` control all trading parameters. You should review and adjust these
before running the bot. Below is a beginner-friendly explanation of each important file.

#### `config/chains.yaml` — Which blockchains to scan

Open this file and update the RPC endpoint references to match your env var names:

```yaml
chains:
  - name: eth
    chain_id: 1
    rpc_endpoints:
      - ${ETH_RPC_1}
      - ${ETH_RPC_2}
    ws_endpoints:
      - ${ETH_WS_1}
    # How many blocks to wait before considering a tx confirmed
    confirmation_depth: 2
```

> The `${...}` syntax is automatically replaced with your environment variable values at startup.
> You do not need to change the YAML — just ensure your env vars are set correctly.

**Solana chain (Phase 7)** is configured separately in the same file:

```yaml
# Phase 7: Solana ingestion configuration.
solana:
  chain_id: "solana"
  confirmation_commitment: "confirmed"
  rpc:
    - url: "${SOLANA_RPC_HTTP_1}" # Set via env var
      priority: 1
      kind: http
    - url: "${SOLANA_WS_1}" # Set via env var
      priority: 1
      kind: ws
  programs:
    - program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8" # Raydium v4
      family: raydium-v4
    - program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P" # PumpFun
      family: pumpfun
```

If you do not set `SOLANA_RPC_HTTP_1` / `SOLANA_WS_1`, the Solana ingestion worker will not
start — this is safe and does not affect EVM chains.

**Solana hybrid transport (Phase 11)** — optional high-performance gRPC stream:

```yaml
# config/chains.yaml (Phase 11 addition)
solana:
  transport:
    mode: rpc # "rpc" (default) or "grpc" (Triton One / Helius Business)
    grpc_endpoint: "" # Required when mode=grpc; loaded from env var preferred
    fallback_on_error: true # Fall back to WebSocket RPC if gRPC fails
    fallback_error_n: 5 # Fall back after N consecutive gRPC errors
```

> Leave `mode: rpc` unless you have a gRPC Solana provider. The WebSocket transport is
> production-ready for most use cases.

#### `config/pipeline.yaml` — Core trading parameters

The most important settings for beginners:

```yaml
capital:
  # How much USD to spend per trade (start small when testing!)
  fixed_entry_size_usd: 5.0

  # Maximum total USD across all open positions
  max_total_exposure_usd: 500.0

  # Maximum number of concurrent open positions
  max_concurrent_positions: 1

position:
  # Take Profit 1: exit 50% of position when up 15%
  tp1_bps: 1500

  # Take Profit 2: exit remaining when up 40%
  tp2_bps: 4000

  # Stop Loss: exit all when down 5%
  sl_bps: 500

  # Maximum time to hold a position (seconds) — 300 = 5 minutes (meme tokens pump or dump fast)
  max_hold_seconds: 300

  # Phase 10: Trailing stop — activates only after TP1 is hit
  trailing_stop_bps: 500 # Exit if price drops 5% from peak after TP1 (0 = disabled)
  trailing_activate_at_tp1: true # Only arm trailing stop after TP1 fires
  tp1_filled_pct_bps: 5000 # Sell 50% of position at TP1

  # Phase 10: Volume-staleness time exit
  volume_staleness_seconds: 1800 # Check after 30 min hold (0 = disabled)
  volume_staleness_min_delta_pct_bps: 200 # Exit if 24h volume grew < 2%

# Phase 11: Creator hygiene filters
edge:
  max_dev_buy_pct_bps: 2000 # Reject if creator bought > 20% supply (0 = disabled)
  max_creator_rug_count: 2 # Reject if creator has ≥ 2 prior rugs (0 = disabled)
  min_dev_wallet_age_seconds: 86400 # Reject wallets < 24 hours old (0 = disabled)

# Phase 10: Consecutive-pass gate
validation:
  required_consecutive_passes: 2 # Token must pass EV gate twice in a row
  consecutive_pass_window_seconds: 30 # Both passes must occur within 30 seconds

# Phase 11: Per-creator dedup
selection:
  max_positions_per_creator: 0 # At most 1 open position per creator wallet (0 = disabled)

# Phase 11: Congestion-aware slippage
models:
  congestion:
    enabled: false
    latency_anchor_ms: 800 # Normal latency baseline
    max_multiplier: 2.0 # Cap slippage multiplier under congestion

# Phase 11: Creator blacklist (auto-populated by learning engine)
learning:
  creator_blacklist:
    enabled: false
    min_rugs_for_blacklist: 2 # Auto-blacklist after 2 confirmed rugs
```

> **Tip for beginners:** Start with `fixed_entry_size_usd: 10.0` and `max_concurrent_positions: 1`
> until you understand how the bot behaves.

#### `config/execution.yaml` — Transaction settings

```yaml
# How many transactions can be in-flight simultaneously (5-20)
concurrency_limit: 10

# How many wallet shards (must match number of SNIPER_WALLET_N_KEY vars)
wallet_shard_count: 4

# Use 'public' mempool for testing, 'private_flashbots' for MEV protection in production
mempool_route: public

# Phase 7: Solana execution parameters (only used when Solana chain is enabled)
solana:
  slippage_cap_bps: 200 # 2% max slippage on Solana swaps
  confirm_timeout_ms: 15000 # Wait up to 15s for tx confirmation
  max_send_attempts: 3 # Retry tx up to 3 times
  priority_fee_lamports: 5000 # Tip for faster inclusion (1 lamport = 0.000000001 SOL)
  wallet_key_paths:
    - "${SOLANA_WALLET_KEY_1}" # File path to keypair JSON
```

#### `config/data_quality.yaml` — Scam detection (Phase 9)

Controls which scam detectors are active and how much each one contributes to the overall risk
score. **Default values are safe to use without changes.**

```yaml
data_quality:
  detectors:
    honeypot_simulation: true # Simulate buy+sell on-chain — most important check
    tax_anomaly: true # Flag tokens with > 10% buy/sell tax
    lp_lock: true # Require liquidity locked for 30+ days
    wash_trading: true # Detect same-wallet circular trading
    rug_authority: true # Detect mint/pause/blacklist functions
    contract_verified: true # Check source code is public on Etherscan/BscScan
    dev_reputation: true # Check deployer wallet history for prior rugs

  risk_weights:
    honeypot: 0.30 # 30% of risk score comes from honeypot check
    tax_anomaly: 0.20 # 20% from tax anomaly
    rug_authority: 0.20 # 20% from dangerous contract functions
    lp_lock_missing: 0.15
    wash_trading: 0.10
    contract_unverified: 0.05
    # dev_reputation weight is applied via serial-launcher risk score (see data_quality.yaml)
```

#### `config/capital.yaml` — Position sizing (Phase 9)

Controls how much money the bot bets per trade. Phase 9 introduced dynamic (Kelly-fraction)
sizing. **Start with the defaults and adjust after observing live behavior.**

```yaml
capital:
  use_dynamic_sizing: true # Phase 9: size ∝ edge × probability × confidence
  base_size_usd: 5.0 # Starting point for Kelly calculation
  min_size_usd: 5.0 # Minimum trade size
  max_size_usd: 500.0 # Maximum trade size cap

  kelly:
    cap: 0.25 # Use at most 25% of Kelly-optimal size (conservative)

  mode_multipliers:
    STRICT: 0.5 # In STRICT mode, halve the position size
    BALANCED: 1.0 # In BALANCED mode, normal sizing
    EXPLORATION: 1.3 # In EXPLORATION mode, 30% larger (exploring new patterns)
    VERY_EXPLORATION: 1.5 # In VERY_EXPLORATION mode, 50% larger (maximum aggression)

  failure_policy:
    on_missing_probability: "reject" # Safest: skip trades with no probability score
    fallback_prior_probability: 0.35 # Only used if above is "fallback_prior"
```

#### `config/probability.yaml` — Trade probability (Phase 9)

Controls how the bot uses its probability model to score each trade opportunity.

```yaml
probability:
  use_model_output: true # Use real model output (Phase 9: default on)
  prior_probability: 0.35 # Conservative fallback for unscored tokens
  min_model_confidence: 0.40 # If model is < 40% confident, use prior instead

  # Telegram alert if > 5% of trades in the last hour used the fallback
  fallback_alert_pct: 0.05
  fallback_alert_window_sec: 3600
```

---

## 6. Database Setup

### Step 1 — Create PostgreSQL user and database

#### macOS / Linux:

```bash
# Connect to PostgreSQL as the superuser
sudo -u postgres psql  # Linux
# OR
psql postgres  # macOS (Homebrew)

# Inside psql, run these commands (replace 'your_secure_password' with your actual password):
CREATE USER sniper WITH PASSWORD 'your_secure_password';
CREATE DATABASE sniper OWNER sniper;
GRANT ALL PRIVILEGES ON DATABASE sniper TO sniper;
\q
```

> **Important:** The password you set here must match `SNIPER_DB_PASSWORD` in your `.env` file.

#### Windows (using psql or pgAdmin):

**Using psql:**

```cmd
# Open Command Prompt or PowerShell and run:
psql -U postgres

# Inside psql:
CREATE USER sniper WITH PASSWORD 'your_secure_password';
CREATE DATABASE sniper OWNER sniper;
GRANT ALL PRIVILEGES ON DATABASE sniper TO sniper;
\q
```

**Using pgAdmin 4 (graphical interface):**

1. Open pgAdmin 4
2. Right-click **Login/Group Roles** → **Create** → **Login/Group Role**
3. Name: `sniper`, password: `your_secure_password`, check "Can login"
4. Right-click **Databases** → **Create** → **Database**
5. Name: `sniper`, Owner: `sniper`

### Step 2 — Run database migrations

Make sure your `.env` is loaded (run `loadenv` or `export $(grep -v '^#' .env | xargs)`), then:

```bash
# Run all migrations (creates all tables)
make migrate-up
```

Or without Make:

```bash
go run ./cmd/ migrate up
```

You should see output like:

```
Running migration: 20260101000001_initial_schema.sql ... OK
Running migration: 20260101000002_add_claimed_at.sql ... OK
...
Running migration: 20260101000015_market_data_symbol.sql ... OK
Running migration: 20260101000016_phase10_additions.sql ... OK
Running migration: 20260101000017_phase11_creator_blacklist.sql ... OK
All 17 migrations applied successfully
```

If you see a connection error, check that:

1. PostgreSQL is running (`brew services list | grep postgres` on macOS)
2. `SNIPER_DB_PASSWORD` is exported in your current shell
3. The `sniper` user and database were created correctly

### Step 3 — Verify the database

```bash
psql -U sniper -d sniper -c "\dt"
```

You should see a list of tables including `events`, `consumer_offsets`, `strategy_versions`,
`pipeline_runs`, `token_lifecycle`, `positions`, `learning_records`, `solana_slot_cursors`,
`execution_receipts`, `dlq_events`, `partition_leases`, `creator_blacklist`, and others.

---

## 7. Run the Bot — Local

### Step 1 — Install Go dependencies

```bash
go mod download
```

### Step 2 — Build the binary

```bash
make build
# OR
go build -o bin/crypto-sniping-bot ./cmd/
```

### Step 3 — Load environment variables

```bash
export $(grep -v '^#' .env | grep -v '^$' | xargs)
```

### Step 4 — Run the bot

```bash
# Option A: Run directly from source (easier for development)
make run
# OR
go run ./cmd/ serve

# Option B: Run the compiled binary
./bin/crypto-sniping-bot serve
```

You should see structured JSON log output like:

```json
{"time":"2026-04-26T10:00:00Z","level":"INFO","msg":"Config loaded","schema_version":"1"}
{"time":"2026-04-26T10:00:00Z","level":"INFO","msg":"Database connected","host":"localhost"}
{"time":"2026-04-26T10:00:00Z","level":"INFO","msg":"Migrations OK","count":17}
{"time":"2026-04-26T10:00:00Z","level":"INFO","msg":"HTTP server started","port":8080}
{"time":"2026-04-26T10:00:00Z","level":"INFO","msg":"Orchestrator started"}
```

### Step 5 — Verify the health endpoint

In a new terminal:

```bash
curl http://localhost:8080/health
# Should return: {"status":"ok"}
```

### Step 6 — Stop the bot

Press `Ctrl+C` in the terminal where the bot is running. It will shut down gracefully.

---

## 8. Run with Docker

Docker lets you run the bot in an isolated container without installing Go on your machine.
The repository ships with a production-ready `Dockerfile`, `docker-compose.yml`, and
`.dockerignore` at the project root — no manual file creation required.

### Step 1 — Install Docker

**macOS:** Download Docker Desktop from [https://www.docker.com/products/docker-desktop/](https://www.docker.com/products/docker-desktop/) and install it. Start Docker Desktop.

**Linux (Ubuntu):**

```bash
sudo apt update
sudo apt install -y docker.io docker-compose-plugin
sudo systemctl start docker
sudo systemctl enable docker
sudo usermod -aG docker $USER  # Allow running Docker without sudo (requires logout/login)
```

**Windows:** Download Docker Desktop from [https://www.docker.com/products/docker-desktop/](https://www.docker.com/products/docker-desktop/) and install it. Enable WSL 2 backend when prompted.

### Step 2 — Create your .env file

The repository includes `.env.example` with every supported variable. Copy it and fill in your
secrets — Docker Compose reads `.env` automatically if placed in the project root.

```bash
cp .env.example .env
nano .env   # or your preferred editor
```

**Required variables (bot will not start without these):**

| Variable                | Description                                           |
| ----------------------- | ----------------------------------------------------- |
| `SNIPER_DB_PASSWORD`    | Strong PostgreSQL password                            |
| `ETH_RPC_1`             | Ethereum HTTP RPC URL (e.g. Infura/Alchemy/QuickNode) |
| `ETH_WS_1`              | Ethereum WebSocket URL                                |
| `SNIPER_WALLET_ADDRESS` | Your EVM wallet address                               |
| `SNIPER_WALLET_KEY`     | Your EVM private key (hex, no 0x prefix)              |

**Solana (Phase 7) — only required if using Solana chain:**

```bash
# In .env, also set:
SOLANA_RPC_HTTP_1=https://api.mainnet-beta.solana.com
SOLANA_WS_1=wss://api.mainnet-beta.solana.com

# Set SOLANA_KEYS_DIR to the directory on your HOST machine that contains
# your Solana keypair JSON file (named solana-wallet-1.json).
# docker-compose.yml mounts this directory read-only at /keys inside the container.
SOLANA_KEYS_DIR=/home/your_username/.config/sniper/keys
```

> **Security:** `.env` is listed in `.gitignore` and must never be committed to version control.

### Step 3 — Docker files in the repository

The following files are already present in the project root:

| File                 | Purpose                                                                                                                                   |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `Dockerfile`         | Multi-stage build: `golang:1.25-alpine` (builder) → `alpine:3.21` (runtime). CGO enabled for go-ethereum. Runs as non-root `sniper` user. |
| `docker-compose.yml` | Three services: `db` (PostgreSQL 16), `migrate` (one-shot migration runner), `bot` (trading daemon).                                      |
| `.dockerignore`      | Excludes source artifacts, secrets, docs, and tests from the build context.                                                               |
| `.env.example`       | Template for all supported environment variables.                                                                                         |

### Step 4 — Run with Docker Compose

```bash
# Build the image and start all three services (db → migrate → bot)
docker compose up --build

# Run in the background (detached mode)
docker compose up --build -d

# View live logs from the bot
docker compose logs -f bot

# Rebuild after code changes
docker compose up --build -d bot

# Stop all services (data is preserved)
docker compose down

# Stop and delete all data including the database volume
docker compose down -v
```

> **Note:** Docker is managed via `docker compose` commands directly. There are no `make docker-*`
> shortcuts — use `docker compose build`, `docker compose up -d`, `docker compose down`, and
> `docker compose logs -f bot` instead.

### Step 5 — Verify

```bash
curl http://localhost:8080/health
# Should return: {"status":"ok"}
```

### Step 6 — Troubleshooting Docker

**`migrate` exits non-zero / bot never starts:**

The `bot` service depends on `migrate` completing successfully (`service_completed_successfully`).
If migrations fail, check the migrate logs:

```bash
docker compose logs migrate
```

**`db` is not ready yet / migrate retries:**

The `migrate` service uses `depends_on: db: condition: service_healthy`. PostgreSQL starts quickly
but the healthcheck (pg_isready) must pass before migrations run. This is automatic — wait a few
seconds and the migrate service will retry.

**Image not rebuilt after code changes:**

Always pass `--build` when you want a fresh image:

```bash
docker compose up --build -d
```

**Solana keypair not found inside container:**

Ensure `SOLANA_KEYS_DIR` in your `.env` points to the correct **host** directory and the file
is named exactly `solana-wallet-1.json`:

```bash
ls -la $SOLANA_KEYS_DIR/solana-wallet-1.json
```

---

## 9. Run on VPS or Cloud

Running the bot on a remote server is strongly recommended for production because:

1. It runs 24/7 even when your laptop is off
2. Datacenter network is faster and more reliable than home internet
3. Servers in Singapore have much lower latency to blockchain nodes than Jakarta home connections

---

### 9.1 Provider Comparison for Jakarta, Indonesia

When running a trading bot from Indonesia, **network latency to blockchain nodes is critical**.
Every millisecond of delay costs you trades. Here is a detailed comparison:

#### Latency context: what matters

The bot connects to:

- Ethereum/BSC RPC nodes (usually US or Singapore)
- Telegram API (nearest CDN node)
- Your PostgreSQL database (runs on the same server — no latency)

**Measured round-trip times from Jakarta:**

| Destination               | From Jakarta Home | From SGP Datacenter |
| ------------------------- | ----------------- | ------------------- |
| US-East (Infura/Alchemy)  | ~175–200ms        | ~170–180ms          |
| Singapore (QuickNode SGP) | ~20–40ms          | ~1–5ms              |
| Jakarta Datacenter        | 5–10ms            | ~180ms              |
| Telegram API              | ~30–60ms          | ~5–15ms             |

**Conclusion:** For best performance, run your bot on a **Singapore VPS** using
**QuickNode Singapore endpoints**. This reduces RPC latency from ~35ms (Jakarta home) to ~3ms.

---

#### Cloud Provider Comparison

| Provider                           | Region              | Specs                    | Price/month (IDR)  | Notes                                 |
| ---------------------------------- | ------------------- | ------------------------ | ------------------ | ------------------------------------- |
| **DigitalOcean**                   | Singapore           | 2 vCPU, 4 GB RAM         | ~Rp 140.000        | Best for beginners, great docs        |
| **Vultr**                          | Singapore           | 2 vCPU, 4 GB RAM         | ~Rp 120.000        | Slightly cheaper, same quality        |
| **Hostinger VPS**                  | Jakarta / Singapore | 2 vCPU, 4 GB RAM         | ~Rp 80.000–120.000 | IDR payment, Indonesian support       |
| **Niagahoster**                    | Indonesia           | 2 vCPU, 4 GB RAM         | ~Rp 100.000        | IDR payment, Bahasa Indonesia support |
| **AWS** (ap-southeast-3)           | Jakarta             | t3.medium (2 vCPU, 4 GB) | ~Rp 200.000        | Has Jakarta region, complex billing   |
| **Google Cloud** (asia-southeast2) | Jakarta             | e2-medium (2 vCPU, 4 GB) | ~Rp 180.000        | Has Jakarta region, complex billing   |
| **Biznet Gio**                     | Jakarta             | 2 vCPU, 4 GB RAM         | ~Rp 150.000        | Indonesian cloud, Bahasa support      |

> **Price note:** USD amounts converted at approximately Rp 16,000/USD. Prices as of early 2026.
> Always verify current pricing on each provider's website.

#### Recommended choice for beginners in Jakarta

**Hostinger VPS or DigitalOcean Singapore** are the best starting points:

- Hostinger supports payment in IDR via local payment methods (bank transfer, GoPay, OVO)
- DigitalOcean has the best documentation and community support in English

**For lowest latency (trading performance):** DigitalOcean or Vultr Singapore.

**For convenience and local payment:** Hostinger Indonesia/Singapore.

---

### 9.2 Setup on a VPS Step-by-Step

This guide uses Ubuntu 22.04 LTS on any provider (steps are identical for DigitalOcean, Vultr,
Hostinger, or AWS).

#### Step 1 — Create a server

On your chosen provider, create a new server (called "Droplet" on DigitalOcean, "Instance" on AWS):

- OS: **Ubuntu 22.04 LTS**
- Size: **2 vCPU, 4 GB RAM** minimum
- Enable SSH key authentication (more secure than password)
- Note the server's **IP address**

#### Step 2 — Connect to your server

```bash
# Replace YOUR_SERVER_IP with the actual IP address
ssh root@YOUR_SERVER_IP

# If using an SSH key file:
ssh -i ~/.ssh/your_key.pem root@YOUR_SERVER_IP
```

#### Step 3 — Install all software on the VPS

```bash
# Update system
apt update && apt upgrade -y

# Install essential tools
apt install -y git curl wget unzip build-essential

# Install Go
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version

# Install PostgreSQL
apt install -y postgresql postgresql-contrib
systemctl start postgresql
systemctl enable postgresql

# Install Docker (optional, for Docker deployment)
apt install -y docker.io docker-compose-plugin
systemctl start docker
systemctl enable docker
```

#### Step 4 — Clone and configure the bot

```bash
cd /opt
git clone https://github.com/YOUR_USERNAME/crypto-sniping-bot.git
cd crypto-sniping-bot
```

Create `.env`:

```bash
nano .env
# Paste all your environment variables (same as Section 5.1)
# Save with Ctrl+O, exit with Ctrl+X
```

If you are enabling Solana trading, also set up the keypair directory on the VPS:

```bash
# Create a secure directory for keys
mkdir -p /etc/sniper/keys
chmod 700 /etc/sniper/keys

# Copy your keypair file from your local machine to the VPS:
# Run this on your LOCAL machine (not on the VPS):
scp ~/.config/sniper/keys/solana-wallet-1.json root@YOUR_SERVER_IP:/etc/sniper/keys/
ssh root@YOUR_SERVER_IP "chmod 600 /etc/sniper/keys/solana-wallet-1.json"

# Then in your .env on the VPS, set:
# SOLANA_WALLET_KEY_1=/etc/sniper/keys/solana-wallet-1.json
```

#### Step 5 — Set up PostgreSQL

```bash
sudo -u postgres psql -c "CREATE USER sniper WITH PASSWORD 'your_secure_password';"
sudo -u postgres psql -c "CREATE DATABASE sniper OWNER sniper;"
sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE sniper TO sniper;"
```

#### Step 6 — Run migrations

```bash
export $(grep -v '^#' .env | grep -v '^$' | xargs)
make migrate-up
```

#### Step 7 — Build the bot

```bash
make build
```

#### Step 8 — Run as a systemd service (runs automatically on boot and restart)

Create a systemd service file:

```bash
cat > /etc/systemd/system/sniper-bot.service << EOF
[Unit]
Description=Crypto Sniping Bot
After=network.target postgresql.service

[Service]
Type=simple
User=root
WorkingDirectory=/opt/crypto-sniping-bot
EnvironmentFile=/opt/crypto-sniping-bot/.env
ExecStart=/opt/crypto-sniping-bot/bin/crypto-sniping-bot serve
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sniper-bot

[Install]
WantedBy=multi-user.target
EOF
```

Enable and start:

```bash
systemctl daemon-reload
systemctl enable sniper-bot
systemctl start sniper-bot
```

Check status:

```bash
systemctl status sniper-bot

# View live logs:
journalctl -u sniper-bot -f
```

#### Step 9 — Configure firewall

```bash
# Allow SSH (so you don't lock yourself out)
ufw allow 22

# Allow health check port (optional — only if you need external monitoring)
ufw allow 8080

# Enable firewall
ufw enable
```

#### Step 10 — Keeping the bot updated

```bash
# Pull latest code
cd /opt/crypto-sniping-bot
git pull

# Rebuild
make build

# Restart service
systemctl restart sniper-bot
```

---

### 9.3 Docker on VPS

If you prefer to run via Docker on the VPS (simpler dependency management):

```bash
cd /opt/crypto-sniping-bot
docker compose up --build -d
```

To view logs: `docker compose logs -f bot`
To restart: `docker compose restart bot`
To update: `git pull && docker compose up --build -d`

---

## 10. Monitoring & Health Check

### Health endpoint

The bot exposes a health check endpoint:

```bash
curl http://localhost:8080/health
# Returns: {"status":"ok"}
```

If running on a VPS, replace `localhost` with your server IP:

```bash
curl http://YOUR_SERVER_IP:8080/health
```

### Viewing logs

```bash
# Local run: logs appear directly in the terminal
# Systemd service:
journalctl -u sniper-bot -f       # Follow live
journalctl -u sniper-bot -n 100   # Last 100 lines

# Docker:
docker compose logs -f bot
```

### Log format

All logs are structured JSON. Key fields:

```json
{
  "time": "2026-04-26T10:00:00Z",
  "level": "INFO",
  "msg": "trade executed",
  "token": "0xABC...",
  "chain": "eth",
  "size_usd": 50.0,
  "tx_hash": "0x123..."
}
```

### Running tests

```bash
# Run all tests (does not require network or real credentials)
make test

# Run with coverage report
make test-cover
# Then open coverage.html in a browser to see coverage
```

---

## 11. Common Errors and Fixes

| Error                                                     | Likely Cause                  | Fix                                                                                      |
| --------------------------------------------------------- | ----------------------------- | ---------------------------------------------------------------------------------------- |
| `dial tcp 127.0.0.1:5432: connect: connection refused`    | PostgreSQL not running        | `brew services start postgresql@16` (macOS) or `systemctl start postgresql` (Linux)      |
| `FATAL: password authentication failed for user "sniper"` | Wrong DB password             | Check `SNIPER_DB_PASSWORD` env var matches the password you set for the `sniper` DB user |
| `dial tcp: lookup mainnet.infura.io: no such host`        | No internet / bad RPC URL     | Check internet connection, verify `ETH_RPC_1` URL is correct                             |
| `json: cannot unmarshal` on config load                   | Malformed YAML                | Check for tabs in YAML files (YAML uses spaces only)                                     |
| `SNIPER_WALLET_KEY must be set`                           | Missing wallet key env var    | Export `SNIPER_WALLET_KEY` in your shell                                                 |
| `insufficient funds for gas`                              | Wallet has no ETH/BNB         | Add ETH or BNB to your wallet                                                            |
| `connection refused` on health check                      | Bot not started or wrong port | Check bot is running and `PORT` env var is correct                                       |
| `migration already applied`                               | Running migrate-up twice      | This is safe — idempotent. Already-applied migrations are skipped.                       |
| `go: module crypto-sniping-bot: not found`                | Running from wrong directory  | `cd` into the project root first                                                         |
| `load keypair: read file: no such file`                   | Wrong Solana wallet path      | Check `SOLANA_WALLET_KEY_1` points to your actual `*.json` keypair file                  |
| `parse keypair: expected 64 bytes`                        | Invalid Solana keypair file   | Re-generate with `solana-keygen new --outfile <path>.json`                               |
| `websocket: dial wss://...`                               | Bad Solana WS URL             | Verify `SOLANA_WS_1` is correct and your Helius/QuickNode key has WebSocket access       |
| `solana: rpc rate limit exceeded`                         | RPC plan limit hit            | Upgrade Helius/QuickNode plan or reduce scan frequency                                   |

---

## 12. Reference — All Environment Variables

Complete reference of every environment variable the bot reads. Variables marked **Required** will
cause startup failure if not set.

| Variable                        | Required     | Default                | Description                                                   |
| ------------------------------- | ------------ | ---------------------- | ------------------------------------------------------------- |
| `SNIPER_DB_PASSWORD`            | **Required** | —                      | PostgreSQL password for the `sniper` database user            |
| `SNIPER_DB_HOST`                | Optional     | `localhost`            | PostgreSQL hostname                                           |
| `SNIPER_DB_NAME`                | Optional     | `sniper`               | PostgreSQL database name                                      |
| `SNIPER_DB_USER`                | Optional     | `sniper`               | PostgreSQL username                                           |
| `SNIPER_DB_SSL_MODE`            | Optional     | `disable`              | PostgreSQL SSL mode (`disable`, `require`, `verify-full`)     |
| `ETH_RPC_1`                     | Required\*   | —                      | Ethereum HTTP RPC endpoint #1 (\*if ETH chain enabled)        |
| `ETH_RPC_2`                     | Optional     | —                      | Ethereum HTTP RPC endpoint #2 (fallback)                      |
| `ETH_WS_1`                      | Required\*   | —                      | Ethereum WebSocket endpoint (\*if ETH chain enabled)          |
| `BSC_RPC_1`                     | Required\*   | —                      | BSC HTTP RPC endpoint #1 (\*if BSC chain enabled)             |
| `BSC_RPC_2`                     | Optional     | —                      | BSC HTTP RPC endpoint #2 (fallback)                           |
| `BSC_WS_1`                      | Required\*   | —                      | BSC WebSocket endpoint (\*if BSC chain enabled)               |
| `SNIPER_WALLET_ADDRESS`         | **Required** | —                      | Primary trading wallet address (0x...)                        |
| `SNIPER_WALLET_KEY`             | **Required** | —                      | Primary wallet private key (64-char hex, no 0x prefix)        |
| `SNIPER_WALLET_0_ADDRESS`       | Optional     | —                      | Shard 0 wallet address (overrides single wallet for sharding) |
| `SNIPER_WALLET_0_KEY`           | Optional     | —                      | Shard 0 private key                                           |
| `SNIPER_WALLET_1_ADDRESS`       | Optional     | —                      | Shard 1 wallet address                                        |
| `SNIPER_WALLET_1_KEY`           | Optional     | —                      | Shard 1 private key                                           |
| `SNIPER_WALLET_2_ADDRESS`       | Optional     | —                      | Shard 2 wallet address                                        |
| `SNIPER_WALLET_2_KEY`           | Optional     | —                      | Shard 2 private key                                           |
| `SNIPER_WALLET_3_ADDRESS`       | Optional     | —                      | Shard 3 wallet address                                        |
| `SNIPER_WALLET_3_KEY`           | Optional     | —                      | Shard 3 private key                                           |
| `SOLANA_RPC_HTTP_1`             | Required‡    | —                      | Solana HTTP RPC endpoint #1 (‡if Solana chain enabled)        |
| `SOLANA_RPC_HTTP_2`             | Optional     | —                      | Solana HTTP RPC endpoint #2 (fallback)                        |
| `SOLANA_WS_1`                   | Required‡    | —                      | Solana WebSocket endpoint (‡if Solana chain enabled)          |
| `SOLANA_WALLET_KEY_1`           | Required‡    | —                      | File path to Solana keypair JSON (‡if Solana chain enabled)   |
| `SOLANA_GRPC_ENDPOINT`          | Optional     | —                      | Solana gRPC endpoint URL (Phase 11; only when `mode: grpc`)   |
| `SOLANA_GRPC_TOKEN`             | Optional     | —                      | Solana gRPC auth token (Phase 11; env only — never in YAML)   |
| `JITO_BUNDLE_URL`               | Optional‡    | —                      | Jito bundle submission URL — HTTPS only (‡if Solana MEV on)   |
| `JITO_TIP_ACCOUNT`              | Optional‡    | —                      | Jito tip account address (‡if Solana MEV on)                  |
| `BIRDEYE_API_KEY`               | Optional     | —                      | BirdEye API key for dev reputation and token metadata DQ      |
| `COPY_TRADE_WALLETS`            | Optional     | —                      | Comma-separated alpha wallet addresses for copy-trade DQ      |
| `SNIPER_TELEGRAM_BOT_TOKEN`     | Optional     | —                      | Telegram bot token (for notifications)                        |
| `SNIPER_TELEGRAM_CHAT_ID`       | Optional     | —                      | Telegram chat/group ID (for notifications)                    |
| `SNIPER_TELEGRAM_ALLOWED_USERS` | Optional     | —                      | Comma-separated Telegram user IDs permitted to issue commands |
| `PORT`                          | Optional     | `8080`                 | HTTP server port for health check endpoint                    |
| `LOG_LEVEL`                     | Optional     | `info`                 | Log verbosity: `debug`, `info`, `warn`, `error`               |
| `CONFIG_PATH`                   | Optional     | `config/pipeline.yaml` | Override path to main config file                             |

> **‡ Solana variables** are only required when `config/chains.yaml` has a `solana:` block with
> valid `rpc:` entries. If the env vars are absent, the Solana ingestion and execution workers
> will not start. EVM (ETH/BSC) workers are unaffected.

> **Security rule:** Never put private keys or the DB password in any YAML file or commit them
> to Git. These values must only ever exist as environment variables.

---

## 13. Phase 7 – 11 Features Overview

This section explains what was added in Phase 7 (Solana Market), Phase 8 (Production Hardening),
and Phase 9 (Profitability Restoration & Signal Integrity) so you can understand what the bot
is doing under the hood.

---

### 13.1 Phase 7 — Solana Market Sniping

Phase 7 added a complete Solana trading pipeline alongside the existing EVM (ETH/BSC) pipeline.
Both pipelines run independently; they do not interfere with each other.

#### What markets does it watch?

The Solana ingestion module watches **two programs** in real time:

| Program            | Program ID                                     | What it is                   |
| ------------------ | ---------------------------------------------- | ---------------------------- |
| **Raydium v4 AMM** | `675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8` | Largest Solana DEX by volume |
| **PumpFun**        | `6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P`  | Meme-coin launch platform    |

It uses the Solana WebSocket `logsSubscribe` API to get notified of new pool creation events
in real time \u2014 equivalent to watching `PairCreated` events on EVM.

#### Solana execution flow

1. WebSocket subscription → `ingestion_solana` module detects new pool events
2. Normalizes to `MarketDataDTO` and emits to the event bus
3. Same 10-layer pipeline processes the event (Data Quality → Feature → Edge → Score → Select)
4. `execution_solana` module signs the swap transaction using your Ed25519 keypair
5. Submits to the Solana network; polls for confirmation up to `confirm_timeout_ms` (default: 15s)
6. Wallet sharding: `hash(tokenAddress) % len(keypairs)` ensures one in-flight tx per wallet

#### New Solana tables (Migration 000012)

Migration `20260101000012_solana_tables.sql` adds:

- `solana_slot_cursors` — tracks which Solana slot each program was last processed at (for gap recovery)
- `solana_execution_receipts` — records on-chain signatures and confirmation status

#### Solana keypair security

The keypair JSON file (`solana-keygen new` output) contains a 64-byte array:

- Bytes 0–31: Ed25519 private key seed
- Bytes 32–63: Public key (derivable from seed — ignored on load)

The file must have permission `600` (readable only by your user). On VPS:

```bash
chmod 600 /etc/sniper/keys/solana-wallet-1.json
```

---

### 13.2 Phase 8 — Production Hardening

Phase 8 added a set of reliability and safety mechanisms that operate transparently. You do not
need to configure these for normal use — they activate automatically.

#### What was added

| Feature                   | What it does                                                                                                                                                                                           |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Reconciliation worker** | Detects positions the bot holds but has no open record for (e.g. after crash). Resolves discrepancy by emitting corrective events.                                                                     |
| **Partition leasing**     | Workers claim exclusive ownership of event partitions via `partition_leases` table. Prevents two workers processing the same event on restart.                                                         |
| **DLQ retry policy**      | Events that fail processing more than `max_transient_retries` (default: 5) times are moved to the Dead Letter Queue (DLQ). The DLQ worker retries them on a slower schedule.                           |
| **Crash recovery**        | On restart, the bot detects in-flight executions (status = `submitted`) that were started before the crash. If they are older than `recovery_grace_sec` (default: 300s), it marks them for evaluation. |
| **Reorg protection**      | On EVM chains, if a confirmed block is re-orged beyond `max_reorg_depth` (default: 12 blocks), the bot emits a halt event and stops trading until the chain stabilizes.                                |

#### New tables (Migrations 000013–000014)

Migration `20260101000013_production_hardening.sql` adds:

- `partition_leases` — worker lease table with TTL
- `dlq_events` — dead-letter queue for unprocessable events

Migration `20260101000014_pr_fixes.sql` adds:

- Index improvements and minor schema corrections from code review

#### HardeningConfig parameters (in `config/pipeline.yaml`)

All Phase 8 parameters have sensible defaults. You can override them in `config/pipeline.yaml`:

```yaml
hardening:
  reconciliation_interval_ms: 30000 # Check for orphaned positions every 30s
  reconciliation_tolerance_bps: 50 # 0.5% tolerance before flagging discrepancy
  partition_lease_ttl_sec: 60 # Worker holds a partition for 60s max
  max_transient_retries: 5 # Retry a failing event up to 5x before DLQ
  max_application_retries: 3 # Retry application-logic errors up to 3x
  recovery_grace_sec: 300 # Wait 5 min after crash before marking tx lost
  max_reorg_depth: 12 # Halt if >12 blocks re-orged (EVM)
```

---

### 13.3 Telegram Operator Commands (Phase 6, fully operational)

The Telegram dispatcher (`internal/telegram/`) is fully wired and active. All notifications flow
through the PostgreSQL event bus \u2014 modules never call Telegram directly.

**Operator commands** (send from your Telegram chat to the bot):

| Command                      | What it does                                                                        |
| ---------------------------- | ----------------------------------------------------------------------------------- |
| `/status`                    | Shows current mode (STRICT/BALANCED/EXPLORATION/VERY_EXPLORATION), active positions |
| `/pnl`                       | Shows today's realized PnL and win/loss rate                                        |
| `/positions`                 | Lists all open positions with entry price and current P&L                           |
| `/position <prefix>`         | Shows detail for one position matched by token address prefix                       |
| `/health`                    | Shows worker health: ingestion, execution, position, learning, telegram             |
| `/pipeline`                  | Shows pipeline stage counters: events processed per stage in the last hour          |
| `/rescan_pipeline`           | Shows rescan worker status and how many tokens are queued per band (15m/30m/45m/1h) |
| `/rescan_status`             | Shows rescan worker enabled/disabled state and last run timestamp                   |
| `/dq [hours]`                | Shows Data Quality rejection breakdown for the last N hours (default: 1)            |
| `/dlq`                       | Shows Dead Letter Queue size and top error types                                    |
| `/executions`                | Last 20 tokens that reached execution stage (success + failed) with full CA address |
| `/version`                   | Shows current strategy version and config snapshot hash                             |
| `/mode strict`               | Switches to STRICT mode (conservative — rug-spike safety)                           |
| `/mode balanced`             | Switches to BALANCED mode (default)                                                 |
| `/mode explore`              | Switches to EXPLORATION mode (relaxed thresholds, starvation recovery)              |
| `/mode very_explore`         | Switches to VERY_EXPLORATION mode (maximum aggression — new-launch sniping)         |
| `/enable_trading`            | Enables live trade execution (bot starts in shadow mode — no trades by default)     |
| `/rescan`                    | Manually triggers a full rescan of eligible tokens in all age bands                 |
| `/force_close <addr_prefix>` | Force-closes a position by token address prefix (use carefully — bypasses TP/SL)    |
| `/kill`                      | Triggers emergency kill switch — halts all new trades immediately                   |
| `/resume`                    | Resumes trading after kill switch (requires confirmation)                           |
| `/help`                      | Lists all available commands with short descriptions                                |

> **Note:** `/kill`, `/resume`, and `/force_close` are destructive commands. They are logged with timestamp and
> require confirmation. The kill switch also fires automatically when daily drawdown exceeds the
> configured threshold (default: 10%).

---

### 13.4 Phase 9 — Profitability Restoration & Signal Integrity

Phase 9 is the **most important phase for profitability**. Phases 0–8 made the bot _functional_;
Phase 9 makes it _profitable_. Before Phase 9, several critical components used hardcoded stubs
(returning fixed values like `0.5` or `0.35`) instead of real on-chain data. Phase 9 replaces
those stubs with real computation and moves every tunable parameter into YAML config files.

The architecture profit formula is:

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

Phase 9 addresses the four factors that were near-zero before it: **DataQuality**, **Probability**,
**Capital**, and **AdaptationQuality**.

---

#### 13.4.1 Real Scam Detection — `config/data_quality.yaml` (Layer 1)

Before Phase 9, the Data Quality Engine checked tokens for scams but used hardcoded weights to
combine the results. Now every weight comes from `config/data_quality.yaml`.

**What scam checks does the bot run?**

| Detector            | What it checks                                                          | Cost       |
| ------------------- | ----------------------------------------------------------------------- | ---------- |
| Honeypot simulation | Simulates a buy-then-sell on-chain — if you can't sell, it's a honeypot | 1 RPC call |
| Tax anomaly         | Checks if buy/sell tax exceeds 10% (a common rug pattern)               | 1 RPC call |
| LP lock             | Verifies liquidity is locked (Unicrypt/PinkLock) for at least 30 days   | 1 API call |
| Wash trading        | Checks if the same wallets keep trading with themselves                 | 1 RPC call |
| Rug authority       | Checks if the contract has dangerous functions (mint, pause, blacklist) | 1 RPC call |
| Contract verified   | Checks if the source code is public on Etherscan/BscScan                | 1 API call |
| Dev reputation      | Checks if the deployer wallet has previously rugged other tokens        | 1 DB query |

**What you can tune:**

The `risk_weights` section in `config/data_quality.yaml` controls how much each detector
contributes to the overall scam risk score (0.0 = ignored, higher = more weight):

```yaml
# config/data_quality.yaml
data_quality:
  risk_weights:
    honeypot: 0.30 # Honeypot is the most reliable signal — highest weight
    tax_anomaly: 0.20 # High tax is a strong rug indicator
    rug_authority: 0.20 # Dangerous contract functions
    lp_lock_missing: 0.15 # Unlocked LP can be pulled at any time
    wash_trading: 0.10 # Wash trading pattern
    contract_unverified: 0.05 # Unverified contract (weaker signal alone)
```

> **Beginner tip:** If the bot is rejecting tokens you believe are safe, lower `honeypot` to
> `0.20`. If it is letting through too many rugs, raise `honeypot` to `0.40`. The default values
> match what was previously hardcoded — they are safe to use as-is.

The bot also lets you tune per-detector thresholds:

```yaml
thresholds:
  tax_total_max_bps: 1000 # Reject if total buy+sell tax > 10%
  wash_unique_ratio_min: 0.30 # Reject if < 30% of wallets are unique
  lp_lock_min_days: 30 # Reject if LP locked for < 30 days
```

---

#### 13.4.2 Real Feature Signals — `config/feature.yaml` (Layer 2)

Before Phase 9, five out of eight trading signals returned a hardcoded `0.5` ("neutral") instead
of computing a real value from on-chain data. This meant the bot could not distinguish a token
with explosive momentum from a dead token.

Phase 9 wires up the following real signals:

| Signal              | What it measures                                                     | Data source            |
| ------------------- | -------------------------------------------------------------------- | ---------------------- |
| `tx_velocity_score` | How many buy transactions happened in the last 30 seconds            | Recent Swap events     |
| `wallet_entropy`    | How many _different_ wallets are buying (high = organic, low = wash) | Recent Swap senders    |
| `token_age`         | How old the token is (sweet spot: 30 seconds to 5 minutes old)       | Pool creation block    |
| `volume_momentum`   | Short-term vs long-term volume ratio (is volume accelerating?)       | Sync event ring buffer |
| `price_momentum`    | Price change between recent Sync events                              | Sync event ring buffer |

These are the signals the edge detection layer uses to decide _"is this token worth buying right now?"_

**Key config options in `config/feature.yaml`:**

```yaml
feature:
  feature_timeout_ms: 500 # If RPC call takes > 500ms, use cold-start default

  tx_velocity:
    window_sec: 30 # Count swaps in the last 30 seconds
    swap_count_normalize_high: 50 # 50+ swaps/30s → score 1.0 (very hot)

  token_age:
    sweet_spot_min_sec: 30 # Token younger than 30s: score 0.2 (too new, risky)
    sweet_spot_max_sec:
      300 # Token 30s–5min old: score 1.0 (ideal window)
      # Token older than 5min: score 0.6 (cooling down)

  # Phase 11: Holder concentration (top-N wallet analysis)
  holder_concentration:
    enabled: true
    top_n: 10 # Check the top 10 holder wallets
    max_acceptable_bps: 5000 # Score drops if top-10 hold > 50% of supply
    confidence_target: 0.80

  # Phase 11: Social presence (Twitter / Telegram / website)
  social_links:
    enabled: true
    confidence_target: 0.70
```

> **Beginner tip:** The `feature_timeout_ms` setting is important if your RPC is slow. If you see
> many `cold_start_default` entries in the logs, increase this to `800` — but beware that slower
> feature computation means slower trade decisions.

---

#### 13.4.3 Real Probability Scores — `config/probability.yaml` (Layer 4)

Before Phase 9, the validation layer always used a fixed probability of `0.35` regardless of the
token. This meant every trade was evaluated as if it had a 35% chance of success — completely
ignoring the probability model that was being trained in the background.

Phase 9 wires the probability model output into the validation gate. Now each token gets its own
probability score based on historical patterns.

**What happens when the model doesn't have enough data?** (e.g., a brand-new launch)

The bot falls back to the `prior_probability` you configure:

```yaml
# config/probability.yaml
probability:
  use_model_output: true # Use model output when available
  prior_probability: 0.35 # Conservative fallback for new tokens
  min_model_confidence: 0.40 # If model confidence < 40%, use prior instead
  prob_join_timeout_ms: 200 # Max 200ms to wait for model output before fallback

  # Alert via Telegram if > 5% of recent trades used fallback in the last hour
  fallback_alert_pct: 0.05
  fallback_alert_window_sec: 3600
```

**What the fallback alert means:** If more than 5% of trades in the last hour used the fallback
probability (meaning the model couldn't score them), you will receive a Telegram alert. This is
your signal that the probability model is struggling — possibly due to a data gap or model drift.

> **Beginner tip:** You do not need to change `prior_probability` immediately. If you find the
> bot is too aggressive on new tokens, lower it to `0.25`. If it is too conservative, raise it
> to `0.45`. Never set it above `0.5` — that would mean you expect to win more than you lose on
> tokens with no data, which is optimistic at best.

---

#### 13.4.4 Dynamic Capital Sizing — `config/capital.yaml` (Layer 7)

Before Phase 9, the bot always bet a fixed dollar amount (e.g., `$50`) on every trade regardless
of how strong or weak the signal was. This is suboptimal — a very strong signal deserves more
capital; a borderline signal deserves less.

Phase 9 introduces **Kelly fraction sizing**: position size is proportional to your edge score,
the model's probability estimate, and the confidence of the features.

```
size = base_size_usd × kelly_fraction × mode_multiplier × cohort_multiplier
```

**Key config in `config/capital.yaml`:**

```yaml
capital:
  use_dynamic_sizing: true # Set to false to revert to fixed_entry_size_usd (emergency only)
  base_size_usd: 5.0 # Starting point for sizing calculation
  min_size_usd: 5.0 # Never allocate less than $5 (not worth gas)
  max_size_usd: 500.0 # Never allocate more than $500 per trade

  kelly:
    cap: 0.25 # Use at most 25% Kelly fraction (full Kelly is too aggressive)

  mode_multipliers:
    STRICT: 0.5 # In STRICT mode, halve the position size
    BALANCED: 1.0 # In BALANCED mode, normal sizing
    EXPLORATION: 1.3 # In EXPLORATION mode, 30% larger (exploring new patterns)
    VERY_EXPLORATION: 1.5 # In VERY_EXPLORATION mode, 50% larger (maximum aggression)

  failure_policy:
    on_missing_probability: "reject" # "reject" or "fallback_prior"
    fallback_prior_probability: 0.35 # Used only if on_missing_probability="fallback_prior"
```

**Understanding `on_missing_probability`:**

| Setting            | What happens when probability model is unavailable                           |
| ------------------ | ---------------------------------------------------------------------------- |
| `reject` (default) | Bot refuses to trade — safest choice                                         |
| `fallback_prior`   | Bot trades using `fallback_prior_probability` — higher throughput, more risk |

> **Beginner recommendation:** Keep `on_missing_probability: reject` for your first week. The bot
> will trade less often, but every trade will have a real probability score backing it.

> **Important:** If `fallback_prior_probability` is `0` or missing, the bot treats it as "unset"
> and rejects trades even when `on_missing_probability: fallback_prior`. This is intentional —
> a missing config value is never silently treated as a safe default.

---

#### 13.4.5 Real-Time Position Monitoring — `config/pipeline.yaml` (Layer 9)

Before Phase 9, positions were monitored on a fixed 5-second timer. Phase 9 changes this to
price-feed-driven monitoring: the position monitor polls whenever a new price event arrives from
the on-chain price feed, not on a clock.

This means:

- **Faster exits**: If a token dumps 15% in 2 seconds, the stop-loss fires in < 1 second instead of up to 5 seconds
- **No unnecessary polling**: Between price events, the monitor sleeps (saves CPU and RPC calls)

The existing position parameters in `config/pipeline.yaml` still control exit behavior:

```yaml
position:
  tp1_bps: 1500 # Take Profit 1: sell 50% when up 15%
  tp2_bps: 4000 # Take Profit 2: sell rest when up 40%
  sl_bps: 500 # Stop Loss: sell all when down 5%
  max_hold_seconds: 300 # Emergency exit after 5 minutes — meme tokens pump or dump fast
```

> **What changed for you:** Your stop-loss and take-profit now trigger faster. If you previously
> set `sl_bps: 500` and were seeing losses deeper than 5%, this improvement will help. If you
> were not seeing that issue, no action is needed.

---

#### 13.4.6 Phase 9 Config Summary

These are all the YAML files Phase 9 adds or changes. All defaults are safe to use without
modification — they match what was previously hardcoded:

| Config file                | Key sections                                      | What to tune                   |
| -------------------------- | ------------------------------------------------- | ------------------------------ |
| `config/data_quality.yaml` | `risk_weights`, `thresholds`, `detectors`         | Scam detection sensitivity     |
| `config/feature.yaml`      | `tx_velocity`, `token_age`, `volume_momentum`     | Feature extraction timing      |
| `config/probability.yaml`  | `prior_probability`, `fallback_alert_pct`         | Probability fallback behavior  |
| `config/capital.yaml`      | `kelly.cap`, `mode_multipliers`, `failure_policy` | Position sizing aggressiveness |
| `config/pipeline.yaml`     | `position.*` (unchanged format)                   | Exit parameters                |

**Nothing is required before your first run.** Review these files after your first 50 live trades
and tune based on what you observe in the Telegram alerts and database.

---

### 13.5 Phase 10 — Reference-Repo Improvements (R1)

Phase 10 is a targeted improvement pass that ports several high-impact techniques from
production-grade open-source sniper bots. Each change is individually gated so it can be disabled
without affecting the rest of the pipeline.

#### 13.5.1 Trailing Stop after TP1 — `config/pipeline.yaml` (Layer 9)

Before Phase 10, the bot had fixed take-profit levels. After TP1 hit, it would simply wait for
TP2. If the price reversed sharply after TP1, the open portion could give back all gains.

Phase 10 adds a **trailing stop** that activates only after TP1 fires:

```yaml
# config/pipeline.yaml
position:
  tp1_bps: 1500 # Still take 50% at +15%
  tp1_filled_pct_bps: 5000 # Sell exactly 50% at TP1
  trailing_stop_bps: 500 # Trailing stop: lock in profits — exit if price drops 5% from peak
  trailing_activate_at_tp1: true # Only activate trailing stop after TP1 is hit
```

> **Beginner tip:** The default `trailing_stop_bps: 500` (5%) is conservative. If you want to
> let winners run longer, try `800` (8%). If you keep getting stopped out on small pullbacks,
> try `300` (3%). Set `trailing_stop_bps: 0` to disable entirely (legacy behaviour).

#### 13.5.2 Consecutive-Pass Gate — `config/pipeline.yaml` (Layer 5)

Phase 10 adds a **debounce filter** to the edge validation layer: a token must pass the EV gate
on multiple consecutive evaluations within a time window before a position is entered.

This eliminates single-spike false positives where a token scores well on one snapshot but
the signal is not sustained.

```yaml
# config/pipeline.yaml
validation:
  required_consecutive_passes: 2 # Token must pass the EV gate twice in a row
  consecutive_pass_window_seconds: 30 # Both passes must occur within 30 seconds
  # Set required_consecutive_passes: 1 (or 0) to disable (single-pass, legacy behaviour)
```

#### 13.5.3 Bonding Curve Progress Filter — `config/data_quality.yaml` (Layer 1, Solana)

For Solana PumpFun tokens, Phase 10 adds a filter that rejects tokens that have already
progressed too far along their bonding curve. Tokens near 100% completion are about to migrate
to Raydium — the sniping window has closed.

```yaml
# config/data_quality.yaml
thresholds:
  max_bonding_curve_progress_bps: 8000 # Reject if > 80% bonded (0 = disabled)
```

#### 13.5.4 Volume-Staleness Time Exit — `config/pipeline.yaml` (Layer 9)

Phase 10 adds a time-based exit trigger for positions where the token's trading volume has
gone stale — the token is still alive but no longer moving.

```yaml
# config/pipeline.yaml
position:
  volume_staleness_seconds: 1800 # Check staleness after 30 min hold
  volume_staleness_min_delta_pct_bps: 200 # Exit if 24h volume grew < 2% (stale)
  # Set volume_staleness_seconds: 0 to disable
```

---

### 13.6 Phase 11 — Reference-Repo Improvements (R2)

Phase 11 ports the remaining high-signal techniques from the reference implementations.
All six features are independently togglable. The defaults are conservative (most off) to
preserve legacy behaviour on first deploy.

#### 13.6.1 Creator Hygiene Filters — `config/pipeline.yaml` (Layer 3)

Phase 11 adds three creator-level filters to the edge detection layer. These prevent the bot
from trading tokens launched by wallets with a history of rugs or suspicious activity.

```yaml
# config/pipeline.yaml
edge:
  max_dev_buy_pct_bps: 2000 # Reject if creator bought > 20% of supply at launch (0 = disabled)
  max_creator_rug_count: 2 # Reject if creator has ≥ 2 confirmed prior rugs (0 = disabled)
  min_dev_wallet_age_seconds: 86400 # Reject wallets younger than 24 hours (0 = disabled)
```

> **How the rug count works:** Every time a position closes as `RUG`, the learning engine records
> a `CreatorRugObservation` in the `creator_blacklist` database table. Next time a token from
> the same creator address is evaluated, `max_creator_rug_count` is checked against that count.
> The threshold is per-chain (ETH rugs don't count against BSC creators).

#### 13.6.2 Per-Creator Position Deduplication — `config/pipeline.yaml` (Layer 6)

Phase 11 prevents the bot from holding multiple open positions in tokens launched by the
same creator wallet simultaneously. This prevents correlated loss if a creator rugs multiple
tokens at once.

```yaml
# config/pipeline.yaml
selection:
  max_positions_per_creator: 1 # At most 1 open position per creator wallet (0 = disabled)
```

#### 13.6.3 Holder Concentration Filter — `config/feature.yaml` (Layer 2)

Phase 11 adds a holder concentration extractor that scores tokens where the top N wallets
hold an unusually large percentage of the supply. High concentration means a few wallets can
dump at any time.

```yaml
# config/feature.yaml
feature:
  holder_concentration:
    enabled: true
    top_n: 10 # Check the top 10 holders
    max_acceptable_bps: 5000 # Score drops if top-10 hold > 50% of supply
    confidence_target: 0.80
```

> **Beginner note:** This feature requires an on-chain `balanceOf` call per unique holder —
> it adds ~1–2 RPC calls per token. On a free-tier plan this is negligible. Enable it for
> better signal quality.

#### 13.6.4 Social Links Presence — `config/feature.yaml` (Layer 2)

Phase 11 adds a feature that checks whether a token's contract metadata includes social links
(Twitter, Telegram, website). Tokens with zero social presence are statistically more likely to
be rug pulls.

```yaml
# config/feature.yaml
feature:
  social_links:
    enabled: true
    confidence_target: 0.70
```

> **Note:** This is a soft signal — it contributes to the feature score but does not alone
> reject a token. Use it in combination with other filters.

#### 13.6.5 Congestion-Aware Slippage — `config/pipeline.yaml` (Layer 4)

Phase 11 adds a congestion multiplier to the slippage model. When on-chain latency is elevated
(network is congested), the slippage estimate is scaled up proportionally to ensure the EV
gate rejects marginal trades that would be unprofitable under actual congestion conditions.

```yaml
# config/pipeline.yaml
models:
  congestion:
    enabled: false # Disabled by default; enable after baseline latency data is collected
    latency_anchor_ms: 800 # "Normal" latency — no adjustment below this
    max_multiplier: 2.0 # Cap the congestion multiplier at 2× base slippage
```

> **Beginner note:** Leave `enabled: false` until you have at least a week of latency baseline
> data from your RPC provider. The congestion multiplier is derived from real-time RPC latency
> measurements — once enabled, you do not need to set a fixed value.

#### 13.6.6 Simulation Diff Evaluation — `config/priority.yaml` (Layer 9→10)

Phase 11 adds a `ComputeExecutionVariance` step to the evaluation layer. After each trade
completes, it compares the simulated swap output (predicted by the slippage model) against the
realized output (what actually happened on-chain). The variance (in basis points) is stored in
the `LearningRecord` and used by the learning engine to improve future slippage estimates.

```yaml
# config/priority.yaml
evaluation:
  enable_simulation_diff: false # Set true to record simulated vs realized slippage variance
```

#### 13.6.7 Solana Hybrid Transport — `config/chains.yaml`

Phase 11 adds a hybrid transport layer for Solana ingestion. When `mode: grpc` is configured
(Triton One or Helius gRPC stream), the bot uses gRPC for ultra-low latency log delivery.
If gRPC fails `fallback_error_n` consecutive times, it automatically falls back to the standard
WebSocket transport without restarting.

```yaml
# config/chains.yaml
solana:
  transport:
    mode: rpc # "rpc" (default WebSocket) or "grpc"
    grpc_endpoint: "" # Required if mode=grpc: e.g. "your-triton-endpoint:443"
    # grpc_auth_token is loaded from env var SOLANA_GRPC_TOKEN (never put in YAML)
    fallback_on_error: true # Automatically fall back to RPC if gRPC fails
    fallback_error_n: 5 # Fall back after this many consecutive gRPC errors
```

> **Beginner recommendation:** Leave `mode: rpc` unless you have a gRPC-capable Solana provider
> (Triton One or Helius Business tier). The WebSocket transport is production-ready for most
> use cases. gRPC provides ~10–20ms lower latency for high-frequency sniping.

#### 13.6.8 Phase 11 Config Summary

| Config file            | New keys added                                                                    | Purpose                           |
| ---------------------- | --------------------------------------------------------------------------------- | --------------------------------- |
| `config/pipeline.yaml` | `edge.max_dev_buy_pct_bps`, `max_creator_rug_count`, `min_dev_wallet_age_seconds` | Creator hygiene gates             |
| `config/pipeline.yaml` | `selection.max_positions_per_creator`                                             | Per-creator dedup                 |
| `config/pipeline.yaml` | `models.congestion` block                                                         | Congestion slippage adjustment    |
| `config/pipeline.yaml` | `learning.creator_blacklist` block                                                | Creator blacklist persistence     |
| `config/feature.yaml`  | `feature.holder_concentration` block                                              | Top-N holder concentration filter |
| `config/feature.yaml`  | `feature.social_links` block                                                      | Social presence signal            |
| `config/chains.yaml`   | `solana.transport` block                                                          | Hybrid gRPC/WebSocket transport   |
| `config/priority.yaml` | `evaluation.enable_simulation_diff`                                               | Sim-diff variance tracking        |

**All new keys have safe defaults and are independently disableable.** No changes are required
before your first run. Enable them gradually after validating baseline behaviour.

---

## 14. Scenario Testing: Local Docker → VPS → Mainnet

This section walks you through a **staged validation path** — from running the bot in a fully
isolated local environment, to a live VPS deployment, to a final production-readiness checklist
before you commit real money on mainnet.

**Do not skip stages.** Every stage catches a different class of problem. Real capital losses
happen when people skip directly from "it builds" to "go live".

---

### 14.1 Stage 1 — Local Docker Scenario Testing (No Real Trades)

**Goal:** Confirm that all services start correctly, the pipeline fires, and the bot is definitely
not submitting real on-chain transactions.

#### What "simulated" means

When no real RPC client or wallet is wired, the execution engine logs `"simulated":true` for
every trade attempt. This is not a configuration flag — it is the default behavior of the
execution worker when RPC endpoints return errors or are unreachable. During local Docker testing
with placeholder credentials, every "execution" is a simulated no-op that writes a result to the
database but sends nothing to any blockchain.

You can verify this in the logs:

```json
{
  "level": "INFO",
  "msg": "execution_result",
  "token": "0xABC...",
  "chain": "eth",
  "simulated": true,
  "size_usd": 50.0
}
```

If you ever see `"simulated":false`, a real transaction was attempted. **Never run with real
wallet keys until you have completed all stages below.**

#### Step 1 — Use test/placeholder credentials

For Stage 1, fill `.env` with real DB password and Telegram credentials, but use **placeholder
values** for all wallet keys and RPC endpoints. The bot will start, ingest nothing real, and
all executions will be simulated.

```bash
# .env for Stage 1 (local Docker testing — no real trades possible)
SNIPER_DB_PASSWORD=localtest123

# Placeholder RPC — bot starts but no real market data flows in
ETH_RPC_1=https://mainnet.infura.io/v3/PLACEHOLDER
ETH_WS_1=wss://mainnet.infura.io/ws/v3/PLACEHOLDER

# Placeholder wallet — no real transactions can be signed
SNIPER_WALLET_ADDRESS=0x0000000000000000000000000000000000000001
SNIPER_WALLET_KEY=0000000000000000000000000000000000000000000000000000000000000001

# Telegram — use real values so you receive test alerts
SNIPER_TELEGRAM_BOT_TOKEN=YOUR_REAL_TOKEN
SNIPER_TELEGRAM_CHAT_ID=YOUR_REAL_CHAT_ID

PORT=8080
LOG_LEVEL=debug
```

#### Step 2 — Start the full Docker stack

```bash
docker compose up --build
```

Watch the output. You should see three services start in order: `db → migrate → bot`.

#### Step 3 — Verify service startup

Check each service passes its health gate:

```bash
# Bot health endpoint
curl http://localhost:8080/health
# Expected: {"status":"ok"}

# Database is up and migrations applied
docker compose logs migrate | tail -5
# Expected: "All N migrations applied successfully"

# Bot pipeline is running
docker compose logs bot | grep -E "orchestrator_ready|ingestion_started|solana"
# Expected: lines showing workers started
```

#### Step 4 — Verify pipeline fires (no real events needed)

The pipeline starts even with placeholder RPC credentials — it attempts to subscribe and will
log connection errors, but continues running. The key check is that all workers started:

```bash
docker compose logs bot | grep -E '"msg"' | head -30
```

Look for these messages (order may vary):

```
"orchestrator_ready"
"ingestion_worker_started"
"execution_worker_started"
"position_worker_started"
"telegram_dispatcher_started"
```

If any worker is missing from the logs, check the full logs for the error:

```bash
docker compose logs bot 2>&1 | grep -i error
```

#### Step 5 — Verify kill switch works

Send `/kill` from your Telegram chat to the bot. You should receive a confirmation reply within
a few seconds. Then check the logs:

```bash
docker compose logs bot | grep kill_switch
# Expected: {"msg":"kill_switch_activated","source":"telegram"}
```

Send `/resume` to re-enable. Confirm you get a reply.

#### Step 6 — Verify no real transactions were submitted

```bash
# Check the database for any execution_result events with simulated=false
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT COUNT(*) FROM events WHERE event_type='execution_result' AND payload->>'simulated'='false';"
# Expected: 0
```

If this returns > 0, you have a real RPC endpoint configured and the bot attempted real trades.
Stop immediately and fix your `.env`.

#### Stage 1 Pass Criteria

- [ ] All three Docker services start without error
- [ ] Health endpoint returns `{"status":"ok"}`
- [ ] All workers appear in logs as started
- [ ] `/kill` Telegram command works and shows in logs
- [ ] Zero rows with `simulated=false` in events table
- [ ] `docker compose down` stops all services cleanly

---

### 14.2 Stage 2 — VPS Staging Deployment (Still No Real Trades)

**Goal:** Replicate Stage 1 exactly on your VPS, confirm the same pass criteria hold, and verify
the network path from VPS → RPC endpoints works.

#### Step 1 — Set up VPS and deploy

Follow Section 9.2 steps 1–7. Use **the same `.env` from Stage 1** (with placeholder wallet keys
and placeholder RPC endpoints) on the VPS. The goal is to prove the deployment process works, not
to test live connectivity yet.

```bash
# On your LOCAL machine — copy the test .env to the VPS
scp .env root@YOUR_VPS_IP:/opt/crypto-sniping-bot/.env

# On the VPS — start with Docker (simpler for this stage)
cd /opt/crypto-sniping-bot
docker compose up --build -d
```

#### Step 2 — Repeat all Stage 1 checks remotely

```bash
# Health check from your local machine (replace with your VPS IP)
curl http://YOUR_VPS_IP:8080/health
# Expected: {"status":"ok"}

# Tail logs remotely
ssh root@YOUR_VPS_IP "docker compose -C /opt/crypto-sniping-bot logs -f bot"
```

Run all the same `docker compose logs` checks from Stage 1, but against the VPS.

#### Step 3 — Test RPC connectivity from VPS

Now test that your **real** RPC endpoints (QuickNode Singapore, Helius, etc.) are reachable from
the VPS. This does not start trading — it only checks network connectivity:

```bash
ssh root@YOUR_VPS_IP

# Test HTTP RPC
curl -s -X POST YOUR_REAL_ETH_RPC_1 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}' | python3 -m json.tool
# Expected: {"result": "0x..."}  (a block number in hex)

# Test Solana RPC (if using Solana)
curl -s -X POST YOUR_REAL_SOLANA_RPC_HTTP_1 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"getSlot","params":[]}' | python3 -m json.tool
# Expected: {"result": 12345678}  (a slot number)
```

If these fail, fix the RPC URLs or check your VPS firewall before proceeding.

#### Step 4 — Test Telegram latency

From the VPS, send a test message to your Telegram bot API to confirm the bot can reach Telegram:

```bash
ssh root@YOUR_VPS_IP
curl -s "https://api.telegram.org/botYOUR_TOKEN/sendMessage?chat_id=YOUR_CHAT_ID&text=VPS+connectivity+test"
# Expected: {"ok":true,...}
```

#### Step 5 — Measure RPC latency from VPS

Run latency tests to your actual RPC endpoints from the VPS to confirm you are getting the
low-latency numbers you expect:

```bash
# HTTP latency to your primary RPC (repeat 5x, look at avg)
for i in 1 2 3 4 5; do
  curl -o /dev/null -s -w "%{time_total}s\n" -X POST YOUR_REAL_ETH_RPC_1 \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
done
# Expected from Singapore VPS to QuickNode Singapore: ~0.003–0.010s
# Expected from Singapore VPS to Infura US-East: ~0.160–0.200s
```

These numbers tell you whether your VPS + RPC combination will be competitive.

#### Stage 2 Pass Criteria

- [ ] All Stage 1 checks pass on the VPS
- [ ] Health endpoint accessible remotely
- [ ] Real RPC endpoints respond correctly from VPS
- [ ] Telegram API reachable from VPS
- [ ] RPC latency within acceptable range for your strategy
- [ ] Solana RPC responds (if Solana is enabled)

---

### 14.3 Stage 3 — Real RPC, Still Placeholder Wallet (Live Data, No Trades)

**Goal:** Switch to your real RPC credentials on the VPS so market data flows in, but keep a
placeholder wallet so no real transactions can be signed. This lets you see the full pipeline
processing real market events without any financial risk.

#### Step 1 — Update .env with real RPC endpoints, keep placeholder wallet

```bash
# On the VPS, edit .env
nano /opt/crypto-sniping-bot/.env
```

Change RPC endpoints to your real values. Leave wallet keys as placeholders:

```bash
# REAL RPC endpoints now
ETH_RPC_1=https://YOUR_REAL_QUICKNODE.quiknode.pro/YOUR_KEY/
ETH_WS_1=wss://YOUR_REAL_QUICKNODE.quiknode.pro/YOUR_KEY/
SOLANA_RPC_HTTP_1=https://mainnet.helius-rpc.com/?api-key=YOUR_REAL_HELIUS_KEY
SOLANA_WS_1=wss://mainnet.helius-rpc.com/?api-key=YOUR_REAL_HELIUS_KEY

# STILL placeholder wallet — cannot sign real transactions
SNIPER_WALLET_ADDRESS=0x0000000000000000000000000000000000000001
SNIPER_WALLET_KEY=0000000000000000000000000000000000000000000000000000000000000001
```

#### Step 2 — Restart and observe live market data

```bash
docker compose down && docker compose up --build -d
docker compose logs -f bot
```

You should now see real market events being ingested and processed through the pipeline:

```json
{"msg":"market_data_ingested","chain":"eth","pair":"0xABC...","price_usd":0.00042}
{"msg":"data_quality_pass","token":"0xABC...","score":0.87}
{"msg":"edge_detected","token":"0xABC...","edge_type":"NEW_LAUNCH_EDGE"}
{"msg":"execution_result","token":"0xABC...","simulated":true,"reason":"wallet_not_configured"}
```

The pipeline runs fully — Data Quality → Features → Edge → Probability → Selection → Capital →
Execution — but execution always produces `simulated:true` because the placeholder wallet cannot
sign a valid transaction.

#### Step 3 — Observe for 1–4 hours

Let the bot run with real market data and a placeholder wallet for at least 1 hour (ideally
4 hours across different times of day). During this time:

- Verify the pipeline processes events without errors or panics
- Confirm Telegram alerts fire for detected opportunities
- Check that `simulated:true` appears on every execution result
- Monitor resource usage: `docker stats` — the bot should use < 200 MB RAM steadily

```bash
# Count simulated vs real executions (all should be simulated)
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT payload->>'simulated' AS simulated, COUNT(*) FROM events
   WHERE event_type='execution_result' GROUP BY 1;"
# Expected: only "true" rows, zero "false" rows
```

#### Stage 3 Pass Criteria

- [ ] Real market events flow through the full pipeline
- [ ] Telegram alerts fire for edge detections
- [ ] Zero `simulated=false` executions
- [ ] No panics or unrecoverable errors in logs over 1+ hours
- [ ] Memory and CPU usage stable (no leak)
- [ ] `/kill` and `/resume` Telegram commands still work correctly

---

### 14.4 Stage 4 — Pre-Mainnet Production Readiness Checklist

Complete **every item** in this checklist before setting a real wallet key. This checklist is
not optional. Each item prevents a specific class of capital loss.

#### 4.1 — Environment Variables

```bash
# Run this script to check for any remaining placeholder values
grep -E 'PLACEHOLDER|YOUR_KEY|YOUR_TOKEN|0000000000000000' /opt/crypto-sniping-bot/.env
# Expected output: EMPTY (no lines printed)
```

Manually verify each critical variable:

- [ ] `SNIPER_DB_PASSWORD` — a strong, unique password (not "localtest123")
- [ ] `ETH_RPC_1` — real QuickNode/Alchemy URL responding correctly (tested in Stage 2)
- [ ] `ETH_WS_1` — real WebSocket URL (same provider as `ETH_RPC_1`)
- [ ] `SNIPER_TELEGRAM_BOT_TOKEN` — your real bot token (you received alerts in Stage 3)
- [ ] `SNIPER_TELEGRAM_CHAT_ID` — your real chat ID
- [ ] `SOLANA_RPC_HTTP_1` — real Helius/QuickNode URL (if Solana enabled)
- [ ] `SOLANA_WS_1` — real WebSocket URL (if Solana enabled)

#### 4.2 — Risk Limits in config/pipeline.yaml

Open `config/pipeline.yaml` and confirm these values are set conservatively for your first
live run. Do not go live with the default maximums:

```yaml
capital:
  # Start with a small fixed entry size — you can raise this after 50+ profitable trades
  fixed_entry_size_usd: 10.0 # Recommended: 10–25 USD for first week

  # Total portfolio exposure limit — never exceed what you can afford to lose entirely
  max_total_exposure_usd: 100.0 # Recommended: 2–5x your entry size for first week

  # Concurrent positions — start with 1, increase only after validating single-position behavior
  max_concurrent_positions: 1 # Recommended: 1 for first week

position:
  # Stop-loss — do not widen this on your first run (default is -5%, tight for meme tokens)
  sl_bps: 500 # 5% max loss per trade (default — do not increase for your first week)

  # Max hold time — keep short for meme tokens
  max_hold_seconds: 300 # 5 minutes maximum (raise only after validating strategy)
```

- [ ] `fixed_entry_size_usd` ≤ 25 USD for the first week
- [ ] `max_total_exposure_usd` ≤ 5× your entry size
- [ ] `max_concurrent_positions` = 1 for the first week
- [ ] `sl_bps` not wider than 500 (5% — the default for meme tokens)
- [ ] `max_hold_seconds` kept at 300 or below for the first week

#### 4.3 — Kill Switch Functional

This must work before any real money is involved. Test it one more time now:

```bash
# 1. Send /kill from Telegram
# 2. Verify log entry appears within 5 seconds:
docker compose logs bot | grep kill_switch
# Expected: {"msg":"kill_switch_activated"}

# 3. Verify database records it:
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT COUNT(*) FROM events WHERE event_type='kill_switch_activated';"
# Expected: >= 1

# 4. Send /resume and verify trading resumes
```

- [ ] `/kill` activates and appears in both logs and database
- [ ] `/resume` works and restores trading state
- [ ] Automatic drawdown kill switch threshold is configured

#### 4.4 — Database State Is Clean

Before going live, ensure the database has no leftover test data from earlier stages:

```bash
# Check for stuck positions from simulated runs
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT COUNT(*) FROM positions WHERE status NOT IN ('closed','exited');"
# If > 0, review and close them manually or reset the database

# Verify strategy version is initialized
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT id, version, created_at FROM strategy_versions ORDER BY created_at DESC LIMIT 1;"
# Expected: 1 row with a valid version number
```

Option to reset the database for a clean slate before going live:

```bash
# WARNING: This deletes ALL data including position history and learning records
docker compose down -v   # removes database volume
docker compose up --build -d  # recreates fresh database with migrations
```

- [ ] No stuck open positions from test runs
- [ ] Strategy version initialized in database
- [ ] Migration count matches expected (`make migrate-up` output shows all 17 applied)

#### 4.5 — Wallet Security

- [ ] Real wallet key is in `.env` only — never in any YAML, README, or committed file
- [ ] `.gitignore` includes `.env` — verify with `git check-ignore -v .env` (should print a match)
- [ ] VPS `.env` file permissions are `600`: `ls -la /opt/crypto-sniping-bot/.env` → `-rw-------`
- [ ] Solana keypair file permissions are `600`: `ls -la /etc/sniper/keys/solana-wallet-1.json` (if Solana)
- [ ] Wallet is funded with **only the amount you are willing to lose completely**
- [ ] The wallet used for trading has **no other assets** (no NFTs, no other tokens, no staking)

Set file permissions:

```bash
chmod 600 /opt/crypto-sniping-bot/.env
chmod 600 /etc/sniper/keys/solana-wallet-1.json  # if Solana enabled
```

#### 4.6 — RPC Redundancy

- [ ] At least 2 HTTP RPC endpoints configured per chain (`ETH_RPC_1` + `ETH_RPC_2`)
- [ ] WebSocket endpoint configured (`ETH_WS_1`)
- [ ] RPC circuit breaker is enabled (it is by default — verify in `config/pipeline.yaml` under `rpc`)
- [ ] RPC latency from VPS tested and acceptable (done in Stage 2 Step 5)

#### 4.7 — Telegram Alerts Working End-to-End

During Stage 3 you should have received real Telegram alerts. Confirm:

- [ ] You received at least one opportunity alert during Stage 3 observation period
- [ ] Alert messages contain chain name, token address, and edge score
- [ ] `/status` command returns current mode and position count
- [ ] `/pnl` command returns a response (even if it shows `0 trades today`)

#### 4.8 — Server Stability

- [ ] VPS has been running for > 24 hours without restart (Stage 3 observation)
- [ ] Memory usage is stable: `docker stats` shows bot container < 300 MB
- [ ] CPU usage is normal: < 20% during typical operation
- [ ] Disk space has > 5 GB free: `df -h /` — database grows over time
- [ ] System time is synchronized: `timedatectl` shows `System clock synchronized: yes`

```bash
# Check all of the above at once:
ssh root@YOUR_VPS_IP "
echo '=== Memory ===' && docker stats --no-stream --format 'table {{.Name}}\t{{.MemUsage}}'
echo '=== Disk ===' && df -h /
echo '=== Time sync ===' && timedatectl | grep synchronized
echo '=== Uptime ===' && uptime
"
```

---

### 14.5 Stage 5 — Shadow Run: Real Wallet, Zero Capital (Optional but Recommended)

**Goal:** Before depositing trading capital, verify that the wallet key works for signing without
any funds in the wallet. The bot will attempt to submit transactions, they will fail with
`insufficient funds`, but you confirm the signing path works correctly.

#### Step 1 — Add your real wallet key with zero balance

Update `.env` with your real wallet private key. **Do not deposit any ETH/BNB/SOL yet.**

```bash
# In /opt/crypto-sniping-bot/.env:
SNIPER_WALLET_ADDRESS=0xYourRealWalletAddress
SNIPER_WALLET_KEY=yourRealPrivateKeyHere
```

```bash
docker compose down && docker compose up --build -d
```

#### Step 2 — Observe signing behavior

With a real key but empty wallet, you should see logs like:

```json
{
  "msg": "execution_attempted",
  "chain": "eth",
  "simulated": false,
  "tx_hash": null,
  "error": "insufficient funds for gas * price + value"
}
```

This confirms:

- `simulated:false` — the bot tried to submit a real transaction (key is wired correctly)
- `error` contains `insufficient funds` — transaction failed at the chain level, not at the bot level
- No funds were spent (the transaction was rejected before broadcast or at broadcast)

- [ ] Logs show `simulated:false` (signing path works)
- [ ] Error is `insufficient funds` (not a signing error or key format error)
- [ ] No unexpected errors or panics

#### Step 3 — Back to placeholder wallet if not ready

If you are not proceeding to mainnet immediately, swap back to a placeholder wallet key. Do not
leave a real private key in `.env` on a server you are not actively monitoring.

---

### 14.6 Stage 6 — Mainnet Go-Live: Canary Funding

**Goal:** Fund the wallet with a small canary amount and run live for 48–72 hours before
increasing capital. This is your final validation with skin in the game.

> ⚠️ **Last warning:** Only proceed if all Stages 1–4 passed and you have completed the Stage 4
> checklist. Capital loss at this stage is real. Start with the minimum amount you are comfortable
> losing entirely.

#### Step 1 — Fund the wallet with canary capital

Transfer the minimum canary amount to your trading wallet:

| Chain  | Canary Amount | What this covers              |
| ------ | ------------- | ----------------------------- |
| ETH    | 0.01–0.02 ETH | 1–2 trades at $10 entry + gas |
| BSC    | 0.05–0.1 BNB  | 2–3 trades at $10 entry + gas |
| Solana | 0.05–0.1 SOL  | 2–3 trades + gas (lamports)   |

Keep `fixed_entry_size_usd: 10.0` in `config/pipeline.yaml`. You should have enough for 1–2 trades
before running out of capital, which limits maximum loss during canary validation.

#### Step 2 — Confirm wallet is funded and ready

```bash
# Verify wallet shows funded in logs at startup
docker compose down && docker compose up --build -d
docker compose logs bot | grep -E '"wallet"|"balance"'
```

#### Step 3 — Monitor the first 24 hours intensively

For the first 24 hours with real capital, check every 2–4 hours:

```bash
# Real-time log tail (run this in a persistent tmux/screen session)
ssh root@YOUR_VPS_IP "cd /opt/crypto-sniping-bot && docker compose logs -f bot"
```

Check Telegram for:

- Trade execution alerts (first real trade will appear here)
- Any error alerts
- PnL summary (send `/pnl` every few hours)

Check the database:

```bash
# Count real trades vs simulated
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT payload->>'simulated', COUNT(*), SUM((payload->>'size_usd')::float) AS total_usd
   FROM events WHERE event_type='execution_result' GROUP BY 1;"

# Check current open positions
docker compose exec db psql -U sniper -d sniper -c \
  "SELECT token_address, chain, entry_price_usd, size_usd, status, created_at
   FROM positions WHERE status='open' ORDER BY created_at DESC;"
```

#### Step 4 — After 48–72 hours: evaluate and decide

After the canary period, evaluate:

| Metric                 | Acceptable for scaling up  | Flag for review                  |
| ---------------------- | -------------------------- | -------------------------------- |
| Win rate               | > 40%                      | < 25% — pause and check edge     |
| Average trade return   | Positive or small negative | > -10% average — stop loss issue |
| Execution success rate | > 90% of attempts succeed  | < 70% — RPC or gas issue         |
| Kill switch triggers   | 0 auto-triggers            | Any auto-trigger — investigate   |
| Bot uptime             | 100%                       | Any crash — fix before scaling   |

**If metrics are acceptable:** Gradually increase `fixed_entry_size_usd` and
`max_total_exposure_usd` — no more than doubling per week. Never increase both at the same time.

**If any metric flags:** Stop new trades (`/kill`), investigate root cause in logs and database,
do not increase capital until resolved.

#### Step 5 — Scaling capital responsibly

```yaml
# Week 1 canary (do not change until metrics pass)
fixed_entry_size_usd: 10.0
max_total_exposure_usd: 50.0
max_concurrent_positions: 1

# Week 2 (only if week 1 metrics passed)
fixed_entry_size_usd: 25.0
max_total_exposure_usd: 150.0
max_concurrent_positions: 2

# Week 3+ (gradual increase based on actual performance)
# Rule: never increase total exposure by more than 2x per week
```

To apply config changes without downtime:

```bash
# Edit config/pipeline.yaml
nano /opt/crypto-sniping-bot/config/pipeline.yaml

# Restart bot (the DB and positions are preserved — only the bot process restarts)
docker compose restart bot

# Verify new config loaded
docker compose logs bot | grep '"msg":"Config loaded"'
```

---

### 14.7 Summary: Stage-by-Stage Gate Table

| Stage | Environment    | Wallet Key  | RPC Endpoints | Capital   | Gate to Pass Before Next Stage               |
| ----- | -------------- | ----------- | ------------- | --------- | -------------------------------------------- |
| 1     | Local Docker   | Placeholder | Placeholder   | None      | All services start, kill switch works        |
| 2     | VPS Docker     | Placeholder | Placeholder   | None      | Same as Stage 1, from remote VPS             |
| 3     | VPS Docker     | Placeholder | **Real**      | None      | Real events flow, zero `simulated=false`     |
| 4     | Checklist      | —           | —             | —         | Every item in Section 14.4 checked off       |
| 5     | VPS (optional) | **Real**    | Real          | None      | `simulated=false` logs, `insufficient funds` |
| 6     | VPS Mainnet    | **Real**    | Real          | 0.01 ETH  | 48–72h canary — metrics pass                 |
| 7+    | VPS Mainnet    | **Real**    | Real          | Scaled up | Gradual scaling, never 2× in one step        |

> **Remember:** The bot's profit formula is `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality`. Rushing to mainnet before validating execution and data quality
> sets two of those factors to near-zero — and the product of any factor near zero is near zero.

---

## 15. Optional Add-ons — Skip Until Profitable

These integrations are **disabled by default** and not required to run the bot. Enable them only
after you have confirmed positive PnL in live canary trading. Each one adds real cost — buying
them before your edge is proven wastes money without improving results.

> **Pricing verified:** May 2026. All prices USD/month unless noted.

| Provider                      | Phase    | Monthly Cost                                                                   | What you unlock                                                  | Skip until...                                       |
| ----------------------------- | -------- | ------------------------------------------------------------------------------ | ---------------------------------------------------------------- | --------------------------------------------------- |
| **BirdEye**                   | Phase 9  | Free (30K CUs, 1 rps) → **$39 Lite** → **$99 Starter**                         | LP-lock score enrichment, copy-trade signal, token security data | You want richer DQ signal beyond Helius DAS         |
| **Jito**                      | Phase 11 | **$0** (tip-only, no subscription)                                             | MEV sandwich protection, atomic bundle execution                 | You are losing trades to front-runners / sandwiches |
| **Solana gRPC (Yellowstone)** | Phase 11 | **~$125 deposit** PAYG ($0.08/GB + $10/M calls for RPC; streaming at $0.08/GB) | Sub-slot ingestion latency via Yellowstone gRPC stream           | You are latency-racing other bots for the same pool |

---

### 15.1 BirdEye (Phase 9)

BirdEye Data Services (BDS) provides enriched on-chain token data — security scores,
LP-lock status, holder concentration, and copy-trade wallet tracking. The bot uses it
in Phase 9 via the `BIRDEYE_API_KEY` env var (Section 3.6).

**Current plans (live as of May 2026, from [docs.birdeye.so/docs/pricing](https://docs.birdeye.so/docs/pricing)):**

| Plan            | Price/mo | CUs included | RPS | Overage per 1M CUs | WebSocket       |
| --------------- | -------- | ------------ | --- | ------------------ | --------------- |
| Standard (Free) | $0       | 30,000       | 1   | Not available      | ❌              |
| Lite            | $39      | 1,500,000    | 15  | $23                | ❌              |
| Starter         | $99      | 5,000,000    | 15  | $19.9              | ❌              |
| Premium         | $199     | 15,000,000   | 50  | $9.9               | ✅ (500 conns)  |
| Business        | $499     | 60,000,000   | 100 | $6.9               | ✅ (2000 conns) |

**When to upgrade:**

- Free tier (30K CUs) is exhausted in minutes during active trading — if you see `429` from
  BirdEye, jump to **Lite ($39)** first.
- Starter ($99) is the first plan with enough headroom for continuous trading (5M CUs/month).
- Premium ($199) adds WebSocket feeds — useful only if you build real-time DQ signal later.

**Env var:** `BIRDEYE_API_KEY` — see Section 3.6 for how to obtain it.

---

### 15.2 Jito Bundles (Phase 11)

Jito is a Solana block-engine service that lets you submit **atomic bundles** (up to 5 txs,
all-or-nothing) and get MEV sandwich protection. The service itself has **no subscription fee** —
you only pay a tip to validators per bundle.

**How the cost model works (from [docs.jito.wtf/lowlatencytxnsend](https://docs.jito.wtf/lowlatencytxnsend/)):**

| Item                              | Cost                                                                                            |
| --------------------------------- | ----------------------------------------------------------------------------------------------- |
| Subscription                      | $0 — completely free to use                                                                     |
| Minimum tip per bundle            | 1,000 lamports (~$0.00014 at current SOL price)                                                 |
| Typical tip (50th percentile)     | ~0.00001 SOL per bundle                                                                         |
| Competitive tip (95th percentile) | ~0.0014 SOL per bundle                                                                          |
| Default rate limit                | **1 req/sec/IP/region** (free)                                                                  |
| Custom rate limit                 | Request via [Jito Discord](https://discord.com/channels/938287290806042626/1336799632898134037) |

**Important notes:**

- Jito `sendTransaction` acts as a direct proxy to Solana's RPC — same signature, MEV protection
  included automatically.
- The `jitodontfront` account trick (add to any instruction) blocks your tx from being sandwiched.
- Use **Singapore** endpoint (`singapore.mainnet.block-engine.jito.wtf`) for lowest latency
  from Jakarta/Southeast Asia.
- The codebase already has Jito support wired in Phase 11 — enable via `JITO_BUNDLE_URL` and
  `JITO_TIP_ACCOUNT` env vars (Section 3.7). Both are read via `os.Getenv` — never put them in YAML.

**When to enable:** Only when you observe on-chain evidence of your swaps being sandwiched
(buy tx appears immediately before yours at a worse price). Paying tips on every trade before
that point raises your cost basis without benefit.

---

### 15.3 Solana gRPC / Yellowstone (Phase 11)

Yellowstone is Triton One's gRPC streaming interface for Solana. Instead of WebSocket
`logsSubscribe` (current default), it delivers raw slot updates and filtered account/tx streams
with sub-slot latency — critical when you are racing other sniping bots for the same new pool.

**Current pricing (live as of May 2026, from [triton.one/pricing](https://triton.one/pricing)):**

| Model                 | Cost                                                       | Includes                          |
| --------------------- | ---------------------------------------------------------- | --------------------------------- |
| PAYG (Shared)         | **$125 minimum deposit** (valid 12 months, non-refundable) | Standard RPC + gRPC streaming     |
| Standard RPC calls    | $0.08/GB bandwidth + $10/million calls                     | getTransaction, getBlock, etc.    |
| Yellowstone streaming | $0.08/GB bandwidth                                         | Filtered gRPC subscription stream |
| Dedicated node        | Custom quote                                               | Reserved capacity, lowest latency |

**How costs scale in practice:**

- A sniping bot at canary volume (~8 trades/day) costs roughly **$1–3/month** in bandwidth on PAYG.
- The $125 deposit covers ~3–6 months of active canary trading before needing a top-up.
- At high-frequency (100+ trades/day), budget $20–50/month in bandwidth.

**Env vars:** `SOLANA_GRPC_ENDPOINT` + `SOLANA_GRPC_TOKEN` — token is read via `os.Getenv` only
(see security constraints in Section 3.8). Never add the token to `config/chains.yaml`.

**When to enable:** When your WebSocket-based ingestion is consistently slower than competing bots
(you see tokens detected 1–3 slots late vs. the earliest on-chain trade). For initial canary
trading, WebSocket `logsSubscribe` via QuickNode or Helius is sufficient.

---

### 15.4 Decision Flowchart

```
Are you profitable on canary (48–72h positive PnL)?
│
├─ NO  → Keep running. Fix Data Quality or Edge first. Add-ons won't help.
│
└─ YES → Are trades being front-run / sandwiched?
         │
         ├─ YES → Enable Jito (Phase 11, $0 subscription + tips)
         │
         └─ NO  → Are you losing pools to faster bots?
                  │
                  ├─ YES → Upgrade to Yellowstone gRPC ($125 deposit)
                  │
                  └─ NO  → Is BirdEye DQ giving 429 errors?
                           │
                           ├─ YES → Upgrade to BirdEye Lite ($39/mo)
                           └─ NO  → You don't need any add-ons yet.
```

---

### 15.5 QuickNode & Helius — RPC Plan Upgrades

The two Solana RPC providers you already have free accounts with are also the two you'll upgrade
first when volume grows. This section tells you exactly which plan to pick and when.

> **Pricing verified:** May 2026, from [quicknode.com/pricing](https://www.quicknode.com/pricing)
> and [helius.dev/pricing](https://www.helius.dev/pricing).

#### QuickNode Plans

| Plan       | Monthly | Annual (-15%) | API Credits | Extra per 1M | RPS | Yellowstone gRPC |
| ---------- | ------- | ------------- | ----------- | ------------ | --- | ---------------- |
| **Free**   | **$0**  | $0            | 10M         | —            | 15  | ✅ Included      |
| **Build**  | **$49** | $42           | 80M         | $0.62        | 50  | ✅ Included      |
| Accelerate | $249    | $212          | 450M        | $0.55        | 125 | ✅ Included      |
| Scale      | $499    | $424          | 950M        | $0.53        | 250 | ✅ Included      |
| Business   | $999    | $849          | 2B          | $0.50        | 500 | ✅ Included      |

> **Key fact:** QuickNode Yellowstone gRPC is **included at no extra cost on all plans** — even
> the free tier. You do not need a Triton One account if you are already on QuickNode Build+.

**When to upgrade QuickNode:**

| Trigger                                           | Recommended action                          |
| ------------------------------------------------- | ------------------------------------------- |
| Free tier running low (>8M credits/month)         | Upgrade to **Build ($49/mo)**               |
| 1,800+ trades/day (credits exhausted)             | Upgrade to **Accelerate ($249/mo)**         |
| You want Yellowstone gRPC _now_ (not just canary) | **Build ($49/mo)** unlocks full gRPC access |
| 429 errors on QuickNode free-tier at 15 RPS cap   | Upgrade to **Build** (50 RPS)               |

#### Helius Plans

| Plan          | Monthly | Credits | Extra per 1M | sendTx/sec | RPS | Staked Conns | LaserStream gRPC |
| ------------- | ------- | ------- | ------------ | ---------- | --- | ------------ | ---------------- |
| **Free**      | **$0**  | 1M      | —            | **1**      | 10  | ❌           | ❌               |
| **Developer** | **$49** | 10M     | $5           | **5**      | 50  | ✅           | Devnet only      |
| Business      | $499    | 100M    | $5           | 50         | 200 | ✅           | ✅               |
| Professional  | $999    | 200M    | $5           | 100        | 500 | ✅           | ✅               |

> **Key fact:** The Helius free tier caps `sendTransaction` at **1 request/second**. That is the
> root cause of the 429 errors you see during active trading. Upgrading to **Developer ($49)**
> raises this limit to 5 tx/sec and adds **Staked Connections** (higher transaction landing rate
> during network congestion). The code already rotates to Helius automatically if QuickNode fails
> — so Helius is your fallback, not your primary `sendTransaction` path.

**When to upgrade Helius:**

| Trigger                                                     | Recommended action                      |
| ----------------------------------------------------------- | --------------------------------------- |
| Free 1M credits exhausted, using Helius as DAS fallback     | Upgrade to **Developer ($49/mo)**       |
| 429 on Helius free during QuickNode outage window           | Upgrade to **Developer** (5 sendTx/sec) |
| Consistent QuickNode outages, Helius is primary for >1h/day | Upgrade to **Developer**                |
| You want Helius LaserStream gRPC (alternative to Triton)    | Upgrade to **Business ($499/mo)**       |

#### Side-by-Side: First Paid Upgrade Comparison

| Feature                             | QuickNode Build ($49)       | Helius Developer ($49) | Winner for sniper bot |
| ----------------------------------- | --------------------------- | ---------------------- | --------------------- |
| Singapore / low-latency node        | ✅                          | ❌ (US-East only)      | **QuickNode**         |
| API credits                         | 80M                         | 10M                    | **QuickNode**         |
| sendTransaction/sec                 | ~50 RPS (no per-method cap) | 5/sec                  | **QuickNode**         |
| Yellowstone gRPC included           | ✅                          | ❌ (Devnet only)       | **QuickNode**         |
| Staked connections                  | ❌                          | ✅                     | **Helius**            |
| Sender (parallel tx to Jito+Helius) | ❌                          | ✅                     | **Helius**            |
| DAS / token metadata APIs           | ⭐⭐⭐⭐                    | ⭐⭐⭐⭐⭐             | **Helius**            |
| Cost per month                      | $49                         | $49                    | Tie                   |

**Recommendation — upgrade in this order:**

1. **QuickNode Build ($49/mo)** — first upgrade. Solves latency (Singapore), eliminates `sendTransaction`
   throttle on QuickNode, unlocks Yellowstone gRPC. Combined free headroom with Helius fallback
   carries you past 800 trades/day.

2. **Helius Developer ($49/mo)** — second upgrade, only after QuickNode is no longer enough OR
   you want Staked Connections to improve Helius's transaction landing rate as a fallback. The
   `Sender` service (included on all Helius plans) already parallelizes tx submission to both
   Helius and Jito endpoints in one call — useful when you enable Phase 11 Jito.

3. **Do not upgrade Helius to Business ($499) or QuickNode to Accelerate ($249)** until you are
   consistently running 200+ trades/day with proven positive expectancy. At that volume, one
   extra winning trade per day covers the cost.
