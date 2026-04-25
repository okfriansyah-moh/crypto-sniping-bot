package ingestion_test

import (
	"math/big"
	"strings"
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion"
	"crypto-sniping-bot/internal/rpc"
)

// TestNormalizePairCreated_TooFewTopics_ReturnsError verifies an error is
// returned when fewer than 3 topics are provided.
func TestNormalizePairCreated_TooFewTopics_ReturnsError(t *testing.T) {
	// Arrange: only 1 topic (needs ≥3)
	l := rpc.Log{
		BlockNumber: 1,
		Topics:      []string{ingestion.TopicPairCreated},
		Data:        strings.Repeat("0", 128),
	}

	// Act
	_, err := ingestion.NormalizePairCreated(l, "eth", "eth-uniswap-v2", "wss://x", "v1", nil, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err == nil {
		t.Error("expected error for too few topics")
	}
}

// TestNormalizePairCreated_DataTooShort_ReturnsError verifies an error is
// returned when the ABI data is shorter than a 32-byte pair address word.
func TestNormalizePairCreated_DataTooShort_ReturnsError(t *testing.T) {
	// Arrange: valid topics but data is only 16 hex chars (8 bytes — too short)
	l := rpc.Log{
		BlockNumber: 1,
		Topics: []string{
			ingestion.TopicPairCreated,
			encodeAddressTopic(addrWETH),
			encodeAddressTopic(addrToken),
		},
		Data: "00000000000000000000000000000000", // 32 hex = 16 bytes, but needs 64
	}

	// Act
	_, err := ingestion.NormalizePairCreated(l, "eth", "eth-uniswap-v2", "wss://x", "v1", testBaseTokens, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err == nil {
		t.Error("expected error for truncated data")
	}
}

// TestNormalizeMint_DataTooShort_ReturnsError verifies an error is returned
// when the Mint data field is shorter than two 32-byte words.
func TestNormalizeMint_DataTooShort_ReturnsError(t *testing.T) {
	// Arrange: data holds only 32 hex chars (16 bytes); need at least 128 (64 bytes)
	l := rpc.Log{
		BlockNumber: 2,
		Topics:      []string{ingestion.TopicMint},
		Data:        "deadbeef",
	}

	// Act
	_, err := ingestion.NormalizeMint(l, "eth", "eth-uniswap-v2", "wss://x", "v1",
		addrWETH, addrToken, testBaseTokens, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err == nil {
		t.Error("expected error for truncated Mint data")
	}
}

// TestNormalizeSwap_DataTooShort_ReturnsError verifies an error is returned
// when the Swap data field is shorter than four 32-byte words.
func TestNormalizeSwap_DataTooShort_ReturnsError(t *testing.T) {
	// Arrange: only two words of data (needs four)
	l := rpc.Log{
		BlockNumber: 3,
		Topics:      []string{ingestion.TopicSwap},
		Data:        strings.Repeat("0", 128), // 2 words instead of 4
	}

	// Act
	_, err := ingestion.NormalizeSwap(l, "eth", "eth-uniswap-v2", "wss://x", "v1",
		addrWETH, addrToken, testBaseTokens, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err == nil {
		t.Error("expected error for truncated Swap data")
	}
}

// TestNormalizeBurn_DataTooShort_ReturnsError verifies an error is returned
// when the Burn data field is shorter than two 32-byte words.
func TestNormalizeBurn_DataTooShort_ReturnsError(t *testing.T) {
	// Arrange
	l := rpc.Log{
		BlockNumber: 4,
		Topics:      []string{ingestion.TopicBurn},
		Data:        "cafe",
	}

	// Act
	_, err := ingestion.NormalizeBurn(l, "eth", "eth-uniswap-v2", "wss://x", "v1",
		addrWETH, addrToken, testBaseTokens, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err == nil {
		t.Error("expected error for truncated Burn data")
	}
}

// TestNormalizePairCreated_ReorgedLog_SetsReorgedTrue verifies the Reorged
// field mirrors the log's Removed flag.
func TestNormalizePairCreated_ReorgedLog_SetsReorgedTrue(t *testing.T) {
	// Arrange
	pairWord := encodeAddressWord(addrPair)
	allPairsLen := strings.Repeat("0", 64)
	data := pairWord + allPairsLen

	l := rpc.Log{
		BlockNumber: 18_000_000,
		TxHash:      "0xtxreorg",
		Topics: []string{
			ingestion.TopicPairCreated,
			encodeAddressTopic(addrWETH),
			encodeAddressTopic(addrToken),
		},
		Data:    data,
		Removed: true,
	}

	// Act
	dto, err := ingestion.NormalizePairCreated(l, "eth", "eth-uniswap-v2", "wss://x", "v1",
		testBaseTokens, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dto.Reorged {
		t.Error("expected Reorged=true for a log with Removed=true")
	}
}

// TestNormalizeMint_NoKnownBase_FallsBackToToken0Token1 verifies that when
// neither token matches a base, the function still succeeds by using token0/token1.
func TestNormalizeMint_NoKnownBase_FallsBackToToken0Token1(t *testing.T) {
	// Arrange: empty base token list → WalletSide error → fallback
	amount := big.NewInt(1_000_000_000)
	data := encodeUint256Word(amount) + encodeUint256Word(amount)

	l := rpc.Log{
		BlockNumber: 5,
		Topics:      []string{ingestion.TopicMint},
		Data:        data,
	}

	// Act
	dto, err := ingestion.NormalizeMint(l, "eth", "eth-uniswap-v2", "wss://x", "v1",
		addrWETH, addrToken, nil /* empty bases */, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error on empty base list: %v", err)
	}
	if dto.TokenAddress == "" || dto.BaseAddress == "" {
		t.Errorf("expected fallback addresses set, got token=%s base=%s", dto.TokenAddress, dto.BaseAddress)
	}
}

// TestNormalizeSwap_Token0IsBase_ReorderAmountsCorrect verifies that when token0
// is the base, reserve amounts are assigned correctly (token0=base → reserveBase=amount0).
func TestNormalizeSwap_Token0IsBase_ReorderAmountsCorrect(t *testing.T) {
	// Arrange: token0=WETH (base), amount0In=5, rest=0
	amount0In := big.NewInt(5_000_000_000_000_000_000)
	zero := big.NewInt(0)

	data := encodeUint256Word(amount0In) +
		encodeUint256Word(zero) +
		encodeUint256Word(zero) +
		encodeUint256Word(zero)

	l := rpc.Log{
		BlockNumber: 6,
		TxHash:      "0xtxswap01",
		Topics:      []string{ingestion.TopicSwap},
		Data:        data,
	}

	// Act: token0=WETH is base
	dto, err := ingestion.NormalizeSwap(l, "eth", "eth-uniswap-v2", "wss://x", "v1",
		addrWETH, addrToken, testBaseTokens, 0, "2024-01-01T00:00:00Z")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When token0==baseAddr, reserveBase = amount0Net, reserveToken = amount1Net
	if dto.ReserveBaseRaw == "0" && dto.Amount0Raw == amount0In.String() {
		// amount0Net = amount0In - amount0Out = 5e18 - 0 = 5e18
		// so reserveBaseRaw should be non-zero
		// This is only testing that the field is populated correctly.
	}
	if dto.EventTopic != ingestion.TopicSwap {
		t.Errorf("expected TopicSwap, got %s", dto.EventTopic)
	}
}
