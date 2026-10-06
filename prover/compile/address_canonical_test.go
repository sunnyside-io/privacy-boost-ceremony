package compile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
	"github.com/testinprod-io/privacy-boost-protocol/pkg/addressprotocol"
)

func TestCanonicalAddressSpecPublicInputs(t *testing.T) {
	for _, shape := range []addressprotocol.Shape{
		{Family: addressprotocol.Epoch, Batch: 2, Inputs: 1, Outputs: 1, Depth: 4, AuthDepth: 4, FeeTokens: 1},
		{Family: addressprotocol.Deposit, Batch: 2, Depth: 4},
		{Family: addressprotocol.Portal, Batch: 2, Depth: 4},
		{Family: addressprotocol.Forced, Batch: 1, Inputs: 2, Depth: 4, AuthDepth: 4},
		{Family: addressprotocol.GiftClaim, Batch: 2, Depth: 4, AuthDepth: 4},
		{Family: addressprotocol.NoteMigration, Batch: 3, Depth: 4},
	} {
		t.Run(string(shape.Family), func(t *testing.T) {
			spec, err := AddressCircuitSpec(shape)
			require.NoError(t, err)
			require.Equal(t, shape.Key(), spec.ID)
			require.Equal(t, 16, spec.MaxTrees)
			if shape.AuthDepth == 0 {
				require.Zero(t, spec.MaxAuthTrees)
			} else {
				require.Equal(t, 16, spec.MaxAuthTrees)
			}
			if shape.Family == addressprotocol.Forced {
				require.EqualValues(t, shape.Inputs, spec.MaxInputs)
				require.Zero(t, spec.MaxInPerTx)
			} else {
				require.Zero(t, spec.MaxInputs)
				require.EqualValues(t, shape.Inputs, spec.MaxInPerTx)
			}
			circuit, err := NewAddressCircuit(shape)
			require.NoError(t, err)
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, circuit)
			require.NoError(t, err)
			public, err := shape.PublicInputs()
			require.NoError(t, err)
			require.EqualValues(t, public, ccs.GetNbPublicVariables()-1)
		})
	}
}

func TestCanonicalAddressCompilationRejectsBeforeCreatingArtifacts(t *testing.T) {
	shape := addressprotocol.Shape{Family: addressprotocol.Deposit, Batch: 1, Inputs: 1, Depth: 4}
	dir := filepath.Join(t.TempDir(), "invalid")
	_, err := CompileAddressWithMetadata(shape, dir)
	require.Error(t, err)
	_, err = NewAddressCircuit(shape)
	require.Error(t, err)
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
}

func TestCanonicalAddressCompilationUsesSharedSpecCache(t *testing.T) {
	shape := addressprotocol.Shape{Family: addressprotocol.Deposit, Batch: 1, Depth: 4}
	dir := t.TempDir()
	result, err := CompileAddressWithMetadata(shape, dir)
	require.NoError(t, err)
	before := setSentinelModTime(t, result.R1CSPath)
	spec, err := AddressCircuitSpec(shape)
	require.NoError(t, err)
	cached, err := CompileWithMetadata(spec, dir)
	require.NoError(t, err)
	after, err := os.Stat(cached.R1CSPath)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime())
	require.Equal(t, result, cached)
}

func TestCanonicalAddressCompilationInvalidatesVersion20Cache(t *testing.T) {
	for _, shape := range []addressprotocol.Shape{
		{Family: addressprotocol.Epoch, Batch: 1, Inputs: 1, Outputs: 1, Depth: 4, AuthDepth: 4, FeeTokens: 1},
		{Family: addressprotocol.Portal, Batch: 1, Depth: 4},
	} {
		t.Run(string(shape.Family), func(t *testing.T) {
			// Arrange - pin the stale version so forgetting a version bump cannot
			// make the test pass by moving its old-version fixture with the compiler.
			dir := t.TempDir()
			first, err := CompileAddressWithMetadata(shape, dir)
			require.NoError(t, err)
			metaPath := first.R1CSPath + ".meta.json"
			stale, err := loadCompileMetadata(metaPath)
			require.NoError(t, err)
			stale.CacheVersion = 20
			require.NoError(t, writeCompileMetadata(metaPath, *stale))
			before := setSentinelModTime(t, first.R1CSPath)
			beforeMeta := setSentinelModTime(t, metaPath)

			// Act - compile the exact same shape through the native entrypoint.
			rebuilt, err := CompileAddressWithMetadata(shape, dir)
			require.NoError(t, err)

			// Assert - replace both artifacts without changing the shape fingerprint.
			after, err := os.Stat(rebuilt.R1CSPath)
			require.NoError(t, err)
			require.True(t, after.ModTime().After(before.ModTime()))
			afterMeta, err := os.Stat(metaPath)
			require.NoError(t, err)
			require.True(t, afterMeta.ModTime().After(beforeMeta.ModTime()))
			current, err := loadCompileMetadata(metaPath)
			require.NoError(t, err)
			require.Equal(t, compileCacheVersion, current.CacheVersion)
			require.Greater(t, current.CacheVersion, 20)
			require.Equal(t, stale.SpecHash, current.SpecHash)
			require.Equal(t, rebuilt.NbConstraints, current.NbConstraints)
			require.Equal(t, rebuilt.DomainSize, current.DomainSize)
			require.Equal(t, rebuilt.RequiredPhase1Power, current.RequiredPhase1Power)

			encoded, err := os.ReadFile(rebuilt.R1CSPath)
			require.NoError(t, err)
			ccs := groth16.NewCS(ecc.BN254)
			_, err = ccs.ReadFrom(bytes.NewReader(encoded))
			require.NoError(t, err)
			require.Equal(t, rebuilt.NbConstraints, ccs.GetNbConstraints())
			public, err := shape.PublicInputs()
			require.NoError(t, err)
			require.EqualValues(t, public+1, ccs.GetNbPublicVariables())

			// Reset timestamps so an unexpected rewrite is observable even on
			// filesystems whose timestamp resolution is coarser than compilation.
			beforeReuse := setSentinelModTime(t, rebuilt.R1CSPath)
			beforeMetaReuse := setSentinelModTime(t, metaPath)
			cached, err := CompileAddressWithMetadata(shape, dir)
			require.NoError(t, err)
			require.Equal(t, rebuilt, cached)
			reused, err := os.Stat(cached.R1CSPath)
			require.NoError(t, err)
			require.Equal(t, beforeReuse.ModTime(), reused.ModTime())
			reusedMeta, err := os.Stat(metaPath)
			require.NoError(t, err)
			require.Equal(t, beforeMetaReuse.ModTime(), reusedMeta.ModTime())
		})
	}
}
