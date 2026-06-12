# Anti-Manipulation Skill

## Purpose

Implement the five concrete detection algorithms that form the Data Quality Engine's
adaptive firewall. Each detector outputs a `[0,1]` risk contribution. The final
`RiskScore` is a weighted aggregate across all five.

**Invariant:** If DataQuality → 0, Profit → 0. This is the non-negotiable gate.

---

## Rules

### Risk Score Aggregation (Weighted)

```go
// Final RiskScore = weighted sum of all five detector contributions
type DetectorWeights struct {
    WashTrading    float64  // w_wash
    RugPull        float64  // w_rug
    Honeypot       float64  // w_honeypot
    FakeLiquidity  float64  // w_fakeliq
    TaxManipulation float64 // w_tax
}

func aggregateRisk(
    washRisk, rugRisk, honeypotRisk, fakeLiqRisk, taxRisk float64,
    weights DetectorWeights,
) float64 {
    return washRisk*weights.WashTrading +
        rugRisk*weights.RugPull +
        honeypotRisk*weights.Honeypot +
        fakeLiqRisk*weights.FakeLiquidity +
        taxRisk*weights.TaxManipulation
}

// Decision from aggregated score + mode thresholds
func makeDecision(riskScore float64, flags []string, thresholds ThresholdProfile) string {
    // Hard override: honeypot sell fail is always reject regardless of score
    for _, f := range flags {
        if f == "HONEYPOT_SELL_FAIL" { return "reject" }
        if f == "SELL_BLOCKED" { return "reject" }  // cannot sell = honeypot
    }

    if riskScore >= thresholds.RejectAbove   { return "reject" }
    if riskScore >= thresholds.RiskyPassAbove { return "risky-pass" }
    return "pass"
}
```

### Detector 1: Wash Trading (Shannon Entropy)

```go
// Signals: tx_count_1m, unique_wallets_1m, wallet_entropy, repeat_ratio
func detectWashTrading(
    txCount1m int,
    uniqueWallets1m int,
    wallets []string,
    cfg WashTradingConfig,
) (risk float64, flags []string) {
    // Ratio check
    if uniqueWallets1m == 0 { uniqueWallets1m = 1 }
    txPerWalletRatio := float64(txCount1m) / float64(uniqueWallets1m)
    txRatioNorm := math.Min(txPerWalletRatio/cfg.MaxTxPerWalletRatio, 1.0)

    // Shannon entropy over wallet frequency distribution
    walletEntropy := shannonEntropy(wallets)  // higher = more diverse = less wash
    entropyNorm := 1.0 - math.Min(walletEntropy/cfg.MaxExpectedEntropy, 1.0)

    // Repeat ratio: wallets that appear more than once
    repeatRatio := computeRepeatRatio(wallets)

    risk = cfg.Weights.TxRatio*txRatioNorm +
        cfg.Weights.LowEntropy*entropyNorm +
        cfg.Weights.RepeatRatio*repeatRatio

    if uniqueWallets1m < cfg.MinUniqueWallets { flags = append(flags, "WASH_LOW_UNIQUENESS") }
    if repeatRatio > cfg.MaxRepeatRatio       { flags = append(flags, "WASH_LOOP_TRADES") }
    return math.Min(risk, 1.0), flags
}

// Shannon entropy: H = -Σ p_i * log2(p_i)
func shannonEntropy(wallets []string) float64 {
    freq := make(map[string]int)
    for _, w := range wallets { freq[w]++ }
    total := float64(len(wallets))
    var h float64
    for _, count := range freq {
        p := float64(count) / total
        h -= p * math.Log2(p)
    }
    return h
}
```

### Detector 2: Rug Pull Risk

