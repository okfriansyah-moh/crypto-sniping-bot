package resource_control

import (
	"errors"
	"sync"
	"time"
)

// ErrWalletGasCap is returned when a wallet has exhausted its daily gas budget.
var ErrWalletGasCap = errors.New("resource_control: wallet daily gas cap reached")

// ErrSystemGasCap is returned when the system-wide daily gas budget is exhausted.
var ErrSystemGasCap = errors.New("resource_control: system daily gas cap reached")

// GasBudget enforces per-wallet and system-wide daily gas caps.
// Gas is measured in gwei to avoid uint64 overflow on wei amounts.
type GasBudget interface {
	// RecordGasUsed records gas consumed by a transaction.
	// Returns ErrWalletGasCap if the wallet cap is exceeded, ErrSystemGasCap for the system cap.
	RecordGasUsed(walletAddress string, gweiUsed int64) error
	// CheckWallet returns nil if the wallet has remaining budget, or an error.
	CheckWallet(walletAddress string) error
	// CheckSystem returns nil if the system cap is not exhausted.
	CheckSystem() error
	// Reset clears all daily counters. Called at midnight UTC.
	Reset()
}

type walletState struct {
	spentGwei int64
}

// GasBudgetImpl is the in-memory daily gas budget tracker.
type GasBudgetImpl struct {
	mu                    sync.Mutex
	wallets               map[string]*walletState
	systemSpentGwei       int64
	walletDailyCapGwei    int64
	systemDailyCapGwei    int64
	resetDate             string // "YYYY-MM-DD" of the last reset
}

// NewGasBudget creates a new GasBudget with the given per-wallet and system caps in gwei.
func NewGasBudget(walletDailyCapGwei, systemDailyCapGwei int64) *GasBudgetImpl {
	return &GasBudgetImpl{
		wallets:            make(map[string]*walletState),
		walletDailyCapGwei: walletDailyCapGwei,
		systemDailyCapGwei: systemDailyCapGwei,
		resetDate:          todayUTC(),
	}
}

func todayUTC() string {
	return time.Now().UTC().Format("2006-01-02")
}

func (g *GasBudgetImpl) maybeReset() {
	today := todayUTC()
	if today != g.resetDate {
		g.wallets = make(map[string]*walletState)
		g.systemSpentGwei = 0
		g.resetDate = today
	}
}

// RecordGasUsed records gas spent by wallet and adds it to the system total.
func (g *GasBudgetImpl) RecordGasUsed(walletAddress string, gweiUsed int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maybeReset()

	ws := g.wallets[walletAddress]
	if ws == nil {
		ws = &walletState{}
		g.wallets[walletAddress] = ws
	}

	if ws.spentGwei+gweiUsed > g.walletDailyCapGwei {
		return ErrWalletGasCap
	}
	if g.systemSpentGwei+gweiUsed > g.systemDailyCapGwei {
		return ErrSystemGasCap
	}

	ws.spentGwei += gweiUsed
	g.systemSpentGwei += gweiUsed
	return nil
}

// CheckWallet returns nil if the wallet has not exhausted its daily cap.
func (g *GasBudgetImpl) CheckWallet(walletAddress string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maybeReset()

	ws := g.wallets[walletAddress]
	if ws == nil {
		return nil
	}
	if ws.spentGwei >= g.walletDailyCapGwei {
		return ErrWalletGasCap
	}
	return nil
}

// CheckSystem returns nil if the system cap is not exhausted.
func (g *GasBudgetImpl) CheckSystem() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.maybeReset()

	if g.systemSpentGwei >= g.systemDailyCapGwei {
		return ErrSystemGasCap
	}
	return nil
}

// Reset clears all daily counters.
func (g *GasBudgetImpl) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wallets = make(map[string]*walletState)
	g.systemSpentGwei = 0
	g.resetDate = todayUTC()
}
