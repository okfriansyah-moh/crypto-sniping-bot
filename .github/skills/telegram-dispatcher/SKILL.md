---
name: telegram-dispatcher
type: skill
description: >
  Telegram integration via event bus only. Use when implementing or reviewing
  Telegram notifications, operator command handling, and the Telegram dispatcher
  service. Modules MUST NOT call Telegram directly — all messages flow through
  the event bus. Direct Telegram calls are a security and coupling violation.
---

# Telegram Dispatcher Skill

## Purpose

Enforce the event-bus-only Telegram integration pattern. The Telegram dispatcher is a
**dedicated consumer** of the event bus — it reads events and sends messages. No module
calls Telegram APIs directly.

**Architecture rule:** `docs/reference/architecture.md` § 2.5 — Telegram via Event Bus Only

---

## Rules

### NEVER Call Telegram Directly From Modules

```go
// ❌ FORBIDDEN in any module under internal/modules/
import "github.com/go-telegram-bot-api/telegram-bot-api/v5"

func (m *ExecutionModule) Process(alloc AllocationDTO) (ExecutionResultDTO, error) {
    // ... execute trade
    bot.Send(tgbotapi.NewMessage(chatID, "Trade executed!"))  // FORBIDDEN
    return result, nil
}
```

```go
// ✅ CORRECT: module emits event; dispatcher reads and sends
func (m *ExecutionModule) Process(alloc AllocationDTO) (ExecutionResultDTO, error) {
    // ... execute trade
    // Module returns DTO — orchestrator emits event to bus
    return result, nil  // dispatcher reads execution_event from bus
}
```

### Dispatcher Architecture

```
Event Bus (Postgres)
    │
    ├── execution_event        → "Trade executed: TOKEN +2.3%"
    ├── position_event         → "TP1 hit: TOKEN PnL: +22% | Size: 50%"
    ├── system_event           → "Mode changed: STRICT"
    ├── halted_event           → "⚠️ System halted: high rug rate"
    └── strategy_promotion_event → "Version promoted: v1.2 → v1.3"
         │
         ▼
    [Telegram Dispatcher Worker]
         │
         ▼
    Telegram Bot API
```

### Dispatcher Worker (Single Consumer)

```go
// internal/workers/run_telegram_dispatcher.go
func RunTelegramDispatcher(ctx context.Context, adapter database.Adapter, cfg TelegramConfig) error {
    bot, err := tgbotapi.NewBotAPI(cfg.BotToken)  // token from env, never config file
    if err != nil {
        return fmt.Errorf("init telegram bot: %w", err)
    }

    for {
        events, err := adapter.ClaimNextEvents(ctx, "telegram_dispatcher", telegramEventTypes, cfg.BatchSize)
        if err != nil {
            logger.Error("claim events failed", "err", err)
            time.Sleep(time.Duration(cfg.RetryDelayMs) * time.Millisecond)
            continue
        }

        for _, event := range events {
            msg := formatMessage(event, cfg)
            if err := sendWithRetry(ctx, bot, cfg.ChatID, msg, cfg); err != nil {
                logger.Warn("telegram send failed", "event_id", event.EventID, "err", err)
                // Non-fatal — notifications failing must NOT block the pipeline
            }
            adapter.MarkEventProcessed(ctx, "telegram_dispatcher", event.EventID)
        }
    }
}
```

### Operator Commands (Incoming — Security Critical)

```go
// Operator command handler via Telegram webhook/polling
// ALL commands are logged to event bus before executing
var allowedCommands = map[string]bool{
    "/status":    true,
    "/mode":      true,
    "/pnl":       true,
    "/positions": true,
    "/kill":      true,   // requires confirmation
    "/resume":    true,   // requires confirmation
    "/version":   true,
    "/rollback":  true,   // requires confirmation
}

func handleOperatorCommand(cmd string, args []string, operatorID int64) {
    // 1. Validate command is in allowlist
    if !allowedCommands[cmd] {
        bot.Send(newMessage(operatorID, "Unknown command"))
        return
    }

    // 2. Log command to event bus BEFORE executing
    adapter.EmitSystemEvent(ctx, SystemEvent{
        EventType: "operator_command",
        Details:   fmt.Sprintf("cmd=%s args=%v operator=%d", cmd, args, operatorID),
    })

    // 3. For destructive commands: require confirmation
    if isDestructive(cmd) {
        requireConfirmation(operatorID, cmd, args)
        return
    }

    // 4. Execute non-destructive read commands
    executeReadCommand(cmd, args, operatorID)
}

func isDestructive(cmd string) bool {
    return cmd == "/kill" || cmd == "/resume" || cmd == "/rollback"
}
```

### Destructive Command Confirmation Pattern

