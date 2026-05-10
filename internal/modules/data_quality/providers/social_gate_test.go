package providers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/internal/modules/data_quality/providers"
)

// helpers

func newTestSocialGate(url string) *providers.SocialGateProvider {
	p := providers.NewSocialGateProvider(slog.Default())
	p.SetBaseURLForTest(url)
	return p
}

func dexPairsJSON(socialTypes []string, hasWebsite bool) []byte {
	type soc struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	type ws struct {
		URL string `json:"url"`
	}
	type info struct {
		Socials  []soc `json:"socials"`
		Websites []ws  `json:"websites"`
	}
	type pair struct {
		Info info `json:"info"`
	}
	socials := make([]soc, len(socialTypes))
	for i, t := range socialTypes {
		socials[i] = soc{Type: t, URL: fmt.Sprintf("https://%s.example.com", t)}
	}
	var websites []ws
	if hasWebsite {
		websites = append(websites, ws{URL: "https://project.example.com"})
	}
	pairs := []pair{{Info: info{Socials: socials, Websites: websites}}}
	b, _ := json.Marshal(pairs)
	return b
}

// TestSocialGate_Name

func TestSocialGate_Name(t *testing.T) {
	p := providers.NewSocialGateProvider(slog.Default())
	if p.Name() != "social_gate" {
		t.Errorf("expected 'social_gate', got %q", p.Name())
	}
}

// TestSocialGate_HasTwitter_LowRisk

func TestSocialGate_HasTwitter_LowRisk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(dexPairsJSON([]string{"twitter"}, true))
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Degraded {
		t.Error("should not be degraded")
	}
	if sig.Score != 0.10 {
		t.Errorf("expected score=0.10 for twitter, got %f", sig.Score)
	}
	if len(sig.Flags) != 0 {
		t.Errorf("expected no flags for twitter presence, got %v", sig.Flags)
	}
}

// TestSocialGate_NoTwitter_OtherSocial_MediumRisk

func TestSocialGate_NoTwitter_OtherSocial_MediumRisk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(dexPairsJSON([]string{"telegram"}, false))
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "bsc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Score != 0.30 {
		t.Errorf("expected score=0.30 for no-twitter social, got %f", sig.Score)
	}
	hasFlag := false
	for _, f := range sig.Flags {
		if strings.Contains(f, "no_twitter") {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Errorf("expected 'no_twitter' flag, got %v", sig.Flags)
	}
}

// TestSocialGate_NoSocials_HighRisk

func TestSocialGate_NoSocials_HighRisk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(dexPairsJSON(nil, false))
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "solana")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Score != 0.50 {
		t.Errorf("expected score=0.50 for no socials, got %f", sig.Score)
	}
	hasFlag := false
	for _, f := range sig.Flags {
		if strings.Contains(f, "no_socials") {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Errorf("expected 'no_socials' flag, got %v", sig.Flags)
	}
}

// TestSocialGate_404_Degraded

func TestSocialGate_404_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "UNKNOWN", "eth")
	if err != nil {
		t.Fatalf("unexpected error on 404 (should fail-open): %v", err)
	}
	if !sig.Degraded {
		t.Error("expected degraded on 404")
	}
	if sig.Score != 0 {
		t.Errorf("expected score=0 on 404, got %f", sig.Score)
	}
}

// TestSocialGate_5xx_Degraded

func TestSocialGate_5xx_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err == nil {
		t.Error("expected error on 5xx")
	}
	if !sig.Degraded {
		t.Error("expected degraded on 5xx")
	}
}

// TestSocialGate_ContextCancelled_Degraded

func TestSocialGate_ContextCancelled_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write(dexPairsJSON([]string{"twitter"}, true))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(ctx, "0xTOKEN", "eth")
	if err == nil {
		t.Error("expected error on context cancel")
	}
	if !sig.Degraded {
		t.Errorf("expected degraded on timeout, sig=%+v", sig)
	}
}

// TestSocialGate_InvalidJSON_Degraded

func TestSocialGate_InvalidJSON_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "eth")
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
	if !sig.Degraded {
		t.Error("expected degraded on parse error")
	}
}

// TestSocialGate_XAlias_RecognisedAsTwitter

func TestSocialGate_XAlias_RecognisedAsTwitter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(dexPairsJSON([]string{"X"}, false)) // capital X alias
	}))
	defer srv.Close()

	p := newTestSocialGate(srv.URL)
	sig, err := p.Evaluate(context.Background(), "0xTOKEN", "base")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Score != 0.10 {
		t.Errorf("X alias should be treated as twitter (score=0.10), got %f", sig.Score)
	}
}
