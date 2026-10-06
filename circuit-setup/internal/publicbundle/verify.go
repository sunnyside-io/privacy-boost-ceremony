package publicbundle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	provercompile "github.com/testinprod-io/privacy-boost-ceremony/prover/compile"

	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/cache"
	finalizepkg "github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/finalize"
	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/model"
	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/phase1"
)

// Verify performs the highest-assurance offline verification for one exported public bundle.
//
// What this guarantees (assuming the local verifier machine is trusted):
//   - The bundle is self-consistent and tamper-evident via manifest-bound SHA256 hashes.
//   - The transcript chain is consistent: origin -> contribution outputs -> final.
//   - The proving/verifying keys in the bundle correspond to the transcript AND the compiled circuit:
//     they are re-derived from (phase1, R1CS, ordered Phase2 outputs) and compared to the manifest hashes.
//
// Why this matters:
//   - Pure hash verification proves "the bundle wasn’t modified after export".
//   - Key re-derivation additionally proves "the exported pk/vk were generated from the transcript",
//     which detects a compromised or buggy finalize/export step that could otherwise write mismatched keys.
//
// Performance note:
//   - R1CS artifacts are compiled into a fresh temporary directory so verification cannot reuse
//     the same compile output that produced the bundle.
//   - Phase1 artifacts remain reusable under cfg.Cache.RootDir because their pinned checksums make
//     that reuse independent from the coordinator's circuit compile cache.
func Verify(bundleDir string, cfg *model.CeremonyConfig, opts VerifyOptions) error {
	return verifyWithCompiler(bundleDir, cfg, opts, provercompile.CompileWithMetadata)
}

type circuitCompiler func(
	provercompile.CircuitSpec,
	string,
) (*provercompile.CompileResult, error)

func verifyWithCompiler(
	bundleDir string,
	cfg *model.CeremonyConfig,
	opts VerifyOptions,
	compile circuitCompiler,
) error {
	// Require a config so the verifier can locate and (re)build the same cached inputs
	// (R1CS + Phase1) that the coordinator would use for this circuit set.
	if cfg == nil {
		return fmt.Errorf("ceremony config is required for verify")
	}

	// Step 1: verify the bundle's internal integrity (hash commitments, transcript adjacency,
	// participant list reconstruction, and optional Ethereum anchor checks).
	//
	// We do this first because it is fast and provides clearer failure modes than a later
	// "pk/vk mismatch" that could be caused by a much earlier corruption.
	if err := VerifyIntegrity(bundleDir, opts); err != nil {
		return err
	}

	// Step 2: load the manifest so we can iterate per-circuit and re-derive keys.
	//
	// VerifyIntegrity already parsed the manifest, but this function intentionally remains
	// self-contained to keep correctness independent from call ordering/implementation details.
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}

	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}

	// Step 3: initialize cache paths used for compile + phase1 reuse.
	//
	// The cache root can differ from stateDir; we always prefer cfg.Cache.RootDir as the
	// reusable cache location so verification can share artifacts with coordinator setup.
	cm := cache.New(cfg.StateDir, cfg.Cache.RootDir)
	if err := cm.Ensure(); err != nil {
		return err
	}

	// Step 4: prepare phase1 resolver (ptau fetch + ptau->ph1 conversion) for derived-power lookups.
	//
	// This resolver is deterministic given the config's checksum map: it always selects a power,
	// obtains the corresponding `.ptau` or `.ph1`, and ensures its SHA256 matches the pinned value.
	ph1 := phase1.New()

	// An independent verifier compiles from source with no cache at all, and the keys stage
	// only proves reproducibility if it does the same. Compiling into the shared cache would
	// reuse the very entry that produced this manifest, making the r1cs comparison below a
	// check against itself. Phase1 and deserialize stay on the shared cache because they
	// derive from the pinned ptau rather than from circuit source.
	compileDir := cm.CompileDir()
	if !opts.ReuseCompileCache {
		isolated, err := os.MkdirTemp(cm.CompileDir(), "verify-")
		if err != nil {
			return err
		}
		defer func() {
			if rmErr := os.RemoveAll(isolated); rmErr != nil {
				logf(
					opts,
					"[ceremony][verify-public][keys] cleanup_compile_dir_failed compileDir=%s err=%v\n",
					isolated,
					rmErr,
				)
			}
		}()
		compileDir = isolated
	}

	// Step 5: per-circuit deep verification.
	//
	// Each circuit is verified independently: we recompile its R1CS, resolve phase1 for the derived
	// power, then export pk/vk from the ordered transcript outputs and compare the pk/vk hashes with
	// what the bundle manifest committed to.
	logf(opts,
		"[ceremony][verify-public][keys] start circuits=%d cacheRoot=%s compileDir=%s reuseCompileCache=%t\n",
		len(manifest.Circuits),
		cfg.Cache.RootDir,
		compileDir,
		opts.ReuseCompileCache,
	)
	if err := verifyCircuitKeySet(manifest.Circuits, func(
		circuit CircuitManifest,
		phase1Cache *finalizepkg.Phase1Cache,
	) error {
		return verifyCircuitKeys(bundleDir, cfg, cm, ph1, circuit, opts, phase1Cache, compile, compileDir)
	}); err != nil {
		return err
	}

	logf(opts, "[ceremony][verify-public][keys] complete\n")
	return nil
}

