package providers_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/internal/modules/data_quality/providers"
)

// helpers

func newTestBirdEye(url string) *providers.BirdEyeProvider {
	p := providers.NewBirdEyeProvider(slog.Default())
	p.SetBaseURLForTest(url)
	p.SetAPIKeyForTest("test-key")
	return p
}

type birdeyeTestData struct {
	ownerPct      float64
	creatorPct    float64
	top10Pct      float64
	freezeAuth    *string
	mintAuth      *string
	isMutable     bool
	lpLockedPct   float64
	includeLPData bool
}

func buildBirdEyeResponse(d birdeyeTestData) []byte {
	type lp struct {
		LpLockedPct float64 `json:"lpLockedPct"`
	}
	type market struct {
		LP lp `json:"lp"`
	}
	type secData struct {
		OwnerPercentage    float64  `json:"ownerPercentage"`
		CreatorPercentage  float64  `json:"creatorPercentage"`
		Top10HolderPercent float64  `json:"top10HolderPercent"`
		FreezeAuthority    *string  `json:"freezeAuthority"`
		MintAuthority      *string  `json:"mintAuthority"`
		IsMutable          bool     `json:"isMutable"`
		Markets            []market `json:"markets"`
	}
	type resp struct {
		Success bool    `json:"success"`
		Data    secData `json:"data"`
	}

	var markets []market
	if d.includeLPData {
		markets = append(markets, market{LP: lp{LpLockedPct: d.lpLockedPct}})
	}

	r := resp{
		Success: true,
		Data: secData{
			OwnerPercentage:    d.ownerPct,
			CreatorPercentage:  d.creatorPct,
			Top10HolderPercent: d.top10Pct,
			FreezeAuthority:    d.freezeAuth,
			MintAuthority:      d.mintAuth,
			IsMutable:          d.isMutable,
			Markets:            markets,
		},
	}
	b, _ := json.Marshal(r)
	return b
}

// TestBirdEye_Name

func TestBirdEye_Name(t *testing.T) {
	p := providers.NewBirdEyeProvider(slog.Default())
	if p.Name() != "birdeye" {
		t.Errorf("expected 'birdeye', got %q", p.Name())
	}
}

// TestBirdEye_NoAPIKey_Degraded

func TestBirdEye_NoAPIKey_Degraded(t *testing.T) {
	p := providers.NewBirdEyeProvider(slog.Default())
	// Don't set API key — should degrade.
	p.SetAPIKeyForTest("")
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err != nil {
		t.Fatalf("should not error on missing key: %v", err)
	}
	if !sig.Degraded {
		t.Error("expected degraded when API key is empty")
	}
	if sig.Score != 0 {
		t.Errorf("expected score=0 when no key, got %f", sig.Score)
	}
}

// TestBirdEye_HighConcentration_HighScore

