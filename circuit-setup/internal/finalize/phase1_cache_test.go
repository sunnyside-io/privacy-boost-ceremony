package finalize

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/consensys/gnark/backend/groth16/bn254/mpcsetup"
)

// TestPhase1CacheDedupesSerialCalls proves a second call for the same path
// reuses the first call's parsed result instead of re-parsing.
func TestPhase1CacheDedupesSerialCalls(t *testing.T) {
	path := writeCachedPhase1(t)
	cache := NewPhase1Cache()

	c1, err := cache.Commons(path)
	if err != nil {
		t.Fatalf("first Commons call: %v", err)
	}
	if got := cache.parseCount.Load(); got != 1 {
		t.Fatalf("parseCount after first call = %d, want 1", got)
	}

	c2, err := cache.Commons(path)
	if err != nil {
		t.Fatalf("second Commons call: %v", err)
	}
	if got := cache.parseCount.Load(); got != 1 {
		t.Fatalf("parseCount after second call to same path = %d, want 1 (should not re-parse)", got)
	}
	if !reflect.DeepEqual(c1, c2) {
		t.Fatal("second call returned a different SrsCommons value for the same path")
	}
}

// TestPhase1CacheDistinctPathsEachParseOnce proves the cache is keyed by
// path, not global: two distinct Phase1 files each get parsed exactly once.
func TestPhase1CacheDistinctPathsEachParseOnce(t *testing.T) {
	path1 := writeCachedPhase1(t)
	path2 := writeCachedPhase1(t)
	cache := NewPhase1Cache()

	if _, err := cache.Commons(path1); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Commons(path2); err != nil {
		t.Fatal(err)
	}
	if got := cache.parseCount.Load(); got != 2 {
		t.Fatalf("parseCount after two distinct paths = %d, want 2", got)
	}
}

// TestPhase1CacheConcurrentCallsSamePathDedupe proves the dedup guarantee
// holds under real concurrency (the scenario multiple finalize workers
// sharing a Phase1 power hit in practice), run under go test -race.
func TestPhase1CacheConcurrentCallsSamePathDedupe(t *testing.T) {
	path := writeCachedPhase1(t)
	cache := NewPhase1Cache()

	const workers = 8
	type result struct {
		commons mpcsetup.SrsCommons
		err     error
	}
	results := make([]result, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			c, err := cache.Commons(path)
			results[idx] = result{commons: c, err: err}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := cache.parseCount.Load(); got != 1 {
		t.Fatalf("parseCount after %d concurrent calls to the same path = %d, want 1", workers, got)
	}
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("worker %d: %v", i, r.err)
		}
		if !reflect.DeepEqual(r.commons, results[0].commons) {
			t.Fatalf("worker %d returned a different SrsCommons value than worker 0", i)
		}
	}
}

func writeCachedPhase1(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.ph1")
	p1 := mpcsetup.NewPhase1(8)
	p1.Initialize(8)
	p1.Contribute()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := p1.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	return path
}
