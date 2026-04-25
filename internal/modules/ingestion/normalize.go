package ingestion

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"crypto-sniping-bot/contracts"
	ingestioninternal "crypto-sniping-bot/internal/modules/ingestion/internal"
	"crypto-sniping-bot/internal/rpc"
)

// NormalizePairCreated converts a PairCreated factory log into a MarketDataDTO.
//
// PairCreated(address indexed token0, address indexed token1, address pair, uint256 allPairsLength)
//   Topics[0] = TopicPairCreated
//   Topics[1] = token0 (indexed, 32-byte padded)
//   Topics[2] = token1 (indexed, 32-byte padded)
//   Data      = pair address (32 bytes) + allPairsLength (32 bytes)
func NormalizePairCreated(
	l rpc.Log, chain, market, endpoint, versionID string,
	baseTokens []string, confirmDepth uint32, ingestedAt string,
) (contracts.MarketDataDTO, error) {
	if len(l.Topics) < 3 {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize PairCreated: need ≥3 topics, got %d", len(l.Topics))
	}

	token0 := topicToAddress(l.Topics[1])
	token1 := topicToAddress(l.Topics[2])

	pairWord, err := decodeWord(l.Data, 0)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize PairCreated: decode pair address: %w", err)
	}
	poolAddress := wordToAddress(pairWord)

	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(token0, token1, baseTokens)
	if err != nil {
		// Unknown base — use token0/token1 as-is; downstream DQ will reject if needed.
		tokenAddr, baseAddr = token0, token1
	}

	eventID := marketDataEventID(chain, l.TxHash, l.LogIndex)
	traceID := eventID
	corrID := contracts.ContentIDFromString(traceID + "|" + strconv.FormatUint(l.BlockNumber, 10))

	return contracts.MarketDataDTO{
		EventID:           eventID,
		TraceID:           traceID,
		CorrelationID:     corrID,
		CausationID:       "",
		VersionID:         versionID,
		Chain:             chain,
		Market:            market,
		BlockNumber:       l.BlockNumber,
		BlockHash:         l.BlockHash,
		TxHash:            l.TxHash,
		LogIndex:          l.LogIndex,
		EventTopic:        TopicPairCreated,
		PoolAddress:       poolAddress,
		TokenAddress:      tokenAddr,
		BaseAddress:       baseAddr,
		Token0Address:     token0,
		Token1Address:     token1,
		Amount0Raw:        "0",
		Amount1Raw:        "0",
		ReserveBaseRaw:    "0",
		ReserveTokenRaw:   "0",
		BlockTimestamp:    l.BlockTimestamp,
		IngestedAt:        ingestedAt,
		RpcEndpoint:       endpoint,
		Transport:         "websocket",
		ConfirmationDepth: confirmDepth,
		Reorged:           l.Removed,
		ExpiresAt:         "",
		Priority:          0,
	}, nil
}

// NormalizeMint converts a Mint pair log into a MarketDataDTO.
//
// Mint(address indexed sender, uint256 amount0, uint256 amount1)
//   Topics[0] = TopicMint
//   Topics[1] = sender (indexed)
//   Data      = amount0 (32 bytes) + amount1 (32 bytes)
func NormalizeMint(
	l rpc.Log, chain, market, endpoint, versionID string,
	token0Addr, token1Addr string, baseTokens []string,
	confirmDepth uint32, ingestedAt string,
) (contracts.MarketDataDTO, error) {
	if len(l.Topics) < 1 {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Mint: need ≥1 topics, got %d", len(l.Topics))
	}

	amount0, err := decodeWordBigStr(l.Data, 0)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Mint: decode amount0: %w", err)
	}
	amount1, err := decodeWordBigStr(l.Data, 1)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Mint: decode amount1: %w", err)
	}

	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(token0Addr, token1Addr, baseTokens)
	if err != nil {
		tokenAddr, baseAddr = token0Addr, token1Addr
	}

	eventID := marketDataEventID(chain, l.TxHash, l.LogIndex)
	traceID := eventID
	corrID := contracts.ContentIDFromString(traceID + "|" + strconv.FormatUint(l.BlockNumber, 10))

	amount0Raw, amount1Raw := amount0, amount1
	reserveBase, reserveToken := reorderAmounts(token0Addr, baseAddr, amount0Raw, amount1Raw)

	return contracts.MarketDataDTO{
		EventID:           eventID,
		TraceID:           traceID,
		CorrelationID:     corrID,
		CausationID:       "",
		VersionID:         versionID,
		Chain:             chain,
		Market:            market,
		BlockNumber:       l.BlockNumber,
		BlockHash:         l.BlockHash,
		TxHash:            l.TxHash,
		LogIndex:          l.LogIndex,
		EventTopic:        TopicMint,
		PoolAddress:       l.Address,
		TokenAddress:      tokenAddr,
		BaseAddress:       baseAddr,
		Token0Address:     token0Addr,
		Token1Address:     token1Addr,
		Amount0Raw:        amount0Raw,
		Amount1Raw:        amount1Raw,
		ReserveBaseRaw:    reserveBase,
		ReserveTokenRaw:   reserveToken,
		BlockTimestamp:    l.BlockTimestamp,
		IngestedAt:        ingestedAt,
		RpcEndpoint:       endpoint,
		Transport:         "websocket",
		ConfirmationDepth: confirmDepth,
		Reorged:           l.Removed,
		ExpiresAt:         "",
		Priority:          0,
	}, nil
}

