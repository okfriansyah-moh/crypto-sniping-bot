// Package telegram — operator command handlers for /status /pnl /positions /kill /resume /version /mode /help.
// All destructive commands (/kill, /resume, /mode) are logged with timestamp and require
// AllowedUserIDs to be configured before execution.
// No remote code execution is permitted via these commands — ever.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// CommandType identifies a Telegram operator command.
type CommandType string

const (
	CmdStatus    CommandType = "/status"
	CmdPnl       CommandType = "/pnl"
	CmdPositions CommandType = "/positions"
	CmdKill      CommandType = "/kill"
	CmdResume    CommandType = "/resume"
	CmdVersion   CommandType = "/version"
	CmdMode      CommandType = "/mode"
	CmdHelp      CommandType = "/help"
)

// isDestructive returns true for commands that modify system state.
func (c CommandType) isDestructive() bool {
	return c == CmdKill || c == CmdResume || c == CmdMode
}

// CommandRequest carries the parsed operator command.
type CommandRequest struct {
	Type     CommandType
	Args     []string
	IssuedAt time.Time
	IssuerID string // Telegram user ID (string form)
}

// CommandResult is the formatted text response to send back.
type CommandResult struct {
	Text        string
	Destructive bool // /kill, /resume — these are logged separately
}

// ParseCommand parses a raw Telegram message into a CommandRequest.
// Returns an error if the message is not a known command.
func ParseCommand(text string, issuerID string) (*CommandRequest, error) {
	text = strings.TrimSpace(text)
	parts := strings.Fields(text)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "/") {
		return nil, fmt.Errorf("commands: not a command: %q", text)
	}
	cmd := CommandType(strings.ToLower(parts[0]))
	switch cmd {
	case CmdStatus, CmdPnl, CmdPositions, CmdKill, CmdResume, CmdVersion, CmdMode, CmdHelp:
		return &CommandRequest{
			Type:     cmd,
			Args:     parts[1:],
			IssuedAt: time.Now().UTC(),
			IssuerID: issuerID,
		}, nil
	}
	return nil, fmt.Errorf("commands: unknown command: %q", parts[0])
}

// Handler processes operator commands.
// It is intentionally interface-driven so the orchestrator or app layer
// can inject real implementations without coupling to this package's internals.
type Handler struct {
	statusFn       func(ctx context.Context) (string, error)
	pnlFn          func(ctx context.Context) (string, error)
	positionsFn    func(ctx context.Context) (string, error)
	killFn         func(ctx context.Context) error
	resumeFn       func(ctx context.Context) error
	versionFn      func(ctx context.Context) (string, error)
	modeFn         func(ctx context.Context, mode string) (string, error)
	allowedUserIDs map[string]struct{} // nil means unconfigured
	logger         *slog.Logger
}

// HandlerOptions carries the injectable functions for the command handler.
type HandlerOptions struct {
	StatusFn    func(ctx context.Context) (string, error)
	PnlFn       func(ctx context.Context) (string, error)
	PositionsFn func(ctx context.Context) (string, error)
	KillFn      func(ctx context.Context) error
	ResumeFn    func(ctx context.Context) error
	VersionFn   func(ctx context.Context) (string, error)
	ModeFn      func(ctx context.Context, mode string) (string, error)

	// AllowedUserIDs is the set of Telegram user IDs permitted to issue commands.
	// When non-empty, any issuer NOT in the list is rejected for ALL commands.
	// When empty (unconfigured), destructive commands (/kill, /resume) are always
	// rejected; read-only commands are allowed but emit a security warning.
	// Set this in production to restrict access to known operator IDs.
	AllowedUserIDs []string

	// Logger is used to emit security warnings. Falls back to slog.Default().
	Logger *slog.Logger
}

// NewHandler creates a Handler with the provided function implementations.
func NewHandler(opts HandlerOptions) *Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		statusFn:       opts.StatusFn,
		pnlFn:          opts.PnlFn,
		positionsFn:    opts.PositionsFn,
		killFn:         opts.KillFn,
		resumeFn:       opts.ResumeFn,
		versionFn:      opts.VersionFn,
		modeFn:         opts.ModeFn,
		allowedUserIDs: allowedSet(opts.AllowedUserIDs),
		logger:         logger,
	}
}

// allowedSet builds a fast-lookup set from a slice of user IDs.
func allowedSet(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			m[id] = struct{}{}
		}
	}
	return m
}

