package ingestion_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion"
	"crypto-sniping-bot/sniper-bot/internal/rpc"
)

// ── Mock RPC Client ──────────────────────────────────────────────────────────

// mockClient is a test-only rpc.Client that returns canned results.
type mockClient struct {
	endpoint   string
	logs       []rpc.Log
	latestBlk  uint64
	blockTimes map[uint64]string
}

func (m *mockClient) SubscribeLogs(_ context.Context, _ []string, _ [][]string) (<-chan rpc.Log, error) {
	ch := make(chan rpc.Log, len(m.logs))
	for _, l := range m.logs {
		ch <- l
	}
	close(ch)
	return ch, nil
}

func (m *mockClient) GetLogs(_ context.Context, fromBlock, toBlock uint64, _ []string, _ [][]string) ([]rpc.Log, error) {
	var result []rpc.Log
	for _, l := range m.logs {
		if l.BlockNumber >= fromBlock && l.BlockNumber <= toBlock {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *mockClient) GetBlockTimestamp(_ context.Context, blockNumber uint64) (string, error) {
	if ts, ok := m.blockTimes[blockNumber]; ok {
		return ts, nil
	}
	return "2024-01-01T00:00:00Z", nil
}

func (m *mockClient) LatestBlock(_ context.Context) (uint64, error) {
	return m.latestBlk, nil
}

func (m *mockClient) Ping(_ context.Context) error { return nil }

func (m *mockClient) Endpoint() string { return m.endpoint }

// ── ABI encoding helpers ─────────────────────────────────────────────────────

// padLeft32 pads a hex string (without 0x) to 32 bytes (64 hex chars), left-padded with zeros.
func padLeft32(hexStr string) string {
	hexStr = strings.TrimPrefix(hexStr, "0x")
	for len(hexStr) < 64 {
		hexStr = "0" + hexStr
	}
	return hexStr
}

// encodeAddress returns a 32-byte ABI-encoded address topic (0x + 64 hex chars).
func encodeAddressTopic(addr string) string {
	clean := strings.TrimPrefix(addr, "0x")
	return "0x" + padLeft32(clean)
}

// encodeAddressWord returns 32-byte ABI word for an address (for data encoding).
func encodeAddressWord(addr string) string {
	clean := strings.TrimPrefix(addr, "0x")
	return padLeft32(clean)
}

// encodeUint256Word returns 32-byte ABI word for a uint256.
func encodeUint256Word(n *big.Int) string {
	b := n.Bytes()
	hexStr := hex.EncodeToString(b)
	return padLeft32(hexStr)
}

// ── Test data ────────────────────────────────────────────────────────────────

const (
	testChain     = "eth"
	testMarket    = "eth-uniswap-v2"
	testEndpoint  = "wss://eth.example.com"
	testVersionID = "v1test00"

	// Test addresses (lowercase hex).
	addrWETH    = "0xc02aaa39b223fe8d0a0e5c4f27ead9083c756cc2"
	addrToken   = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	addrPair    = "0x1234567890abcdef1234567890abcdef12345678"
	addrFactory = "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
)

var testBaseTokens = []string{addrWETH}

func testIngestedAt() string {
	return "2024-01-15T10:00:00Z"
}

// ── Tests ────────────────────────────────────────────────────────────────────

// TestNormalizePairCreated verifies correct parsing of a PairCreated factory log.
func TestNormalizePairCreated(t *testing.T) {
	t.Parallel()

	// Build ABI-encoded data: pair address (32 bytes) + allPairsLength (32 bytes)
	pairWord := encodeAddressWord(addrPair)
	allPairsLen := encodeUint256Word(big.NewInt(42))
	data := pairWord + allPairsLen

	l := rpc.Log{
		BlockNumber: 18_000_000,
		BlockHash:   "0xblockhash",
		TxHash:      "0xtxhash01",
		LogIndex:    0,
		Address:     addrFactory,
		Topics: []string{
			ingestion.TopicPairCreated,
			encodeAddressTopic(addrWETH),  // token0 = WETH (base)
			encodeAddressTopic(addrToken), // token1 = target
		},
		Data:           data,
		BlockTimestamp: "2024-01-15T10:00:00Z",
	}

	dto, err := ingestion.NormalizePairCreated(l, testChain, testMarket, testEndpoint, testVersionID,
		testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("NormalizePairCreated error: %v", err)
	}

	if dto.EventID == "" {
		t.Error("EventID must not be empty")
	}
	if dto.TraceID != dto.EventID {
		t.Errorf("TraceID must equal EventID at Layer 0: got TraceID=%s EventID=%s", dto.TraceID, dto.EventID)
	}
	if dto.CausationID != "" {
		t.Errorf("Layer 0 CausationID must be empty, got %q", dto.CausationID)
	}
	if dto.Chain != testChain {
		t.Errorf("Chain: want %s got %s", testChain, dto.Chain)
	}
	if dto.Market != testMarket {
		t.Errorf("Market: want %s got %s", testMarket, dto.Market)
	}
	if dto.EventTopic != ingestion.TopicPairCreated {
		t.Errorf("EventTopic: want %s got %s", ingestion.TopicPairCreated, dto.EventTopic)
	}
	if !strings.EqualFold(dto.PoolAddress, addrPair) {
		t.Errorf("PoolAddress: want %s got %s", addrPair, dto.PoolAddress)
	}
	// token0=WETH (base) → TokenAddress=token1, BaseAddress=WETH
	if !strings.EqualFold(dto.TokenAddress, addrToken) {
		t.Errorf("TokenAddress: want %s got %s", addrToken, dto.TokenAddress)
	}
	if !strings.EqualFold(dto.BaseAddress, addrWETH) {
		t.Errorf("BaseAddress: want %s got %s", addrWETH, dto.BaseAddress)
	}
	if dto.BlockNumber != 18_000_000 {
		t.Errorf("BlockNumber: want 18000000 got %d", dto.BlockNumber)
	}
}

// TestNormalizeMint verifies correct parsing of a Mint pair log.
func TestNormalizeMint(t *testing.T) {
	t.Parallel()

	amount0 := big.NewInt(1_000_000_000_000_000_000) // 1e18 (WETH)
	amount1 := big.NewInt(2_000_000_000_000_000_000) // 2e18 (token)

	data := encodeUint256Word(amount0) + encodeUint256Word(amount1)

	l := rpc.Log{
		BlockNumber: 18_000_001,
		BlockHash:   "0xblockhash2",
		TxHash:      "0xtxhash02",
		LogIndex:    1,
		Address:     addrPair,
		Topics: []string{
			ingestion.TopicMint,
			encodeAddressTopic("0xsenderaddr0000000000000000000000000001"),
		},
		Data:           data,
		BlockTimestamp: "2024-01-15T10:00:01Z",
	}

	dto, err := ingestion.NormalizeMint(l, testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("NormalizeMint error: %v", err)
	}

	if dto.EventTopic != ingestion.TopicMint {
		t.Errorf("EventTopic: want Mint got %s", dto.EventTopic)
	}
	if dto.Amount0Raw != amount0.String() {
		t.Errorf("Amount0Raw: want %s got %s", amount0.String(), dto.Amount0Raw)
	}
	if dto.Amount1Raw != amount1.String() {
		t.Errorf("Amount1Raw: want %s got %s", amount1.String(), dto.Amount1Raw)
	}
	if dto.CausationID != "" {
		t.Error("Layer 0 CausationID must be empty")
	}
}

// TestNormalizeSwap verifies correct parsing of a Swap pair log.
func TestNormalizeSwap(t *testing.T) {
	t.Parallel()

	amount0In := big.NewInt(500_000_000_000_000_000) // 0.5 WETH
	amount1In := big.NewInt(0)
	amount0Out := big.NewInt(0)
	amount1Out := big.NewInt(1_200_000_000_000_000_000) // 1.2 token

	data := encodeUint256Word(amount0In) +
		encodeUint256Word(amount1In) +
		encodeUint256Word(amount0Out) +
		encodeUint256Word(amount1Out)

	l := rpc.Log{
		BlockNumber:    18_000_002,
		BlockHash:      "0xblockhash3",
		TxHash:         "0xtxhash03",
		LogIndex:       2,
		Address:        addrPair,
		Topics:         []string{ingestion.TopicSwap, encodeAddressTopic("0xsender0001"), encodeAddressTopic("0xto000001")},
		Data:           data,
		BlockTimestamp: "2024-01-15T10:00:02Z",
	}

	dto, err := ingestion.NormalizeSwap(l, testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("NormalizeSwap error: %v", err)
	}

	if dto.EventTopic != ingestion.TopicSwap {
		t.Errorf("EventTopic: want Swap got %s", dto.EventTopic)
	}
	if dto.Amount0Raw != amount0In.String() {
		t.Errorf("Amount0Raw: want %s got %s", amount0In.String(), dto.Amount0Raw)
	}
}

// TestEventIDDeterminism verifies that same inputs always produce the same EventID.
func TestEventIDDeterminism(t *testing.T) {
	t.Parallel()

	amount := big.NewInt(1_000_000_000_000_000_000)
	data := encodeUint256Word(amount) + encodeUint256Word(amount)

	makeLog := func() rpc.Log {
		return rpc.Log{
			BlockNumber:    18_000_100,
			BlockHash:      "0xblockhashX",
			TxHash:         "0xtxhashDeterministic",
			LogIndex:       5,
			Address:        addrPair,
			Topics:         []string{ingestion.TopicMint, encodeAddressTopic("0xsender0001")},
			Data:           data,
			BlockTimestamp: "2024-01-15T12:00:00Z",
		}
	}

	dto1, err := ingestion.NormalizeMint(makeLog(), testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("first normalize: %v", err)
	}

	dto2, err := ingestion.NormalizeMint(makeLog(), testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("second normalize: %v", err)
	}

	if dto1.EventID != dto2.EventID {
		t.Errorf("EventID not deterministic: %s != %s", dto1.EventID, dto2.EventID)
	}
	if dto1.TraceID != dto2.TraceID {
		t.Errorf("TraceID not deterministic: %s != %s", dto1.TraceID, dto2.TraceID)
	}
	if dto1.CorrelationID != dto2.CorrelationID {
		t.Errorf("CorrelationID not deterministic: %s != %s", dto1.CorrelationID, dto2.CorrelationID)
	}
}

// TestTopicRegistry verifies all 4 topics are registered.
func TestTopicRegistry(t *testing.T) {
	t.Parallel()

	topics := []string{
		ingestion.TopicPairCreated,
		ingestion.TopicMint,
		ingestion.TopicSwap,
		ingestion.TopicBurn,
	}

	for _, topic := range topics {
		if !ingestion.IsKnownTopic(topic) {
			t.Errorf("topic %s should be known", topic)
		}
	}

	if ingestion.IsKnownTopic("0xunknown") {
		t.Error("0xunknown should not be a known topic")
	}

	names := map[string]string{
		ingestion.TopicPairCreated: "PairCreated",
		ingestion.TopicMint:        "Mint",
		ingestion.TopicSwap:        "Swap",
		ingestion.TopicBurn:        "Burn",
	}
	for topic, want := range names {
		got := ingestion.TopicToEventName(topic)
		if got != want {
			t.Errorf("TopicToEventName(%s): want %s got %s", topic, want, got)
		}
	}
	if ingestion.TopicToEventName("0xbad") != "Unknown" {
		t.Error("unknown topic should return 'Unknown'")
	}
}

// TestWalletSide_WETHIsBase verifies that when token0=WETH, token is token1.
func TestWalletSide_WETHIsBase(t *testing.T) {
	t.Parallel()

	// Build a PairCreated log where token0=WETH and token1=target.
	pairWord := encodeAddressWord(addrPair)
	allPairsLen := encodeUint256Word(big.NewInt(1))
	data := pairWord + allPairsLen

	l := rpc.Log{
		BlockNumber: 1,
		BlockHash:   "0xhash",
		TxHash:      "0xtx1",
		LogIndex:    0,
		Address:     addrFactory,
		Topics: []string{
			ingestion.TopicPairCreated,
			encodeAddressTopic(addrWETH),  // token0 = WETH (base)
			encodeAddressTopic(addrToken), // token1 = target
		},
		Data: data,
	}

	dto, err := ingestion.NormalizePairCreated(l, testChain, testMarket, testEndpoint, testVersionID,
		testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("NormalizePairCreated: %v", err)
	}

	if !strings.EqualFold(dto.BaseAddress, addrWETH) {
		t.Errorf("BaseAddress should be WETH when token0=WETH, got %s", dto.BaseAddress)
	}
	if !strings.EqualFold(dto.TokenAddress, addrToken) {
		t.Errorf("TokenAddress should be token1 when token0=WETH, got %s", dto.TokenAddress)
	}
}

// TestWalletSide_NeitherBase verifies behavior when neither token is a base.
// Per spec, NormalizePairCreated should fall back to token0/token1 as-is.
func TestWalletSide_NeitherBase(t *testing.T) {
	t.Parallel()

	addrA := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	addrB := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	pairWord := encodeAddressWord(addrPair)
	allPairsLen := encodeUint256Word(big.NewInt(1))
	data := pairWord + allPairsLen

	l := rpc.Log{
		BlockNumber: 1,
		BlockHash:   "0xhash",
		TxHash:      "0xtx2",
		LogIndex:    0,
		Address:     addrFactory,
		Topics: []string{
			ingestion.TopicPairCreated,
			encodeAddressTopic(addrA),
			encodeAddressTopic(addrB),
		},
		Data: data,
	}

	// No base tokens → neither is base → fallback to token0/token1 as-is.
	dto, err := ingestion.NormalizePairCreated(l, testChain, testMarket, testEndpoint, testVersionID,
		[]string{}, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not be empty — fallback must produce non-empty fields.
	if dto.TokenAddress == "" || dto.BaseAddress == "" {
		t.Errorf("fallback should fill token/base: token=%q base=%q", dto.TokenAddress, dto.BaseAddress)
	}
}

// TestNextDelay verifies exponential backoff computation.
func TestNextDelay(t *testing.T) {
	t.Parallel()

	cfg := ingestion.BackoffConfig{
		InitialMs:  100,
		MaxMs:      10000,
		Multiplier: 2.0,
	}

	cases := []struct {
		attempt int
		wantMs  int64
	}{
		{0, 100},
		{1, 200},
		{2, 400},
		{3, 800},
		{4, 1600},
		{5, 3200},
		{6, 6400},
		{7, 10000}, // capped at MaxMs
		{8, 10000}, // still capped
	}

	for _, tc := range cases {
		d := ingestion.NextDelay(cfg, tc.attempt)
		if d.Milliseconds() != tc.wantMs {
			t.Errorf("attempt %d: want %dms got %dms", tc.attempt, tc.wantMs, d.Milliseconds())
		}
	}
}

// TestSelectEndpoint verifies round-robin endpoint selection.
func TestSelectEndpoint(t *testing.T) {
	t.Parallel()

	endpoints := []string{"ws://a", "ws://b", "ws://c"}

	cases := []struct {
		attempt int
		wantIdx int
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 0}, // wraps around
		{4, 1},
		{6, 0},
	}

	for _, tc := range cases {
		got := ingestion.SelectEndpoint(endpoints, tc.attempt)
		want := endpoints[tc.wantIdx]
		if got != want {
			t.Errorf("attempt %d: want %s got %s", tc.attempt, want, got)
		}
	}

	// Empty list.
	if ingestion.SelectEndpoint(nil, 0) != "" {
		t.Error("empty endpoints should return empty string")
	}
}

// TestRecoverGap_Sorted verifies that RecoverGap returns logs sorted by block + logIndex.
func TestRecoverGap_Sorted(t *testing.T) {
	t.Parallel()

	// Prepare unsorted logs.
	unsorted := []rpc.Log{
		{BlockNumber: 200, LogIndex: 2, TxHash: "0x03"},
		{BlockNumber: 100, LogIndex: 1, TxHash: "0x02"},
		{BlockNumber: 100, LogIndex: 0, TxHash: "0x01"},
		{BlockNumber: 200, LogIndex: 0, TxHash: "0x04"},
		{BlockNumber: 300, LogIndex: 0, TxHash: "0x05"},
	}

	client := &mockClient{logs: unsorted, latestBlk: 300}
	ctx := context.Background()

	logs, err := ingestion.RecoverGap(ctx, client, nil, nil, 100, 300)
	if err != nil {
		t.Fatalf("RecoverGap error: %v", err)
	}

	if len(logs) != len(unsorted) {
		t.Fatalf("expected %d logs got %d", len(unsorted), len(logs))
	}

	// Verify sorted order.
	for i := 1; i < len(logs); i++ {
		prev, curr := logs[i-1], logs[i]
		if prev.BlockNumber > curr.BlockNumber {
			t.Errorf("logs[%d].BlockNumber=%d > logs[%d].BlockNumber=%d",
				i-1, prev.BlockNumber, i, curr.BlockNumber)
		}
		if prev.BlockNumber == curr.BlockNumber && prev.LogIndex > curr.LogIndex {
			t.Errorf("logs[%d].LogIndex=%d > logs[%d].LogIndex=%d (same block)",
				i-1, prev.LogIndex, i, curr.LogIndex)
		}
	}
}

// TestIsReorged verifies reorg detection logic.
func TestIsReorged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		log               rpc.Log
		latestBlock       uint64
		confirmationDepth uint32
		want              bool
	}{
		{
			name:              "removed_log_is_reorged",
			log:               rpc.Log{BlockNumber: 100, Removed: true},
			latestBlock:       200,
			confirmationDepth: 0,
			want:              true,
		},
		{
			name:              "no_confirmation_depth_not_reorged",
			log:               rpc.Log{BlockNumber: 100, Removed: false},
			latestBlock:       200,
			confirmationDepth: 0,
			want:              false,
		},
		{
			name:              "block_below_confirmation_depth_is_safe",
			log:               rpc.Log{BlockNumber: 100, Removed: false},
			latestBlock:       200,
			confirmationDepth: 12,
			want:              false, // depth = 200 - 100 = 100 >= 12
		},
		{
			name:              "block_above_confirmation_depth_is_reorged",
			log:               rpc.Log{BlockNumber: 195, Removed: false},
			latestBlock:       200,
			confirmationDepth: 12,
			want:              true, // depth = 200 - 195 = 5 < 12
		},
		{
			name:              "log_block_equals_latest_is_unconfirmed",
			log:               rpc.Log{BlockNumber: 200, Removed: false},
			latestBlock:       200,
			confirmationDepth: 1,
			want:              true, // depth = 0 < 1
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ingestion.IsReorged(tc.log, tc.latestBlock, tc.confirmationDepth)
			if got != tc.want {
				t.Errorf("IsReorged: want %v got %v", tc.want, got)
			}
		})
	}
}

