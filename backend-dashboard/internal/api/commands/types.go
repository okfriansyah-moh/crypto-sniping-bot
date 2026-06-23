package commands

// SubmitRequest is the JSON body for POST /api/v1/commands.
type SubmitRequest struct {
	CommandType  string            `json:"command_type"`
	IssuerID     string            `json:"issuer_id"`
	Args         map[string]string `json:"args,omitempty"`
	ConfirmToken string            `json:"confirm_token,omitempty"` // unused on submit; confirm uses /commands/confirm
}

// ConfirmRequest is the JSON body for POST /api/v1/commands/confirm.
type ConfirmRequest struct {
	ConfirmToken string            `json:"confirm_token"`
	IssuerID     string            `json:"issuer_id"`
	CommandType  string            `json:"command_type"`
	Args         map[string]string `json:"args,omitempty"`
}

// Response is returned for accepted commands and confirmation challenges.
type Response struct {
	Status       string `json:"status"` // accepted | confirmation_required
	CommandID    string `json:"command_id,omitempty"`
	ConfirmToken string `json:"confirm_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
}