```go
// Signals: LP lock status, owner privileges, holder concentration
func detectRugPull(
    lpLockStrength float64,  // [0,1]: 0=unlocked, 1=permanently burned
    ownerPrivileges []string, // ["mint", "setTax", "blacklist", "pause", "upgrade"]
    top5HolderPct float64,   // e.g., 0.75 = top 5 holders own 75%
    cfg RugPullConfig,
) (risk float64, flags []string) {
    lockRisk := 1.0 - lpLockStrength

    // Privilege score: weighted by severity
    privilegeScore := computePrivilegeScore(ownerPrivileges, cfg.PrivilegeWeights)

    concentrationNorm := math.Min(top5HolderPct/cfg.MaxHolderConcentration, 1.0)

    risk = cfg.Weights.LPLock*lockRisk +
        cfg.Weights.OwnerPrivilege*privilegeScore +
        cfg.Weights.Concentration*concentrationNorm

    if lpLockStrength < cfg.MinLPLockStrength        { flags = append(flags, "LP_UNLOCKED") }
    if privilegeScore > cfg.PrivilegeRiskThreshold   { flags = append(flags, "OWNER_PRIVILEGED") }
    if top5HolderPct > cfg.MaxHolderConcentration    { flags = append(flags, "HOLDER_CONCENTRATED") }
    return math.Min(risk, 1.0), flags
}

func computePrivilegeScore(privileges []string, weights map[string]float64) float64 {
    var score float64
    for _, priv := range privileges {
        score += weights[priv]  // e.g., "mint" = 1.0, "setTax" = 0.7, "blacklist" = 0.9
    }
    return math.Min(score, 1.0)
}
```

### Detector 3: Honeypot Simulation (callStatic)

```go
// Simulate buy → sell using callStatic on the router — no real transactions
// This is the ONLY reliable honeypot test
func detectHoneypot(
    ctx context.Context,
    tokenAddress string,
    routerAddress string,
    rpcPool *RPCPool,
    cfg HoneypotConfig,
) (risk float64, flags []string, err error) {
    // Dry-run buy (callStatic — not executed on-chain)
    buyResult, err := callStaticSwap(ctx, rpcPool, routerAddress, SwapParams{
        TokenIn:  cfg.WETHAddress,
        TokenOut: tokenAddress,
        Amount:   cfg.SimulationAmountWETH,
    })
    if err != nil { return 1.0, []string{"HONEYPOT_BUY_FAIL"}, nil }

    // Dry-run sell
    sellResult, err := callStaticSwap(ctx, rpcPool, routerAddress, SwapParams{
        TokenIn:  tokenAddress,
        TokenOut: cfg.WETHAddress,
        Amount:   buyResult.TokensOut,
    })
    if err != nil {
        // Sell reverts = definitive honeypot
        return 1.0, []string{"HONEYPOT_SELL_FAIL", "SELL_BLOCKED"}, nil
    }

    // Effective sell tax
    effectiveTax := 1.0 - (sellResult.ETHOut / buyResult.ExpectedETHOut)
    if effectiveTax > cfg.MaxSellTax {
        flags = append(flags, "SELL_TAX_HIGH")
        risk = effectiveTax  // proportional to effective tax
    }

    return math.Min(risk, 1.0), flags, nil
}
```

### Detector 4: Fake Liquidity

```go
// Signals: LP add/remove events, LP token lock status, liquidity volatility
func detectFakeLiquidity(
    lpEvents []LPEvent,  // sorted by timestamp
    lpLockStrength float64,
    cfg FakeLiquidityConfig,
) (risk float64, flags []string) {
    // Short-term add→remove within config.short_window_sec
    flashIndicator := computeFlashAddRemove(lpEvents, cfg.ShortWindowSec)

    // Liquidity volatility (std dev / mean of liquidity changes)
    liquidityVol := computeLiquidityVolatility(lpEvents)
    volNorm := math.Min(liquidityVol/cfg.MaxExpectedVolatility, 1.0)

    lockRisk := 1.0 - lpLockStrength

    risk = cfg.Weights.FlashIndicator*flashIndicator +
        cfg.Weights.Volatility*volNorm +
        cfg.Weights.LPUnlocked*lockRisk

    if flashIndicator > cfg.FlashThreshold { flags = append(flags, "LP_FLASH_ADDED_REMOVED") }
    if volNorm > cfg.VolatilityThreshold   { flags = append(flags, "LP_VOLATILE") }
    if lpLockStrength < cfg.MinLockStrength { flags = append(flags, "LP_NOT_LOCKED") }
    return math.Min(risk, 1.0), flags
}
```

### Detector 5: Tax Manipulation

