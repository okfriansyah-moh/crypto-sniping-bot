package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// VersionProposal is the module-level output of a parameter update cycle.
// It contains all data needed to construct a database.StrategyVersion; the
// caller (worker/orchestrator) is responsible for the database conversion.
type VersionProposal struct {
	StrategyVersionID string
	ConfigSnapshot    []byte
	CreatedAt         string
	Status            string // always "draft"
	ParentVersionID   string
}

// Updater creates bounded StrategyVersion candidates when evaluation data
// indicates a parameter family should be adjusted.
//
// Invariants (must never be violated):
//   - Exactly 1 parameter family updated per cycle.
//   - |Δparam| ≤ MaxDeltaPct (10%) per field per cycle.
//   - N ≥ MinSampleSize (30) required before any update.
type Updater struct {
	cfg         *config.LearningConfig
	cycleFamily int // round-robin index into cfg.Families
}

// NewUpdater returns an Updater with the given learning config.
func NewUpdater(cfg *config.LearningConfig) *Updater {
	return &Updater{cfg: cfg}
}

// paramMap is a mutable snapshot of tuneable parameters.
type paramMap map[string]float64

// Propose builds a new parameter map with a bounded delta applied to one family.
// Returns the new map and the family name used.
func (u *Updater) Propose(
	_ context.Context,
	activeSnapshot []byte,
	eval contracts.EvaluationDTO,
) (paramMap, string, error) {
	if len(u.cfg.Families) == 0 {
		return nil, "", fmt.Errorf("updater: no parameter families configured")
	}

	family := u.cfg.Families[u.cycleFamily%len(u.cfg.Families)]

	// Decode current config snapshot.
	current := make(paramMap)
	if err := json.Unmarshal(activeSnapshot, &current); err != nil {
		// If snapshot is not a flat map, fall back to empty.
		current = make(paramMap)
	}

	// Apply a gradient signal based on expectancy and FP rate.
	updated := applyBoundedDelta(current, family, eval, u.cfg.MaxDeltaPct)

	// Advance round-robin counter (deterministic: family selection is predictable).
	u.cycleFamily++

	return updated, family, nil
}

// ProposeVersion is a convenience wrapper: it calls Propose and then BuildStrategyVersion
// in one step, returning a ready-to-persist VersionProposal.
// activeSnapshot is the JSON config snapshot of the active strategy version.
// parentVersionID is the ID of the active strategy version (used for rollback linkage).
// minSampleSize is read from cfg — returns error when sample gate not met.
func (u *Updater) ProposeVersion(
	ctx context.Context,
	activeSnapshot []byte,
	parentVersionID string,
	eval contracts.EvaluationDTO,
	traceID string,
) (VersionProposal, error) {
	minSamples := u.cfg.MinSampleSize
	if minSamples <= 0 {
		minSamples = 30
	}
	if int(eval.SampleSize) < minSamples {
		return VersionProposal{}, fmt.Errorf("updater: insufficient samples: have %d need %d",
			eval.SampleSize, minSamples)
	}

	params, family, err := u.Propose(ctx, activeSnapshot, eval)
	if err != nil {
		return VersionProposal{}, err
	}

	return BuildStrategyVersion(params, parentVersionID, family, eval.SampleSize, traceID)
}


func NewVersionID(params paramMap) (string, error) {
	// Sort keys for determinism.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type kv struct {
		K string  `json:"k"`
		V float64 `json:"v"`
	}
	sorted := make([]kv, 0, len(keys))
	for _, k := range keys {
		sorted = append(sorted, kv{K: k, V: params[k]})
	}
	b, err := json.Marshal(sorted)
	if err != nil {
		return "", fmt.Errorf("new version id: %w", err)
	}
	return contracts.ContentIDFromString(string(b)), nil
}

// BuildStrategyVersion constructs a VersionProposal from a proposed param map.
func BuildStrategyVersion(
	params paramMap,
	parentVersionID string,
	family string,
	sampleSize int32,
	traceID string,
) (VersionProposal, error) {
	versionID, err := NewVersionID(params)
	if err != nil {
		return VersionProposal{}, err
	}

	snapshot, err := json.Marshal(params)
	if err != nil {
		return VersionProposal{}, fmt.Errorf("build strategy version: marshal: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_ = family     // stored in event payload
	_ = sampleSize // stored in event payload
	_ = traceID    // stored in event payload

	return VersionProposal{
		StrategyVersionID: versionID,
		ConfigSnapshot:    snapshot,
		CreatedAt:         now,
		Status:            "draft",
		ParentVersionID:   parentVersionID,
	}, nil
}

// applyBoundedDelta computes a directional delta for the given family
// and clamps it to maxDeltaPct.
func applyBoundedDelta(current paramMap, family string, eval contracts.EvaluationDTO, maxDeltaPct float64) paramMap {
	updated := make(paramMap, len(current))
	for k, v := range current {
		updated[k] = v
	}

	// Determine adjustment direction from expectancy trend.
	// Positive expectancy → relax thresholds slightly (more trades).
	// Negative / low expectancy → tighten.
	direction := 1.0
	if eval.Expectancy < 0 || float64(eval.FalsePositiveCount) > float64(eval.TruePositiveCount)*2 {
		direction = -1.0
	}

	// Select family-specific parameter keys.
	keys := familyKeys(family)
	for _, k := range keys {
		if v, ok := updated[k]; ok {
			delta := v * maxDeltaPct * direction
			updated[k] = v + clamp(delta, -math.Abs(v*maxDeltaPct), math.Abs(v*maxDeltaPct))
		}
	}

	return updated
}

func familyKeys(family string) []string {
	switch family {
	case "thresholds":
		return []string{"edge.min_velocity_score", "edge.min_liquidity_score", "validation.ev_threshold_bps"}
	case "weights":
		return []string{"features.momentum_weight", "features.liquidity_weight", "features.velocity_weight"}
	case "cohort_mults":
		return []string{"cohort.high.multiplier", "cohort.mid.multiplier", "cohort.low.multiplier"}
	default:
		return nil
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
