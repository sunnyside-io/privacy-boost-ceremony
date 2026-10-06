package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
)

// These fingerprints were recorded from the pinned backend source before the
// public compiler run. A fresh output directory excludes cached coordinator data.
func TestNativeCompilationMatchesRecordedSource(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-compilation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Circuits []struct {
			Spec         ceremonyCircuitSpec `json:"spec"`
			SHA256       string              `json:"r1csSha256"`
			Power        int                 `json:"derivedPhase1Power"`
			Constraints  int                 `json:"constraints"`
			PublicInputs int                 `json:"publicInputs"`
		} `json:"circuits"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Circuits) != 19 {
		t.Fatalf("expected 19 selected native shapes, got %d", len(fixture.Circuits))
	}
	raw, err = os.ReadFile("../../circuit-setup/configs/production.ceremony.2026-04.config.json")
	if err != nil {
		t.Fatal(err)
	}
	var config ceremonyConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	specs := make([]ceremonyCircuitSpec, 0, len(fixture.Circuits))
	for _, expected := range fixture.Circuits {
		specs = append(specs, expected.Spec)
	}
	if !reflect.DeepEqual(specs, config.Circuits) {
		t.Fatal("recorded native shape matrix differs from the proposed round config")
	}
	for _, expected := range fixture.Circuits {
		t.Run(expected.Spec.ID, func(t *testing.T) {
			c := expected.Spec
			result, err := CompileWithMetadata(CircuitSpec{
				AssetFormat: c.AssetFormat, ID: c.ID, Name: c.Name, Type: CircuitType(c.Type), BatchSize: c.BatchSize,
				MaxInputs: c.MaxInputs, MaxInPerTx: c.MaxInPerTx, MaxOutPerTx: c.MaxOutPerTx,
				Depth: c.Depth, AuthDepth: c.AuthDepth, MaxTrees: c.MaxTrees,
				MaxAuthTrees: c.MaxAuthTrees, MaxFeeTokens: c.MaxFeeTokens,
			}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(result.R1CSPath)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != expected.SHA256 || result.NbConstraints != expected.Constraints || result.RequiredPhase1Power != expected.Power {
				t.Fatalf("public compilation differs from the recorded native relation: %+v", result)
			}
			f, err := os.Open(result.R1CSPath)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			ccs := groth16.NewCS(ecc.BN254)
			if _, err := ccs.ReadFrom(f); err != nil {
				t.Fatal(err)
			}
			if got := ccs.GetNbPublicVariables() - 1; got != expected.PublicInputs {
				t.Fatalf("public inputs = %d, expected %d", got, expected.PublicInputs)
			}
		})
	}
}
