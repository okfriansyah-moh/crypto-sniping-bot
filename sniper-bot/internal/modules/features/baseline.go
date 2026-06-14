package features

import (
	"sort"
	"sync"
)

// BaselineStore is a thread-safe per-(market, signal) ring buffer of recent
// raw signal values. The features worker owns one instance and feeds an
// immutable BaselineSnapshot into the pure module on every event so the
// module remains a pure function.
//
// Persistence: the in-memory ring buffer is the authoritative hot-path
// state. The features worker rehydrates it from the database on startup
// (see BaselineStore.Hydrate) and persists dirty (market, signal)
// entries on a debounced flush cadence (see DirtyKeys / ClearDirty).
type BaselineStore struct {
	mu      sync.RWMutex
	maxLen  int
	history map[string]map[string][]float64 // market → signal → ring buffer
	// dirty tracks (market, signal) tuples mutated since the last
	// ClearDirty call. Used by the worker's debounced flush goroutine
	// to limit DB writes to changed rows only.
	dirty map[string]map[string]struct{}
}

// BaselineKey identifies a single (market, signal) ring buffer.
// Returned by DirtyKeys for the persistence flush loop.
type BaselineKey struct {
	Market string
	Signal string
}

// NewBaselineStore returns a BaselineStore that keeps at most maxLen most
// recent values per (market, signal). maxLen <= 0 falls back to 256 to
// guarantee bounded memory.
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

// Append records a raw signal value for (market, signal). Oldest entries
// are evicted when the per-signal buffer would exceed maxLen.
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

// Snapshot returns an immutable copy of the per-signal histories for a market.
// The returned slices are owned by the caller — safe to read without locks.
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

// AppendBatch records the raw values produced for one event in a single
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

// Hydrate seeds the in-memory ring buffers from a persisted snapshot
// (typically the result of database.Adapter.LoadBaselines). Existing
// entries for the same (market, signal) are overwritten — Hydrate is
// expected to be called once at worker startup, BEFORE the event loop.
//
// Each per-signal slice is bounded to the store's maxLen by retaining
// the trailing window (oldest values are dropped). Hydrated entries are
// NOT marked dirty — there is no need to write back what we just read.
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
// last ClearDirty call, sorted lexicographically by (market, signal) for
// determinism. Safe to call concurrently with Append / AppendBatch.
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
// Called by the worker's flush loop after a successful SaveBaseline so
// that throttled / failed writes naturally remain dirty for the next
// flush cycle. A no-op when the entry is already clean.
//
// Concurrency note: a value Appended between DirtyKeys() and MarkClean()
// will be flushed next cycle (the Append re-marks it). At-least-once
// persistence semantics — never silently dropped.
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
// Returns nil when the buffer is absent. Used by the persistence flush to
// extract the slice to write under a single lock acquisition.
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
	History map[string][]float64 // signal name → recent raw values
}

// History returns the rolling window for a signal, or nil when absent.
// Always safe to call on a zero-value BaselineSnapshot.
func (b BaselineSnapshot) HistoryFor(signal string) []float64 {
	if b.History == nil {
		return nil
	}
	return b.History[signal]
}
