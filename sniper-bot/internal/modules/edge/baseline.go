package edge

import (
	"sort"
	"sync"
)

// BaselineStore is a thread-safe per-(market, signal) ring buffer of recent
// raw signal values used by the edge module to derive the adaptive momentum
// threshold (rolling-window quantile per the momentum-detector skill).
//
// Mirrors the pattern used in internal/modules/features/baseline.go: the
// EdgeWorker owns one instance, snapshots it before invoking the pure
// module, and appends the post-process values so the module remains
// deterministic and side-effect free.
//
// Persistence: the in-memory ring buffer is the authoritative hot-path
// state. The edge worker rehydrates it from the database on startup
// (Hydrate) and persists dirty (market, signal) entries on a debounced
// flush cadence (DirtyKeys / ClearDirty). Cold-start (samples < min)
// falls back to the configured MinPriceMomentum.
type BaselineStore struct {
	mu      sync.RWMutex
	maxLen  int
	history map[string]map[string][]float64 // market → signal → ring buffer
	// dirty tracks (market, signal) tuples mutated since the last
	// ClearDirty call.
	dirty map[string]map[string]struct{}
}

// BaselineKey identifies a single (market, signal) ring buffer.
// Returned by DirtyKeys for the persistence flush loop.
type BaselineKey struct {
	Market string
	Signal string
}

// NewBaselineStore returns a BaselineStore that keeps at most maxLen values
// per (market, signal). maxLen <= 0 falls back to 256 to guarantee bounded
// memory.
func NewBaselineStore(maxLen int) *BaselineStore {
	if maxLen <= 0 {
		maxLen = 256
	}
	return &BaselineStore{
		maxLen:  maxLen,
		history: make(map[string]map[string][]float64),
		dirty:   make(map[string]map[string]struct{}),
	}
}

// Append records a single raw signal value for (market, signal). Oldest
// entries are evicted when the per-signal buffer would exceed maxLen.
func (s *BaselineStore) Append(market, signal string, value float64) {
	if market == "" || signal == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bySignal, ok := s.history[market]
	if !ok {
		bySignal = make(map[string][]float64)
		s.history[market] = bySignal
	}
	buf := bySignal[signal]
	buf = append(buf, value)
	if len(buf) > s.maxLen {
		buf = buf[len(buf)-s.maxLen:]
	}
	bySignal[signal] = buf
	s.markDirtyLocked(market, signal)
}

// AppendBatch records multiple signal values for one event in a single
// critical section to amortize lock cost.
func (s *BaselineStore) AppendBatch(market string, raws map[string]float64) {
	if market == "" || len(raws) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bySignal, ok := s.history[market]
	if !ok {
		bySignal = make(map[string][]float64)
		s.history[market] = bySignal
	}
	for signal, value := range raws {
		if signal == "" {
			continue
		}
		buf := bySignal[signal]
		buf = append(buf, value)
		if len(buf) > s.maxLen {
			buf = buf[len(buf)-s.maxLen:]
		}
		bySignal[signal] = buf
		s.markDirtyLocked(market, signal)
	}
}

// Snapshot returns an immutable copy of the per-signal histories for a
// market. The returned slices are owned by the caller — safe to read
// without locks.
func (s *BaselineStore) Snapshot(market string) BaselineSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySignal := s.history[market]
	out := BaselineSnapshot{
		Market:  market,
		History: make(map[string][]float64, len(bySignal)),
	}
	for sig, buf := range bySignal {
		copied := make([]float64, len(buf))
		copy(copied, buf)
		out.History[sig] = copied
	}
	return out
}

// Hydrate seeds the in-memory ring buffers from a persisted snapshot.
// Bounded to the store's maxLen by retaining the trailing window.
// Hydrated entries are NOT marked dirty.
func (s *BaselineStore) Hydrate(snapshot map[string]map[string][]float64) {
	if len(snapshot) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for market, bySignal := range snapshot {
		if market == "" || len(bySignal) == 0 {
			continue
		}
		dst, ok := s.history[market]
		if !ok {
			dst = make(map[string][]float64, len(bySignal))
			s.history[market] = dst
		}
		for signal, values := range bySignal {
			if signal == "" {
				continue
			}
			buf := values
			if len(buf) > s.maxLen {
				buf = buf[len(buf)-s.maxLen:]
			}
			copied := make([]float64, len(buf))
			copy(copied, buf)
			dst[signal] = copied
		}
	}
}

// DirtyKeys returns the set of (market, signal) tuples mutated since the
// last ClearDirty call, sorted lexicographically for determinism.
func (s *BaselineStore) DirtyKeys() []BaselineKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.dirty) == 0 {
		return nil
	}
	keys := make([]BaselineKey, 0)
	for market, signals := range s.dirty {
		for signal := range signals {
			keys = append(keys, BaselineKey{Market: market, Signal: signal})
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Market != keys[j].Market {
			return keys[i].Market < keys[j].Market
		}
		return keys[i].Signal < keys[j].Signal
	})
	return keys
}

// MarkClean removes a single (market, signal) entry from the dirty set.
// Called after a successful SaveBaseline so that throttled / failed
// writes naturally remain dirty for the next flush cycle.
func (s *BaselineStore) MarkClean(market, signal string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if signals, ok := s.dirty[market]; ok {
		delete(signals, signal)
		if len(signals) == 0 {
			delete(s.dirty, market)
		}
	}
}

// ClearDirty drops the entire dirty-tracking set. Test-only helper —
// production flushes use MarkClean per successful write.
func (s *BaselineStore) ClearDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = make(map[string]map[string]struct{})
}

// Values returns a defensive copy of the ring buffer for (market, signal).
// Returns nil when the buffer is absent.
func (s *BaselineStore) Values(market, signal string) []float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	bySignal := s.history[market]
	if bySignal == nil {
		return nil
	}
	buf := bySignal[signal]
	if buf == nil {
		return nil
	}
	out := make([]float64, len(buf))
	copy(out, buf)
	return out
}

func (s *BaselineStore) markDirtyLocked(market, signal string) {
	signals, ok := s.dirty[market]
	if !ok {
		signals = make(map[string]struct{})
		s.dirty[market] = signals
	}
	signals[signal] = struct{}{}
}

// BaselineSnapshot is the immutable view passed to the pure module.
type BaselineSnapshot struct {
	Market  string
	History map[string][]float64
}

// HistoryFor returns the rolling window for a signal, or nil when absent.
// Always safe to call on a zero-value BaselineSnapshot.
func (b BaselineSnapshot) HistoryFor(signal string) []float64 {
	if b.History == nil {
		return nil
	}
	return b.History[signal]
}

// Signal name constants — used as keys into the BaselineStore.
const (
	SignalPriceMomentum  = "price_momentum"
	SignalVolumeMomentum = "volume_momentum"
)
