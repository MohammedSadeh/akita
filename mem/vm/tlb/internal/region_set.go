package internal

import (
	"sort"

	"github.com/sarchlab/akita/v3/mem/vm"
)

// RegionSet is a TLB set in which each way covers one naturally aligned
// virtual-address region and holds several sub-entries, one per page in that
// region. It models the sub-entry packing of the last-level TLB on the
// reference platform, where one entry covers a 1 MB region as sixteen 64 KB
// sub-entries.
//
// A miss on an absent sub-entry of a region that is already resident fills
// that sub-entry in place, without evicting anything. A way is evicted only
// when a region that is not yet resident must be installed.
type RegionSet interface {
	Set

	// RegionWay reports which way, if any, holds the region containing vAddr
	// for the given process.
	RegionWay(pid vm.PID, vAddr uint64) (wayID int, found bool)
}

// regionKey identifies a resident region. Keying on the process identifier as
// well as the region keeps two tenants' translations of the same region in
// separate entries, so contention between them is purely capacity contention.
type regionKey struct {
	pid    vm.PID
	region uint64
}

type regionBlock struct {
	key       regionKey
	valid     bool
	subs      []vm.Page
	wayID     int
	lastVisit uint64
}

type regionSetImpl struct {
	blocks     []*regionBlock
	regionMap  map[regionKey]int
	visitList  []*regionBlock
	visitCount uint64
	log2Region uint64
	log2Page   uint64
	numSubs    int
}

// NewRegionSet creates a set whose ways each cover a region of 2^log2Region
// bytes, made of 2^(log2Region-log2Page) sub-entries of 2^log2Page bytes.
func NewRegionSet(numWays int, log2Region, log2Page uint64) RegionSet {
	if log2Region < log2Page {
		panic("a region must be at least one page")
	}

	s := &regionSetImpl{
		blocks:     make([]*regionBlock, numWays),
		regionMap:  make(map[regionKey]int),
		visitList:  make([]*regionBlock, 0, numWays),
		log2Region: log2Region,
		log2Page:   log2Page,
		numSubs:    1 << (log2Region - log2Page),
	}

	for i := range s.blocks {
		s.blocks[i] = &regionBlock{
			wayID: i,
			subs:  make([]vm.Page, s.numSubs),
		}
		s.Visit(i)
	}

	return s
}

func (s *regionSetImpl) keyOf(pid vm.PID, vAddr uint64) regionKey {
	return regionKey{pid: pid, region: vAddr >> s.log2Region}
}

func (s *regionSetImpl) subIndex(vAddr uint64) int {
	return int((vAddr >> s.log2Page) & uint64(s.numSubs-1))
}

// Lookup hits only if the region is resident and the requested sub-entry
// within it is valid.
func (s *regionSetImpl) Lookup(pid vm.PID, vAddr uint64) (
	wayID int,
	page vm.Page,
	found bool,
) {
	wayID, ok := s.regionMap[s.keyOf(pid, vAddr)]
	if !ok {
		return 0, vm.Page{}, false
	}

	page = s.blocks[wayID].subs[s.subIndex(vAddr)]
	if !page.Valid {
		return 0, vm.Page{}, false
	}

	return wayID, page, true
}

// Update writes a page into its sub-entry. If the way currently holds a
// different region, that region is dropped in its entirety first.
func (s *regionSetImpl) Update(wayID int, page vm.Page) {
	b := s.blocks[wayID]
	key := s.keyOf(page.PID, page.VAddr)

	if !b.valid || b.key != key {
		if b.valid {
			delete(s.regionMap, b.key)
		}
		for i := range b.subs {
			b.subs[i] = vm.Page{}
		}
		b.key = key
		b.valid = true
		s.regionMap[key] = wayID
	}

	b.subs[s.subIndex(page.VAddr)] = page
}

// Evict returns the least recently used way.
func (s *regionSetImpl) Evict() (wayID int, ok bool) {
	if len(s.visitList) == 0 {
		return 0, false
	}

	lru := s.visitList[0]
	s.visitList = s.visitList[1:]

	return lru.wayID, true
}

// Visit marks a way as most recently used.
func (s *regionSetImpl) Visit(wayID int) {
	b := s.blocks[wayID]

	for i, x := range s.visitList {
		if x.wayID == wayID {
			s.visitList = append(s.visitList[:i], s.visitList[i+1:]...)
			break
		}
	}

	s.visitCount++
	b.lastVisit = s.visitCount

	idx := sort.Search(len(s.visitList), func(i int) bool {
		return s.visitList[i].lastVisit > b.lastVisit
	})

	s.visitList = append(s.visitList, nil)
	copy(s.visitList[idx+1:], s.visitList[idx:])
	s.visitList[idx] = b
}

// PageAt describes the occupant of a way for eviction accounting. For a region
// set the occupant is the whole region rather than a single page, so this
// returns a page carrying the owning process and the region base address,
// marked valid if the way holds a region at all.
func (s *regionSetImpl) PageAt(wayID int) vm.Page {
	b := s.blocks[wayID]
	if !b.valid {
		return vm.Page{}
	}

	return vm.Page{
		PID:   b.key.pid,
		VAddr: b.key.region << s.log2Region,
		Valid: true,
	}
}

// RegionWay reports which way holds the region containing vAddr for pid.
func (s *regionSetImpl) RegionWay(pid vm.PID, vAddr uint64) (int, bool) {
	wayID, ok := s.regionMap[s.keyOf(pid, vAddr)]
	return wayID, ok
}
