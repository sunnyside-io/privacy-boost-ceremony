package compile

import (
	"github.com/consensys/gnark/frontend"
	"github.com/testinprod-io/privacy-boost-protocol/pkg/addressprotocol"
)

// AddressCircuitSpec is the canonical mapping used by standalone compilation
// and runtime proving. Unused dimensions stay zero; root capacities are fixed
// by the protocol rather than being additional, unhashed shape parameters.
func AddressCircuitSpec(shape addressprotocol.Shape) (CircuitSpec, error) {
	if err := shape.Validate(); err != nil {
		return CircuitSpec{}, err
	}

	spec := CircuitSpec{
		ID:           shape.Key(),
		Type:         CircuitType(shape.Family),
		AssetFormat:  AddressAssetFormat,
		BatchSize:    int(shape.Batch),
		MaxOutPerTx:  int(shape.Outputs),
		Depth:        int(shape.Depth),
		AuthDepth:    int(shape.AuthDepth),
		MaxFeeTokens: int(shape.FeeTokens),
		MaxTrees:     16,
	}

	// Forced withdrawals have one input set; epochs have an input set per transfer.
	if shape.Family == addressprotocol.Forced {
		spec.MaxInputs = int(shape.Inputs)
	} else {
		spec.MaxInPerTx = int(shape.Inputs)
	}
	if shape.AuthDepth != 0 {
		spec.MaxAuthTrees = 16
	}

	return spec, nil
}

// NewAddressCircuit validates the native shape and allocates its circuit with
// the protocol's fixed root capacities.
func NewAddressCircuit(shape addressprotocol.Shape) (frontend.Circuit, error) {
	spec, err := AddressCircuitSpec(shape)
	if err != nil {
		return nil, err
	}

	return newCircuit(spec)
}

// CompileAddressWithMetadata validates the native shape and emits its R1CS
// and compilation metadata in outDir.
func CompileAddressWithMetadata(shape addressprotocol.Shape, outDir string) (*CompileResult, error) {
	spec, err := AddressCircuitSpec(shape)
	if err != nil {
		return nil, err
	}

	return CompileWithMetadata(spec, outDir)
}