```go
// Signals: buy/sell tax from simulation, dynamic tax capability, tax asymmetry
func detectTaxManipulation(
    buyTax float64,
    sellTax float64,
    hasDynamicTax bool,
    cfg TaxConfig,
) (risk float64, flags []string) {
    // Effective tax risk (use sell tax as worst case)
    taxNorm := math.Min(sellTax/cfg.MaxAcceptableTax, 1.0)

    // Dynamic tax: contract can change tax at any time
    dynamicFlag := 0.0
    if hasDynamicTax { dynamicFlag = 1.0; flags = append(flags, "TAX_DYNAMIC") }

    // Asymmetry: buy tax much lower than sell tax (bait-and-switch)
    asymmetry := 0.0
    if sellTax > buyTax*cfg.MaxTaxAsymmetryRatio {
        asymmetry = 1.0
        flags = append(flags, "TAX_ASYMMETRIC")
    }

    if sellTax > cfg.MaxAcceptableTax { flags = append(flags, "SELL_TAX_HIGH") }

    risk = cfg.Weights.TaxNorm*taxNorm +
        cfg.Weights.DynamicTax*dynamicFlag +
        cfg.Weights.Asymmetry*asymmetry
    return math.Min(risk, 1.0), flags
}
```

### Adaptive Threshold Controller

```go
// Monitor FP/FN rates and adjust thresholds within bounds
func adaptThresholds(
    current ThresholdProfile,
    fpRate float64,   // accepted tokens that rugged
    fnRate float64,   // rejected tokens that pumped
    cfg AdaptiveConfig,
) ThresholdProfile {
    updated := current  // copy

    if fpRate > cfg.FPAlertThreshold {
        // Tighten: more rejections needed
        updated.RejectAbove = boundedUpdate(current.RejectAbove, current.RejectAbove*0.95, cfg.MaxDeltaPct)
    } else if fnRate > cfg.FNAlertThreshold {
        // Relax: missing too many good tokens
        updated.RejectAbove = boundedUpdate(current.RejectAbove, current.RejectAbove*1.05, cfg.MaxDeltaPct)
    }
    return updated
}
```

### Detector 6: Liquidity Cascade Detection

```go
// Detects coordinated fake liquidity injection followed by a dump cascade.
// This pattern injects liquidity to attract buyers then withdraws rapidly
// causing a price and liquidity collapse.
//
// Risk contribution weight: w_liquidation (config: data_quality.yaml)
//
// Flags emitted: "FAKE_LIQUIDITY_CASCADE"

type LiquidityCascadeInput struct {
    Prices    []float64  // time-ordered price bars
    Volumes   []float64  // matching volume bars
    Liquidity []float64  // pool liquidity bars
}

// ComputeVolumeSpike returns the ratio of current volume to baseline (rolling avg).
// Threshold: 3.0× baseline (crypto default). Configurable.
func ComputeVolumeSpike(volumes []float64, lookback int) float64 {
    if len(volumes) == 0 || lookback <= 0 { return 0 }
    window := volumes
    if len(volumes) > lookback+1 {
        window = volumes[:len(volumes)-1]
    }
    if len(window) > lookback {
        window = window[len(window)-lookback:]
    }
    var sum float64
    for _, v := range window {
        sum += v
    }
    baseline := sum / float64(len(window))
    if baseline == 0 { return 0 }
    current := volumes[len(volumes)-1]
    return current / baseline
}

// DetectLiquidationCascade returns true when price drops ≥3% AND volume
// spikes ≥5× baseline within a 5-bar rolling window.
func DetectLiquidationCascade(
    prices  []float64,
    volumes []float64,
    cfg     CascadeConfig,  // from config: min_price_drop=0.03, volume_spike=5.0, window=5
) (bool, float64) {
    n := len(prices)
    w := cfg.WindowBars
    if n < w+1 { return false, 0 }

    windowPrices := prices[n-w-1 : n]
    windowVols   := volumes[n-w : n]

    startPrice := windowPrices[0]
    endPrice   := windowPrices[len(windowPrices)-1]
    if startPrice == 0 { return false, 0 }

    priceDrop := (startPrice - endPrice) / startPrice

    spikeRatio := ComputeVolumeSpike(append(volumes[:n-w], windowVols...), 20)

    cascadeActive := priceDrop >= cfg.MinPriceDrop && spikeRatio >= cfg.VolumeSpikeMultiplier
    return cascadeActive, priceDrop
}

// DetectFakeLiquidityCascade is the main entry point for Detector 6.
// Returns a [0,1] risk contribution and the FAKE_LIQUIDITY_CASCADE flag if triggered.
func DetectFakeLiquidityCascade(
    input LiquidityCascadeInput,
    cfg   CascadeConfig,
) (riskContribution float64, flags []string) {
    cascade, dropPct := DetectLiquidationCascade(input.Prices, input.Volumes, cfg)
    if !cascade {
        return 0.0, nil
    }

    // Scale risk contribution by severity: deeper drop = higher risk
    // cap contribution at 1.0
    severity := dropPct / 0.10  // 10% drop = max severity
    if severity > 1.0 { severity = 1.0 }

    return severity, []string{"FAKE_LIQUIDITY_CASCADE"}
}
```

