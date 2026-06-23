---
name: exposure-monitor
type: skill
description: >
  Portfolio exposure monitoring with hard limits enforced BEFORE allocation.
  Use when implementing or reviewing the Capital Engine (Layer 7) allocation
  gate and the position sizing limits. Hard limits: 80% max portfolio exposure,
  20 concurrent positions, 0.5% single position (5% absolute ceiling).
  Exposure computed from current market value, not entry value.
---

# Exposure Monitor Skill

## Purpose

Prevent capital concentration risk by computing and enforcing portfolio exposure
limits before any new allocation is approved. This is a **pre-trade gate** — it
runs in the Capital Engine before producing `AllocationDTO`, not after execution.

**Hard limits (from config, never hardcoded):**

```
Total portfolio exposure:     ≤ 80%
Concurrent open positions:    ≤ 20
Single position exposure:     ≤ 0.5% of portfolio (soft), 5% (hard ceiling)
```

**Warning threshold:** 75% total exposure → emit warning system_event.

---

## Rules

### Compute Portfolio Exposure

```go
// Uses CURRENT MARKET VALUE — not entry value.
// Entry value understates exposure for winning positions.

type PerMarketExposure struct {
    TokenAddress   string
    MarketValue    float64  // current price × qty
    ExposurePct    float64  // market_value / total_portfolio_value
}

type PortfolioExposure struct {
    TotalExposure      float64  // sum of all open position market values
    TotalExposurePct   float64  // total_exposure / portfolio_value
    PerMarket          map[string]PerMarketExposure
    PositionCount      int
    PortfolioValue     float64  // total capital (from capital engine)
}

func ComputePortfolioExposure(
    openPositions   []contracts.PositionState,
    portfolioValue  float64,
    currentPrices   map[string]float64,  // token_address → current price
) PortfolioExposure {
    result := PortfolioExposure{
        PerMarket:      make(map[string]PerMarketExposure),
        PortfolioValue: portfolioValue,
        PositionCount:  len(openPositions),
    }

    for _, pos := range openPositions {
        price, ok := currentPrices[pos.TokenAddress]
        if !ok || price <= 0 {
            price = pos.EntryPrice  // fallback to entry price if market price unavailable
        }
        marketValue := price * pos.Quantity
        result.TotalExposure += marketValue

        result.PerMarket[pos.TokenAddress] = PerMarketExposure{
            TokenAddress: pos.TokenAddress,
            MarketValue:  marketValue,
            ExposurePct:  safeDiv(marketValue, portfolioValue),
        }
    }

    result.TotalExposurePct = safeDiv(result.TotalExposure, portfolioValue)
    return result
}

func safeDiv(num, denom float64) float64 {
    if denom == 0 { return 0 }
    return num / denom
}
```

### Exposure Limit Check (Pre-Allocation Gate)

```go
type ExposureViolation struct {
    Rule    string
    Current float64
    Limit   float64
    Detail  string
}

type ExposureCheckResult struct {
    Allowed    bool
    Violations []ExposureViolation
    Warning    bool  // true if approaching limit (75%)
}

type AllocationProposal struct {
    TokenAddress    string
    ProposedAmount  float64  // USD value to allocate
}

func CheckExposureLimits(
    current   PortfolioExposure,
    proposal  AllocationProposal,
    cfg       ExposureConfig,
) ExposureCheckResult {
    var violations []ExposureViolation

    // Rule 1: Max total portfolio exposure
    projectedTotalPct := safeDiv(current.TotalExposure+proposal.ProposedAmount, current.PortfolioValue)
    if projectedTotalPct > cfg.MaxTotalExposurePct {
        violations = append(violations, ExposureViolation{
            Rule:    "max_total_exposure",
            Current: projectedTotalPct,
            Limit:   cfg.MaxTotalExposurePct,
            Detail:  fmt.Sprintf("projected=%.2f%% limit=%.2f%%", projectedTotalPct*100, cfg.MaxTotalExposurePct*100),
        })
    }

    // Rule 2: Max concurrent positions
    if current.PositionCount >= cfg.MaxConcurrentPositions {
        violations = append(violations, ExposureViolation{
            Rule:    "max_positions",
            Current: float64(current.PositionCount),
            Limit:   float64(cfg.MaxConcurrentPositions),
            Detail:  fmt.Sprintf("open=%d max=%d", current.PositionCount, cfg.MaxConcurrentPositions),
        })
    }

    // Rule 3: Single position hard ceiling (5%)
    singlePositionPct := safeDiv(proposal.ProposedAmount, current.PortfolioValue)
    if singlePositionPct > cfg.SinglePositionHardCeilingPct {
        // Cap to ceiling — don't block, but reduce
        violations = append(violations, ExposureViolation{
            Rule:    "single_position_ceiling",
            Current: singlePositionPct,
            Limit:   cfg.SinglePositionHardCeilingPct,
            Detail:  fmt.Sprintf("proposed=%.2f%% ceiling=%.2f%%", singlePositionPct*100, cfg.SinglePositionHardCeilingPct*100),
        })
    }

    // Warning threshold (75%)
    warning := projectedTotalPct > cfg.WarnExposurePct

    return ExposureCheckResult{
        Allowed:    len(violations) == 0,
        Violations: violations,
        Warning:    warning,
    }
}
```

