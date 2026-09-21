package tlb

import (
	"math/rand"
	"testing"
)

// These tests verify the linear GF(2) set-index mapping reported for the
// reference platform: output bit r is the XOR of every eighth virtual-address
// bit starting at bit 20+r, over VA[46:20]. They check the three properties
// the mapping must have, rather than re-deriving the implementation.

func newHashTLB() *TLB {
	return &TLB{numSets: 256, pageSize: 4096}
}

// Addresses spaced 1 MB apart, k<<20 for k in [0, 256), only set VA bits
// 20..27. Each output bit r then reduces to bit r of k alone, so the mapping
// must be the identity: the k-th megabyte lands in set k. This is the property
// that makes 1 MB-spaced pages the natural building block for eviction sets.
func TestGF2HashIdentityOnMBSpacing(t *testing.T) {
	tlb := newHashTLB()
	for k := uint64(0); k < 256; k++ {
		if got := tlb.vAddrToSetID(k << 20); got != int(k) {
			t.Fatalf("set(%d MB) = %d, want %d", k, got, k)
		}
	}
}

// A mapping that is linear over GF(2) must satisfy set(a^b) == set(a)^set(b)
// for any two addresses. Checked on random addresses drawn from VA[46:20].
func TestGF2HashIsLinear(t *testing.T) {
	tlb := newHashTLB()
	rng := rand.New(rand.NewSource(1234))
	mask := uint64((1<<47)-1) &^ uint64((1<<20)-1)
	for i := 0; i < 10000; i++ {
		a := rng.Uint64() & mask
		b := rng.Uint64() & mask
		if tlb.vAddrToSetID(a^b) != tlb.vAddrToSetID(a)^tlb.vAddrToSetID(b) {
			t.Fatalf("not linear for a=%#x b=%#x", a, b)
		}
	}
}

// Bits below 20 are not inputs to the mapping, so every address inside one
// 1 MB-aligned region must land in the same set.
func TestGF2HashIgnoresLowBits(t *testing.T) {
	tlb := newHashTLB()
	base := uint64(0x5a) << 20
	want := tlb.vAddrToSetID(base)
	for off := uint64(0); off < 1<<20; off += 4096 {
		if got := tlb.vAddrToSetID(base + off); got != want {
			t.Fatalf("offset %#x moved set: got %d, want %d", off, got, want)
		}
	}
}
