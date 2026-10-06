package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestNativeCompilationRejectsHistoricalRelease preserves the old fingerprints
// and proves that current native constraints cannot be attributed to those keys.
func TestNativeCompilationRejectsHistoricalRelease(t *testing.T) {
	// Arrange - use independently recorded production-release artifacts.
	raw, err := os.ReadFile("testdata/round3-compilation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Circuits []struct {
			Spec       ceremonyCircuitSpec `json:"spec"`
			R1CSSHA256 string              `json:"r1csSha256"`
			Power      int                 `json:"derivedPhase1Power"`
		} `json:"circuits"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Circuits) != 12 {
		t.Fatalf("expected 12 production shapes, got %d", len(fixture.Circuits))
	}
	configRaw, err := os.ReadFile(filepath.Clean("../../circuit-setup/configs/production.ceremony.2026-03.config.json"))
	if err != nil {
		t.Fatalf("read production config: %v", err)
	}
	var production ceremonyConfig
	if err := json.Unmarshal(configRaw, &production); err != nil {
		t.Fatalf("parse production config: %v", err)
	}
	fixtureSpecs := make([]ceremonyCircuitSpec, 0, len(fixture.Circuits))
	for _, expected := range fixture.Circuits {
		fixtureSpecs = append(fixtureSpecs, expected.Spec)
	}
	if !reflect.DeepEqual(fixtureSpecs, production.Circuits) {
		t.Fatal("round-3 compilation fixture does not match the production config")
	}
	for _, expected := range fixture.Circuits {
		t.Run(expected.Spec.ID, func(t *testing.T) {
			c := expected.Spec
			// Act - compile with the public dependency and a fresh cache directory.
			result, err := CompileWithMetadata(CircuitSpec{
				ID: c.ID, Name: c.Name, Type: CircuitType(c.Type), BatchSize: c.BatchSize,
				MaxInputs: c.MaxInputs, MaxInPerTx: c.MaxInPerTx, MaxOutPerTx: c.MaxOutPerTx,
				Depth: c.Depth, AuthDepth: c.AuthDepth, MaxTrees: c.MaxTrees,
				MaxAuthTrees: c.MaxAuthTrees, MaxFeeTokens: c.MaxFeeTokens,
			}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := os.ReadFile(result.R1CSPath)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(compiled)
			// Native compilation must not reproduce the historical token-ID relation.
			if got := hex.EncodeToString(sum[:]); got == expected.R1CSSHA256 {
				t.Errorf("native circuit unexpectedly reused a historical release relation")
			}
		})
	}
}

// TestCompilationReplacesPreUpgradeCache ensures a source upgrade cannot silently
// return an old circuit from a cache whose shape parameters have not changed.
func TestCompilationReplacesPreUpgradeCache(t *testing.T) {
	// Arrange - an old cache has matching dimensions but obsolete compiler metadata.
	spec := CircuitSpec{ID: "d1", Type: CircuitTypeDeposit, BatchSize: 1, Depth: 4, MaxTrees: 2}
	dir := t.TempDir()
	path := filepath.Join(dir, "d1.r1cs")
	if err := os.WriteFile(path, []byte("old circuit"), 0o600); err != nil {
		t.Fatal(err)
	}
	specHash, err := compileSpecHash(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCompileMetadata(path+".meta.json", compileCacheMetadata{CacheVersion: 1, SpecHash: specHash}); err != nil {
		t.Fatal(err)
	}
	// Act - use the same circuit shape with the upgraded compiler.
	result, err := CompileWithMetadata(spec, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Assert - a real compilation replaces the old artifact and its metadata.
	if result.NbConstraints == 0 {
		t.Fatal("reused the pre-upgrade artifact")
	}
	meta, err := loadCompileMetadata(path + ".meta.json")
	if err != nil {
		t.Fatal(err)
	}
	if meta.CacheVersion != compileCacheVersion {
		t.Fatalf("cache version %d was not upgraded", meta.CacheVersion)
	}
}
