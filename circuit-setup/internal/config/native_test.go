package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/model"
)

func TestNativeRoundConfigValidation(t *testing.T) {
	path := "../../configs/production.ceremony.2026-04.config.json"
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Circuits) != 19 {
		t.Fatal("offline verification must consume the complete shareable native config")
	}
	cfg.GitHubAuth.ClientID = ""
	if err := Validate(cfg); err != nil {
		t.Fatalf("offline verification does not need the coordinator client ID: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*model.CeremonyConfig)
		want   string
	}{
		{"duplicate", func(c *model.CeremonyConfig) { c.Circuits[1].ID = c.Circuits[0].ID }, "duplicate circuit id"},
		{"format", func(c *model.CeremonyConfig) { c.Circuits[0].AssetFormat = "unknown" }, "unsupported asset format"},
		{"note roots", func(c *model.CeremonyConfig) { c.Circuits[0].MaxTrees = 15 }, "sixteen note roots"},
		{"auth roots", func(c *model.CeremonyConfig) { c.Circuits[0].MaxAuthTrees = 15 }, "authorization root count"},
		{"unused dimension", func(c *model.CeremonyConfig) { c.Circuits[13].MaxInPerTx = 1 }, "unused input dimension"},
		{"migration format", func(c *model.CeremonyConfig) { c.Circuits[17].AssetFormat = "" }, "NoteMigration requires"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c model.CeremonyConfig
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&c)
			if err := Validate(&c); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
