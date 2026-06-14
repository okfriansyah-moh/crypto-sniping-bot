package postgres

import (
	"strings"
	"testing"
)

// pipelineStatsCumulativeExclusions mirrors the NOT IN filters in pipelineStatsCountSQL.
// Tests use this map to simulate cumulative funnel semantics without a live database.
var pipelineStatsCumulativeExclusions = map[string][]string{
	"dq_passed": {
		"DETECTED", "REJECTED", "DQ_SKIPPED",
	},
	"feature_ready": {
		"DETECTED", "REJECTED", "DQ_PASSED", "DQ_SKIPPED",
	},
	"edge_detected": {
		"DETECTED", "REJECTED", "DQ_PASSED", "FEATURE_READY", "DQ_SKIPPED",
	},
	"validated": {
		"DETECTED", "REJECTED", "DQ_PASSED", "FEATURE_READY", "EDGE_DETECTED", "DQ_SKIPPED",
	},
}

func countCumulative(states []string, excluded []string) int64 {
	excl := make(map[string]struct{}, len(excluded))
	for _, s := range excluded {
		excl[s] = struct{}{}
	}
	var n int64
	for _, state := range states {
		if _, skip := excl[state]; !skip {
			n++
		}
	}
	return n
}

func TestPipelineStats_SQLExcludesDQSkippedFromDQPassed(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"DETECTED", "REJECTED", "DQ_SKIPPED"} {
		if !strings.Contains(pipelineStatsCountSQL, "'"+state+"'") {
			t.Fatalf("pipelineStatsCountSQL must reference %q", state)
		}
	}
	if !strings.Contains(pipelineStatsCountSQL, "'DETECTED','REJECTED','DQ_SKIPPED'))") {
		t.Error("dq_passed filter must exclude DETECTED, REJECTED, and DQ_SKIPPED")
	}
}

func TestPipelineStats_SQLExcludesDQSkippedFromDownstreamStages(t *testing.T) {
	t.Parallel()
	checks := []struct {
		stage    string
		fragment string
	}{
		{
			stage:    "feature_ready",
			fragment: "'DETECTED','REJECTED','DQ_PASSED','DQ_SKIPPED'))",
		},
		{
			stage:    "edge_detected",
			fragment: "'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','DQ_SKIPPED'))",
		},
		{
			stage:    "validated",
			fragment: "'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','EDGE_DETECTED','DQ_SKIPPED'))",
		},
	}
	for _, tc := range checks {
		tc := tc
		t.Run(tc.stage, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(pipelineStatsCountSQL, tc.fragment) {
				t.Errorf("%s filter must exclude DQ_SKIPPED; expected fragment %q", tc.stage, tc.fragment)
			}
		})
	}
}

func TestPipelineStats_DQSkippedStaysOutOfDQPassed(t *testing.T) {
	t.Parallel()
	states := []string{"DQ_SKIPPED", "DQ_SKIPPED", "DQ_SKIPPED"}
	got := countCumulative(states, pipelineStatsCumulativeExclusions["dq_passed"])
	if got != 0 {
		t.Errorf("DQPassed = %d, want 0 for tokens stuck in DQ_SKIPPED", got)
	}
}

func TestPipelineStats_DQSkippedDoesNotInflateDownstreamStages(t *testing.T) {
	t.Parallel()
	states := []string{"DQ_SKIPPED"}
	for stage, excluded := range pipelineStatsCumulativeExclusions {
		got := countCumulative(states, excluded)
		if got != 0 {
			t.Errorf("%s = %d for DQ_SKIPPED token, want 0", stage, got)
		}
	}
}