// NormalizeSwap converts a Swap pair log into a MarketDataDTO.
//
// Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)
//   Topics[0] = TopicSwap
//   Topics[1] = sender (indexed)
//   Topics[2] = to (indexed)
//   Data      = amount0In(32) + amount1In(32) + amount0Out(32) + amount1Out(32)
func NormalizeSwap(
	l rpc.Log, chain, market, endpoint, versionID string,
	token0Addr, token1Addr string, baseTokens []string,
	confirmDepth uint32, ingestedAt string,
) (contracts.MarketDataDTO, error) {
	if len(l.Topics) < 1 {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Swap: need ≥1 topics, got %d", len(l.Topics))
	}

	amount0In, err := decodeWordBigStr(l.Data, 0)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Swap: decode amount0In: %w", err)
	}
	amount1In, err := decodeWordBigStr(l.Data, 1)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Swap: decode amount1In: %w", err)
	}
	amount0Out, err := decodeWordBigStr(l.Data, 2)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Swap: decode amount0Out: %w", err)
	}
	amount1Out, err := decodeWordBigStr(l.Data, 3)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Swap: decode amount1Out: %w", err)
	}

	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(token0Addr, token1Addr, baseTokens)
	if err != nil {
		tokenAddr, baseAddr = token0Addr, token1Addr
	}

	// Net amount for each side: in - out (signed flow).
	amount0Net := bigSubStr(amount0In, amount0Out)
	amount1Net := bigSubStr(amount1In, amount1Out)

	reserveBase, reserveToken := reorderAmounts(token0Addr, baseAddr, amount0Net, amount1Net)

	_ = amount0Out
	_ = amount1Out

	eventID := marketDataEventID(chain, l.TxHash, l.LogIndex)
	traceID := eventID
	corrID := contracts.ContentIDFromString(traceID + "|" + strconv.FormatUint(l.BlockNumber, 10))

	return contracts.MarketDataDTO{
		EventID:           eventID,
		TraceID:           traceID,
		CorrelationID:     corrID,
		CausationID:       "",
		VersionID:         versionID,
		Chain:             chain,
		Market:            market,
		BlockNumber:       l.BlockNumber,
		BlockHash:         l.BlockHash,
		TxHash:            l.TxHash,
		LogIndex:          l.LogIndex,
		EventTopic:        TopicSwap,
		PoolAddress:       l.Address,
		TokenAddress:      tokenAddr,
		BaseAddress:       baseAddr,
		Token0Address:     token0Addr,
		Token1Address:     token1Addr,
		Amount0Raw:        amount0In,
		Amount1Raw:        amount1In,
		ReserveBaseRaw:    reserveBase,
		ReserveTokenRaw:   reserveToken,
		BlockTimestamp:    l.BlockTimestamp,
		IngestedAt:        ingestedAt,
		RpcEndpoint:       endpoint,
		Transport:         "websocket",
		ConfirmationDepth: confirmDepth,
		Reorged:           l.Removed,
		ExpiresAt:         "",
		Priority:          0,
	}, nil
}

