package compile

import (
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

// Pin the contract's native input vectors and the dedicated NoteMigration schema.
func TestAddressPublicInputShapes(t *testing.T) {
	for _, tc := range []struct {
		kind   CircuitType
		public int
	}{
		{CircuitTypeEpoch, 51},
		{CircuitTypeDeposit, 34},
		{CircuitTypePortal, 37},
		{CircuitTypeForced, 31},
		{CircuitTypeGiftClaim, 57},
		{CircuitTypeNoteMigration, 29},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			spec := CircuitSpec{
				ID:           "address-" + string(tc.kind),
				Type:         tc.kind,
				AssetFormat:  AddressAssetFormat,
				BatchSize:    2,
				MaxInputs:    2,
				MaxInPerTx:   1,
				MaxOutPerTx:  1,
				Depth:        4,
				AuthDepth:    4,
				MaxFeeTokens: 1,
				MaxTrees:     16,
				MaxAuthTrees: 16,
			}
			c, err := newCircuit(spec)
			require.NoError(t, err)
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, c)
			require.NoError(t, err)
			require.Equal(t, tc.public, ccs.GetNbPublicVariables()-1)

			// Explicit and default native formats use distinct cache identities.
			addressHash, err := compileSpecHash(spec)
			require.NoError(t, err)
			spec.AssetFormat = ""
			defaultFormatHash, err := compileSpecHash(spec)
			require.NoError(t, err)
			require.NotEqual(t, defaultFormatHash, addressHash)
		})
	}

	_, err := newCircuit(CircuitSpec{Type: CircuitTypeDeposit, AssetFormat: "unknown"})
	require.Error(t, err)
}