### Integration: Capital Engine Gate

```go
// The Capital Engine calls CheckExposureLimits BEFORE computing AllocationDTO.
// If not allowed, AllocationDTO.Amount = 0 and AllocationDTO.Rejected = true.

func (m *CapitalModule) ComputeAllocation(
    ctx      context.Context,
    adapter  database.Adapter,
    edge     contracts.ValidatedEdgeDTO,
    prices   map[string]float64,
    portfolio PortfolioSnapshot,
) (contracts.AllocationDTO, error) {
    current := ComputePortfolioExposure(portfolio.OpenPositions, portfolio.Value, prices)

    proposal := AllocationProposal{
        TokenAddress:   edge.TokenAddress,
        ProposedAmount: computeBaseAllocation(edge, portfolio.Value, m.cfg),
    }

    check := CheckExposureLimits(current, proposal, m.cfg.ExposureCfg)

    if check.Warning {
        adapter.EmitSystemEvent(ctx, "portfolio_exposure_warning", map[string]any{
            "total_exposure_pct": current.TotalExposurePct,
            "threshold":         m.cfg.ExposureCfg.WarnExposurePct,
        })
    }

    if !check.Allowed {
        adapter.EmitSystemEvent(ctx, "portfolio_exposure_limit_hit", map[string]any{
            "violations": violationsToStrings(check.Violations),
        })
        return contracts.AllocationDTO{
            Amount:       0,
            Rejected:     true,
            RejectReason: "exposure_limit",
        }, nil
    }

    // Cap single-position size to soft limit (0.5%) even if hard ceiling not breached
    maxSinglePct := m.cfg.ExposureCfg.SinglePositionSoftLimitPct
    maxAmount := portfolio.Value * maxSinglePct
    if proposal.ProposedAmount > maxAmount {
        proposal.ProposedAmount = maxAmount
    }

    return buildAllocationDTO(edge, proposal.ProposedAmount), nil
}
```

### Monitoring: Alert Levels

```go
// Emit to event bus (Telegram dispatcher consumes):
//   75% → system_event "portfolio_exposure_warning"
//   80% → system_event "portfolio_exposure_limit_hit"
//   Any single position > 5% → system_event "single_position_ceiling_breach"

func MonitorExposureAlerts(
    ctx     context.Context,
    adapter database.Adapter,
    exp     PortfolioExposure,
    cfg     ExposureConfig,
) {
    if exp.TotalExposurePct >= cfg.MaxTotalExposurePct {
        adapter.EmitSystemEvent(ctx, "portfolio_exposure_critical", map[string]any{
            "exposure_pct":    exp.TotalExposurePct,
            "position_count":  exp.PositionCount,
        })
    } else if exp.TotalExposurePct >= cfg.WarnExposurePct {
        adapter.EmitSystemEvent(ctx, "portfolio_exposure_warning", map[string]any{
            "exposure_pct": exp.TotalExposurePct,
        })
    }

    for token, mkt := range exp.PerMarket {
        if mkt.ExposurePct > cfg.SinglePositionHardCeilingPct {
            adapter.EmitSystemEvent(ctx, "single_position_ceiling_breach", map[string]any{
                "token_address": token,
                "exposure_pct":  mkt.ExposurePct,
            })
        }
    }
}
```

---

## Anti-Patterns

```
❌ Computing exposure AFTER allocation (gate must run before AllocationDTO is produced)
❌ Using entry value instead of current market value (underestimates winning positions)
❌ Hardcoding limits (all limits must come from shared/config/pipeline.yaml)
❌ Blocking entirely when warning threshold hit (warn + monitor; only block at hard limit)
❌ Skipping exposure check for "small" allocations (1 basis point can push you over the limit)
❌ Not emitting system_event on limit violations (operators must be alerted)
```

---

## Config Reference (`shared/config/pipeline.yaml`)

```yaml
exposure_monitor:
  max_total_exposure_pct: 0.80 # 80% of portfolio
  warn_exposure_pct: 0.75 # 75% warning
  max_concurrent_positions: 20
  single_position_soft_limit_pct: 0.005 # 0.5%
  single_position_hard_ceiling_pct: 0.05 # 5% absolute ceiling
```

---

## Checklist

- [ ] Exposure computed from CURRENT market value, not entry value
- [ ] Check runs BEFORE AllocationDTO is produced (pre-trade gate)
- [ ] Warning emitted at 75%; hard block at 80%
- [ ] Single-position size capped to soft limit (0.5%) after gate passes
- [ ] Position count checked against max concurrent (20)
- [ ] All limits from config — no hardcoded values
- [ ] System events emitted for warning and critical thresholds

---

## References

- `docs/reference/architecture.md` § 3.7 — Capital Engine (Layer 7)
- `docs/archive/architecture-context/9_capital_engine.md` — Allocation sizing
- `.github/skills/capital-sizing/SKILL.md` — Kelly-adjacent sizing
- `.github/skills/drawdown-protection/SKILL.md` — HWM + drawdown tiers
- `shared/contracts/allocation.go` — `AllocationDTO`, `rejected`, `reject_reason` fields
