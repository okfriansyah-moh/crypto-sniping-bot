package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

const maxGateEvidenceBytes = 256 * 1024

type gateEvidenceFile struct {
	Timestamp           string `json:"timestamp"`
	DetectedMode        string `json:"detected_mode"`
	OperationalEvidence struct {
		TracesCompleted int64 `json:"traces_completed"`
	} `json:"operational_evidence"`
	ThroughputMetrics struct {
		WsolTokenAddressEmitted  int64       `json:"wsol_token_address_emitted"`
		IngestionValidTokenRatio flexFloat64 `json:"ingestion_valid_token_ratio"`
		MarketProbesBacklogRatio flexFloat64 `json:"market_probes_backlog_ratio"`
		DQPassOrRiskyPass        int64       `json:"dq_pass_or_risky_pass"`
		ShadowObserverFailed     int64       `json:"shadow_observer_failed"`
		ThroughputVerdict        string      `json:"throughput_verdict"`
		IngestionEmitted         int64       `json:"ingestion_emitted"`
	} `json:"throughput_metrics"`
}

// flexFloat64 unmarshals gate evidence ratios encoded as JSON numbers or strings.
type flexFloat64 float64

func (f *flexFloat64) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*f = 0
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexFloat64(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return err
	}
	*f = flexFloat64(v)
	return nil
}

type gateMetrics struct {
	wsol        int64
	validRatio  float64
	backlog     float64
	dqPass      int64
	traces      int64
	shadow      int64
	ingestionL0 int64
}

// BuildGateEvidence loads the newest gate_evidence_*.json snapshot and merges
// live DB counters (24h window) for traces and DQ pass counts.
func BuildGateEvidence(
	ctx context.Context,
	db database.Adapter,
	evidenceDir string,
) (*contracts.GateEvidenceResponseDTO, error) {
	metrics := gateMetrics{}
	out := &contracts.GateEvidenceResponseDTO{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Criteria:  buildGateCriteria(metrics),
	}

	path, err := findLatestGateEvidence(evidenceDir)
	if err != nil {
		return nil, fmt.Errorf("find gate evidence: %w", err)
	}
	if path != "" {
		fileMetrics, fileOut, parseErr := loadGateEvidenceFile(path)
		if parseErr != nil {
			return nil, fmt.Errorf("load gate evidence %s: %w", path, parseErr)
		}
		metrics = fileMetrics
		*out = *fileOut
	}

	if err := mergeLiveGateMetrics(ctx, db, &metrics); err != nil {
		return nil, fmt.Errorf("merge live gate metrics: %w", err)
	}

	out.WSOLTokenAddressEmitted = metrics.wsol
	out.IngestionValidTokenRatio = metrics.validRatio
	out.MarketProbesBacklogRatio = metrics.backlog
	out.DQPassOrRiskyPass = metrics.dqPass
	out.TracesCompleted = metrics.traces
	out.ShadowObserverFailed = metrics.shadow
	out.ThroughputVerdict = computeThroughputVerdict(metrics)
	out.Criteria = buildGateCriteria(metrics)

	return out, nil
}

func findLatestGateEvidence(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "gate_evidence_*.json"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	sort.Slice(matches, func(i, j int) bool {
		ii, errI := os.Stat(matches[i])
		jj, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return matches[i] > matches[j]
		}
		return ii.ModTime().After(jj.ModTime())
	})
	return matches[0], nil
}

