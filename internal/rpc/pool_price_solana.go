package rpc

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	wsolMint = "So11111111111111111111111111111111111111112"

	pumpfunBondingCurveAccountSize = 49
	pumpfunTokenDecimals           = 6
	solDecimals                    = 9

	// Pump.fun AMM pool state — vault pubkeys after 8-byte discriminator +
	// bump(1) + index(2) + creator(32) + base_mint(32) + quote_mint(32) + lp_mint(32).
	pumpfunAMMBaseVaultOffset  = 139
	pumpfunAMMQuoteVaultOffset = 171

	// Raydium AMM V4 LiquidityState layout — coin/pc vault pubkeys.
	raydiumCoinVaultOffset = 336
	raydiumPCVaultOffset   = 368
	raydiumCoinDecimalsOff = 32
	raydiumPCDecimalsOff   = 40

	defaultPriceCommitment = "confirmed"
)

// PoolResolver looks up the on-chain pool account for a token.
type PoolResolver func(ctx context.Context, chain, tokenAddress string) (poolAddress string, found bool, err error)

// SolanaAccountReader is the minimal RPC surface for pool price reads.
type SolanaAccountReader interface {
	GetAccountInfo(ctx context.Context, pubkey, commitment string) (*AccountInfo, error)
	GetMultipleAccounts(ctx context.Context, pubkeys []string, commitment string) ([]*AccountInfo, error)
}

// SolanaPoolPriceConfig configures the on-chain Solana pool price client.
type SolanaPoolPriceConfig struct {
	RPC                SolanaAccountReader
	PoolResolver       PoolResolver
	CacheTTL           time.Duration
	StaleMaxMultiplier int
	Logger             *slog.Logger
}

// SolanaPoolPriceClient reads bonding-curve / AMM vault reserves via cached RPC.
type SolanaPoolPriceClient struct {
	rpc                SolanaAccountReader
	poolResolver       PoolResolver
	cacheTTL           time.Duration
	staleMaxMultiplier int
	logger             *slog.Logger

	mu    sync.Mutex
	cache map[string]poolPriceCacheEntry
}

type poolPriceCacheEntry struct {
	price     string
	fetchedAt time.Time
	stale     bool
}

// NewSolanaPoolPriceClient returns a client that prices Solana tokens from pool reserves.
func NewSolanaPoolPriceClient(cfg SolanaPoolPriceConfig) *SolanaPoolPriceClient {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	mult := cfg.StaleMaxMultiplier
	if mult <= 0 {
		mult = 3
	}
	return &SolanaPoolPriceClient{
		rpc:                cfg.RPC,
		poolResolver:       cfg.PoolResolver,
		cacheTTL:           ttl,
		staleMaxMultiplier: mult,
		logger:             logger,
		cache:              make(map[string]poolPriceCacheEntry),
	}
}

// GetTokenPrice returns price in SOL per whole token (matches DEXScreener priceNative).
func (c *SolanaPoolPriceClient) GetTokenPrice(ctx context.Context, tokenAddress, chain string) (string, error) {
	if !strings.EqualFold(chain, "solana") {
		return "", fmt.Errorf("pool_price_solana: unsupported chain %q", chain)
	}
	if tokenAddress == "" {
		return "", fmt.Errorf("pool_price_solana: empty token address")
	}
	if c.rpc == nil || c.poolResolver == nil {
		return "", fmt.Errorf("pool_price_solana: not configured")
	}

	key := chain + ":" + tokenAddress
	if price, ok := c.cachedFresh(key); ok {
		return price, nil
	}

	pool, found, err := c.poolResolver(ctx, chain, tokenAddress)
	if err != nil {
		if price, ok := c.cachedStale(key); ok {
			c.logger.Warn("pool_price_stale_fallback", "token", tokenAddress, "reason", "pool_resolver_error", "error", err)
			return price, nil
		}
		return "", fmt.Errorf("pool_price_solana: resolve pool: %w", err)
	}
	if !found || pool == "" {
		if price, ok := c.cachedStale(key); ok {
			c.logger.Warn("pool_price_stale_fallback", "token", tokenAddress, "reason", "pool_not_found")
			return price, nil
		}
		return "", fmt.Errorf("pool_price_solana: no pool for token %s", tokenAddress)
	}

	price, fetchErr := c.fetchPriceFromPool(ctx, pool)
	if fetchErr != nil || price == "" {
		if price, ok := c.cachedStale(key); ok {
			c.logger.Warn("pool_price_stale_fallback", "token", tokenAddress, "pool", pool, "error", fetchErr)
			return price, nil
		}
		if fetchErr != nil {
			return "", fetchErr
		}
		return "", fmt.Errorf("pool_price_solana: empty price for pool %s", pool)
	}

	c.storeCache(key, price, false)
	c.logger.Debug("pool_price_fetched", "token", tokenAddress, "pool", pool, "price_sol", price)
	return price, nil
}

