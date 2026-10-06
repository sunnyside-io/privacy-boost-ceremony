// Package config tests guard required fields and production policy defaults.
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/model"
	"github.com/testinprod-io/privacy-boost-protocol/pkg/addressprotocol"
)

// These published registry-ID configs are historical records, not presets for
// the address-native compiler. Pin their original bytes, including formatting.
var archivedProductionConfigs = map[string]string{
	"prod-ceremony-2026-02.config.json":       "6a31251d53c2342a28c7dec0c0a60f5ddf1250144395680aab350e62ce569659",
	"prod-ceremony-2026-01.config.json":       "6de2c71c7b6faa8235abda628b9a34c89c02f592d0f67258cf88a2d2a8673d05",
	"production.ceremony.2026-03.config.json": "199505cb53dcbd89a1acc9d6dbb78a038db41bd72396e9d2aae7d1a7cee0aab0",
	"production.ceremony.2026-01.config.json": "6de2c71c7b6faa8235abda628b9a34c89c02f592d0f67258cf88a2d2a8673d05",
	"production.ceremony.2026-02.config.json": "6a31251d53c2342a28c7dec0c0a60f5ddf1250144395680aab350e62ce569659",
}

func TestArchivedProductionConfigs(t *testing.T) {
	for name, want := range archivedProductionConfigs {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../configs", name))
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
				t.Fatalf("archived ceremony config changed: got SHA-256 %s, want %s", got, want)
			}
		})
	}
}

func TestTrackedCircuitPresets(t *testing.T) {
	paths, err := filepath.Glob("../../configs/*.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("find circuit presets: %v", err)
	}
	for _, path := range paths {
		if _, archived := archivedProductionConfigs[filepath.Base(path)]; archived {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			families := make(map[model.CircuitType]bool)
			migrations := make(map[int]bool)
			for _, circuit := range cfg.Circuits {
				if _, err := circuit.AddressShape(); err != nil {
					t.Fatalf("%s: %v", circuit.ID, err)
				}
				families[circuit.Type] = true
				if circuit.Type == model.CircuitTypeNoteMigrationV1 {
					migrations[circuit.BatchSize] = true
				}
			}
			if strings.Contains(filepath.Base(path), "benchmark") {
				return
			}
			for _, family := range []model.CircuitType{model.CircuitTypeEpoch, model.CircuitTypeDeposit, model.CircuitTypePortal, model.CircuitTypeForced, model.CircuitTypeGiftClaim, model.CircuitTypeNoteMigrationV1} {
				if !families[family] {
					t.Errorf("missing rollout family %s", family)
				}
			}
			if !migrations[int(addressprotocol.SmallNoteMigration)] || !migrations[int(addressprotocol.BulkNoteMigration)] {
				t.Error("rollout requires NoteMigration capacities 3 and 16")
			}
		})
	}
}
