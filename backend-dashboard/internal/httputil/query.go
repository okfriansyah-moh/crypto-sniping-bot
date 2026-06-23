package httputil

import (
	"net/http"
	"strconv"

	"crypto-sniping-bot/shared/database"
)

// WindowHoursFromRequest parses window_hours (default 24, max 168).
func WindowHoursFromRequest(r *http.Request) int {
	raw := r.URL.Query().Get("window_hours")
	if raw == "" {
		return database.CapDQWindowHours(0)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return database.CapDQWindowHours(0)
	}
	return database.CapDQWindowHours(n)
}

// LimitFromRequest parses limit for activity feeds (defaultLimit when unset/invalid).
func LimitFromRequest(r *http.Request, defaultLimit int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return database.CapRecentEventsLimit(defaultLimit)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return database.CapRecentEventsLimit(defaultLimit)
	}
	return database.CapRecentEventsLimit(n)
}
