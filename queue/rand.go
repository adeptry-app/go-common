package queue

import (
	crand "crypto/rand"
	"encoding/binary"
)

// randFloat64 returns a uniform value in [0, 1) from crypto/rand.
// Falls back to 0.5 (midpoint jitter) if the system entropy source fails.
func randFloat64() float64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return 0.5
	}
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}