**Integration — add to `aggregateRisk`:**

```go
// Updated 6-detector aggregation (add w_liquidation to DetectorWeights):
type DetectorWeights struct {
    WashTrading     float64  // w_wash
    RugPull         float64  // w_rug
    Honeypot        float64  // w_honeypot
    FakeLiquidity   float64  // w_fakeliq
    TaxManipulation float64  // w_tax
    Liquidation     float64  // w_liquidation (new)
}

func aggregateRiskSixDetectors(
    washRisk, rugRisk, honeypotRisk, fakeLiqRisk, taxRisk, cascadeRisk float64,
    weights DetectorWeights,
) float64 {
    return washRisk*weights.WashTrading +
        rugRisk*weights.RugPull +
        honeypotRisk*weights.Honeypot +
        fakeLiqRisk*weights.FakeLiquidity +
        taxRisk*weights.TaxManipulation +
        cascadeRisk*weights.Liquidation
}
```

> See `.github/skills/liquidity-event-detector/SKILL.md` for the full
> `ComputeOrderImbalance()` and composite liquidity score implementation.

---

### Anti-Patterns

```go
// ❌ Only checking buy simulation (not sell)
simulateBuy(token)  // Without sell check — misses all honeypots

// ❌ Ignoring SELL_BLOCKED flag
if riskScore > threshold { return "reject" }  // Misses honeypot with low score but sell failure

// ❌ Hardcoded weights
washRisk * 0.3 + rugRisk * 0.4  // Wrong — must come from config

// ❌ No Shannon entropy — just wallet count
if uniqueWallets < 5 { return highRisk }  // Too simple — misses coordinated wash

// ✅ Correct: hard override on sell failure
for _, f := range flags {
    if f == "HONEYPOT_SELL_FAIL" { return "reject" }
}
```

---

## Checklist

```
[ ] Six detectors: wash, rug, honeypot, fakeliq, tax, liquidity_cascade
[ ] Honeypot uses callStatic (dry-run) — never real transactions
[ ] HONEYPOT_SELL_FAIL = hard reject regardless of aggregate score
[ ] Shannon entropy computed for wash trading (not just wallet count)
[ ] Privilege weights configurable: mint/setTax/blacklist/pause/upgrade
[ ] LP lock strength: [0,1] scale (0=unlocked, 1=burned)
[ ] All weights and thresholds loaded from config/data_quality.yaml
[ ] Adaptive controller bounded: Δthreshold ≤ MaxDeltaPct per cycle
[ ] FP and FN signals both tracked (requires shadow observer for FN)
[ ] Three-outcome decision: "pass" | "risky-pass" | "reject"
[ ] FAKE_LIQUIDITY_CASCADE flag triggers even if aggregate score is low
[ ] CascadeConfig thresholds (min_price_drop, volume_spike, window) from config
[ ] Six detectors: wash, rug, honeypot, fakeliq, tax
[ ] Detector outputs are deterministic (same input = same output)
```

---

## References

- Architecture context: `docs/architecture-context/3_data_quality_engine.md`
- Architecture: `docs/architecture.md` § 3.1 (Data Quality Engine)
- DTO spec: `docs/dto_contracts.md` § 3.2 (DataQualityDTO)
- Roadmap: `docs/implementation_roadmap.md` Phase 2.1
- Config: `config/data_quality.yaml`
- `.github/skills/liquidity-event-detector/SKILL.md` — Full cascade + imbalance algorithms