func (c *SolanaPoolPriceClient) cachedFresh(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok {
		return "", false
	}
	if time.Since(entry.fetchedAt) > c.cacheTTL {
		return "", false
	}
	return entry.price, true
}

func (c *SolanaPoolPriceClient) cachedStale(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.cache[key]
	if !ok || entry.price == "" {
		return "", false
	}
	maxStale := c.cacheTTL * time.Duration(c.staleMaxMultiplier)
	if time.Since(entry.fetchedAt) > maxStale {
		return "", false
	}
	return entry.price, true
}

func (c *SolanaPoolPriceClient) storeCache(key, price string, stale bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = poolPriceCacheEntry{price: price, fetchedAt: time.Now(), stale: stale}
}

func (c *SolanaPoolPriceClient) fetchPriceFromPool(ctx context.Context, pool string) (string, error) {
	acc, err := c.rpc.GetAccountInfo(ctx, pool, defaultPriceCommitment)
	if err != nil {
		return "", fmt.Errorf("get_account_info: %w", err)
	}
	if acc == nil {
		return "", fmt.Errorf("pool account not found: %s", pool)
	}
	raw, err := base64.StdEncoding.DecodeString(accountDataB64(acc))
	if err != nil {
		return "", fmt.Errorf("decode pool data: %w", err)
	}

	if len(raw) >= pumpfunBondingCurveAccountSize {
		if price, ok := priceFromBondingCurve(raw); ok {
			return price, nil
		}
	}
	if len(raw) >= pumpfunAMMQuoteVaultOffset+32 {
		if price, err := c.priceFromPumpfunAMMVaults(ctx, raw); err == nil && price != "" {
			return price, nil
		}
	}
	if len(raw) >= raydiumPCVaultOffset+32 {
		if price, err := c.priceFromRaydiumVaults(ctx, raw); err == nil && price != "" {
			return price, nil
		}
	}
	return "", fmt.Errorf("unsupported pool layout for %s (len=%d)", pool, len(raw))
}

func priceFromBondingCurve(data []byte) (string, bool) {
	if len(data) < pumpfunBondingCurveAccountSize {
		return "", false
	}
	virtualToken := binary.LittleEndian.Uint64(data[8:16])
	virtualSol := binary.LittleEndian.Uint64(data[16:24])
	realToken := binary.LittleEndian.Uint64(data[24:32])
	realSol := binary.LittleEndian.Uint64(data[32:40])
	solLamports := virtualSol + realSol
	tokenRaw := virtualToken + realToken
	return formatSolPerToken(solLamports, tokenRaw, pumpfunTokenDecimals), solLamports > 0 && tokenRaw > 0
}