func TestBirdEye_HighConcentration_HighScore(t *testing.T) {
	body := buildBirdEyeResponse(birdeyeTestData{
		top10Pct: 0.85, // critical
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Score < 0.35 {
		t.Errorf("high concentration should produce score >= 0.35, got %f", sig.Score)
	}
	hasConcentrationFlag := false
	for _, f := range sig.Flags {
		if strings.Contains(f, "concentration") {
			hasConcentrationFlag = true
		}
	}
	if !hasConcentrationFlag {
		t.Errorf("expected concentration flag, got %v", sig.Flags)
	}
}

// TestBirdEye_HighCreator_HighRisk

func TestBirdEye_HighCreator_HighRisk(t *testing.T) {
	body := buildBirdEyeResponse(birdeyeTestData{
		creatorPct: 0.25, // > 20% → critical
		top10Pct:   0.30, // low concentration
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "bsc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.CreatorRiskScore < 0.80 {
		t.Errorf("creator pct 25%% should yield high CreatorRiskScore, got %f", sig.CreatorRiskScore)
	}
	hasCreatorFlag := false
	for _, f := range sig.Flags {
		if strings.Contains(f, "creator_high") {
			hasCreatorFlag = true
		}
	}
	if !hasCreatorFlag {
		t.Errorf("expected creator_high flag, got %v", sig.Flags)
	}
}

// TestBirdEye_MintAuthority_AuthorityRisk

func TestBirdEye_MintAuthority_AuthorityRisk(t *testing.T) {
	mintAuth := "MintAuthority111111"
	body := buildBirdEyeResponse(birdeyeTestData{
		mintAuth: &mintAuth,
		top10Pct: 0.20,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "solana")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hasMintFlag := false
	for _, f := range sig.Flags {
		if strings.Contains(f, "mint_authority") {
			hasMintFlag = true
		}
	}
	if !hasMintFlag {
		t.Errorf("expected mint_authority_set flag, got %v", sig.Flags)
	}
}

// TestBirdEye_LPUnlocked_Flag

func TestBirdEye_LPUnlocked_Flag(t *testing.T) {
	body := buildBirdEyeResponse(birdeyeTestData{
		includeLPData: true,
		lpLockedPct:   10.0, // 10% locked → mostly unlocked
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "base")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.LpLockPct != 10.0 {
		t.Errorf("expected LpLockPct=10, got %f", sig.LpLockPct)
	}
	hasLPFlag := false
	for _, f := range sig.Flags {
		if strings.Contains(f, "lp_unlocked") {
			hasLPFlag = true
		}
	}
	if !hasLPFlag {
		t.Errorf("expected lp_unlocked flag, got %v", sig.Flags)
	}
}

// TestBirdEye_LPLocked_NoFlag

func TestBirdEye_LPLocked_NoFlag(t *testing.T) {
	body := buildBirdEyeResponse(birdeyeTestData{
		includeLPData: true,
		lpLockedPct:   95.0, // 95% locked → safe
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range sig.Flags {
		if strings.Contains(f, "lp_unlocked") {
			t.Errorf("should not have lp_unlocked flag for 95%% locked, flags: %v", sig.Flags)
		}
	}
}

// TestBirdEye_404_Degraded

func TestBirdEye_404_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "UNKNOWN", "eth")
	if err != nil {
		t.Fatalf("should not error on 404 (fail-open): %v", err)
	}
	if !sig.Degraded {
		t.Error("expected degraded on 404")
	}
}

// TestBirdEye_5xx_Degraded

func TestBirdEye_5xx_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err == nil {
		t.Error("expected error on 5xx")
	}
	if !sig.Degraded {
		t.Error("expected degraded on 5xx")
	}
}

// TestBirdEye_ContextCancelled_Degraded

func TestBirdEye_ContextCancelled_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(buildBirdEyeResponse(birdeyeTestData{}))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(ctx, "0xTOKEN", "eth")
	if err == nil {
		t.Error("expected error on context timeout")
	}
	if !sig.Degraded {
		t.Errorf("expected degraded on timeout, sig=%+v", sig)
	}
}

// TestBirdEye_ScoreInRange

func TestBirdEye_ScoreInRange(t *testing.T) {
	cases := []birdeyeTestData{
		{top10Pct: 0.9, creatorPct: 0.25}, // high risk
		{top10Pct: 0.1, creatorPct: 0.01}, // low risk
		{top10Pct: 0.5, creatorPct: 0.08}, // medium risk
	}
	for _, tc := range cases {
		body := buildBirdEyeResponse(tc)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
		}))

		p := newTestBirdEye(srv.URL)
		sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
		srv.Close()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sig.Score < 0 || sig.Score > 1 {
			t.Errorf("score out of [0,1]: %f for case %+v", sig.Score, tc)
		}
	}
}

// TestBirdEye_NotSuccess_Degraded

func TestBirdEye_NotSuccess_Degraded(t *testing.T) {
	body := []byte(`{"success":false,"data":{}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := newTestBirdEye(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err != nil {
		t.Fatalf("should not error on not-success: %v", err)
	}
	if !sig.Degraded {
		t.Error("expected degraded when success=false")
	}
}
