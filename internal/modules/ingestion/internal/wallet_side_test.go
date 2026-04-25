package ingestioninternal_test

import (
	"testing"

	ingestioninternal "crypto-sniping-bot/internal/modules/ingestion/internal"
)

const (
	addrWETH  = "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
	addrToken = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	addrDAI   = "0x6b175474e89094c44da98b954eedeac495271d0f"
)

// TestWalletSide_Token0IsBase verifies token1 is returned as the sniping target
// when token0 matches a known base token.
func TestWalletSide_Token0IsBase(t *testing.T) {
	// Arrange
	baseTokens := []string{addrWETH}

	// Act
	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(addrWETH, addrToken, baseTokens)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenAddr != addrToken {
		t.Errorf("tokenAddr: want %s, got %s", addrToken, tokenAddr)
	}
	if baseAddr != addrWETH {
		t.Errorf("baseAddr: want %s, got %s", addrWETH, baseAddr)
	}
}

// TestWalletSide_Token1IsBase verifies token0 is returned as the sniping target
// when token1 matches a known base token.
func TestWalletSide_Token1IsBase(t *testing.T) {
	// Arrange
	baseTokens := []string{addrWETH}

	// Act
	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(addrToken, addrWETH, baseTokens)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenAddr != addrToken {
		t.Errorf("tokenAddr: want %s, got %s", addrToken, tokenAddr)
	}
	if baseAddr != addrWETH {
		t.Errorf("baseAddr: want %s, got %s", addrWETH, baseAddr)
	}
}

// TestWalletSide_NeitherIsBase_ReturnsError verifies an error is returned when
// neither token matches any known base token.
func TestWalletSide_NeitherIsBase_ReturnsError(t *testing.T) {
	// Arrange
	baseTokens := []string{addrWETH}

	// Act
	tokenAddr, baseAddr, err := ingestioninternal.WalletSide("0xaaaa", "0xbbbb", baseTokens)

	// Assert
	if err == nil {
		t.Error("expected error when neither token is a known base")
	}
	if tokenAddr != "" || baseAddr != "" {
		t.Errorf("expected empty addresses on error, got tokenAddr=%s baseAddr=%s", tokenAddr, baseAddr)
	}
}

// TestWalletSide_EmptyBaseTokens_ReturnsError verifies an error is returned
// when the base token list is empty.
func TestWalletSide_EmptyBaseTokens_ReturnsError(t *testing.T) {
	// Arrange
	baseTokens := []string{}

	// Act
	_, _, err := ingestioninternal.WalletSide(addrToken, addrWETH, baseTokens)

	// Assert
	if err == nil {
		t.Error("expected error with empty base token list")
	}
}

// TestWalletSide_CaseInsensitive verifies matching is case-insensitive for both
// token addresses and base token list entries.
func TestWalletSide_CaseInsensitive(t *testing.T) {
	// Arrange: token0 in uppercase, base list in lowercase
	token0Upper := "0xC02AAA39B223FE8D0A0E5C4F27EAD9083C756CC2"
	baseTokens := []string{addrWETH}

	// Act
	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(token0Upper, addrToken, baseTokens)

	// Assert
	if err != nil {
		t.Fatalf("expected case-insensitive match, got error: %v", err)
	}
	if tokenAddr != addrToken {
		t.Errorf("tokenAddr: want %s, got %s", addrToken, tokenAddr)
	}
	if baseAddr != token0Upper {
		t.Errorf("baseAddr: want %s, got %s", token0Upper, baseAddr)
	}
}

// TestWalletSide_MultipleBaseTokens verifies correct matching when the list
// contains multiple base tokens and token1 matches the second one.
func TestWalletSide_MultipleBaseTokens_SecondMatches(t *testing.T) {
	// Arrange
	baseTokens := []string{addrWETH, addrDAI}

	// Act
	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(addrToken, addrDAI, baseTokens)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenAddr != addrToken {
		t.Errorf("tokenAddr: want %s, got %s", addrToken, tokenAddr)
	}
	if baseAddr != addrDAI {
		t.Errorf("baseAddr: want %s, got %s", addrDAI, baseAddr)
	}
}

// TestWalletSide_Deterministic verifies identical inputs always produce
// identical outputs (no randomness).
func TestWalletSide_Deterministic(t *testing.T) {
	// Arrange
	baseTokens := []string{addrWETH, addrDAI}

	// Act: call twice with identical inputs
	token1, base1, err1 := ingestioninternal.WalletSide(addrToken, addrWETH, baseTokens)
	token2, base2, err2 := ingestioninternal.WalletSide(addrToken, addrWETH, baseTokens)

	// Assert
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if token1 != token2 || base1 != base2 {
		t.Errorf("WalletSide is non-deterministic: (%s,%s) vs (%s,%s)", token1, base1, token2, base2)
	}
}