```go
// /kill, /resume, /rollback require confirmation
var pendingConfirmations = sync.Map{}  // operatorID → PendingCommand

func requireConfirmation(operatorID int64, cmd string, args []string) {
    token := generateConfirmToken()  // random token for this session
    pendingConfirmations.Store(operatorID, PendingCommand{cmd, args, token, time.Now()})
    bot.Send(newMessage(operatorID,
        fmt.Sprintf("Confirm %s with: /confirm %s (expires in 60s)", cmd, token)))
}

func handleConfirm(operatorID int64, token string) {
    pending, ok := pendingConfirmations.Load(operatorID)
    if !ok { bot.Send(newMessage(operatorID, "No pending command")); return }

    cmd := pending.(PendingCommand)
    if time.Since(cmd.IssuedAt) > 60*time.Second {
        pendingConfirmations.Delete(operatorID)
        bot.Send(newMessage(operatorID, "Confirmation expired"))
        return
    }
    if cmd.ConfirmToken != token {
        bot.Send(newMessage(operatorID, "Invalid confirmation token"))
        return
    }

    pendingConfirmations.Delete(operatorID)
    executeDestructiveCommand(cmd.Command, cmd.Args, operatorID)
}
```

### Security Requirements

```
1. Bot token MUST come from environment variable — never config.yaml, never source code
2. Operator auth: only pre-configured chatIDs may issue commands
3. All commands logged to event bus with operator ID
4. No remote code execution via Telegram — ever
5. Rate limit: max 30 messages/sec (Telegram API limit)
6. Telegram failures are non-fatal — NEVER block the trading pipeline
```

```go
// Bot token from env — never from config
token := os.Getenv("TELEGRAM_BOT_TOKEN")
if token == "" {
    return fmt.Errorf("TELEGRAM_BOT_TOKEN env var not set")
}

// Operator allowlist from config (chat IDs)
authorizedOps := cfg.AuthorizedOperatorIDs  // from config/telegram.yaml — NOT hardcoded
func isAuthorized(chatID int64) bool {
    for _, id := range authorizedOps { if id == chatID { return true } }
    return false
}
```

### Message Templates (Structured)

```go
// Consistent message format for observability
func formatTradeMessage(event TelegramEvent, result ExecutionResultDTO) string {
    pnlEmoji := "🟢"
    if result.PnLPct < 0 { pnlEmoji = "🔴" }
    return fmt.Sprintf(
        "%s Trade: %s\nPnL: %.1f%% | Size: $%.0f\nExit: %s | Age: %ds",
        pnlEmoji,
        shortAddress(result.TokenAddress),
        result.PnLPct,
        result.AllocatedUSD,
        result.ExitReason,
        result.DurationSec,
    )
}
```

### Anti-Patterns

```go
// ❌ Direct Telegram call from module
bot.Send(msg)  // in any file under internal/modules/ — FORBIDDEN

// ❌ Bot token in config file (tracked by git)
bot_token: "1234567:AABBcc..."  // config/telegram.yaml — FORBIDDEN

// ❌ Blocking pipeline on Telegram failure
if err := bot.Send(msg); err != nil { return err }  // FORBIDDEN — non-fatal

// ❌ Unauthenticated commands
bot.Handle("/kill", func(c tb.Context) error { killSystem() })  // FORBIDDEN

// ❌ No confirmation for destructive commands
executeKill()  // FORBIDDEN — must require /confirm token

// ✅ Correct
if err := sendWithRetry(ctx, bot, chatID, msg, cfg); err != nil {
    logger.Warn("telegram notification failed", "err", err)
    // Continue — trading is more important than notifications
}
```

---

## Checklist

```
[ ] No Telegram imports in internal/modules/ — any module
[ ] Bot token from TELEGRAM_BOT_TOKEN env var only
[ ] Dispatcher is a dedicated worker — single consumer group "telegram_dispatcher"
[ ] All operator commands logged to event bus before executing
[ ] Destructive commands (/kill, /resume, /rollback) require confirmation token
[ ] Confirmation tokens expire after 60 seconds
[ ] Authorized operators defined in config (not hardcoded)
[ ] Telegram failures are non-fatal — pipeline continues
[ ] Rate limiting applied (max 30/sec Telegram API limit)
[ ] No remote code execution via Telegram commands
[ ] Dispatcher uses SELECT ... FOR UPDATE SKIP LOCKED (event bus pattern)
```

---

## References

- Architecture: `docs/reference/architecture.md` § 2.5 (Telegram via Event Bus), § 4.4 (Operator Commands)
- Architecture context: `docs/archive/architecture-context/2_system_backbone.md`
- Roadmap: `docs/reference/implementation_roadmap.md` Phase 6
- Config: `shared/config/telegram.yaml` (authorized IDs, templates — NO bot token)
