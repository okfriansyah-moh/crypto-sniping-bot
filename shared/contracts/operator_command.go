package contracts

import (
	"encoding/json"
	"fmt"
)

// Operator command event bus type — dashboard-originated control plane (Phase 5).
const OperatorCommandEventType = "operator_command_event"

// Operator command types (dashboard POST /api/v1/commands).
const (
	CommandTypeMode       = "mode"
	CommandTypeKill       = "kill"
	CommandTypeResume     = "resume"
	CommandTypeForceClose = "force_close"
)

// OperatorCommandDTO is the payload for operator_command_event on the event bus.
// Emitted by backend-dashboard; consumed by sniper-bot operator command worker.
//
// CommandID = SHA256(canonical_json without command_id field)[:16] — content-addressable.
//
// Source file: contracts/operator_command.go
// Producer:    backend-dashboard (Phase 5+)
// Consumer:    sniper-bot/internal/workers (Phase 5+)
type OperatorCommandDTO struct {
	CommandID    string            `json:"command_id"`              // SHA256(content)[:16]
	CommandType  string            `json:"command_type"`            // mode | kill | resume | force_close
	IssuerID     string            `json:"issuer_id"`               // dashboard operator id (from auth)
	Args         map[string]string `json:"args,omitempty"`          // e.g. mode=EXPLORATION, token_address=...
	ConfirmToken string            `json:"confirm_token,omitempty"` // required for destructive commands
	Timestamp    string            `json:"timestamp"`               // ISO 8601 UTC
}

// NewOperatorCommandDTO builds a command with a content-addressable CommandID.
// timestamp must be ISO 8601 UTC (use event time, not wall clock, in replay paths).
func NewOperatorCommandDTO(commandType, issuerID, confirmToken, timestamp string, args map[string]string) (OperatorCommandDTO, error) {
	if commandType == "" {
		return OperatorCommandDTO{}, fmt.Errorf("operator command: command_type required")
	}
	if issuerID == "" {
		return OperatorCommandDTO{}, fmt.Errorf("operator command: issuer_id required")
	}
	if timestamp == "" {
		return OperatorCommandDTO{}, fmt.Errorf("operator command: timestamp required")
	}
	if args == nil {
		args = map[string]string{}
	}
	cmd := OperatorCommandDTO{
		CommandType:  commandType,
		IssuerID:     issuerID,
		Args:         args,
		ConfirmToken: confirmToken,
		Timestamp:    timestamp,
	}
	id, err := cmd.DeriveCommandID()
	if err != nil {
		return OperatorCommandDTO{}, err
	}
	cmd.CommandID = id
	return cmd, nil
}

// DeriveCommandID returns SHA256(canonical_json without command_id)[:16].
func (c OperatorCommandDTO) DeriveCommandID() (string, error) {
	clone := c
	clone.CommandID = ""
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("marshal operator command: %w", err)
	}
	return ContentID(payload), nil
}

// Validate checks required fields and allowed command types.
func (c OperatorCommandDTO) Validate() error {
	if c.CommandID == "" {
		return fmt.Errorf("operator command: command_id required")
	}
	if c.CommandType == "" {
		return fmt.Errorf("operator command: command_type required")
	}
	switch c.CommandType {
	case CommandTypeMode, CommandTypeKill, CommandTypeResume, CommandTypeForceClose:
	default:
		return fmt.Errorf("operator command: unknown command_type %q", c.CommandType)
	}
	if c.IssuerID == "" {
		return fmt.Errorf("operator command: issuer_id required")
	}
	if c.Timestamp == "" {
		return fmt.Errorf("operator command: timestamp required")
	}
	derived, err := c.DeriveCommandID()
	if err != nil {
		return err
	}
	if derived != c.CommandID {
		return fmt.Errorf("operator command: command_id mismatch (got %q, want %q)", c.CommandID, derived)
	}
	return nil
}

// IsDestructive reports whether the command requires a confirmation token before apply.
func (c OperatorCommandDTO) IsDestructive() bool {
	switch c.CommandType {
	case CommandTypeKill, CommandTypeResume, CommandTypeForceClose:
		return true
	default:
		return false
	}
}
