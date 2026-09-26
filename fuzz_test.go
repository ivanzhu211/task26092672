package probe

import (
	"bytes"
	"testing"
)

// Probe must never panic, must always respect the read budget, and
// any successful result must have positive dimensions.
func FuzzProbe(f *testing.F) {
	for _, s := range allSamples() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		cr := &countingReader{r: bytes.NewReader(data)}
		r, err := Probe(cr, int64(len(data)))
		if cr.read > readBudget {
			t.Fatalf("read %d bytes, budget %d", cr.read, readBudget)
		}
		if err == nil && (r.Width <= 0 || r.Height <= 0) {
			t.Fatalf("non-positive result %+v", r)
		}
	})
}
