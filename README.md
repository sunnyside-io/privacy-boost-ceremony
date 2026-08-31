# Privacy Boost Ceremony

Contributor CLI and verification tools for the [Privacy Boost](https://github.com/testinprod-io) trusted setup ceremony.

The ceremony uses Groth16 multi-party computation (MPC) via [gnark](https://github.com/Consensys/gnark). Each contributor generates local randomness, mixes it into the phase2 parameters, and submits the result. As long as at least one participant is honest and destroys their randomness, the final parameters are secure.

## Status

There have been two production ceremony rounds. Each one is recorded under [`rounds/`](rounds/), with its circuit shapes, release tags, config file and config checksum.

- [First round, `prod-ceremony-2026-01`](rounds/2026-01.md). Complete.
- [Second round, `prod-ceremony-2026-02`](rounds/2026-02.md). Open, and the round `contribute.sh` currently serves.

`main` always carries the current round's config. Past rounds stay reachable through their own record above and through the release tag they ran under.

**The first round is complete.** Its public bundle and derived keys are available for download:

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
