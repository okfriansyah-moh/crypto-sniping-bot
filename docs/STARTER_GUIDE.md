# Starter Guide — crypto-sniping-bot

> **Complete beginner-to-production guide.** This document covers everything you need to get this codebase running from scratch — including all external accounts, API keys, environment variables, and deployment options. Written for someone in Jakarta, Indonesia with no prior experience running this type of system.

---

> ⚠️ **RISK DISCLAIMER** — This is an automated trading bot. Trading cryptocurrency is high-risk and you can lose all of your money. Only use funds you can completely afford to lose. Always start in shadow mode (paper trading) before enabling live trading. The authors take no responsibility for financial losses.

---

## Table of Contents

1. [What Is This Bot?](#1-what-is-this-bot)
2. [What You Need — Overview](#2-what-you-need--overview)
3. [Part A: Installing Prerequisites](#3-part-a-installing-prerequisites)
   - [macOS](#31-macos)
   - [Linux (Ubuntu/Debian)](#32-linux-ubuntudebian)
   - [Windows](#33-windows)
4. [Part B: Obtaining API Keys and Credentials](#4-part-b-obtaining-api-keys-and-credentials)
   - [Ethereum RPC (Alchemy)](#41-ethereum-rpc--alchemy)
   - [BSC RPC (QuickNode)](#42-bsc-rpc--quicknode)
   - [Free Backup RPC Endpoints](#43-free-backup-rpc-endpoints)
   - [Telegram Bot (BotFather)](#44-telegram-bot--botfather)
5. [Part C: Crypto Wallet Setup](#5-part-c-crypto-wallet-setup)
6. [Part D: Cloning and Configuring the Project](#6-part-d-cloning-and-configuring-the-project)
   - [Clone the Repository](#61-clone-the-repository)
   - [Configure RPC Endpoints (chains.yaml)](#62-configure-rpc-endpoints-chainshyaml)
   - [Configure the Pipeline (pipeline.yaml)](#63-configure-the-pipeline-pipelineyaml)
7. [Complete Environment Variables Reference](#7-complete-environment-variables-reference)
8. [Setting Up PostgreSQL](#8-setting-up-postgresql)
9. [Running the Bot Locally](#9-running-the-bot-locally)
10. [Running with Docker](#10-running-with-docker)
11. [Running on VPS or Cloud (Recommended for Production)](#11-running-on-vps-or-cloud-recommended-for-production)
12. [Jakarta, Indonesia — Server Recommendations and Comparison](#12-jakarta-indonesia--server-recommendations-and-comparison)
13. [Security Checklist](#13-security-checklist)
14. [Troubleshooting](#14-troubleshooting)
15. [Common Commands Quick Reference](#15-common-commands-quick-reference)

---

## 1. What Is This Bot?

This is a **crypto sniping bot** — a program that:

1. **Watches** decentralized exchanges (DEX) like Uniswap (Ethereum) and PancakeSwap (BSC) in real time
2. **Detects** newly launched tokens that show strong early momentum
3. **Filters out scams** (rug pulls, honeypots, wash trading)
4. **Buys** when it detects a profitable opportunity
5. **Sells** automatically at take-profit or stop-loss targets
6. **Learns** from past trades to improve over time

It connects to the blockchain through **RPC endpoints** (think of these as web APIs that let you read/write blockchain data). It uses **PostgreSQL** as its database to store trade history and events. It sends you alerts via **Telegram**.

**Language**: Go (Golang) — a fast, compiled programming language

**Current supported chains:**

- Ethereum Mainnet (Uniswap V2 and V3)
- BNB Smart Chain / BSC (PancakeSwap V2)

---

## 2. What You Need — Overview

Before you start, you need to collect/create the following accounts and credentials. Keep them safe.

| What                  | Where to Get                   | Cost                | Purpose                        |
| --------------------- | ------------------------------ | ------------------- | ------------------------------ |
| **Go 1.25+**          | golang.org                     | Free                | Compile and run the bot        |
| **PostgreSQL 14+**    | postgresql.org                 | Free                | Bot's database                 |
| **Git**               | git-scm.com                    | Free                | Download code                  |
| **Alchemy account**   | alchemy.com                    | Free tier available | Ethereum RPC access            |
| **QuickNode account** | quicknode.com                  | Free tier available | BSC RPC access                 |
| **Telegram account**  | telegram.org                   | Free                | Bot alerts & control           |
| **Ethereum wallet**   | MetaMask or generated          | Free                | Wallet for trading             |
| **ETH/BNB for gas**   | Exchange (Tokocrypto, Indodax) | Real money          | Pay transaction fees           |
| **Trading capital**   | Exchange                       | Real money          | The actual funds to trade with |

---

## 3. Part A: Installing Prerequisites

### 3.1 macOS

**Step 1: Install Homebrew (package manager)**

Open Terminal (press `Command + Space`, type "Terminal", press Enter) and run:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

When asked for your password, type your Mac login password (you won't see characters appear — that's normal). Press Enter.

**Step 2: Install Go**

```bash
brew install go@1.25
```

Verify it installed:

```bash
go version
# Expected output: go version go1.25.0 darwin/arm64  (or amd64 on Intel Mac)
```

If `go` command is not found after install:

```bash
echo 'export PATH="/opt/homebrew/opt/go@1.25/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

**Step 3: Install PostgreSQL**

```bash
brew install postgresql@16
brew services start postgresql@16
```

Add to your PATH:

```bash
echo 'export PATH="/opt/homebrew/opt/postgresql@16/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Verify PostgreSQL is running:

```bash
psql --version
pg_isready
# Expected: /tmp:5432 - accepting connections
```

**Step 4: Install Git**

Git usually comes pre-installed on macOS. Check:

```bash
git --version
```

If not installed:

```bash
brew install git
```

---

### 3.2 Linux (Ubuntu/Debian)

Open your terminal and run these commands one by one.

**Step 1: Update system packages**

```bash
sudo apt-get update && sudo apt-get upgrade -y
```

**Step 2: Install Git and basic tools**

```bash
sudo apt-get install -y git curl wget build-essential
```

**Step 3: Install Go 1.25**

```bash
# Download Go (replace with latest 1.25.x from golang.org/dl if this URL changes)
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz

# Remove any old Go installation
sudo rm -rf /usr/local/go

# Extract to /usr/local
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz

# Add Go to your PATH permanently
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
# Expected: go version go1.25.0 linux/amd64
```

> **Note for ARM servers (e.g., AWS Graviton, Ampere):** Download `go1.25.0.linux-arm64.tar.gz` instead.

**Step 4: Install PostgreSQL 16**

```bash
# Install PostgreSQL
sudo apt-get install -y postgresql postgresql-contrib

# Start and enable on boot
sudo systemctl start postgresql
sudo systemctl enable postgresql

# Verify
sudo systemctl status postgresql
# Look for: Active: active (running)
```

---

### 3.3 Windows

> **Recommendation for Windows users**: For best compatibility, use **WSL 2** (Windows Subsystem for Linux) and follow the Linux instructions above. It runs a real Linux environment inside Windows.

**Option A: WSL 2 (Recommended)**

1. Open PowerShell as Administrator (right-click Start → "Windows PowerShell (Admin)")
2. Run:
   ```powershell
   wsl --install
   ```
3. Restart your computer
4. Open "Ubuntu" from Start menu
5. Follow the [Linux instructions](#32-linux-ubuntudebian) above

**Option B: Native Windows Install**

1. **Install Go:**
   - Go to https://go.dev/dl/
   - Download `go1.25.0.windows-amd64.msi`
   - Double-click the installer and follow the wizard
   - Open a new Command Prompt and verify: `go version`

2. **Install PostgreSQL:**
   - Go to https://www.postgresql.org/download/windows/
   - Download the installer for PostgreSQL 16
   - Run the installer. During install:
     - Remember the password you set for the `postgres` user
     - Default port: 5432 (leave as is)
   - After install, PostgreSQL runs as a Windows Service automatically

3. **Install Git:**
   - Go to https://git-scm.com/download/win
   - Download and run the installer
   - Use all default options

4. **Install Make (optional, for Makefile commands):**
   ```powershell
   # In PowerShell as Admin
   choco install make  # requires Chocolatey package manager
   ```

---

## 4. Part B: Obtaining API Keys and Credentials

This is the most important section. Without these, the bot cannot connect to the blockchain.

### 4.1 Ethereum RPC — Alchemy

**What is this?** An RPC (Remote Procedure Call) endpoint is a URL that lets your bot read blockchain data (new tokens, prices, pool events) and send transactions. Think of it like an API for the blockchain.

**Why Alchemy?** Reliable, generous free tier, low latency to Singapore/Indonesia.

**Step-by-step:**

1. Go to **https://www.alchemy.com**
2. Click **"Sign Up"** — create a free account (use your email)
3. After logging in, click **"Create new app"**
4. Fill in:
   - **Name:** `crypto-sniper-eth` (or anything you want)
   - **Chain:** Ethereum
   - **Network:** Mainnet
5. Click **"Create app"**
6. On your app dashboard, click **"API Key"** or **"View Key"**
7. You will see two important values:
   - **HTTPS endpoint:** `https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY`
   - **WebSocket endpoint:** `wss://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY`
8. **Copy both URLs** — you will need them for `config/chains.yaml`

> **Free tier limits:** 300 million Compute Units/month. For a bot running 24/7, this may run out. Consider upgrading to Growth plan (~$49/month) for production use.

**Getting WebSocket (WSS) endpoint:**

- On Alchemy dashboard, click your app
- Click "Networks" tab
- Select "Ethereum Mainnet"
- Under "WebSocket," you see `wss://eth-mainnet.g.alchemy.com/v2/YOUR_KEY`

---

### 4.2 BSC RPC — QuickNode

**What is this?** Same concept as above, but for Binance Smart Chain (BSC).

**Step-by-step:**

1. Go to **https://www.quicknode.com**
2. Click **"Sign Up"** — create a free account
3. Click **"Create an endpoint"**
4. Select:
   - **Chain:** BNB Smart Chain
   - **Network:** Mainnet
5. Click **"Continue"** and choose the **Free plan** (or Starter if you want better performance)
6. After creation, you see your endpoint:
   - **HTTP Provider:** `https://example-endpoint.bsc.quiknode.pro/YOUR_TOKEN/`
   - **WSS Provider:** `wss://example-endpoint.bsc.quiknode.pro/YOUR_TOKEN/`
7. **Copy both URLs**

> **Alternative for BSC:** You can also use public BSC RPC endpoints for testing (not recommended for production — they have rate limits and can be unreliable):
>
> - HTTP: `https://bsc-dataseed.binance.org/`
> - HTTP: `https://bsc-dataseed1.defibit.io/`

---

### 4.3 Free Backup RPC Endpoints

For production, you should have **at least 2 RPC endpoints per chain** (the bot will fall back to the second if the first fails). Here are free options:

**Ethereum backup options:**
| Provider | Sign-up URL | Free Tier |
|---------|------------|-----------|
| Infura | https://infura.io | 100,000 req/day |
| Ankr | https://www.ankr.com/rpc/ | 100 req/sec (no signup needed) |
| PublicNode | https://ethereum.publicnode.com | Unlimited (unstable) |

**Infura quick setup:**

1. Go to https://infura.io → Sign up
2. Create a new project
3. Copy HTTPS endpoint: `https://mainnet.infura.io/v3/YOUR_PROJECT_ID`
4. WebSocket: `wss://mainnet.infura.io/ws/v3/YOUR_PROJECT_ID`

**BSC backup options:**
| Provider | URL | Notes |
|---------|-----|-------|
| Ankr | `https://rpc.ankr.com/bsc` | Free, no signup |
| GetBlock | https://getblock.io | 40K req/day free |
| PublicNode | `https://bsc.publicnode.com` | Unstable |

---

### 4.4 Telegram Bot — BotFather

The bot sends you trade notifications and lets you control it via Telegram commands (`/status`, `/kill`, `/resume`, etc.).

**Step-by-step to create a Telegram bot:**

1. Open Telegram (mobile or desktop: https://web.telegram.org)
2. Search for **`@BotFather`** (official Telegram bot)
3. Start a chat with BotFather and type: `/newbot`
4. BotFather asks: **"Alright, a new bot. How are we going to call it?"**
   - Type a display name, e.g.: `My Sniper Bot`
5. BotFather asks: **"Now let's choose a username for your bot."**
   - Type a unique username ending in `bot`, e.g.: `mysniper_alert_bot`
6. BotFather replies with your **Bot Token**, which looks like:
   ```
   7123456789:AAGmXpQBJSHD8-CkN1234abcdefghijklmno
   ```
7. **Save this token** — this is your `TELEGRAM_BOT_TOKEN`

**Getting your Chat ID:**

You also need your Telegram Chat ID (so the bot knows where to send messages).

1. Search for `@userinfobot` in Telegram
2. Start a chat and send `/start`
3. It replies with your ID, like: `Your user ID: 987654321`
4. **Save this number** — this is your `TELEGRAM_CHAT_ID`

> **For group alerts:** Add your bot to a Telegram group, then send a message in the group, then visit:
> `https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getUpdates`
> Find the `"chat":{"id":...}` value — it will be a negative number (e.g., `-100123456789`).

**Activate your bot:**

Before the bot can send you messages, you must start a conversation:

1. Search for your bot username in Telegram
2. Press `Start` (or type `/start`)

---

## 5. Part C: Crypto Wallet Setup

> ⚠️ **CRITICAL SECURITY RULE:** Create a **dedicated new wallet ONLY for this bot**. Never use your main wallet that holds your savings. If the bot's private key is ever compromised, only this dedicated wallet is at risk.

**What you need:**

- A wallet address (public, like a bank account number)
- A private key (secret, like a PIN — never share this)

**Option A: Create a new wallet with MetaMask**

1. Install MetaMask browser extension: https://metamask.io
2. Click **"Create new wallet"**
3. Set a strong password
4. **Save your 12-word seed phrase** in a safe place (offline paper is best)
5. After wallet is created, click the account menu → **"Account details"** → **"Export Private Key"**
6. Enter your MetaMask password
7. Copy the private key (64 hex characters, e.g. `a3b2c1...`)
8. Your wallet address is shown at the top (starts with `0x`)

**Option B: Use the Go Ethereum tool (command line)**

```bash
# Install geth (Ethereum client) on Mac
brew install ethereum

# On Linux
sudo apt-get install ethereum

# Generate a new account
geth account new
# Enter a password when prompted
# It shows: Public address of the key: 0xYOURWALLETADDRESS
# Key file path is printed — get the private key from it
```

**Funding your wallet:**

You need ETH (for Ethereum trades) and/or BNB (for BSC trades) in your bot wallet:

1. **Buy ETH/BNB in Indonesia**: Use [Indodax](https://indodax.com), [Tokocrypto](https://tokocrypto.com), or [Pintu](https://pintu.co.id) — all support IDR
2. **Withdraw to your bot wallet address**
3. Start small — even $20–50 equivalent for initial testing

**Important**: The wallet also needs ETH/BNB for **gas fees** (blockchain transaction fees). Gas fees on Ethereum can be $5–50+ per transaction depending on network congestion. BSC is cheaper (~$0.10–0.50 per transaction).

---

## 6. Part D: Cloning and Configuring the Project

### 6.1 Clone the Repository

Open your terminal and run:

```bash
# Clone the repository
git clone <YOUR_REPO_URL> crypto-sniping-bot

# Enter the directory
cd crypto-sniping-bot
```

> Replace `<YOUR_REPO_URL>` with the actual Git repository URL.

### 6.2 Configure RPC Endpoints (chains.yaml)

Open `config/chains.yaml` in a text editor. You need to replace the placeholder values with your real RPC URLs.

**Find these lines:**

```yaml
chains:
  eth:
    rpc_endpoints:
      - "${ETH_RPC_1}"
      - "${ETH_RPC_2}"
    ws_endpoints:
      - "${ETH_WS_1}"
  bsc:
    rpc_endpoints:
      - "${BSC_RPC_1}"
      - "${BSC_RPC_2}"
    ws_endpoints:
      - "${BSC_WS_1}"
```

**Replace with your actual URLs** (example using Alchemy + Infura for ETH, QuickNode + Ankr for BSC):

```yaml
chains:
  eth:
    rpc_endpoints:
      - "https://eth-mainnet.g.alchemy.com/v2/YOUR_ALCHEMY_API_KEY"
      - "https://mainnet.infura.io/v3/YOUR_INFURA_PROJECT_ID"
    ws_endpoints:
      - "wss://eth-mainnet.g.alchemy.com/v2/YOUR_ALCHEMY_API_KEY"
  bsc:
    rpc_endpoints:
      - "https://example-endpoint.bsc.quiknode.pro/YOUR_QUICKNODE_TOKEN/"
      - "https://rpc.ankr.com/bsc"
    ws_endpoints:
      - "wss://example-endpoint.bsc.quiknode.pro/YOUR_QUICKNODE_TOKEN/"
```

> ⚠️ **Never commit your real API keys to Git.** Use `.gitignore` to exclude your local `chains.yaml` if it contains real keys, or use only environment variables.

### 6.3 Configure the Pipeline (pipeline.yaml)

Open `config/pipeline.yaml`. The key settings for getting started:

```yaml
# These are already set in the file — review and adjust as needed

capital:
  fixed_entry_size_usd: 50.0 # How much USD per trade (start small!)
  max_total_exposure_usd: 500.0 # Maximum total open exposure
  max_concurrent_positions: 1 # Keep at 1 until you understand the system

execution:
  eth_price_usd: 3500.0 # Update this to current ETH price (approximate is fine)
  mode: "shadow" # IMPORTANT: Use "shadow" for paper trading first!
```

> **Shadow mode** = the bot calculates trades and logs them but does NOT submit real transactions. Use this for at least 1–2 weeks before switching to `"live"` mode.

---

## 7. Complete Environment Variables Reference

These are ALL the environment variables the bot reads. Set them before running.

**How to set environment variables:**

- **Linux/macOS**: `export VARIABLE_NAME="value"` in your terminal, or add to `~/.bashrc` / `~/.zshrc`
- **Windows (CMD)**: `set VARIABLE_NAME=value`
- **Windows (PowerShell)**: `$env:VARIABLE_NAME = "value"`
- **Production/VPS**: Create a `.env` file (see [Section 9](#9-running-the-bot-locally)) or use systemd environment files

---

### Required Variables

These MUST be set or the bot will refuse to start:

| Variable             | Example                    | Description                                              |
| -------------------- | -------------------------- | -------------------------------------------------------- |
| `SNIPER_DB_PASSWORD` | `mysecretpass`             | PostgreSQL database password. Never put in config files. |
| `SNIPER_WALLET_KEY`  | `a3b2c1...` (64 hex chars) | Private key of your trading wallet. No `0x` prefix.      |

---

### Strongly Recommended Variables

| Variable                | Example             | Description                                       |
| ----------------------- | ------------------- | ------------------------------------------------- |
| `SNIPER_WALLET_ADDRESS` | `0xAbCd...`         | Your wallet's public address (EIP-55 checksummed) |
| `TELEGRAM_BOT_TOKEN`    | `7123456789:AAG...` | Bot token from BotFather                          |
| `TELEGRAM_CHAT_ID`      | `987654321`         | Your Telegram user/chat ID                        |

---

### Optional Override Variables

These override the values set in `config/pipeline.yaml`:

| Variable             | Default (from config)  | Description                                     |
| -------------------- | ---------------------- | ----------------------------------------------- |
| `SNIPER_DB_HOST`     | `localhost`            | Database host (useful for Docker/VPS)           |
| `SNIPER_DB_NAME`     | `sniper`               | Database name                                   |
| `SNIPER_DB_USER`     | `sniper`               | Database user                                   |
| `SNIPER_DB_SSL_MODE` | `disable`              | SSL mode: `disable`, `require`, `verify-full`   |
| `LOG_LEVEL`          | `info`                 | Logging level: `debug`, `info`, `warn`, `error` |
| `PORT`               | `8080`                 | HTTP health server port                         |
| `CONFIG_PATH`        | `config/pipeline.yaml` | Custom path to pipeline.yaml                    |

---

### Multi-Wallet Sharding (Advanced)

For production with multiple wallets (reduces nonce conflicts):

| Variable                  | Example          | Description               |
| ------------------------- | ---------------- | ------------------------- |
| `SNIPER_WALLET_0_ADDRESS` | `0xWallet0...`   | First wallet address      |
| `SNIPER_WALLET_0_KEY`     | `privatekey0...` | First wallet private key  |
| `SNIPER_WALLET_1_ADDRESS` | `0xWallet1...`   | Second wallet address     |
| `SNIPER_WALLET_1_KEY`     | `privatekey1...` | Second wallet private key |
| `SNIPER_WALLET_2_ADDRESS` | `0xWallet2...`   | Third wallet address      |
| `SNIPER_WALLET_2_KEY`     | `privatekey2...` | Third wallet private key  |
| `SNIPER_WALLET_3_ADDRESS` | `0xWallet3...`   | Fourth wallet address     |
| `SNIPER_WALLET_3_KEY`     | `privatekey3...` | Fourth wallet private key |

The bot shards trades across wallets using `hash(tokenAddress) % wallet_count`. The number of wallets is controlled by `execution.wallet_shard_count` in `pipeline.yaml` (default: 4).

---

### Complete .env File Template

Create a file named `.env` in the project root (this file is gitignored):

```bash
# ─── REQUIRED ──────────────────────────────────────────────────────────────────
SNIPER_DB_PASSWORD=change_this_to_strong_password

# ─── WALLET ────────────────────────────────────────────────────────────────────
# WARNING: NEVER share or commit your private key
SNIPER_WALLET_ADDRESS=0xYOUR_WALLET_ADDRESS_HERE
SNIPER_WALLET_KEY=YOUR_64_CHAR_PRIVATE_KEY_HERE_NO_0x_PREFIX

# ─── TELEGRAM (optional but recommended) ───────────────────────────────────────
TELEGRAM_BOT_TOKEN=1234567890:AABBccDDeeFFggHHiiJJkkLLmmNNooP
TELEGRAM_CHAT_ID=987654321

# ─── DATABASE OVERRIDES (optional) ─────────────────────────────────────────────
# Only needed if you change from defaults
# SNIPER_DB_HOST=localhost
# SNIPER_DB_NAME=sniper
# SNIPER_DB_USER=sniper
# SNIPER_DB_SSL_MODE=disable

# ─── LOGGING ────────────────────────────────────────────────────────────────────
LOG_LEVEL=info

# ─── MULTI-WALLET (optional, for sharding) ─────────────────────────────────────
# SNIPER_WALLET_0_ADDRESS=0xWallet0
# SNIPER_WALLET_0_KEY=privatekey0
# SNIPER_WALLET_1_ADDRESS=0xWallet1
# SNIPER_WALLET_1_KEY=privatekey1
# SNIPER_WALLET_2_ADDRESS=0xWallet2
# SNIPER_WALLET_2_KEY=privatekey2
# SNIPER_WALLET_3_ADDRESS=0xWallet3
# SNIPER_WALLET_3_KEY=privatekey3
```

---

## 8. Setting Up PostgreSQL

The bot needs a PostgreSQL database. Here is how to set it up.

### 8.1 Create the Database and User

**On macOS and Linux:**

```bash
# Connect to PostgreSQL as the superuser
sudo -u postgres psql    # Linux
# OR on macOS (Homebrew install, no sudo needed):
psql postgres

# Run these commands inside psql:
CREATE USER sniper WITH PASSWORD 'change_this_password';
CREATE DATABASE sniper OWNER sniper;
GRANT ALL PRIVILEGES ON DATABASE sniper TO sniper;
\q
```

> Replace `change_this_password` with a strong password. This same password goes in your `.env` file as `SNIPER_DB_PASSWORD`.

**On Windows (using pgAdmin or psql):**

1. Open pgAdmin (installed with PostgreSQL)
2. Right-click "Login/Group Roles" → Create → Login/Group Role
3. Set name: `sniper`, password: `change_this_password`
4. Under "Privileges" tab, enable "Can login"
5. Right-click "Databases" → Create → Database
6. Name: `sniper`, Owner: `sniper`

### 8.2 Run Database Migrations

Migrations create all the required tables. Run this once before the first start:

```bash
# Make sure you are in the project root directory
cd crypto-sniping-bot

# Load your environment variables
export SNIPER_DB_PASSWORD="change_this_password"

# Run all migrations
go run ./cmd migrate up
```

Expected output:

```
INFO migration applied migration=20260101000001_initial_schema.sql
INFO migration applied migration=20260101000002_add_claimed_at.sql
INFO migration applied migration=20260101000003_trading_tables.sql
...
INFO all migrations applied
```

---

## 9. Running the Bot Locally

### 9.1 Quick Start (Manual Environment Variables)

```bash
# Navigate to project root
cd crypto-sniping-bot

# Build the binary
go build -o bin/crypto-sniping-bot ./cmd/

# Set environment variables (replace with your real values)
export SNIPER_DB_PASSWORD="your_db_password"
export SNIPER_WALLET_ADDRESS="0xYourWalletAddress"
export SNIPER_WALLET_KEY="your64charprivatekey"
export TELEGRAM_BOT_TOKEN="your_telegram_bot_token"
export TELEGRAM_CHAT_ID="your_chat_id"

# Run the bot
./bin/crypto-sniping-bot serve
```

### 9.2 Using a .env File (Recommended)

Install `direnv` or use a simple shell function to load `.env`:

```bash
# Simple method: source the .env file
set -a; source .env; set +a

# Then run
go run ./cmd/ serve
```

Or use the Makefile:

```bash
make run
```

### 9.3 Verifying the Bot Is Running

When the bot starts successfully, you see logs like:

```json
{"time":"...","level":"INFO","msg":"orchestrator_ready","version_id":"v1-abc123"}
{"time":"...","level":"INFO","msg":"http_server_started","addr":":8080"}
```

**Health check:**

```bash
curl http://localhost:8080/health
# Expected: {"status":"ok"}
```

**Check logs for errors:**

```bash
# Run with debug logging to see everything
LOG_LEVEL=debug go run ./cmd/ serve
```

### 9.4 Running Tests

```bash
# Run all tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run with coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

---

## 10. Running with Docker

> Docker lets you package the bot and its dependencies into a container — making it easy to run anywhere consistently.

### 10.1 Install Docker

**macOS:**

```bash
brew install --cask docker
# Then open Docker Desktop from Applications
```

**Linux (Ubuntu):**

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
newgrp docker
docker --version
```

**Windows:** Download Docker Desktop from https://www.docker.com/products/docker-desktop

### 10.2 Create a Dockerfile

The project does not include a Dockerfile by default. Create one in the project root:

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
CMD ["/app/sniper", "serve"]
EOF
```

### 10.3 Create docker-compose.yml

```bash
cat > docker-compose.yml << 'EOF'
version: "3.9"

services:
  postgres:
    image: postgres:16-alpine
    restart: always
    environment:
      POSTGRES_USER: sniper
      POSTGRES_PASSWORD: ${SNIPER_DB_PASSWORD}
      POSTGRES_DB: sniper
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sniper"]
      interval: 10s
      timeout: 5s
      retries: 5

  sniper:
    build: .
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      - SNIPER_DB_HOST=postgres
      - SNIPER_DB_NAME=sniper
      - SNIPER_DB_USER=sniper
      - SNIPER_DB_PASSWORD=${SNIPER_DB_PASSWORD}
      - SNIPER_DB_SSL_MODE=disable
      - SNIPER_WALLET_ADDRESS=${SNIPER_WALLET_ADDRESS}
      - SNIPER_WALLET_KEY=${SNIPER_WALLET_KEY}
      - TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
      - TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID}
      - LOG_LEVEL=${LOG_LEVEL:-info}
    ports:
      - "8080:8080"

volumes:
  postgres_data:
EOF
```

### 10.4 Running with Docker Compose

```bash
# Make sure your .env file is set up first (Section 7)

# Run migrations first
docker-compose run --rm sniper /app/sniper migrate up

# Start everything
docker-compose up -d

# View logs
docker-compose logs -f sniper

# Stop
docker-compose down

# Stop and remove database data (WARNING: deletes all trade history)
docker-compose down -v
```

---

## 11. Running on VPS or Cloud (Recommended for Production)

Running on a VPS (Virtual Private Server) is recommended for production because:

- 24/7 uptime (your laptop can turn off)
- Faster internet / lower latency to blockchain RPC endpoints
- Better reliability

### 11.1 Setting Up a VPS (Ubuntu 22.04 LTS example)

**Step 1: SSH into your server**

```bash
ssh root@YOUR_SERVER_IP
```

If you set up an SSH key during VPS creation:

```bash
ssh -i ~/.ssh/id_rsa ubuntu@YOUR_SERVER_IP
```

**Step 2: Create a non-root user (security best practice)**

```bash
adduser sniper
usermod -aG sudo sniper
su - sniper
```

**Step 3: Install prerequisites**

Follow the [Linux installation instructions](#32-linux-ubuntudebian) above.

**Step 4: Clone and configure**

```bash
git clone <YOUR_REPO_URL> ~/crypto-sniping-bot
cd ~/crypto-sniping-bot
```

Edit your configs and create the `.env` file:

```bash
nano .env
# Paste your credentials, save with Ctrl+X, Y, Enter
```

**Step 5: Set up PostgreSQL**

```bash
sudo apt-get install -y postgresql postgresql-contrib
sudo systemctl start postgresql
sudo systemctl enable postgresql
sudo -u postgres psql -c "CREATE USER sniper WITH PASSWORD 'STRONG_PASSWORD_HERE';"
sudo -u postgres psql -c "CREATE DATABASE sniper OWNER sniper;"
```

Update your `.env` with the PostgreSQL password.

**Step 6: Run migrations**

```bash
set -a; source .env; set +a
go run ./cmd/ migrate up
```

**Step 7: Build the binary**

```bash
go build -o bin/sniper ./cmd/
```

### 11.2 Running as a systemd Service (Keeps Bot Running 24/7)

Create a systemd service file:

```bash
sudo nano /etc/systemd/system/sniper.service
```

Paste this content (adjust paths to match your setup):

```ini
[Unit]
Description=Crypto Sniping Bot
After=network.target postgresql.service
Requires=postgresql.service

[Service]
Type=simple
User=sniper
WorkingDirectory=/home/sniper/crypto-sniping-bot
EnvironmentFile=/home/sniper/crypto-sniping-bot/.env
ExecStart=/home/sniper/crypto-sniping-bot/bin/sniper serve
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=sniper

[Install]
WantedBy=multi-user.target
```

Save the file, then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable sniper
sudo systemctl start sniper
sudo systemctl status sniper
```

**View live logs:**

```bash
journalctl -u sniper -f
```

---

## 12. Jakarta, Indonesia — Server Recommendations and Comparison

This section helps you choose the best server option for running from Indonesia. Latency matters for a sniping bot — lower latency to the blockchain = faster trade execution.

### 12.1 Latency Context

From Jakarta, approximate latency to major data centers:
| Location | Latency from Jakarta | Notes |
|----------|---------------------|-------|
| Singapore | ~20–40ms | Closest major hub, best choice |
| Tokyo | ~60–80ms | Good alternative |
| US East (N. Virginia) | ~200–250ms | Too slow for sniping |
| EU (Frankfurt) | ~190–220ms | Too slow for sniping |
| Jakarta (IDCloudHost) | ~5–15ms | Local but poor RPC proximity |

> **Key insight:** Your bot should be as close as possible to the RPC endpoint servers. Most major RPC providers (Alchemy, Infura, QuickNode) have **Singapore edge nodes**, so **Singapore is the best region** for you.

### 12.2 VPS Provider Comparison (Singapore Region)

| Provider            | Plan               | Price/month (approx IDR) | CPU        | RAM | Storage      | Recommended for                                   |
| ------------------- | ------------------ | ------------------------ | ---------- | --- | ------------ | ------------------------------------------------- |
| **DigitalOcean**    | Basic Droplet 2GB  | ~Rp 90,000 ($6)          | 1 vCPU     | 2GB | 50GB SSD     | Best balance for beginners                        |
| **Vultr**           | Cloud Compute 2GB  | ~Rp 90,000 ($6)          | 1 vCPU     | 2GB | 55GB SSD     | Similar to DigitalOcean                           |
| **Linode (Akamai)** | Nanode 2GB         | ~Rp 105,000 ($7)         | 1 vCPU     | 2GB | 50GB SSD     | Good reliability                                  |
| **Hetzner Cloud**   | CX21               | ~Rp 75,000 ($4.50)       | 2 vCPU     | 4GB | 40GB SSD     | Best price but no SG datacenter (use Helsinki)    |
| **AWS (EC2)**       | t3.small Singapore | ~Rp 200,000+ ($13+)      | 2 vCPU     | 2GB | Separate EBS | Enterprise grade, more complex                    |
| **GCP (Compute)**   | e2-small Singapore | ~Rp 170,000+ ($11)       | 0.5–2 vCPU | 2GB | -            | Reliable, more complex billing                    |
| **IDCloudHost**     | VPS M              | ~Rp 75,000               | 2 vCPU     | 2GB | 20GB SSD     | Local data center, cheaper but higher RPC latency |
| **Domainesia**      | VPS Starter        | ~Rp 100,000              | 1 vCPU     | 1GB | 20GB SSD     | Budget option, limited performance                |

### 12.3 My Recommendation for Jakarta

**For beginners → Start with DigitalOcean Singapore**

1. Go to https://www.digitalocean.com
2. Sign up with email (they sometimes offer $200 credit for 60 days for new accounts — check for promo codes)
3. Create Droplet:
   - **Image:** Ubuntu 22.04 LTS
   - **Plan:** Basic → Regular → 2GB RAM / 1 vCPU (Rp ~90,000/month)
   - **Datacenter:** Singapore (SGP1)
   - **Authentication:** Add an SSH key (more secure than password)
4. Click "Create Droplet"

**Why DigitalOcean?**

- Simple UI, good documentation (in English and Indonesian tutorials available)
- Predictable flat-rate pricing (no surprise bills like AWS)
- Singapore datacenter has ~25ms latency from Jakarta
- Good support

**For serious production use → AWS Singapore (t3.medium)**

- More expensive but extremely reliable
- Has dedicated Singapore datacenter (ap-southeast-1)
- t3.medium: 2 vCPU, 4GB RAM ~$35/month
- Better when trading real money

### 12.4 Payment Methods from Indonesia

Most VPS providers accept **credit cards** and **PayPal**. From Indonesia:

- **Visa/Mastercard (credit or debit)**: Accepted everywhere
- **PayPal**: Accepted by DigitalOcean, Vultr, Linode
- **Virtual credit card** (Jenius, Flip.id): Works with most international providers
- **Gopay/OVO/Dana**: Generally NOT accepted directly; use Jenius or Flip for international payments

### 12.5 Internet Requirements

The bot is not bandwidth-heavy but needs:

- Stable connection (use a VPS, not your home internet, for 24/7 uptime)
- For local development: any broadband (Indihome 20Mbps+, Biznet, First Media are fine)

---

## 13. Security Checklist

Before running the bot with real money, verify all of these:

### Private Key Security

- [ ] Private key is only in the `.env` file, never in `config/pipeline.yaml`
- [ ] `.env` is listed in `.gitignore` (verify: `cat .gitignore | grep .env`)
- [ ] You have never committed `.env` to Git (`git log --all --full-history -- .env` shows nothing)
- [ ] Your bot wallet is a dedicated wallet — not your main savings wallet
- [ ] You have the seed phrase written down offline in a secure location
- [ ] Bot wallet only holds the amount you're willing to risk

### Server Security (VPS)

- [ ] SSH access uses SSH keys, not password
- [ ] Root login is disabled: edit `/etc/ssh/sshd_config`, set `PermitRootLogin no`
- [ ] UFW firewall is enabled: `sudo ufw enable && sudo ufw allow ssh && sudo ufw allow 8080`
- [ ] PostgreSQL is NOT exposed to the internet (only listens on localhost)
- [ ] Regular system updates: `sudo apt-get update && sudo apt-get upgrade`

### Bot Configuration

- [ ] Start with `mode: "shadow"` in pipeline.yaml — never start with live mode first
- [ ] `fixed_entry_size_usd` is set to a small amount (e.g., $20–50) for initial testing
- [ ] You understand what `max_total_exposure_usd` means and it is set conservatively

### Telegram

- [ ] Telegram bot token is not shared with anyone
- [ ] Only you have access to the chat where the bot sends messages

---

## 14. Troubleshooting

### "config not found" or "pipeline.yaml not found"

```
Error: pipeline.yaml not found; set CONFIG_PATH or run from project root
```

**Fix:** Make sure you are running the bot from the project root directory:

```bash
cd /path/to/crypto-sniping-bot
go run ./cmd/ serve
```

Or set `CONFIG_PATH`:

```bash
CONFIG_PATH=/absolute/path/to/config/pipeline.yaml go run ./cmd/ serve
```

---

### "connection refused" (PostgreSQL not connecting)

```
Error: db_connect_failed ... connect: connection refused
```

**Check if PostgreSQL is running:**

```bash
pg_isready         # macOS/Linux
# or
sudo systemctl status postgresql  # Linux
```

**Check credentials:**

```bash
psql -U sniper -d sniper -h localhost
# If this fails, verify SNIPER_DB_PASSWORD is set correctly
```

**Check if database exists:**

```bash
sudo -u postgres psql -c "\l"  # Lists all databases
```

---

### "no route to host" or RPC connection errors

```
WARN rpc_connect_failed ...
```

**Check your RPC URLs in chains.yaml are correct:**

```bash
# Test an Alchemy endpoint manually
curl -X POST "https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
```

You should get a response like `{"jsonrpc":"2.0","id":1,"result":"0x..."}`. If you get an error, your API key may be wrong.

---

### "wallet_private_key must not be set in config files"

```
Error: config: wallet_private_key must not be set in config files; use SNIPER_WALLET_KEY env var
```

**Fix:** Remove `wallet_private_key` from `config/pipeline.yaml` and instead set:

```bash
export SNIPER_WALLET_KEY="your_private_key_here"
```

---

### "SNIPER_DB_PASSWORD not set" or no password

The bot reads the database password only from the environment variable `SNIPER_DB_PASSWORD`. Make sure it is set:

```bash
echo $SNIPER_DB_PASSWORD  # Should print your password (not empty)
export SNIPER_DB_PASSWORD="your_password"
```

---

### Bot starts but no trades happening

1. Check `LOG_LEVEL=debug` to see what's happening
2. Verify RPC endpoints are returning data (check for `market_data_event` in logs)
3. Confirm `execution.mode` is `"shadow"` or `"live"` in pipeline.yaml
4. The bot only trades when it finds edges — on a slow market day, you may see no action for hours

---

### Out of memory on VPS

If the bot gets killed due to OOM:

- Increase VPS to 4GB RAM
- Or reduce `database.pool.max_open_conns` from 20 to 5 in pipeline.yaml

---

## 15. Common Commands Quick Reference

```bash
# ─── Build ──────────────────────────────────────────────────────────────────────
go build -o bin/sniper ./cmd/         # Build binary
make build                             # Same via Makefile

# ─── Run ────────────────────────────────────────────────────────────────────────
go run ./cmd/ serve                    # Run bot
go run ./cmd/ migrate up               # Run database migrations
go run ./cmd/ migrate down             # Rollback last migration

# ─── Test ───────────────────────────────────────────────────────────────────────
go test ./...                          # Run all tests
go test -v -race ./...                 # With race detection
go test ./internal/modules/edge/...    # Test specific module

# ─── Docker ─────────────────────────────────────────────────────────────────────
docker-compose up -d                   # Start all services
docker-compose logs -f sniper          # Watch bot logs
docker-compose down                    # Stop all services
docker-compose restart sniper          # Restart bot only

# ─── systemd (VPS) ──────────────────────────────────────────────────────────────
sudo systemctl start sniper            # Start bot service
sudo systemctl stop sniper             # Stop bot service
sudo systemctl restart sniper          # Restart
sudo systemctl status sniper           # Check status
journalctl -u sniper -f                # Watch live logs
journalctl -u sniper --since today     # Today's logs

# ─── Database ───────────────────────────────────────────────────────────────────
psql -U sniper -d sniper               # Connect to database
\dt                                    # List all tables (inside psql)
SELECT * FROM events ORDER BY created_at DESC LIMIT 10;  # Last 10 events

# ─── Health Check ───────────────────────────────────────────────────────────────
curl http://localhost:8080/health      # Check if bot HTTP server is up
```

---

## Appendix: Minimum Hardware Requirements

| Environment                      | CPU    | RAM | Storage | Notes                    |
| -------------------------------- | ------ | --- | ------- | ------------------------ |
| Local development                | 1 core | 2GB | 10GB    | For building and testing |
| Shadow mode (paper trading)      | 1 vCPU | 2GB | 20GB    | Watching but not trading |
| Production (live trading)        | 2 vCPU | 4GB | 40GB    | For real money operation |
| High-frequency (multiple chains) | 4 vCPU | 8GB | 80GB    | ETH + BSC simultaneously |

---

_Last updated: April 2026. For architecture details, see [`docs/architecture.md`](architecture.md). For DTO contracts, see [`docs/dto_contracts.md`](dto_contracts.md)._
