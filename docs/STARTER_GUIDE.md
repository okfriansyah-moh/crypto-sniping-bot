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
   - [Blockchain RPC Endpoints (Infura, Alchemy, QuickNode)](#31-blockchain-rpc-endpoints)
   - [Telegram Bot Token](#32-telegram-bot-token)
   - [Crypto Wallet Private Key](#33-crypto-wallet-private-key)
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

---

## 1. Understanding the Stack

Before touching any code, understand what this bot actually is and what it needs to run.

### What the bot does

This is a **DEX (Decentralized Exchange) sniping bot**. It monitors blockchain events in real time,
detects new token launches on Uniswap (Ethereum) and PancakeSwap (BSC), scores them through an
11-layer analysis pipeline, and executes buy/sell trades automatically.

### What the bot is made of

| Component            | Technology                                        | Why                                            |
| -------------------- | ------------------------------------------------- | ---------------------------------------------- |
| Application language | **Go 1.25**                                       | Fast, compiled, low memory usage               |
| Database             | **PostgreSQL**                                    | Stores all pipeline state, events, trades      |
| Blockchain access    | **RPC endpoints** (Infura, Alchemy, or QuickNode) | Reads blockchain data and submits transactions |
| Notifications        | **Telegram Bot API**                              | Sends alerts to your phone                     |
| Blockchain wallet    | **Ethereum-compatible private key**               | Signs and submits on-chain transactions        |

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

You need three types of credentials:

1. **RPC endpoint URLs** — to read from and write to the blockchain
2. **Telegram Bot Token + Chat ID** — to receive alerts on your phone
3. **Wallet private key** — to sign and submit trades

---

### 3.1 Blockchain RPC Endpoints

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

| Provider   | Price (Free)     | SEA Server?  | Recommended Use            |
| ---------- | ---------------- | ------------ | -------------------------- |
| Infura     | 100K req/day     | No (US/EU)   | Backup/testing only        |
| Alchemy    | 300M CU/month    | No (US/EU)   | Backup/testing only        |
| QuickNode  | 8M credits/month | ✅ Singapore | **Primary — best latency** |
| Public BSC | Unlimited        | ✅ Various   | BSC only, production risky |
| Ankr       | 500 req/sec free | ✅ Singapore | Alternative to QuickNode   |

**Recommended setup for Jakarta users:**

- Primary ETH: QuickNode Singapore
- Fallback ETH: Alchemy
- Primary BSC: QuickNode Singapore BSC or Public endpoints

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

> **Note:** The Telegram dispatcher is implemented and ready in the codebase
> (`internal/telegram/`). The wiring in `server.go` is being completed in Phase 6.
> You still need these values now so you don't have to hunt for them later.

---

### 3.3 Crypto Wallet Private Key

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
# TELEGRAM (for notifications — wiring in progress in Phase 6)
# =============================================================================

SNIPER_TELEGRAM_BOT_TOKEN=1234567890:YourTelegramBotTokenHere
SNIPER_TELEGRAM_CHAT_ID=123456789

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

#### `config/pipeline.yaml` — Core trading parameters

The most important settings for beginners:

```yaml
capital:
  # How much USD to spend per trade (start small when testing!)
  fixed_entry_size_usd: 50.0

  # Maximum total USD across all open positions
  max_total_exposure_usd: 500.0

  # Maximum number of concurrent open positions
  max_concurrent_positions: 1

position:
  # Take Profit 1: exit 50% of position when up 20%
  tp1_bps: 2000

  # Take Profit 2: exit remaining when up 50%
  tp2_bps: 5000

  # Stop Loss: exit all when down 15%
  sl_bps: 1500

  # Maximum time to hold a position (seconds) — 3600 = 1 hour
  max_hold_seconds: 3600
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
Running migration: 20260101000011_phase6_hardening.sql ... OK
All migrations applied successfully
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
`pipeline_runs`, `token_lifecycle`, `positions`, `learning_records`, and others.

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
{"time":"2026-04-26T10:00:00Z","level":"INFO","msg":"Migrations OK","count":11}
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

Docker lets you run the bot in an isolated container without installing Go on your machine directly.

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

### Step 2 — Create a Dockerfile

The repository does not include a Dockerfile yet. Create one in the project root:

```bash
cat > Dockerfile << 'EOF'
# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /sniper ./cmd/

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /sniper /app/sniper
COPY config/ /app/config/
EXPOSE 8080
ENTRYPOINT ["/app/sniper", "serve"]
EOF
```

### Step 3 — Create a docker-compose.yml

```bash
cat > docker-compose.yml << 'EOF'
version: "3.9"

services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: sniper
      POSTGRES_PASSWORD: ${SNIPER_DB_PASSWORD}
      POSTGRES_DB: sniper
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sniper"]
      interval: 5s
      timeout: 5s
      retries: 10

  migrate:
    build: .
    command: ["/app/sniper", "migrate", "up"]
    environment:
      SNIPER_DB_HOST: db
      SNIPER_DB_NAME: sniper
      SNIPER_DB_USER: sniper
      SNIPER_DB_PASSWORD: ${SNIPER_DB_PASSWORD}
      SNIPER_DB_SSL_MODE: disable
      CONFIG_PATH: /app/config/pipeline.yaml
    depends_on:
      db:
        condition: service_healthy
    restart: on-failure

  bot:
    build: .
    environment:
      SNIPER_DB_HOST: db
      SNIPER_DB_NAME: sniper
      SNIPER_DB_USER: sniper
      SNIPER_DB_PASSWORD: ${SNIPER_DB_PASSWORD}
      SNIPER_DB_SSL_MODE: disable
      ETH_RPC_1: ${ETH_RPC_1}
      ETH_RPC_2: ${ETH_RPC_2}
      ETH_WS_1: ${ETH_WS_1}
      BSC_RPC_1: ${BSC_RPC_1}
      BSC_RPC_2: ${BSC_RPC_2}
      BSC_WS_1: ${BSC_WS_1}
      SNIPER_WALLET_ADDRESS: ${SNIPER_WALLET_ADDRESS}
      SNIPER_WALLET_KEY: ${SNIPER_WALLET_KEY}
      SNIPER_TELEGRAM_BOT_TOKEN: ${SNIPER_TELEGRAM_BOT_TOKEN}
      SNIPER_TELEGRAM_CHAT_ID: ${SNIPER_TELEGRAM_CHAT_ID}
      PORT: 8080
      LOG_LEVEL: ${LOG_LEVEL:-info}
      CONFIG_PATH: /app/config/pipeline.yaml
    ports:
      - "8080:8080"
    depends_on:
      migrate:
        condition: service_completed_successfully
    restart: unless-stopped

volumes:
  pgdata:
EOF
```

### Step 4 — Run with Docker Compose

```bash
# Load your .env file (Docker Compose reads it automatically if named .env)
# Just ensure your .env file is in the project root.

# Build and start everything (database + migrations + bot)
docker compose up --build

# To run in the background (detached mode):
docker compose up --build -d

# View logs:
docker compose logs -f bot

# Stop everything:
docker compose down

# Stop and delete all data (including database):
docker compose down -v
```

### Step 5 — Verify

```bash
curl http://localhost:8080/health
# Should return: {"status":"ok"}
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

---

## 12. Reference — All Environment Variables

Complete reference of every environment variable the bot reads. Variables marked **Required** will
cause startup failure if not set.

| Variable                    | Required     | Default                | Description                                                   |
| --------------------------- | ------------ | ---------------------- | ------------------------------------------------------------- |
| `SNIPER_DB_PASSWORD`        | **Required** | —                      | PostgreSQL password for the `sniper` database user            |
| `SNIPER_DB_HOST`            | Optional     | `localhost`            | PostgreSQL hostname                                           |
| `SNIPER_DB_NAME`            | Optional     | `sniper`               | PostgreSQL database name                                      |
| `SNIPER_DB_USER`            | Optional     | `sniper`               | PostgreSQL username                                           |
| `SNIPER_DB_SSL_MODE`        | Optional     | `disable`              | PostgreSQL SSL mode (`disable`, `require`, `verify-full`)     |
| `ETH_RPC_1`                 | Required\*   | —                      | Ethereum HTTP RPC endpoint #1 (\*if ETH chain enabled)        |
| `ETH_RPC_2`                 | Optional     | —                      | Ethereum HTTP RPC endpoint #2 (fallback)                      |
| `ETH_WS_1`                  | Required\*   | —                      | Ethereum WebSocket endpoint (\*if ETH chain enabled)          |
| `BSC_RPC_1`                 | Required\*   | —                      | BSC HTTP RPC endpoint #1 (\*if BSC chain enabled)             |
| `BSC_RPC_2`                 | Optional     | —                      | BSC HTTP RPC endpoint #2 (fallback)                           |
| `BSC_WS_1`                  | Required\*   | —                      | BSC WebSocket endpoint (\*if BSC chain enabled)               |
| `SNIPER_WALLET_ADDRESS`     | **Required** | —                      | Primary trading wallet address (0x...)                        |
| `SNIPER_WALLET_KEY`         | **Required** | —                      | Primary wallet private key (64-char hex, no 0x prefix)        |
| `SNIPER_WALLET_0_ADDRESS`   | Optional     | —                      | Shard 0 wallet address (overrides single wallet for sharding) |
| `SNIPER_WALLET_0_KEY`       | Optional     | —                      | Shard 0 private key                                           |
| `SNIPER_WALLET_1_ADDRESS`   | Optional     | —                      | Shard 1 wallet address                                        |
| `SNIPER_WALLET_1_KEY`       | Optional     | —                      | Shard 1 private key                                           |
| `SNIPER_WALLET_2_ADDRESS`   | Optional     | —                      | Shard 2 wallet address                                        |
| `SNIPER_WALLET_2_KEY`       | Optional     | —                      | Shard 2 private key                                           |
| `SNIPER_WALLET_3_ADDRESS`   | Optional     | —                      | Shard 3 wallet address                                        |
| `SNIPER_WALLET_3_KEY`       | Optional     | —                      | Shard 3 private key                                           |
| `SNIPER_TELEGRAM_BOT_TOKEN` | Optional     | —                      | Telegram bot token (for notifications)                        |
| `SNIPER_TELEGRAM_CHAT_ID`   | Optional     | —                      | Telegram chat/group ID (for notifications)                    |
| `PORT`                      | Optional     | `8080`                 | HTTP server port for health check endpoint                    |
| `LOG_LEVEL`                 | Optional     | `info`                 | Log verbosity: `debug`, `info`, `warn`, `error`               |
| `CONFIG_PATH`               | Optional     | `config/pipeline.yaml` | Override path to main config file                             |

> **Security rule:** Never put private keys or the DB password in any YAML file or commit them
> to Git. These values must only ever exist as environment variables.

---
