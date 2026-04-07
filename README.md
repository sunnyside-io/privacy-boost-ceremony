# Privacy Boost Ceremony

Contributor CLI and verification tools for the [Privacy Boost](https://github.com/testinprod-io) trusted setup ceremony.

The ceremony uses Groth16 multi-party computation (MPC) via [gnark](https://github.com/Consensys/gnark). Each contributor generates local randomness, mixes it into the phase2 parameters, and submits the result. As long as at least one participant is honest and destroys their randomness, the final parameters are secure.

## Status

**Privacy Boost V1 Production Ceremony is complete.**

The public bundle and derived keys are available for download:

- **Public bundle:** https://file.ceremony.privacyboost.io/prod-20260401-public.tar.gz
- **Keys:** https://file.ceremony.privacyboost.io/prod-20260401-keys.tar.gz

### Verify

Download and extract the public bundle, then run:

```bash
go build -o ./bin/ceremony ./cmd/ceremony
./bin/ceremony verify-public --bundle-dir <BUNDLE_DIR>
```

Full verification took under 30 hours on an M1 Pro MacBook.

See [Public Verification](PUBLIC_VERIFICATION.md) for a detailed explanation of how verification works.

## Quick Start

No need to clone the repository — just download and run:

```bash
curl -fsSLO https://raw.githubusercontent.com/testinprod-io/privacy-boost-ceremony/main/circuit-setup/contribute.sh
bash contribute.sh
```

The script will prompt for the coordinator URL (provided by the ceremony coordinator), then present options to download a pre-built binary, build with local Go, or build with Docker.

See [Contributor Guide](circuit-setup/contributor-guide.md) for detailed instructions.

## License

See [LICENSE](LICENSE).
