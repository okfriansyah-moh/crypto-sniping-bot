package rpc

import (
	"testing"
	"time"
)

// ── detectDialect ─────────────────────────────────────────────────────────────

func TestDetectDialect_HeliusProviderHint_ReturnsHelius(t *testing.T) {
	// Arrange / Act
	d := detectDialect("helius", "https://example.com/rpc")

	// Assert
	if d.Name() != "helius" {
		t.Errorf("Name: want %q, got %q", "helius", d.Name())
	}
}

func TestDetectDialect_QuicknodeHint_ReturnsQuicknode(t *testing.T) {
	// Arrange / Act
	d := detectDialect("quicknode", "https://example.com/rpc")

	// Assert
	if d.Name() != "quicknode" {
		t.Errorf("Name: want %q, got %q", "quicknode", d.Name())
	}
}

func TestDetectDialect_QNHint_ReturnsQuicknode(t *testing.T) {
	// Arrange: "qn" is an alias for quicknode.
	d := detectDialect("qn", "https://example.com/rpc")

	// Assert
	if d.Name() != "quicknode" {
		t.Errorf("Name: want %q, got %q", "quicknode", d.Name())
	}
}

func TestDetectDialect_UppercaseHint_IsCaseInsensitive(t *testing.T) {
	// Arrange / Act
	d := detectDialect("HELIUS", "https://example.com/rpc")

	// Assert
	if d.Name() != "helius" {
		t.Errorf("Name: want %q, got %q", "helius", d.Name())
	}
}

func TestDetectDialect_HeliusURL_AutoDetected(t *testing.T) {
	// Arrange: no provider hint, but URL contains helius-rpc.com.
	d := detectDialect("", "https://mainnet.helius-rpc.com/?api-key=abc")

	// Assert
	if d.Name() != "helius" {
		t.Errorf("Name: want %q, got %q", "helius", d.Name())
	}
}

func TestDetectDialect_HeliusDevnetURL_AutoDetected(t *testing.T) {
	// Arrange
	d := detectDialect("", "wss://devnet.helius-rpc.com/ws?api-key=abc")

	// Assert
	if d.Name() != "helius" {
		t.Errorf("Name: want %q, got %q", "helius", d.Name())
	}
}

func TestDetectDialect_QuicknodeURL_AutoDetected(t *testing.T) {
	// Arrange
	d := detectDialect("", "https://my-node.quiknode.pro/abc123/")

	// Assert
	if d.Name() != "quicknode" {
		t.Errorf("Name: want %q, got %q", "quicknode", d.Name())
	}
}

func TestDetectDialect_UnknownURL_ReturnsGeneric(t *testing.T) {
	// Arrange: no hint, unrecognized URL.
	d := detectDialect("", "https://api.mainnet-beta.solana.com")

	// Assert
	if d.Name() != "generic" {
		t.Errorf("Name: want %q, got %q", "generic", d.Name())
	}
}

func TestDetectDialect_EmptyHintAndURL_ReturnsGeneric(t *testing.T) {
	// Arrange / Act
	d := detectDialect("", "")

	// Assert
	if d.Name() != "generic" {
		t.Errorf("Name: want %q, got %q", "generic", d.Name())
	}
}

// ── ProviderDialect behaviour ─────────────────────────────────────────────────

func TestQuicknodeDialect_IsRateLimited(t *testing.T) {
	// Arrange
	d := detectDialect("quicknode", "")

	// Act / Assert: QuickNode uses -32003 for quota exhaustion.
	if !d.IsRateLimited(-32003) {
		t.Error("QuickNode: IsRateLimited(-32003) want true")
	}
	if d.IsRateLimited(-32429) {
		t.Error("QuickNode: IsRateLimited(-32429) want false")
	}
	if d.IsRateLimited(0) {
		t.Error("QuickNode: IsRateLimited(0) want false")
	}
}

func TestHeliusDialect_IsRateLimited(t *testing.T) {
	// Arrange
	d := detectDialect("helius", "")

	// Act / Assert: Helius uses both -32003 and -32429.
	if !d.IsRateLimited(-32003) {
		t.Error("Helius: IsRateLimited(-32003) want true")
	}
	if !d.IsRateLimited(-32429) {
		t.Error("Helius: IsRateLimited(-32429) want true")
	}
	if d.IsRateLimited(0) {
		t.Error("Helius: IsRateLimited(0) want false")
	}
}

func TestGenericDialect_IsRateLimited(t *testing.T) {
	// Arrange
	d := detectDialect("", "")

	// Act / Assert: Generic falls back to -32003 only.
	if !d.IsRateLimited(-32003) {
		t.Error("Generic: IsRateLimited(-32003) want true")
	}
	if d.IsRateLimited(-32429) {
		t.Error("Generic: IsRateLimited(-32429) want false")
	}
}

func TestQuicknodeDialect_WSPingInterval(t *testing.T) {
	// Arrange
	d := detectDialect("quicknode", "")

	// Act / Assert: QuickNode ping interval is 20 s.
	if got := d.WSPingInterval(); got != 20*time.Second {
		t.Errorf("WSPingInterval: want 20s, got %v", got)
	}
}

func TestHeliusDialect_WSPingInterval(t *testing.T) {
	// Arrange
	d := detectDialect("helius", "")

	// Act / Assert: Helius ping interval is 30 s.
	if got := d.WSPingInterval(); got != 30*time.Second {
		t.Errorf("WSPingInterval: want 30s, got %v", got)
	}
}

func TestGenericDialect_WSPingInterval(t *testing.T) {
	// Arrange
	d := detectDialect("", "")

	// Act / Assert: Generic ping interval is 30 s (conservative).
	if got := d.WSPingInterval(); got != 30*time.Second {
		t.Errorf("WSPingInterval: want 30s, got %v", got)
	}
}
