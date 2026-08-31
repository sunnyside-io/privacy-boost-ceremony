package compile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// productionConfigPath points at the tracked config the production ceremony runs.
// Driving the dispatch test from that file (rather than a hand-written list) means
// a circuit type added to the ceremony without a matching newCircuit arm fails here
// instead of at coordinator setup time.
const productionConfigPath = "../../circuit-setup/configs/production.ceremony.config.json"

// ceremonyConfig mirrors the subset of the ceremony config this package consumes.
// Field tags match circuit-setup/internal/model.CircuitSpec so the parse stays honest.
type ceremonyConfig struct {
	Circuits []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Type         string `json:"type"`
		BatchSize    int    `json:"batchSize,omitempty"`
		MaxInputs    int    `json:"maxInputs,omitempty"`
		MaxInPerTx   int    `json:"maxInputsPerTransfer,omitempty"`
		MaxOutPerTx  int    `json:"maxOutputsPerTransfer,omitempty"`
		Depth        int    `json:"depth"`
		AuthDepth    int    `json:"authDepth,omitempty"`
		MaxTrees     int    `json:"maxTrees,omitempty"`
		MaxAuthTrees int    `json:"maxAuthTrees,omitempty"`
		MaxFeeTokens int    `json:"maxFeeTokens,omitempty"`
	} `json:"circuits"`
}

// TestNewCircuitCoversProductionConfig asserts every circuit in the production
// ceremony config resolves to a constructed circuit. Constructing a circuit only
// allocates its witness struct, so this exercises the full type dispatch without
// paying any constraint-compilation cost.
func TestNewCircuitCoversProductionConfig(t *testing.T) {
	// Arrange - load the tracked production ceremony config.
	raw, err := os.ReadFile(filepath.Clean(productionConfigPath))
	if err != nil {
		t.Fatalf("read production config: %v", err)
	}
	var cfg ceremonyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse production config: %v", err)
	}
	if len(cfg.Circuits) == 0 {
		t.Fatal("production config declares no circuits")
	}

	seenTypes := make(map[string]int, len(cfg.Circuits))
	for _, c := range cfg.Circuits {
		t.Run(c.ID, func(t *testing.T) {
			// Act - resolve the config entry through the production dispatch.
			circuit, err := newCircuit(CircuitSpec{
				ID:           c.ID,
				Name:         c.Name,
				Type:         CircuitType(c.Type),
				BatchSize:    c.BatchSize,
				MaxInputs:    c.MaxInputs,
				MaxInPerTx:   c.MaxInPerTx,
				MaxOutPerTx:  c.MaxOutPerTx,
				Depth:        c.Depth,
				AuthDepth:    c.AuthDepth,
				MaxTrees:     c.MaxTrees,
				MaxAuthTrees: c.MaxAuthTrees,
				MaxFeeTokens: c.MaxFeeTokens,
			})

			// Assert - the type is dispatched and yields a circuit.
			if err != nil {
				t.Fatalf("newCircuit(%s, type=%s): %v", c.ID, c.Type, err)
			}
			if circuit == nil {
				t.Fatalf("newCircuit(%s, type=%s) returned a nil circuit", c.ID, c.Type)
			}
		})
		seenTypes[c.Type]++
	}

	// Assert - the config actually exercises every type this package dispatches,
	// so a silently unreachable arm cannot pass as covered.
	for _, want := range []CircuitType{
		CircuitTypeEpoch,
		CircuitTypeDeposit,
		CircuitTypeForced,
		CircuitTypePortal,
		CircuitTypeGiftClaim,
	} {
		if seenTypes[string(want)] == 0 {
			t.Errorf("production config exercises no %q circuit", want)
		}
	}
}

// TestNewCircuitRejectsUnknownType keeps the default arm meaningful: an
// unrecognised type must fail loudly rather than resolve to some other circuit.
func TestNewCircuitRejectsUnknownType(t *testing.T) {
	// Arrange - a spec whose type no arm handles.
	spec := CircuitSpec{ID: "x1", Name: "x1", Type: CircuitType("not_a_circuit_type"), Depth: 24}

	// Act - resolve it through the production dispatch.
	circuit, err := newCircuit(spec)

	// Assert - the dispatch rejects it.
	if err == nil {
		t.Fatal("expected an error for an unknown circuit type, got nil")
	}
	if circuit != nil {
		t.Fatalf("expected a nil circuit for an unknown type, got %T", circuit)
	}
}
