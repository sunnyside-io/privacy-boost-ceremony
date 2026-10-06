// Package finalize exports Groth16 proving and verifying keys from final phase2 artifacts.
package finalize

import (
	"fmt"
	"os"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/groth16/bn254/mpcsetup"
	cs_bn254 "github.com/consensys/gnark/constraint/bn254"
)

// ExportKeysFromPhase2 verifies contribution chain and writes resulting pk/vk files.
// cache may be nil, in which case phase1Path is parsed and sealed inline as before;
// passing a shared *Phase1Cache lets multiple circuits resolved to the same Phase1
// power reuse one parse within a run.
func ExportKeysFromPhase2(
	phase1Path,
	r1csPath string,
	contributionPhase2Paths []string,
	pkOutPath,
	vkOutPath string,
	cache *Phase1Cache,
) error {
	commons, err := resolveCommons(cache, phase1Path)
	if err != nil {
		return err
	}

	// Load r1cs constraint system.
	r1csFile, err := os.Open(r1csPath)
	if err != nil {
		return err
	}
	defer r1csFile.Close()

	// Declare local variables used in this stage.
	var r1cs cs_bn254.R1CS
	if _, err := r1cs.ReadFrom(r1csFile); err != nil {
		return err
	}

	// Stream the contribution chain, retaining only the previous and next Phase2
	// object at any time. This inlines mpcsetup.VerifyPhase2's own logic
	// (Initialize -> pairwise Verify -> Seal) one contribution at a time instead of
	// requiring the full chain materialized up front, so peak retention is O(1)
	// rather than O(len(contributionPhase2Paths)).
	prev := new(mpcsetup.Phase2)
	evals := prev.Initialize(&r1cs, &commons)
	for _, phase2Path := range contributionPhase2Paths {
		next, err := readPhase2Artifact(phase2Path)
		if err != nil {
			return err
		}
		if err := prev.Verify(next); err != nil {
			return err
		}
		prev = next
	}

	// Seal the final contribution and extract proving/verifying keys.
	pk, vk := prev.Seal(&commons, &evals, nil)

	// Return the computed result to the caller.
	return writeKeys(pk, vk, pkOutPath, vkOutPath)
}

// ExportKeysFromPhase2WithCachedEvaluations uses setup-time cached evaluations to skip re-initialize.
// cache may be nil; see ExportKeysFromPhase2 for the shared-cache contract.
func ExportKeysFromPhase2WithCachedEvaluations(
	phase1Path,
	originPhase2Path string,
	contributionPhase2Paths []string,
	evals *mpcsetup.Phase2Evaluations,
	pkOutPath,
	vkOutPath string,
	cache *Phase1Cache,
) error {
	// Evaluate the guard condition for this branch.
	if evals == nil {
		return fmt.Errorf("cached evaluations are required")
	}

	commons, err := resolveCommons(cache, phase1Path)
	if err != nil {
		return err
	}

	// Load origin phase2 and walk contribution chain.
	origin, err := readPhase2Artifact(originPhase2Path)
	if err != nil {
		return err
	}

	// Compute values for the next processing step.
	prev := origin

	// Iterate through each item in this collection.
	for _, phase2Path := range contributionPhase2Paths {
		next, err := readPhase2Artifact(phase2Path)
		if err != nil {
			return err
		}

		// Check the operation result and return on error.
		if err := prev.Verify(next); err != nil {
			return err
		}

		// Advance the pointer to the latest verified state.
		prev = next
	}

	// Seal final contribution with cached evals and persist keys.
	pk, vk := prev.Seal(&commons, evals, nil)
	return writeKeys(pk, vk, pkOutPath, vkOutPath)
}

// readPhase2Artifact loads and deserializes a phase2 artifact from disk.
func readPhase2Artifact(path string) (*mpcsetup.Phase2, error) {
	// Open the required input file from disk.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Compute values for the next processing step.
	p2 := new(mpcsetup.Phase2)
	if _, err := p2.ReadFrom(f); err != nil {
		return nil, err
	}

	// Return the computed result to the caller.
	return p2, nil
}

// writeKeys persists proving and verifying keys to the given output paths.
func writeKeys(pk groth16.ProvingKey, vk groth16.VerifyingKey, pkOutPath, vkOutPath string) error {
	// Write proving key.
	pkFile, err := os.Create(pkOutPath)
	if err != nil {
		return err
	}
	defer pkFile.Close()

	// Check the operation result and return on error.
	if _, err := pk.WriteTo(pkFile); err != nil {
		return err
	}

	// Write verifying key.
	vkFile, err := os.Create(vkOutPath)
	if err != nil {
		return err
	}
	defer vkFile.Close()

	// Check the operation result and return on error.
	if _, err := vk.WriteTo(vkFile); err != nil {
		return err
	}

	// Finish successfully after all steps complete.
	return nil
}