func TestPipelineStats_AcceptedTokensRollUpCumulatively(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		states       []string
		wantDQPassed int64
		wantFeature  int64
		wantEdge     int64
		wantValidated int64
	}{
		{
			name:          "VALIDATED token counts in all funnel stages",
			states:        []string{"VALIDATED"},
			wantDQPassed:  1,
			wantFeature:   1,
			wantEdge:      1,
			wantValidated: 1,
		},
		{
			name:          "FEATURE_READY rolls up through feature but not edge or validated",
			states:        []string{"FEATURE_READY"},
			wantDQPassed:  1,
			wantFeature:   1,
			wantEdge:      0,
			wantValidated: 0,
		},
		{
			name:   "mixed window excludes skipped and preserves cumulative rollup",
			states: []string{"DETECTED", "REJECTED", "DQ_SKIPPED", "DQ_PASSED", "FEATURE_READY", "VALIDATED"},
			wantDQPassed:  3, // DQ_PASSED, FEATURE_READY, VALIDATED
			wantFeature:   2, // FEATURE_READY, VALIDATED
			wantEdge:      1, // VALIDATED only
			wantValidated: 1,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["dq_passed"]); got != tc.wantDQPassed {
				t.Errorf("DQPassed = %d, want %d", got, tc.wantDQPassed)
			}
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["feature_ready"]); got != tc.wantFeature {
				t.Errorf("FeatureReady = %d, want %d", got, tc.wantFeature)
			}
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["edge_detected"]); got != tc.wantEdge {
				t.Errorf("EdgeDetected = %d, want %d", got, tc.wantEdge)
			}
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["validated"]); got != tc.wantValidated {
				t.Errorf("Validated = %d, want %d", got, tc.wantValidated)
			}
		})
	}
}

func TestPipelineStats_PreservesFailedFromStateLogic(t *testing.T) {
	t.Parallel()
	// Selected/executed/position_open use positive IN filters and failed_from CTE —
	// unchanged by the DQ_SKIPPED fix.
	if !strings.Contains(pipelineStatsCountSQL, "WITH failed_from AS") {
		t.Error("query must retain failed_from CTE for FAILED-stage accounting")
	}
	for _, fragment := range []string{
		"AS selected",
		"AS executed",
		"AS position_open",
		"ff.from_state = 'SELECTED'",
		"ff.from_state IN ('EXECUTED','POSITION_OPEN')",
		"ff.from_state = 'POSITION_OPEN'",
	} {
		if !strings.Contains(pipelineStatsCountSQL, fragment) {
			t.Errorf("pipelineStatsCountSQL must preserve FAILED accounting fragment %q", fragment)
		}
	}
	// DQ_SKIPPED must not appear in the selected positive filter.
	selectedIdx := strings.Index(pipelineStatsCountSQL, "AS selected")
	if selectedIdx < 0 {
		t.Fatal("missing selected aggregate")
	}
	selectedSection := pipelineStatsCountSQL[selectedIdx : selectedIdx+200]
	if strings.Contains(selectedSection, "DQ_SKIPPED") {
		t.Error("selected filter must not reference DQ_SKIPPED (positive IN list only)")
	}
}

func TestRescanPipelineStats_SQLAnchorsOnRescanTransport(t *testing.T) {
	t.Parallel()
	if !strings.Contains(rescanPipelineStatsCountSQL, "transport LIKE 'rescan_%'") {
		t.Error("rescan query must anchor on market_data.transport LIKE 'rescan_%'")
	}
	if !strings.Contains(rescanPipelineStatsCountSQL, "WITH rescan_tokens AS") {
		t.Error("rescan query must use rescan_tokens CTE for distinct-token anchoring")
	}
}

func TestRescanPipelineStats_SQLExcludesDQSkippedFromDQPassed(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"DETECTED", "REJECTED", "DQ_SKIPPED"} {
		if !strings.Contains(rescanPipelineStatsCountSQL, "'"+state+"'") {
			t.Fatalf("rescanPipelineStatsCountSQL must reference %q", state)
		}
	}
	if !strings.Contains(rescanPipelineStatsCountSQL, "'DETECTED','REJECTED','DQ_SKIPPED'))") {
		t.Error("rescan dq_passed filter must exclude DETECTED, REJECTED, and DQ_SKIPPED")
	}
}