// NormalizeBurn converts a Burn pair log into a MarketDataDTO.
//
// Burn(address indexed sender, uint256 amount0, uint256 amount1, address indexed to)
//   Topics[0] = TopicBurn
//   Topics[1] = sender (indexed)
//   Topics[2] = to (indexed)
//   Data      = amount0 (32 bytes) + amount1 (32 bytes)
func NormalizeBurn(
	l rpc.Log, chain, market, endpoint, versionID string,
	token0Addr, token1Addr string, baseTokens []string,
	confirmDepth uint32, ingestedAt string,
) (contracts.MarketDataDTO, error) {
	if len(l.Topics) < 1 {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Burn: need ≥1 topics, got %d", len(l.Topics))
	}

	amount0, err := decodeWordBigStr(l.Data, 0)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Burn: decode amount0: %w", err)
	}
	amount1, err := decodeWordBigStr(l.Data, 1)
	if err != nil {
		return contracts.MarketDataDTO{}, fmt.Errorf("normalize Burn: decode amount1: %w", err)
	}

	tokenAddr, baseAddr, err := ingestioninternal.WalletSide(token0Addr, token1Addr, baseTokens)
	if err != nil {
		tokenAddr, baseAddr = token0Addr, token1Addr
	}

	reserveBase, reserveToken := reorderAmounts(token0Addr, baseAddr, amount0, amount1)

	eventID := marketDataEventID(chain, l.TxHash, l.LogIndex)
	traceID := eventID
	corrID := contracts.ContentIDFromString(traceID + "|" + strconv.FormatUint(l.BlockNumber, 10))

	return contracts.MarketDataDTO{
		EventID:           eventID,
		TraceID:           traceID,
		CorrelationID:     corrID,
		CausationID:       "",
		VersionID:         versionID,
		Chain:             chain,
		Market:            market,
		BlockNumber:       l.BlockNumber,
		BlockHash:         l.BlockHash,
		TxHash:            l.TxHash,
		LogIndex:          l.LogIndex,
		EventTopic:        TopicBurn,
		PoolAddress:       l.Address,
		TokenAddress:      tokenAddr,
		BaseAddress:       baseAddr,
		Token0Address:     token0Addr,
		Token1Address:     token1Addr,
		Amount0Raw:        amount0,
		Amount1Raw:        amount1,
		ReserveBaseRaw:    reserveBase,
		ReserveTokenRaw:   reserveToken,
		BlockTimestamp:    l.BlockTimestamp,
		IngestedAt:        ingestedAt,
		RpcEndpoint:       endpoint,
		Transport:         "websocket",
		ConfirmationDepth: confirmDepth,
		Reorged:           l.Removed,
		ExpiresAt:         "",
		Priority:          0,
	}, nil
}

// ── ABI decoding helpers ──────────────────────────────────────────────────────

// topicToAddress extracts the last 20 bytes of a 32-byte topic as a hex address.
func topicToAddress(topic string) string {
	clean := strings.TrimPrefix(topic, "0x")
	if len(clean) < 40 {
		return ""
	}
	return "0x" + strings.ToLower(clean[len(clean)-40:])
}

// decodeWord decodes the nth 32-byte word from hex data (no 0x prefix required).
func decodeWord(hexData string, wordIndex int) ([]byte, error) {
	clean := strings.TrimPrefix(hexData, "0x")
	start := wordIndex * 64
	end := start + 64
	if len(clean) < end {
		return nil, fmt.Errorf("data too short: need %d hex chars, have %d", end, len(clean))
	}
	return hex.DecodeString(clean[start:end])
}

// decodeWordBigStr decodes the nth 32-byte word as a base-10 big.Int string.
func decodeWordBigStr(hexData string, wordIndex int) (string, error) {
	word, err := decodeWord(hexData, wordIndex)
	if err != nil {
		return "", err
	}
	n := new(big.Int).SetBytes(word)
	return n.String(), nil
}

// wordToAddress extracts the 20-byte address from a 32-byte ABI word.
func wordToAddress(word []byte) string {
	if len(word) < 32 {
		return ""
	}
	return "0x" + hex.EncodeToString(word[12:32])
}

// bigSubStr computes a - b as a big integer string (may be negative).
func bigSubStr(a, b string) string {
	ai := new(big.Int)
	bi := new(big.Int)
	ai.SetString(a, 10) //nolint:errcheck
	bi.SetString(b, 10) //nolint:errcheck
	return new(big.Int).Sub(ai, bi).String()
}

// reorderAmounts returns (baseAmount, tokenAmount) given token0Address and baseAddress.
// If token0 == baseAddr, base is amount0 and token is amount1; otherwise swapped.
func reorderAmounts(token0Addr, baseAddr, amount0, amount1 string) (baseAmount, tokenAmount string) {
	if strings.EqualFold(token0Addr, baseAddr) {
		return amount0, amount1
	}
	return amount1, amount0
}

// marketDataEventID derives the content-addressable EventID for a market data event.
// EventID = SHA256(chain | txHash | logIndex)[:16]
func marketDataEventID(chain, txHash string, logIndex uint32) string {
	return contracts.ContentIDFromString(
		chain + "|" + txHash + "|" + strconv.FormatUint(uint64(logIndex), 10),
	)
}
