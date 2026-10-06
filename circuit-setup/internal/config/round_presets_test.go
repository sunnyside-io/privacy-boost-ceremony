package config

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/testinprod-io/privacy-boost-protocol/pkg/addressprotocol"
)

func TestNumberedProductionReleasePreset(t *testing.T) {
	// Arrange - both the release asset and numbered record identify one round.
	active, err := os.ReadFile("../../configs/production.ceremony.config.json")
	if err != nil {
		t.Fatal(err)
	}
	numbered, err := os.ReadFile("../../configs/production.ceremony.2026-04.config.json")
	if err != nil {
		t.Fatal(err)
	}
	// Act - use the same loader as the coordinator and contributor.
	cfg, err := Load("../../configs/production.ceremony.config.json")
	if err != nil {
		t.Fatal(err)
	}
	// Assert - release stamps cannot silently select a different preset.
	if !bytes.Equal(active, numbered) {
		t.Fatal("numbered production record differs from the release asset")
	}
	if cfg.ID != "prod-ceremony-2026-04" || !cfg.Production {
		t.Fatalf("unexpected production round: %s", cfg.ID)
	}
	assertSerialPreset(t, "../../configs/production.ceremony.config.json")
	want := []string{"s1", "s5", "m1", "m4", "l1", "l3", "sp1", "w51", "w201", "d1", "d4", "d14", "p1", "p6", "f13", "g1", "g3", "note-migration-v1-3", "note-migration-v1-16"}
	got := make([]string, 0, len(cfg.Circuits))
	for _, spec := range cfg.Circuits {
		got = append(got, spec.ID)
		if spec.AssetFormat != addressprotocol.AssetFormat {
			t.Fatalf("%s has an incompatible asset format", spec.ID)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("production selection changed: %v", got)
	}
}

func TestNativeRehearsalUsesProductionShapesInSeparateState(t *testing.T) {
	production, err := Load("../../configs/production.ceremony.config.json")
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := Load("../../configs/rehearsal.native.ceremony.config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(production.Circuits, rehearsal.Circuits) {
		t.Fatal("rehearsal does not cover the production selection")
	}
	if rehearsal.Production || rehearsal.ID == production.ID || rehearsal.StateDir == production.StateDir || rehearsal.Cache.RootDir == production.Cache.RootDir {
		t.Fatal("rehearsal must use separate non-production state and cache")
	}
	assertSerialPreset(t, "../../configs/rehearsal.native.ceremony.config.json")
}

// The standalone verifier does not model coordinator worker settings. Inspect
// their published JSON values to preserve the same operational round preset.
func assertSerialPreset(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var preset struct {
		Server struct {
			Setup    int `json:"setupMaxWorkers"`
			Finalize int `json:"finalizeMaxWorkers"`
		} `json:"server"`
	}
	if err := json.Unmarshal(raw, &preset); err != nil {
		t.Fatal(err)
	}
	if preset.Server.Setup != 1 || preset.Server.Finalize != 1 {
		t.Fatal("round concurrency changed")
	}
}
