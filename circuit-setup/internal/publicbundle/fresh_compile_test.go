package publicbundle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/cache"
	"github.com/testinprod-io/privacy-boost-ceremony/circuit-setup/internal/model"
	provercompile "github.com/testinprod-io/privacy-boost-ceremony/prover/compile"
)

// This checks compiler-directory selection. Real transcript derivation is
// exercised separately by TestVerifyDerivesKeysFromTranscript.
func TestPublicVerificationExcludesCoordinatorCompileCache(t *testing.T) {
	root := t.TempDir()
	bundle := writeBundleFixture(t, root)
	cfg := &model.CeremonyConfig{StateDir: root, Cache: model.CacheSpec{RootDir: root}}
	cm := cache.New(root, root)
	if err := cm.Ensure(); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(cm.CompileDir(), "c1.r1cs")
	if err := os.WriteFile(stale, []byte("coordinator cache"), 0600); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop after compiler probe")
	called := false
	err := verifyWithCompiler(bundle, cfg, VerifyOptions{}, func(spec provercompile.CircuitSpec, dir string) (*provercompile.CompileResult, error) {
		called = true
		if spec.ID != "c1" || spec.Type != provercompile.CircuitTypeDeposit || dir == cm.CompileDir() {
			t.Errorf("unexpected compiler inputs: %+v, directory %s", spec, dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Error("independent verification reused existing circuit artifacts")
		}
		return nil, stop
	})
	if !called || !errors.Is(err, stop) {
		t.Fatalf("compiler probe not reached or wrong error: %v", err)
	}
	data, err := os.ReadFile(stale)
	if err != nil || string(data) != "coordinator cache" {
		t.Fatalf("verification changed coordinator data: %q, %v", data, err)
	}
}

func TestPublicVerificationPreservesNativeShapeFormat(t *testing.T) {
	spec := model.CircuitSpec{ID: "migration", Type: model.CircuitTypeNoteMigrationV1, AssetFormat: provercompile.AddressAssetFormat, BatchSize: 16, Depth: 24, MaxTrees: 16}
	got := toCompileSpec(spec)
	if got.AssetFormat != provercompile.AddressAssetFormat || got.Type != provercompile.CircuitTypeNoteMigration || got.BatchSize != 16 || got.Depth != 24 || got.MaxTrees != 16 {
		t.Fatalf("native circuit identity or dimensions were dropped: %+v", got)
	}
}
