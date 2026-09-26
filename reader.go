package brandprobe

import (
	"encoding/binary"
	"io"
)

// maxBudget is the maximum number of bytes a single Probe may read
// through the provided ReaderAt.
const maxBudget = 1 << 20 // 1 MiB

// budgetReader wraps an io.ReaderAt, enforcing the read budget and the
// declared file size. Any read that would exceed the budget fails with
// ErrBudgetExceeded without touching the underlying reader. Any read
// that extends beyond the declared size fails with ErrCorrupt.
type budgetReader struct {
	r    io.ReaderAt
	size int64
	used int64
}

func newBudgetReader(r io.ReaderAt, size int64) *budgetReader {
	if size < 0 {
		size = 0
	}
	return &budgetReader{r: r, size: size}
}

// readAt reads len(p) bytes at offset off.
func (b *budgetReader) readAt(p []byte, off int64) error {
	if off < 0 || int64(len(p)) > b.size-off {
		return ErrCorrupt
	}
	if int64(len(p)) > maxBudget-b.used {
		return ErrBudgetExceeded
	}
	if len(p) == 0 {
		return nil
	}
	if _, err := b.r.ReadAt(p, off); err != nil {
		return ErrCorrupt
	}
	b.used += int64(len(p))
	return nil
}

// bytes returns a freshly allocated slice of n bytes at offset off.
// n must already be bounded by the caller's structural limits.
func (b *budgetReader) bytes(off, n int64) ([]byte, error) {
	if n < 0 || n > maxBudget {
		return nil, ErrBudgetExceeded
	}
	p := make([]byte, n)
	if err := b.readAt(p, off); err != nil {
		return nil, err
	}
	return p, nil
}

func (b *budgetReader) u8(off int64) (uint8, error) {
	var p [1]byte
	if err := b.readAt(p[:], off); err != nil {
		return 0, err
	}
	return p[0], nil
}

func (b *budgetReader) u16(off int64, bo binary.ByteOrder) (uint16, error) {
	var p [2]byte
	if err := b.readAt(p[:], off); err != nil {
		return 0, err
	}
	return bo.Uint16(p[:]), nil
}

func (b *budgetReader) u32(off int64, bo binary.ByteOrder) (uint32, error) {
	var p [4]byte
	if err := b.readAt(p[:], off); err != nil {
		return 0, err
	}
	return bo.Uint32(p[:]), nil
}