func (c *SolanaPoolPriceClient) priceFromPumpfunAMMVaults(ctx context.Context, poolData []byte) (string, error) {
	baseVault := pubkeyFromBytes(poolData[pumpfunAMMBaseVaultOffset : pumpfunAMMBaseVaultOffset+32])
	quoteVault := pubkeyFromBytes(poolData[pumpfunAMMQuoteVaultOffset : pumpfunAMMQuoteVaultOffset+32])
	if baseVault == "" || quoteVault == "" {
		return "", fmt.Errorf("invalid pumpfun amm vault pubkeys")
	}
	accounts, err := c.rpc.GetMultipleAccounts(ctx, []string{baseVault, quoteVault}, defaultPriceCommitment)
	if err != nil {
		return "", err
	}
	if len(accounts) < 2 || accounts[0] == nil || accounts[1] == nil {
		return "", fmt.Errorf("pumpfun amm vault accounts missing")
	}
	baseRaw, err := decodeSPLTokenAmount(accountDataB64(accounts[0]))
	if err != nil {
		return "", err
	}
	quoteRaw, err := decodeSPLTokenAmount(accountDataB64(accounts[1]))
	if err != nil {
		return "", err
	}
	// CreatePool layout: base vault = token, quote vault = SOL.
	price := formatSolPerToken(quoteRaw, baseRaw, pumpfunTokenDecimals)
	if price == "" {
		return "", fmt.Errorf("zero pumpfun amm reserves")
	}
	return price, nil
}

func (c *SolanaPoolPriceClient) priceFromRaydiumVaults(ctx context.Context, poolData []byte) (string, error) {
	coinVault := pubkeyFromBytes(poolData[raydiumCoinVaultOffset : raydiumCoinVaultOffset+32])
	pcVault := pubkeyFromBytes(poolData[raydiumPCVaultOffset : raydiumPCVaultOffset+32])
	if coinVault == "" || pcVault == "" {
		return "", fmt.Errorf("invalid raydium vault pubkeys")
	}
	coinDecimals := int(binary.LittleEndian.Uint64(poolData[raydiumCoinDecimalsOff : raydiumCoinDecimalsOff+8]))
	pcDecimals := int(binary.LittleEndian.Uint64(poolData[raydiumPCDecimalsOff : raydiumPCDecimalsOff+8]))
	if coinDecimals <= 0 || coinDecimals > 18 {
		coinDecimals = pumpfunTokenDecimals
	}
	if pcDecimals <= 0 || pcDecimals > 18 {
		pcDecimals = solDecimals
	}

	accounts, err := c.rpc.GetMultipleAccounts(ctx, []string{coinVault, pcVault}, defaultPriceCommitment)
	if err != nil {
		return "", err
	}
	if len(accounts) < 2 || accounts[0] == nil || accounts[1] == nil {
		return "", fmt.Errorf("raydium vault accounts missing")
	}
	coinMint, coinAmt, err := decodeSPLTokenAccount(accountDataB64(accounts[0]))
	if err != nil {
		return "", err
	}
	pcMint, pcAmt, err := decodeSPLTokenAccount(accountDataB64(accounts[1]))
	if err != nil {
		return "", err
	}

	switch {
	case coinMint == wsolMint:
		return formatSolPerToken(coinAmt, pcAmt, pcDecimals), nil
	case pcMint == wsolMint:
		return formatSolPerToken(pcAmt, coinAmt, coinDecimals), nil
	default:
		return "", fmt.Errorf("raydium pool has no WSOL leg")
	}
}

func accountDataB64(acc *AccountInfo) string {
	if acc == nil || len(acc.Data) == 0 {
		return ""
	}
	return acc.Data[0]
}

func decodeSPLTokenAmount(dataB64 string) (uint64, error) {
	_, amount, err := decodeSPLTokenAccount(dataB64)
	return amount, err
}

func decodeSPLTokenAccount(dataB64 string) (mint string, amount uint64, err error) {
	raw, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return "", 0, err
	}
	if len(raw) < 72 {
		return "", 0, fmt.Errorf("spl token account too short: %d", len(raw))
	}
	mint = pubkeyFromBytes(raw[0:32])
	amount = binary.LittleEndian.Uint64(raw[64:72])
	return mint, amount, nil
}

