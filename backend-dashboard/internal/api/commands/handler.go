package commands

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

const maxCommandBodyBytes = 4096

var validModes = map[string]struct{}{
	"STRICT":            {},
	"BALANCED":          {},
	"EXPLORATION":       {},
	"VERY_EXPLORATION":  {},
}

// Handler serves POST /api/v1/commands (mode executes; destructive returns confirm challenge).
type Handler struct {
	db    database.Adapter
	dash  *config.DashboardConfig
	store *PendingStore
	now   func() time.Time
}

// ConfirmHandler serves POST /api/v1/commands/confirm (destructive execute after token).
type ConfirmHandler struct {
	db    database.Adapter
	dash  *config.DashboardConfig
	store *PendingStore
	now   func() time.Time
}

// NewHandler wires the command submission handler.
func NewHandler(db database.Adapter, dash *config.DashboardConfig, store *PendingStore) *Handler {
	return &Handler{db: db, dash: dash, store: store, now: time.Now}
}

// NewConfirmHandler wires the destructive confirmation handler.
func NewConfirmHandler(db database.Adapter, dash *config.DashboardConfig, store *PendingStore) *ConfirmHandler {
	return &ConfirmHandler{db: db, dash: dash, store: store, now: time.Now}
}

// ServeHTTP handles POST /api/v1/commands.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteMethodNotAllowed(w)
		return
	}
	var req SubmitRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}
	if err := authorizeIssuer(req.IssuerID); err != nil {
		httputil.WriteError(w, http.StatusForbidden, err.Error())
		return
	}
	commandType := strings.TrimSpace(req.CommandType)
	if err := validateCommandType(commandType); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	args := normalizeArgs(req.Args)
	if err := validateArgs(commandType, args); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := h.now()
	cmd := contracts.OperatorCommandDTO{CommandType: commandType}
	if cmd.IsDestructive() {
		token, expiresAt, err := h.store.Issue(commandType, req.IssuerID, args, now)
		if err != nil {
			httputil.WriteError(w, http.StatusInternalServerError, "confirmation unavailable")
			return
		}
		httputil.WriteJSON(w, http.StatusAccepted, Response{
			Status:       "confirmation_required",
			ConfirmToken: token,
			ExpiresAt:    expiresAt.UTC().Format(time.RFC3339Nano),
		})
		return
	}

	dto, err := buildCommand(commandType, req.IssuerID, "", args, now)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := emitOperatorCommand(r.Context(), h.db, dto); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "command emit failed")
		return
	}
	httputil.WriteJSON(w, http.StatusAccepted, Response{
		Status:    "accepted",
		CommandID: dto.CommandID,
	})
}

// ServeHTTP handles POST /api/v1/commands/confirm.
func (h *ConfirmHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httputil.WriteMethodNotAllowed(w)
		return
	}
	var req ConfirmRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		return
	}
	if err := authorizeIssuer(req.IssuerID); err != nil {
		httputil.WriteError(w, http.StatusForbidden, err.Error())
		return
	}
	commandType := strings.TrimSpace(req.CommandType)
	if err := validateCommandType(commandType); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	cmd := contracts.OperatorCommandDTO{CommandType: commandType}
	if !cmd.IsDestructive() {
		httputil.WriteError(w, http.StatusBadRequest, "command_type does not require confirmation")
		return
	}
	args := normalizeArgs(req.Args)
	if err := validateArgs(commandType, args); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := h.now()
	if err := h.store.Redeem(req.ConfirmToken, req.IssuerID, commandType, args, now); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	dto, err := buildCommand(commandType, req.IssuerID, strings.TrimSpace(req.ConfirmToken), args, now)
	if err != nil {
		httputil.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := emitOperatorCommand(r.Context(), h.db, dto); err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "command emit failed")
		return
	}
	httputil.WriteJSON(w, http.StatusAccepted, Response{
		Status:    "accepted",
		CommandID: dto.CommandID,
	})
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxCommandBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			httputil.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return err
		}
		httputil.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		httputil.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return err
	}
	return nil
}

func authorizeIssuer(issuerID string) error {
	issuerID = strings.TrimSpace(issuerID)
	if issuerID == "" {
		return errors.New("issuer_id required")
	}
	allowed := config.DashboardAllowedOperators()
	if len(allowed) == 0 {
		return errors.New("operator allowlist not configured")
	}
	for _, id := range allowed {
		if id == issuerID {
			return nil
		}
	}
	return errors.New("unauthorized issuer")
}

func validateCommandType(commandType string) error {
	switch commandType {
	case contracts.CommandTypeMode,
		contracts.CommandTypeKill,
		contracts.CommandTypeResume,
		contracts.CommandTypeForceClose:
		return nil
	default:
		return errors.New("unknown command_type")
	}
}

func validateArgs(commandType string, args map[string]string) error {
	switch commandType {
	case contracts.CommandTypeMode:
		mode := strings.ToUpper(strings.TrimSpace(args["mode"]))
		if mode == "" {
			return errors.New("args.mode required")
		}
		if _, ok := validModes[mode]; !ok {
			return errors.New("invalid mode")
		}
		args["mode"] = mode
	case contracts.CommandTypeForceClose:
		if strings.TrimSpace(args["position_id"]) == "" && strings.TrimSpace(args["token_address"]) == "" {
			return errors.New("args.position_id or args.token_address required")
		}
	}
	return nil
}

func normalizeArgs(args map[string]string) map[string]string {
	if args == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		out[k] = strings.TrimSpace(v)
	}
	return out
}
