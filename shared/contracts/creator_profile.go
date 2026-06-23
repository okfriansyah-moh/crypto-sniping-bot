package contracts

import "time"

// CreatorProfile is the immutable DTO returned by the database adapter
// for a wallet's cumulative token-launch history on a given chain.
// Produced by Adapter.GetCreatorProfile from the creator_profiles table.
// Populated by the creator_profile_aggregator worker (Task 8).
//
// Source file: contracts/creator_profile.go
// Producer:    internal/workers/creator_profile_aggregator
// Consumer:    internal/modules/data_quality (Task 9 serial-launcher check)
type CreatorProfile struct {
	Chain          string
	CreatorAddress string

	// TotalTokens is the count of market_data_events observed for this creator.
	// Incremented once per unique token launch event.
	TotalTokens int64

	// Outcome buckets — each is a non-negative count of tokens whose
	// resolved outcome falls into the corresponding category.
	RugTokens       int64
	MigratedTokens  int64
	GoldenGemTokens int64
	WinTokens       int64
	LossTokens      int64

	// Timestamps are in UTC.
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	LastUpdatedAt time.Time
}