// Handle dispatches the command and returns the reply text.
// Destructive commands (/kill, /resume) are logged with the issuer ID.
// Authorization rules:
//   - Empty IssuerID is always rejected.
//   - If AllowedUserIDs is configured, any unlisted issuer is rejected for ALL commands.
//   - If AllowedUserIDs is unconfigured (empty), destructive commands are always rejected.
func (h *Handler) Handle(ctx context.Context, req *CommandRequest) (*CommandResult, error) {
	if req.IssuerID == "" {
		return nil, fmt.Errorf("commands: missing issuer id")
	}

	if h.allowedUserIDs != nil {
		// Allowlist configured: strict enforcement for all commands.
		if _, ok := h.allowedUserIDs[req.IssuerID]; !ok {
			return nil, fmt.Errorf("commands: unauthorized issuer: %q", req.IssuerID)
		}
	} else if req.Type.isDestructive() {
		// Unconfigured allowlist: fail-closed for destructive commands.
		return nil, fmt.Errorf("commands: destructive command %q rejected: AllowedUserIDs not configured", req.Type)
	} else {
		// Unconfigured allowlist: allow read-only commands but emit a security warning.
		// Production deployments MUST configure AllowedUserIDs to restrict access.
		h.logger.Warn("telegram_command_unauthenticated",
			"command", req.Type,
			"issuer_id", req.IssuerID,
			"note", "AllowedUserIDs not configured; set allowed_user_ids in config to restrict access",
		)
	}

	switch req.Type {
	case CmdStatus:
		if h.statusFn == nil {
			return &CommandResult{Text: "status: not configured"}, nil
		}
		text, err := h.statusFn(ctx)
		if err != nil {
			return nil, fmt.Errorf("commands: status: %w", err)
		}
		return &CommandResult{Text: text}, nil

	case CmdPnl:
		if h.pnlFn == nil {
			return &CommandResult{Text: "pnl: not configured"}, nil
		}
		text, err := h.pnlFn(ctx)
		if err != nil {
			return nil, fmt.Errorf("commands: pnl: %w", err)
		}
		return &CommandResult{Text: text}, nil

	case CmdPositions:
		if h.positionsFn == nil {
			return &CommandResult{Text: "positions: not configured"}, nil
		}
		text, err := h.positionsFn(ctx)
		if err != nil {
			return nil, fmt.Errorf("commands: positions: %w", err)
		}
		return &CommandResult{Text: text}, nil

	case CmdKill:
		if h.killFn == nil {
			return &CommandResult{Text: "kill: not configured", Destructive: true}, nil
		}
		if err := h.killFn(ctx); err != nil {
			return nil, fmt.Errorf("commands: kill: %w", err)
		}
		return &CommandResult{
			Text:        fmt.Sprintf("🛑 Kill switch activated by %s at %s", req.IssuerID, req.IssuedAt.Format(time.RFC3339)),
			Destructive: true,
		}, nil

	case CmdResume:
		if h.resumeFn == nil {
			return &CommandResult{Text: "resume: not configured", Destructive: true}, nil
		}
		if err := h.resumeFn(ctx); err != nil {
			return nil, fmt.Errorf("commands: resume: %w", err)
		}
		return &CommandResult{
			Text:        fmt.Sprintf("▶️ Trading resumed by %s at %s", req.IssuerID, req.IssuedAt.Format(time.RFC3339)),
			Destructive: true,
		}, nil

	case CmdVersion:
		if h.versionFn == nil {
			return &CommandResult{Text: "version: not configured"}, nil
		}
		text, err := h.versionFn(ctx)
		if err != nil {
			return nil, fmt.Errorf("commands: version: %w", err)
		}
		return &CommandResult{Text: text}, nil

	case CmdMode:
		if len(req.Args) == 0 {
			return &CommandResult{
				Text:        "Usage: /mode <strict|balanced|explore>",
				Destructive: true,
			}, nil
		}
		modeArg := strings.ToLower(req.Args[0])
		// Normalize alias: "explore" → "EXPLORATION"
		switch modeArg {
		case "strict":
			modeArg = "STRICT"
		case "balanced":
			modeArg = "BALANCED"
		case "explore", "exploration":
			modeArg = "EXPLORATION"
		default:
			return &CommandResult{
				Text:        fmt.Sprintf("❌ Unknown mode %q — valid values: strict, balanced, explore", req.Args[0]),
				Destructive: true,
			}, nil
		}
		if h.modeFn == nil {
			return &CommandResult{Text: "mode: not configured", Destructive: true}, nil
		}
		text, err := h.modeFn(ctx, modeArg)
		if err != nil {
			return nil, fmt.Errorf("commands: mode: %w", err)
		}
		return &CommandResult{Text: text, Destructive: true}, nil

	case CmdHelp:
		return &CommandResult{Text: helpText()}, nil
	}

	return nil, fmt.Errorf("commands: unhandled command type: %q", req.Type)
}

// helpText returns a static listing of all available operator commands.
func helpText() string {
	return "<b>Available Commands</b>\n\n" +
		"<b>📊 Read-only</b>\n" +
		"/status — System mode, drawdown, positions, exposure, strategy\n" +
		"/pnl — 24h realized drawdown and open exposure summary\n" +
		"/positions — List all open positions with size and entry price\n" +
		"/version — Active strategy version ID and status\n\n" +
		"<b>⚙️ Operational</b>\n" +
		"/mode strict — Switch to STRICT mode (conservative thresholds)\n" +
		"/mode balanced — Switch to BALANCED mode (default)\n" +
		"/mode explore — Switch to EXPLORATION mode (relaxed thresholds)\n\n" +
		"<b>🔴 Destructive</b>\n" +
		"/kill — Activate kill switch (halts all trading immediately)\n" +
		"/resume — Clear kill switch (resumes trading)\n\n" +
		"<b>ℹ️ Help</b>\n" +
		"/help — Show this message\n\n" +
		"<i>Destructive commands require AllowedUserIDs to be configured.</i>"
}