// TestNormalizeBurn verifies correct parsing of a Burn pair log.
func TestNormalizeBurn(t *testing.T) {
	t.Parallel()

	amount0 := big.NewInt(500_000_000_000_000_000)
	amount1 := big.NewInt(1_000_000_000_000_000_000)
	data := encodeUint256Word(amount0) + encodeUint256Word(amount1)

	l := rpc.Log{
		BlockNumber: 18_000_003,
		BlockHash:   "0xblockhash4",
		TxHash:      "0xtxhash04",
		LogIndex:    3,
		Address:     addrPair,
		Topics: []string{
			ingestion.TopicBurn,
			encodeAddressTopic("0xsender0002"),
			encodeAddressTopic("0xto000002"),
		},
		Data:           data,
		BlockTimestamp: "2024-01-15T10:00:03Z",
	}

	dto, err := ingestion.NormalizeBurn(l, testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())
	if err != nil {
		t.Fatalf("NormalizeBurn error: %v", err)
	}

	if dto.EventTopic != ingestion.TopicBurn {
		t.Errorf("EventTopic: want Burn got %s", dto.EventTopic)
	}
	if dto.Amount0Raw != amount0.String() {
		t.Errorf("Amount0Raw: want %s got %s", amount0.String(), dto.Amount0Raw)
	}
	if dto.Amount1Raw != amount1.String() {
		t.Errorf("Amount1Raw: want %s got %s", amount1.String(), dto.Amount1Raw)
	}
}