func TestRescanPipelineStats_SQLExcludesDQSkippedFromDownstreamStages(t *testing.T) {
	t.Parallel()
	checks := []struct {
		stage    string
		fragment string
	}{
		{
			stage:    "feature_ready",
			fragment: "'DETECTED','REJECTED','DQ_PASSED','DQ_SKIPPED'))",
		},
		{
			stage:    "edge_detected",
			fragment: "'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','DQ_SKIPPED'))",
		},
		{
			stage:    "validated",
			fragment: "'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','EDGE_DETECTED','DQ_SKIPPED'))",
		},
	}
	for _, tc := range checks {
		tc := tc
		t.Run(tc.stage, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(rescanPipelineStatsCountSQL, tc.fragment) {
				t.Errorf("%s filter must exclude DQ_SKIPPED; expected fragment %q", tc.stage, tc.fragment)
			}
		})
	}
}

func TestRescanPipelineStats_DQSkippedStaysOutOfDQPassed(t *testing.T) {
	t.Parallel()
	states := []string{"DQ_SKIPPED", "DQ_SKIPPED", "DQ_SKIPPED"}
	got := countCumulative(states, pipelineStatsCumulativeExclusions["dq_passed"])
	if got != 0 {
		t.Errorf("DQPassed = %d, want 0 for rescanned tokens stuck in DQ_SKIPPED", got)
	}
}

func TestRescanPipelineStats_DQSkippedDoesNotInflateDownstreamStages(t *testing.T) {
	t.Parallel()
	states := []string{"DQ_SKIPPED"}
	for stage, excluded := range pipelineStatsCumulativeExclusions {
		got := countCumulative(states, excluded)
		if got != 0 {
			t.Errorf("%s = %d for rescanned DQ_SKIPPED token, want 0", stage, got)
		}
	}
}

func TestRescanPipelineStats_AcceptedRescannedTokensRollUpCumulatively(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		states        []string
		wantDQPassed  int64
		wantFeature   int64
		wantEdge      int64
		wantValidated int64
	}{
		{
			name:          "VALIDATED rescanned token counts in all funnel stages",
			states:        []string{"VALIDATED"},
			wantDQPassed:  1,
			wantFeature:   1,
			wantEdge:      1,
			wantValidated: 1,
		},
		{
			name:   "mixed rescanned window excludes skipped and preserves cumulative rollup",
			states: []string{"DETECTED", "REJECTED", "DQ_SKIPPED", "DQ_PASSED", "FEATURE_READY", "VALIDATED"},
			wantDQPassed:  3,
			wantFeature:   2,
			wantEdge:      1,
			wantValidated: 1,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["dq_passed"]); got != tc.wantDQPassed {
				t.Errorf("DQPassed = %d, want %d", got, tc.wantDQPassed)
			}
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["feature_ready"]); got != tc.wantFeature {
				t.Errorf("FeatureReady = %d, want %d", got, tc.wantFeature)
			}
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["edge_detected"]); got != tc.wantEdge {
				t.Errorf("EdgeDetected = %d, want %d", got, tc.wantEdge)
			}
			if got := countCumulative(tc.states, pipelineStatsCumulativeExclusions["validated"]); got != tc.wantValidated {
				t.Errorf("Validated = %d, want %d", got, tc.wantValidated)
			}
		})
	}
}

func TestRescanPipelineStats_PreservesFailedFromStateLogic(t *testing.T) {
	t.Parallel()
	if !strings.Contains(rescanPipelineStatsCountSQL, "WITH rescan_tokens AS") {
		t.Error("query must retain rescan_tokens CTE")
	}
	if !strings.Contains(rescanPipelineStatsCountSQL, "failed_from AS") {
		t.Error("query must retain failed_from CTE for FAILED-stage accounting")
	}
	for _, fragment := range []string{
		"AS selected",
		"AS executed",
		"AS position_open",
		"ff.from_state = 'SELECTED'",
		"ff.from_state IN ('EXECUTED','POSITION_OPEN')",
		"ff.from_state = 'POSITION_OPEN'",
	} {
		if !strings.Contains(rescanPipelineStatsCountSQL, fragment) {
			t.Errorf("rescanPipelineStatsCountSQL must preserve FAILED accounting fragment %q", fragment)
		}
	}
}
