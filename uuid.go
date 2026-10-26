package govee

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newUUIDString returns a random RFC 4122 version 4 UUID string, used for
// the requestId field of every API request. It falls back to a
// time-based non-UUID string if crypto/rand fails (essentially never).
func newUUIDString() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3])
		return fmt.Sprintf("req-%d", n)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}
