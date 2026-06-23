package configs

import (
	"net/http"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/configs — manifest metadata only, never secret values.
type Handler struct {
	configDir string
}

// NewHandler wires the config manifest vertical slice.
func NewHandler(configDir string) *Handler {
	return &Handler{configDir: configDir}
}

// ServeHTTP returns YAML manifest entries as a JSON array.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	entries, err := operator.BuildConfigManifest(h.configDir)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "config manifest unavailable")
		return
	}
	if entries == nil {
		entries = []contracts.ConfigManifestEntryDTO{}
	}

	httputil.WriteJSON(w, http.StatusOK, entries)
}
