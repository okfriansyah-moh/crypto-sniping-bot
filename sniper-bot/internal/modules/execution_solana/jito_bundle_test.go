package execution_solana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func jitoTestConfig(enabled, shadow bool) config.JitoConfig {
	return config.JitoConfig{
		Enabled:         enabled,
		ShadowMode:      shadow,
		TipLamports:     1000,
		MaxBundleSize:   5,
		SubmitTimeoutMs: 500,
	}
}

func TestJitoClient_Disabled_ReturnsNil(t *testing.T) {
	c, err := NewJitoClient(jitoTestConfig(false, false), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.SubmitBundle(context.Background(), []string{"tx1"}); err != nil {
		t.Fatalf("want nil when disabled, got %v", err)
	}
}

func TestJitoClient_ShadowMode_NoHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	t.Setenv("JITO_BUNDLE_URL", srv.URL)
	t.Setenv("JITO_TIP_ACCOUNT", "FakeAccount111111111111111111111111111111111")

	c, err := NewJitoClient(jitoTestConfig(true, true), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.SubmitBundle(context.Background(), []string{"tx1"}); err != nil {
		t.Fatalf("shadow submit: %v", err)
	}
	if called {
		t.Fatal("HTTP should not be called in shadow mode")
	}
}

func TestJitoClient_BundleTooLarge_ReturnsError(t *testing.T) {
	t.Setenv("JITO_BUNDLE_URL", "http://localhost:9999")
	t.Setenv("JITO_TIP_ACCOUNT", "FakeAccount111111111111111111111111111111111")

	cfg := jitoTestConfig(true, false)
	cfg.MaxBundleSize = 2
	c, err := NewJitoClient(cfg, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	txns := []string{"tx1", "tx2", "tx3"}
	if err := c.SubmitBundle(context.Background(), txns); err == nil {
		t.Fatal("expected error for oversized bundle")
	}
}

func TestJitoClient_SuccessfulSubmit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("want Content-Type application/json, got %s", ct)
		}
		resp := jitoRPCResponse{Result: "bundle-uuid-abc123"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("JITO_BUNDLE_URL", srv.URL)
	t.Setenv("JITO_TIP_ACCOUNT", "FakeAccount111111111111111111111111111111111")

	c, err := NewJitoClient(jitoTestConfig(true, false), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.SubmitBundle(context.Background(), []string{"tx1", "tx2"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
}

func TestJitoClient_RPCError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := jitoRPCResponse{}
		resp.Error = &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{Code: -32600, Message: "bundle rejected"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("JITO_BUNDLE_URL", srv.URL)
	t.Setenv("JITO_TIP_ACCOUNT", "FakeAccount111111111111111111111111111111111")

	c, err := NewJitoClient(jitoTestConfig(true, false), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := c.SubmitBundle(context.Background(), []string{"tx1"}); err == nil {
		t.Fatal("expected RPC error to be returned")
	}
}

func TestJitoClient_MissingEnvWhenEnabled_ReturnsError(t *testing.T) {
	t.Setenv("JITO_BUNDLE_URL", "")
	t.Setenv("JITO_TIP_ACCOUNT", "")
	_, err := NewJitoClient(jitoTestConfig(true, false), nil)
	if err == nil {
		t.Fatal("expected error when JITO_BUNDLE_URL missing with enabled=true shadow=false")
	}
}

func TestJitoClient_TipAccount_ReturnedFromEnv(t *testing.T) {
	t.Setenv("JITO_BUNDLE_URL", "http://localhost:9999")
	t.Setenv("JITO_TIP_ACCOUNT", "TipAccXYZ")
	c, err := NewJitoClient(jitoTestConfig(true, true), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if got := c.TipAccount(); got != "TipAccXYZ" {
		t.Errorf("TipAccount: want TipAccXYZ, got %s", got)
	}
}

func TestJitoClient_HTTPBundleURL_ReturnsError(t *testing.T) {
	// HIGH-01: plain HTTP must be rejected to prevent MITM on live bundle submissions.
	t.Setenv("JITO_BUNDLE_URL", "http://not-https.example.com/bundle")
	t.Setenv("JITO_TIP_ACCOUNT", "TipAccount123")
	_, err := NewJitoClient(jitoTestConfig(true, false), nil)
	if err == nil {
		t.Fatal("expected error: JITO_BUNDLE_URL must use https scheme")
	}
}
