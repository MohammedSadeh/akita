package internal

import (
	"testing"

	"github.com/sarchlab/akita/v3/mem/vm"
)

const (
	testLog2Region = 20 // 1 MB regions
	testLog2Page   = 16 // 64 KB pages: sixteen sub-entries per region
)

func pg(pid vm.PID, vAddr uint64) vm.Page {
	return vm.Page{PID: pid, VAddr: vAddr, Valid: true}
}

// All sixteen 64 KB pages of one 1 MB region must pack into a single way.
func TestRegionSetPacksSubEntries(t *testing.T) {
	s := NewRegionSet(8, testLog2Region, testLog2Page)

	w, ok := s.Evict()
	if !ok {
		t.Fatal("no way available to evict")
	}

	for i := uint64(0); i < 16; i++ {
		s.Update(w, pg(1, i<<testLog2Page))
		s.Visit(w)
	}

	for i := uint64(0); i < 16; i++ {
		way, p, found := s.Lookup(1, i<<testLog2Page)
		if !found || way != w || p.VAddr != i<<testLog2Page {
			t.Fatalf("sub-entry %d: found=%v way=%d", i, found, way)
		}
	}

	if way, found := s.RegionWay(1, 5<<testLog2Page); !found || way != w {
		t.Fatal("RegionWay did not report the resident region")
	}
}

// The same region under a different process identifier is a different entry,
// so one tenant can never hit on another tenant's translation.
func TestRegionSetSeparatesPIDs(t *testing.T) {
	s := NewRegionSet(8, testLog2Region, testLog2Page)

	w, _ := s.Evict()
	s.Update(w, pg(1, 0))
	s.Visit(w)

	if _, _, found := s.Lookup(2, 0); found {
		t.Fatal("tenant 2 hit tenant 1's translation")
	}
	if _, found := s.RegionWay(2, 0); found {
		t.Fatal("tenant 2 matched tenant 1's region")
	}
}

// Installing a new region into an evicted way drops the old region entirely,
// and the way then reports the new owner.
func TestRegionSetReplacesRegion(t *testing.T) {
	s := NewRegionSet(1, testLog2Region, testLog2Page)

	w, _ := s.Evict()
	s.Update(w, pg(1, 0))
	s.Visit(w)

	w2, _ := s.Evict()
	if w2 != w {
		t.Fatalf("single-way set evicted way %d, want %d", w2, w)
	}
	s.Update(w2, pg(2, 1<<testLog2Region))
	s.Visit(w2)

	if _, _, found := s.Lookup(1, 0); found {
		t.Fatal("old region still resident after replacement")
	}
	if victim := s.PageAt(w2); victim.PID != 2 || !victim.Valid {
		t.Fatalf("PageAt reports PID %d valid=%v, want PID 2 valid", victim.PID, victim.Valid)
	}
}
