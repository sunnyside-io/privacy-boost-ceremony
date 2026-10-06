package finalize

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/consensys/gnark/backend/groth16/bn254/mpcsetup"
)

// Phase1Cache resolves a Phase1 (.ph1) file's parsed, sealed SrsCommons at
// most once per distinct path within the cache's lifetime, so multiple
// circuits that share the same Phase1 power (identical resolved path) reuse
// one parse instead of each independently re-opening and re-sealing the same
// file.
//
// Safe for concurrent use: gnark's Phase2.Initialize never mutates its
// commons argument (verified against gnark v0.16.3 source: lagrangeCoeffsG1/
// G2 defensively copy out of commons.G1.Tau/G2.Tau before any in-place FFT
// mutation, and every other write in Initialize targets a freshly-allocated
// field on the receiver or on its returned Phase2Evaluations, never commons
// itself), so handing the same *SrsCommons value out to multiple concurrent
// finalize-worker goroutines for read-only use is safe once sync.Once's
// write has happened-before every read.
//
// Scope one Phase1Cache per Finalize() call; do not share across ceremonies.
type Phase1Cache struct {
	mu      sync.Mutex
	entries map[string]*phase1CacheEntry

	// parseCount is incremented exactly once per distinct path, inside the
	// sync.Once body. White-box test hook only (finalize package tests).
	parseCount atomic.Int64
}

type phase1CacheEntry struct {
	once    sync.Once
	commons mpcsetup.SrsCommons
	err     error
}

// NewPhase1Cache returns an empty cache.
func NewPhase1Cache() *Phase1Cache {
	return &Phase1Cache{entries: make(map[string]*phase1CacheEntry)}
}

// Commons returns the parsed, sealed SrsCommons for phase1Path, opening and
// parsing the file only on the first call for a given path; subsequent
// calls for the same path (including concurrent ones) block on that first
// call's result instead of re-parsing.
func (c *Phase1Cache) Commons(phase1Path string) (mpcsetup.SrsCommons, error) {
	c.mu.Lock()
	entry, ok := c.entries[phase1Path]
	if !ok {
		entry = &phase1CacheEntry{}
		c.entries[phase1Path] = entry
	}
	c.mu.Unlock()

	entry.once.Do(func() {
		c.parseCount.Add(1)
		entry.commons, entry.err = sealPhase1(phase1Path)
	})
	return entry.commons, entry.err
}

// resolveCommons returns the sealed SrsCommons for phase1Path via cache when
// cache is non-nil, or by parsing inline otherwise.
func resolveCommons(cache *Phase1Cache, phase1Path string) (mpcsetup.SrsCommons, error) {
	if cache != nil {
		return cache.Commons(phase1Path)
	}
	return sealPhase1(phase1Path)
}

func sealPhase1(phase1Path string) (mpcsetup.SrsCommons, error) {
	phase1File, err := os.Open(phase1Path)
	if err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	defer phase1File.Close()

	var ph1 mpcsetup.Phase1
	if _, err := ph1.ReadFrom(phase1File); err != nil {
		return mpcsetup.SrsCommons{}, err
	}
	return ph1.Seal([]byte("privacy-boost-phase1")), nil
}
