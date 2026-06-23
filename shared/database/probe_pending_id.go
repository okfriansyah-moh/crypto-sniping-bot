package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ProbePendingID derives a content-addressable pending row ID from the source
// event and the hour boundary when the token becomes eligible for retry.
// Used for incomplete-probe background retries (one row per source event).
func ProbePendingID(sourceEventID string, dueAt time.Time) string {
	hourUnix := dueAt.Truncate(time.Hour).Unix()
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", sourceEventID, hourUnix)))
	return hex.EncodeToString(h[:])[:16]
}

// ProbePendingBudgetID derives a token-scoped pending row ID for budget
// deferrals so multiple rescan/fresh events for the same token collapse
// into one queue row per hour.
func ProbePendingBudgetID(chain, token string, dueAt time.Time) string {
	hourUnix := dueAt.Truncate(time.Hour).Unix()
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", chain, token, hourUnix)))
	return hex.EncodeToString(h[:])[:16]
}