func formatSolPerToken(solLamports, tokenRaw uint64, tokenDecimals int) string {
	if solLamports == 0 || tokenRaw == 0 {
		return ""
	}
	tokenScale := math.Pow10(tokenDecimals)
	price := (float64(solLamports) / math.Pow10(solDecimals)) / (float64(tokenRaw) / tokenScale)
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return ""
	}
	return strconv.FormatFloat(price, 'f', -1, 64)
}

func pubkeyFromBytes(b []byte) string {
	if len(b) != 32 {
		return ""
	}
	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return ""
	}
	return base58Encode(b)
}

// base58Alphabet is the Bitcoin/Solana base58 alphabet.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func base58Encode(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	zeros := 0
	for zeros < len(input) && input[zeros] == 0 {
		zeros++
	}
	num := make([]byte, len(input))
	copy(num, input)
	var encoded []byte
	for len(num) > 0 {
		carry := 0
		for i := len(num) - 1; i >= 0; i-- {
			val := int(num[i]) + carry*256
			num[i] = byte(val / 58)
			carry = val % 58
		}
		encoded = append(encoded, base58Alphabet[carry])
		for len(num) > 0 && num[0] == 0 {
			num = num[1:]
		}
	}
	for i := 0; i < zeros; i++ {
		encoded = append(encoded, '1')
	}
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	return string(encoded)
}

// RoutingPriceClient uses on-chain Solana pricing with DEXScreener fallback.
type RoutingPriceClient struct {
	solana   *SolanaPoolPriceClient
	fallback *DEXScreenerPriceClient
	logger   *slog.Logger
}

// NewRoutingPriceClient returns a composite price client.
func NewRoutingPriceClient(solana *SolanaPoolPriceClient, fallback *DEXScreenerPriceClient, logger *slog.Logger) *RoutingPriceClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &RoutingPriceClient{solana: solana, fallback: fallback, logger: logger}
}

// GetTokenPrice delegates to on-chain Solana pricing when available.
func (c *RoutingPriceClient) GetTokenPrice(ctx context.Context, tokenAddress, chain string) (string, error) {
	if strings.EqualFold(chain, "solana") && c.solana != nil {
		price, err := c.solana.GetTokenPrice(ctx, tokenAddress, chain)
		if err == nil && price != "" {
			return price, nil
		}
		if err != nil {
			c.logger.Debug("pool_price_miss", "token", tokenAddress, "error", err)
		}
	}
	if c.fallback == nil {
		return "", fmt.Errorf("routing price client: no fallback configured")
	}
	return c.fallback.GetTokenPrice(ctx, tokenAddress, chain)
}

// NewConfiguredPriceClient builds the price client from application config.
func NewConfiguredPriceClient(
	cfg PriceOracleModeConfig,
	solRPC SolanaAccountReader,
	poolResolver PoolResolver,
	logger *slog.Logger,
) *RoutingPriceClient {
	fallback := NewDEXScreenerPriceClient(logger)
	if !strings.EqualFold(cfg.Mode, "on_chain") || solRPC == nil || poolResolver == nil {
		return NewRoutingPriceClient(nil, fallback, logger)
	}
	pool := NewSolanaPoolPriceClient(SolanaPoolPriceConfig{
		RPC:                solRPC,
		PoolResolver:       poolResolver,
		CacheTTL:           cfg.CacheTTL,
		StaleMaxMultiplier: cfg.StaleMaxMultiplier,
		Logger:             logger,
	})
	return NewRoutingPriceClient(pool, fallback, logger)
}

// PriceOracleModeConfig is a minimal config view for rpc package wiring.
type PriceOracleModeConfig struct {
	Mode               string
	CacheTTL           time.Duration
	StaleMaxMultiplier int
}