func loadGateEvidenceFile(path string) (gateMetrics, *contracts.GateEvidenceResponseDTO, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return gateMetrics{}, &contracts.GateEvidenceResponseDTO{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}, nil
		}
		return gateMetrics{}, nil, err
	}
	defer f.Close()

	limited := io.LimitReader(f, maxGateEvidenceBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return gateMetrics{}, nil, err
	}

	var raw gateEvidenceFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return gateMetrics{}, nil, err
	}

	metrics := gateMetrics{
		wsol:        raw.ThroughputMetrics.WsolTokenAddressEmitted,
		validRatio:  float64(raw.ThroughputMetrics.IngestionValidTokenRatio),
		backlog:     float64(raw.ThroughputMetrics.MarketProbesBacklogRatio),
		dqPass:      raw.ThroughputMetrics.DQPassOrRiskyPass,
		traces:      raw.OperationalEvidence.TracesCompleted,
		shadow:      raw.ThroughputMetrics.ShadowObserverFailed,
		ingestionL0: raw.ThroughputMetrics.IngestionEmitted,
	}

	ts := raw.Timestamp
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	out := &contracts.GateEvidenceResponseDTO{
		Timestamp:                ts,
		DetectedMode:             raw.DetectedMode,
		WSOLTokenAddressEmitted:  metrics.wsol,
		IngestionValidTokenRatio: metrics.validRatio,
		MarketProbesBacklogRatio: metrics.backlog,
		DQPassOrRiskyPass:        metrics.dqPass,
		TracesCompleted:          metrics.traces,
		ShadowObserverFailed:     metrics.shadow,
		ThroughputVerdict:        raw.ThroughputMetrics.ThroughputVerdict,
	}
	return metrics, out, nil
}

func mergeLiveGateMetrics(ctx context.Context, db database.Adapter, metrics *gateMetrics) error {
	stats, err := db.GetPipelineStats(ctx, 24)
	if err != nil {
		return err
	}
	if stats != nil && stats.Evaluated > metrics.traces {
		metrics.traces = stats.Evaluated
	}
	if stats != nil && stats.Detected > metrics.ingestionL0 {
		metrics.ingestionL0 = stats.Detected
	}

	dq, err := db.GetDQBreakdown(ctx, 24, "")
	if err != nil {
		return err
	}
	if dq != nil {
		liveDQ := dq.PassCount + dq.RiskyPassCount
		if liveDQ > metrics.dqPass {
			metrics.dqPass = liveDQ
		}
	}
	return nil
}

func buildGateCriteria(m gateMetrics) []contracts.GateCriterionDTO {
	validPct := "—"
	if m.validRatio > 0 {
		validPct = fmt.Sprintf("%.0f%%", m.validRatio*100)
	}
	backlogPct := "—"
	if m.backlog > 0 {
		backlogPct = fmt.Sprintf("%.0f%%", m.backlog*100)
	}

	criteria := []contracts.GateCriterionDTO{
		{
			Label:  "Valid token ratio",
			Value:  validPct,
			Passed: m.validRatio == 0 || m.validRatio >= 0.80,
		},
		{
			Label:  "Probe backlog",
			Value:  backlogPct,
			Passed: m.backlog == 0 || m.backlog <= 0.05,
		},
		{
			Label:  "DQ pass",
			Value:  fmt.Sprintf("%d", m.dqPass),
			Passed: m.dqPass >= 1,
		},
		{
			Label:  "Full traces",
			Value:  fmt.Sprintf("%d", m.traces),
			Passed: m.traces >= 1,
		},
		{
			Label:  "Shadow observer",
			Value:  fmt.Sprintf("%d errors", m.shadow),
			Passed: m.shadow == 0,
		},
	}

	// WSOL check applies to Solana only; include when evidence captured it.
	if m.wsol > 0 || m.validRatio > 0 || m.backlog > 0 {
		criteria = append([]contracts.GateCriterionDTO{{
			Label:  "WSOL as token",
			Value:  fmt.Sprintf("%d", m.wsol),
			Passed: m.wsol == 0,
		}}, criteria...)
	}

	return criteria
}

func computeThroughputVerdict(m gateMetrics) string {
	if m.wsol > 0 || m.shadow > 0 {
		return "CODE_DEFECT"
	}
	if m.validRatio > 0 && m.validRatio < 0.80 {
		return "CODE_DEFECT"
	}
	if m.backlog > 0.05 {
		return "CODE_DEFECT"
	}
	if m.ingestionL0 > 0 && m.dqPass == 0 {
		return "CODE_DEFECT"
	}
	if m.ingestionL0 == 0 && m.traces == 0 {
		return "MARKET_QUIET"
	}
	if m.ingestionL0 < 5 && m.traces == 0 {
		return "MARKET_QUIET"
	}
	return "HEALTHY"
}