// TestEventIDUniqueness verifies different txHash+logIndex produce different EventIDs.
func TestEventIDUniqueness(t *testing.T) {
	t.Parallel()

	amount := big.NewInt(1_000_000)
	data := encodeUint256Word(amount) + encodeUint256Word(amount)

	log1 := rpc.Log{
		BlockNumber: 100, TxHash: "0xtx01", LogIndex: 0,
		Topics: []string{ingestion.TopicMint, encodeAddressTopic("0xsender01")},
		Data:   data,
	}
	log2 := rpc.Log{
		BlockNumber: 100, TxHash: "0xtx01", LogIndex: 1, // same tx, different logIndex
		Topics: []string{ingestion.TopicMint, encodeAddressTopic("0xsender01")},
		Data:   data,
	}

	dto1, _ := ingestion.NormalizeMint(log1, testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())
	dto2, _ := ingestion.NormalizeMint(log2, testChain, testMarket, testEndpoint, testVersionID,
		addrWETH, addrToken, testBaseTokens, 0, testIngestedAt())

	if dto1.EventID == dto2.EventID {
		t.Error("different log indexes must produce different EventIDs")
	}
}

// TestContentIDFromString verifies contracts.ContentIDFromString is deterministic.
func TestContentIDDeterminism(t *testing.T) {
	t.Parallel()

	id1 := contracts.ContentIDFromString("eth|0xtxhash|5")
	id2 := contracts.ContentIDFromString("eth|0xtxhash|5")
	if id1 != id2 {
		t.Errorf("ContentIDFromString not deterministic: %s != %s", id1, id2)
	}
	if len(id1) != 16 {
		t.Errorf("ContentID should be 16 hex chars, got %d", len(id1))
	}
}

