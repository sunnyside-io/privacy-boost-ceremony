# Privacy Boost Ceremony Client

Contributor CLI for the Privacy Boost trusted setup ceremony. This tool lets participants authenticate, compute phase2 contributions locally, and submit them to the coordinator. It also includes an offline verification tool for published ceremony bundles.

## Quickstart (Contributor)

Download and run the standalone contributor script — no need to clone the repository first:

```bash
curl -fsSLO https://raw.githubusercontent.com/testinprod-io/privacy-boost-ceremony/main/circuit-setup/contribute.sh
bash contribute.sh
```

The script presents an interactive menu to choose how to obtain the ceremony binary:

1. **Download pre-built release** — fastest, downloads a verified binary from GitHub Releases.
2. **Build from source (local Go)**. Clones the repo at the release tag and builds with your Go toolchain (requires Go 1.25.13+).
3. **Build from source (Docker)** — clones the repo and builds + runs inside a Docker container. No local toolchain needed.

For more details, see `circuit-setup/contributor-guide.md`.

## Commands

```bash
# Contribute to the ceremony
ceremony contribute --config <path> --coordinator-url <url>
  [--state-dir <dir>] [--quiet] [--no-browser]

# Verify a published ceremony bundle offline
ceremony verify-public --bundle-dir <dir> [--quiet]
  [--require-anchor] [--rpc-url <url>]
  [--anchor-chain-id <id>] [--anchor-tx-hash <hash>] [--min-confirmations <n>]
```

At the end of a successful `contribute` run, the CLI asks whether to save a
richer `contribution-receipt.json`. If you answer `yes`, the receipt and local
input/output `.ph2` artifacts are kept in the state directory. If you answer
`no`, those local artifacts are cleaned up.

## Building

```bash
CGO_ENABLED=0 go build -o ./bin/ceremony ./cmd/ceremony
```

## Native production round

The active `production.ceremony.config.json` is the exact numbered Round 4 preset, `production.ceremony.2026-04.config.json`. It selects 19 native configurations and excludes retired s12, s25, m8 and l6. Setup and finalization each use one worker. All selected native relations use new keys, including deposit and portal. The numbered Round 1, 2 and 3 presets preserve their historical bytes. The rehearsal preset uses the same selected shapes in separate disposable state.
