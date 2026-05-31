// Package workers provides wiring-layer implementations of module interfaces.
// This file implements data_quality.CreatorProfileReader using the database adapter.
package workers

import (
	"context"

	"crypto-sniping-bot/database"
)

// adapterCreatorProfileReader implements data_quality.CreatorProfileReader
// by delegating to database.Adapter.GetCreatorProfile. It lives in the workers
// package (wiring layer) so that internal/modules/data_quality/ remains
// database-free per the module isolation invariant.
type adapterCreatorProfileReader struct {
	adapter database.Adapter
}

// newAdapterCreatorProfileReader returns a CreatorProfileReader backed by the adapter.
func newAdapterCreatorProfileReader(adapter database.Adapter) *adapterCreatorProfileReader {
	return &adapterCreatorProfileReader{adapter: adapter}
}

// GetCount returns the total number of tokens launched by creator on chain,
// as recorded in the creator_profiles table. Returns (0, false, nil) when the
// creator has no profile yet (cold start) — the caller must treat known==false
// as fail-closed and leave CreatorPrevTokenCountKnown unchanged.
func (r *adapterCreatorProfileReader) GetCount(ctx context.Context, chain, creator string) (count int32, known bool, err error) {
	profile, found, err := r.adapter.GetCreatorProfile(ctx, chain, creator)
	if err != nil {
		return 0, false, err
	}
	if !found {
		// Creator has no profile yet — cold start or not yet aggregated.
		return 0, false, nil
	}
	total := profile.TotalTokens
	if total <= 0 {
		// Profile row exists but count is zero — treat as unknown to avoid
		// incorrectly setting Known=true for a creator with no launches.
		return 0, false, nil
	}
	// Saturate int64 → int32: TotalTokens is expected to be tiny in practice
	// (typical serial launchers have < 500 tokens). Cap at MaxInt32 to avoid
	// overflow while preserving fail-closed semantics.
	if total > int64(^uint32(0)>>1) {
		total = int64(^uint32(0) >> 1)
	}
	return int32(total), true, nil
}