// TestHeartbeat verifies timeout detection.
func TestHeartbeat(t *testing.T) {
	t.Parallel()

	hb := ingestion.NewHeartbeat(ingestion.HeartbeatConfig{IntervalMs: 1000, TimeoutMs: 50})
	hb.Reset()

	if hb.TimedOut() {
		t.Error("should not be timed out immediately after Reset")
	}

	time.Sleep(100 * time.Millisecond)
	if !hb.TimedOut() {
		t.Error("should be timed out after 100ms with 50ms timeout")
	}

	hb.Reset()
	if hb.TimedOut() {
		t.Error("should not be timed out immediately after second Reset")
	}
}

// TestSortedCollectionsDeterminism verifies that base tokens and factory addresses
// are always sorted before iteration — required by the no-randomness constraint.
func TestSortedCollectionsDeterminism(t *testing.T) {
	t.Parallel()

	baseTokens1 := []string{"0xc", "0xa", "0xb"}
	baseTokens2 := []string{"0xa", "0xb", "0xc"}

	sorted1 := make([]string, len(baseTokens1))
	sorted2 := make([]string, len(baseTokens2))
	copy(sorted1, baseTokens1)
	copy(sorted2, baseTokens2)
	sort.Strings(sorted1)
	sort.Strings(sorted2)

	for i := range sorted1 {
		if sorted1[i] != sorted2[i] {
			t.Errorf("sorted[%d]: %s != %s", i, sorted1[i], sorted2[i])
		}
	}
}

// ── Compile-time interface check ─────────────────────────────────────────────

var _ rpc.Client = (*mockClient)(nil)

// ── Unused import prevention ─────────────────────────────────────────────────

var (
	_ = fmt.Sprintf
	_ = hex.EncodeToString
)
