// Package telegram — operator command handlers for /status /pnl /positions /kill /resume /version.
// All destructive commands (/kill, /resume) are logged with timestamp and require
// a confirmation token before execution.
// No remote code execution is permitted via these commands — ever.
package telegram

import (
	"context"
	"fmt"
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
)

// CommandRequest carries the parsed operator command.
type CommandRequest struct {
	Type      CommandType
	Args      []string
	IssuedAt  time.Time
	IssuerID  string // Telegram user ID (string form)
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
	case CmdStatus, CmdPnl, CmdPositions, CmdKill, CmdResume, CmdVersion:
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
	statusFn    func(ctx context.Context) (string, error)
	pnlFn       func(ctx context.Context) (string, error)
	positionsFn func(ctx context.Context) (string, error)
	killFn      func(ctx context.Context) error
	resumeFn    func(ctx context.Context) error
	versionFn   func(ctx context.Context) (string, error)
}

// HandlerOptions carries the injectable functions for the command handler.
type HandlerOptions struct {
	StatusFn    func(ctx context.Context) (string, error)
	PnlFn       func(ctx context.Context) (string, error)
	PositionsFn func(ctx context.Context) (string, error)
	KillFn      func(ctx context.Context) error
	ResumeFn    func(ctx context.Context) error
	VersionFn   func(ctx context.Context) (string, error)
}

// NewHandler creates a Handler with the provided function implementations.
func NewHandler(opts HandlerOptions) *Handler {
	return &Handler{
		statusFn:    opts.StatusFn,
		pnlFn:       opts.PnlFn,
		positionsFn: opts.PositionsFn,
		killFn:      opts.KillFn,
		resumeFn:    opts.ResumeFn,
		versionFn:   opts.VersionFn,
	}
}

// Handle dispatches the command and returns the reply text.
// Destructive commands (/kill, /resume) are logged with the issuer ID.
func (h *Handler) Handle(ctx context.Context, req *CommandRequest) (*CommandResult, error) {
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
	}

	return nil, fmt.Errorf("commands: unhandled command type: %q", req.Type)
}
