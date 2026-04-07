# Public Verification

This document explains how anyone can independently verify the Privacy Boost V1 ceremony results using the `verify-public` command.

## What is Public Verification?

Public verification allows anyone to confirm that the ceremony's final proving and verifying keys were correctly derived from the chain of participant contributions. It ensures that no contributions were omitted, reordered, or tampered with, and that the published keys match what the transcript produces.

The verification is fully offline, deterministic, and side-effect free — it creates temporary files during execution and cleans them up afterward.

## How It Works

Verification runs in two phases:

### Phase 1: Integrity Verification

Fast checks that validate the bundle's internal consistency without any cryptographic re-derivation:

1. **Manifest validation** — loads `manifest.json` and rejects unsupported versions.
2. **Config snapshot binding** — recomputes the SHA-256 of the exported ceremony config and compares it to the manifest commitment.
3. **Bundle root hash** — recomputes a deterministic SHA-256 over all bundle files (excluding the manifest itself) and compares to the committed root.
4. **Contribution chain linkage** — for each circuit, walks the ordered contribution chain and verifies:
   - Each contribution's input hash matches the previous contribution's output hash (adjacency).
   - Step indices are sequential with no gaps or reordering.
   - Transcript hashes bind input, output, and participant identity together.
   - The final phase2 artifact hash matches the transcript head.
5. **Proving/verifying key digests** — hashes the published `.pk` and `.vk` files and compares them to manifest commitments.
6. **Participant and contribution counts** — reconstructs totals from circuit records and verifies they match the manifest-level aggregates.

### Phase 2: Deep Verification (Key Re-derivation)

Higher-assurance checks that cryptographically re-derive the proving and verifying keys from scratch:

1. **Compile R1CS** — compiles (or reuses cached) the constraint system for each circuit spec embedded in the manifest, then verifies the R1CS hash matches.
2. **Resolve Phase1 artifact** — fetches and validates the powers-of-tau file for the required power level, verifying its hash against the manifest.
3. **Re-derive keys** — uses the Phase1 artifact, compiled R1CS, and ordered Phase2 transcript outputs to independently derive the proving key (pk) and verifying key (vk).
4. **Compare key hashes** — hashes the re-derived keys and compares them to the manifest commitments. If they match, the published keys are exactly what the transcript produces.

Re-derived keys are created in a temporary directory and deleted after verification — no published artifacts are modified.

## Running Verification

### 1. Download and extract the public bundle

```bash
curl -LO https://file.ceremony.privacyboost.io/prod-20260401-public.tar.gz
tar xzf prod-20260401-public.tar.gz
```

### 2. Build the verification tool

```bash
go build -o ./bin/ceremony ./cmd/ceremony
```

### 3. Run verification

```bash
./bin/ceremony verify-public --bundle-dir <BUNDLE_DIR>
```

Full verification took under 30 hours on an M1 Pro MacBook.

### Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--bundle-dir` | Yes | Path to the extracted public bundle directory |
| `--quiet` | No | Suppress non-essential output |

## Security Properties

- **Deterministic** — the same bundle always produces the same verification result on any machine.
- **Tamper-evident** — any modification to contributions, keys, or metadata causes hash mismatches.
- **Chain-linked** — contributions form an unbroken hash chain; insertions, deletions, or reordering are detected.
- **Reproducible** — the verifier re-derives keys from first principles (Phase1 + R1CS + transcript) without trusting the coordinator.
- **Side-effect free** — verification only reads the bundle and creates temporary files that are cleaned up.