// verifyCircuitKeySet gives every circuit in one Verify call the same Phase1
// cache, so circuits with a shared power parse and seal the SRS only once.
func verifyCircuitKeySet(
	circuits []CircuitManifest,
	verify func(CircuitManifest, *finalizepkg.Phase1Cache) error,
) error {
	phase1Cache := finalizepkg.NewPhase1Cache()
	for _, circuit := range circuits {
		if err := verify(circuit, phase1Cache); err != nil {
			return fmt.Errorf("verify circuit %s: %w", circuit.CircuitID, err)
		}
	}
	return nil
}

// verifyCircuitKeys performs the "key re-derivation" portion of Verify for one circuit.
//
// It intentionally uses only inputs committed to by the bundle manifest (spec JSON + transcript outputs)
// and the verifier's locally derived artifacts (R1CS + phase1) to prove that the published pk/vk hashes
// are consistent with the transcript.
func verifyCircuitKeys(
	bundleDir string,
	cfg *model.CeremonyConfig,
	cm *cache.Manager,
	ph1 *phase1.Service,
	circuit CircuitManifest,
	opts VerifyOptions,
	phase1Cache *finalizepkg.Phase1Cache,
	compile circuitCompiler,
	compileDir string,
) error {
	logf(opts, "[ceremony][verify-public][keys] circuit_start id=%s\n", circuit.CircuitID)

	// Load the circuit spec JSON from the manifest. This JSON is already hash-committed in the
	// manifest (circuitSpecSha256), so using it here binds key derivation to exactly what the
	// public bundle claims the circuit parameters are.
	if strings.TrimSpace(circuit.CircuitSpecJSON) == "" {
		return fmt.Errorf("missing circuit spec json")
	}

	var spec model.CircuitSpec
	if err := json.Unmarshal([]byte(circuit.CircuitSpecJSON), &spec); err != nil {
		return fmt.Errorf("parse circuit spec json: %w", err)
	}

	// Ensure we have a stable circuit ID. In normal exports the ID is present in the spec JSON.
	// This fallback keeps verification robust against malformed specs while still binding to the
	// manifest circuit identity.
	if strings.TrimSpace(spec.ID) == "" {
		spec.ID = circuit.CircuitID
	}

	// Compile the R1CS for this exact spec. compileDir is a fresh directory unless the caller
	// opted into cache reuse, so this normally recompiles from the circuit source the verifier
	// actually holds. That is what gives the hash comparison below its meaning: the compile
	// cache keys on the spec alone, so a source change that moves the constraint count would
	// otherwise be served from cache and never detected.
	compileRes, err := compile(toCompileSpec(spec), compileDir)
	if err != nil {
		return fmt.Errorf("compile r1cs: %w", err)
	}
	logf(opts,
		"[ceremony][verify-public][keys] r1cs_ready id=%s requiredPhase1Power=%d constraints=%d domain=%d\n",
		circuit.CircuitID,
		compileRes.RequiredPhase1Power,
		compileRes.NbConstraints,
		compileRes.DomainSize,
	)
	if compileRes.RequiredPhase1Power != circuit.DerivedPhase1Power {
		return fmt.Errorf(
			"derived phase1 power mismatch: manifest=%d computed=%d",
			circuit.DerivedPhase1Power,
			compileRes.RequiredPhase1Power,
		)
	}

	r1csDigest, err := digest(compileRes.R1CSPath)
	if err != nil {
		return err
	}
	if r1csDigest != circuit.R1CSSHA256 {
		return fmt.Errorf(
			"r1cs hash mismatch after compilation: manifest=%s computed=%s",
			circuit.R1CSSHA256,
			r1csDigest,
		)
	}

	// Resolve the Phase1 artifact for the derived power. This is critical: Phase1 must match the
	// R1CS size constraints, and its integrity is enforced by cfg.Phase1.ExpectedSHA256ByPower.
	// Resolve (and checksum-verify) phase1 and produce a gnark-compatible `.ph1` path.
	_, phase1Path, err := ph1.EnsurePhase1(
		cfg.Phase1,
		cm.Phase1Dir(),
		cm.DeserializeDir(),
		compileRes.RequiredPhase1Power,
	)
	if err != nil {
		return fmt.Errorf("resolve phase1: %w", err)
	}
	logf(opts,
		"[ceremony][verify-public][keys] phase1_ready id=%s power=%d phase1Path=%s\n",
		circuit.CircuitID,
		compileRes.RequiredPhase1Power,
		phase1Path,
	)

	phase1Digest, err := digest(phase1Path)
	if err != nil {
		return err
	}
	if phase1Digest != circuit.Phase1SHA256 {
		return fmt.Errorf("phase1 hash mismatch after resolution")
	}

	// Extract the ordered Phase2 transcript outputs from the public manifest.
	//
	// We deliberately use the manifest's ordering/indexing (already validated by VerifyIntegrity) so
	// key derivation is tied to the exact published transcript chain.
	if len(circuit.Contributions) == 0 {
		return fmt.Errorf("no contributions recorded in public manifest")
	}

	contribOuts := make([]string, 0, len(circuit.Contributions))
	for i, rec := range circuit.Contributions {
		p, err := absBundlePath(bundleDir, rec.OutputPath)
		if err != nil {
			return err
		}

		// Belt-and-suspenders: VerifyIntegrity already enforces this invariant, but we keep the
		// check local to this step so misordered transcripts cannot accidentally be "verified"
		// via unintended iteration semantics.
		if rec.Index != i+1 {
			return fmt.Errorf("contribution index mismatch: got=%d expected=%d", rec.Index, i+1)
		}

		contribOuts = append(contribOuts, p)
	}

	// Export keys into a temporary directory so verification is side-effect free: it never writes
	// into the bundle directory or the coordinator's state directory.
	tmpDir, err := os.MkdirTemp("", "ceremony_verify_*")
	if err != nil {
		return err
	}

	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			logf(
				opts,
				"[ceremony][verify-public][keys] cleanup_tmpdir_failed tmpDir=%s err=%v\n",
				tmpDir,
				rmErr,
			)
		}
	}()

	pkTmp := filepath.Join(tmpDir, circuit.CircuitID+".pk")
	vkTmp := filepath.Join(tmpDir, circuit.CircuitID+".vk")

	// Cryptographically derive pk/vk from (phase1, R1CS, transcript outputs).
	//
	// This is the same "full" finalize path, so it verifies the transcript chain and produces keys
	// that should match exactly what the coordinator exported when producing the public bundle.
	if err := finalizepkg.ExportKeysFromPhase2(
		phase1Path,
		compileRes.R1CSPath,
		contribOuts,
		pkTmp,
		vkTmp,
		phase1Cache,
	); err != nil {
		return fmt.Errorf("export keys from transcript: %w", err)
	}
	logf(opts,
		"[ceremony][verify-public][keys] keys_derived id=%s pk=%s vk=%s\n",
		circuit.CircuitID,
		pkTmp,
		vkTmp,
	)

	// Hash the derived keys and compare to the public manifest commitments. We compare hashes instead
	// of byte-by-byte comparisons against bundle files because the bundle already commits to the hashes
	// and hash mismatch provides a concise, portable proof of disagreement.
	pkDigest, err := digest(pkTmp)
	if err != nil {
		return err
	}

	vkDigest, err := digest(vkTmp)
	if err != nil {
		return err
	}

	if pkDigest != circuit.PKSHA {
		return fmt.Errorf("pk hash mismatch after key derivation")
	}

	if vkDigest != circuit.VKSHA {
		return fmt.Errorf("vk hash mismatch after key derivation")
	}

	logf(opts, "[ceremony][verify-public][keys] circuit_ok id=%s\n", circuit.CircuitID)
	return nil
}

// toCompileSpec adapts ceremony circuit specs into the prover compiler's circuit model.
//
// This is a pure mapping function: the verifier uses the same circuit dimensions as coordinator
// setup so its fresh compile represents the circuit whose keys the bundle publishes.
func toCompileSpec(c model.CircuitSpec) provercompile.CircuitSpec {
	return provercompile.CircuitSpec{
		AssetFormat:  c.AssetFormat,
		ID:           c.ID,
		Name:         c.Name,
		Type:         provercompile.CircuitType(c.Type),
		BatchSize:    c.BatchSize,
		MaxInputs:    c.MaxInputs,
		MaxInPerTx:   c.MaxInPerTx,
		MaxOutPerTx:  c.MaxOutPerTx,
		Depth:        c.Depth,
		AuthDepth:    c.AuthDepth,
		MaxTrees:     c.MaxTrees,
		MaxAuthTrees: c.MaxAuthTrees,
		MaxFeeTokens: c.MaxFeeTokens,
	}
}
