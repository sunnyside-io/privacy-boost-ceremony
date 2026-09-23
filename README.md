# Privacy Boost Ceremony

Contributor CLI and verification tools for the [Privacy Boost](https://github.com/testinprod-io) trusted setup ceremony.

The ceremony uses Groth16 multi-party computation (MPC) via [gnark](https://github.com/Consensys/gnark). Each contributor generates local randomness, mixes it into the phase2 parameters, and submits the result. As long as at least one participant is honest and destroys their randomness, the final parameters are secure.

## Status

Three production ceremony rounds have finalized. Each one is recorded under [`rounds/`](rounds/), with its circuit shapes, release tags, config file and config checksum.

- [First round, `prod-ceremony-2026-01`](rounds/2026-01.md). Complete.
- [Second round, `prod-ceremony-2026-02`](rounds/2026-02.md). Complete. Its deposit and portal keys are retained in the planned round-three upgrade.
- [Third round, `prod-ceremony-2026-03`](rounds/2026-03.md). Finalized on 2026-09-23, covering 12 circuits and 192 contributions. The round record pins the final bundle digests and describes the planned deployment scope.

`main` always carries the current round's config. Past rounds stay reachable through their own record above and through the release tag they ran under.

**Downloads for the first two rounds** are listed below. Round three is finalized, but its public download URL is not recorded here yet. Finalization does not establish deployment on any network.

Second round, `prod-ceremony-2026-02`, 21 circuits and 490 contributions:

- **Public bundle:** https://file.ceremony.privacyboost.io/prod-20260902-public.tar
- **Keys:** https://file.ceremony.privacyboost.io/prod-20260902-keys.tar

First round, `prod-ceremony-2026-01`, 18 circuits:

- **Public bundle:** https://file.ceremony.privacyboost.io/prod-20260401-public.tar.gz
- **Keys:** https://file.ceremony.privacyboost.io/prod-20260401-keys.tar.gz

### Verify

The current source targets round 3 and matches the backend `ceremony/v0.0.5` circuit sources and compiler (gnark v0.16.3, gnark-crypto v0.21.0, Go 1.25.13). Use the release or source revision recorded for an earlier round when verifying its bundle. Circuit compilation differs between rounds, so the current source cannot reconstruct earlier rounds' keys.

For a round-3 bundle, build the current source and run:

```bash
go build -o ./bin/ceremony ./cmd/ceremony
./bin/ceremony verify-public --bundle-dir <BUNDLE_DIR>
```

Full verification of the second round took just under 8 hours on an M1 Pro MacBook, covering all 21 circuits and 490 contributions, and not counting the time to download the bundle. That run reused an already-converted powers-of-tau cache, so allow roughly 3 more hours on a first run, when the verifier fetches about 2.4 GB of powers-of-tau files and converts them. The first round's bundle is about three times larger and its full verification took under 30 hours on the same machine.

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
