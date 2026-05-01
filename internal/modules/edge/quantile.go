package edge

import "sort"

// quantile returns the q-th quantile (q ∈ [0,1]) of values using linear
// interpolation between adjacent ranks. The input is copied before sorting,
// so callers MAY pass a snapshot slice without worrying about mutation.
//
// Edge cases:
//   - len(values) == 0  → 0
//   - len(values) == 1  → values[0]
//   - q clamped to [0,1]
//
// Pure: same input → same output. No randomness.
func quantile(values []float64, q float64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return values[0]
	}
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	pos := q * float64(n-1)
	lo := int(pos)
	if lo >= n-1 {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}
