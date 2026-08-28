# Contributor Guide

This guide explains how participants contribute with a single command. Auth is handled by the command.

## Quickstart (Recommended)

Download and run the standalone contributor script. You do not need to clone the repository first.

Pin to a signed release tag instead of the mutable `main` branch:

```bash
# Find the latest tag at https://github.com/sunnyside-io/privacy-boost-ceremony/releases,
# then substitute it for <tag> below (for example ceremony/v1.2.3).
curl -fsSLO https://github.com/sunnyside-io/privacy-boost-ceremony/releases/download/<tag>/contribute.sh
curl -fsSLO https://github.com/sunnyside-io/privacy-boost-ceremony/releases/download/<tag>/contribute.sh.cosign.bundle

cosign verify-blob \
  --bundle contribute.sh.cosign.bundle \
  --certificate-identity "https://github.com/sunnyside-io/privacy-boost-backend/.github/workflows/ceremony-release.yml@refs/tags/<tag>" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  contribute.sh

bash contribute.sh --coordinator-url <COORDINATOR_URL> --release-version <tag>
```

The `--certificate-identity` above names a different repository from the download URLs on purpose. Releases are built and signed by the backend workflow, then republished to the public ceremony repository, so the identity must stay on the signing repository even though the assets are fetched from the public one. Changing it to match the download host makes verification fail.

Quick start without script verification (fetches `contribute.sh` from the mutable `main` branch, though the ceremony binary it downloads is still signature-verified either way):

```bash
curl -fsSLO https://raw.githubusercontent.com/sunnyside-io/privacy-boost-ceremony/main/circuit-setup/contribute.sh
bash contribute.sh --coordinator-url <COORDINATOR_URL>
```

The script stores downloaded files, source builds, and local ceremony state under `./privacy-boost-ceremony` by default.

For public releases, the published `contribute.sh` is equivalent:

```bash
bash circuit-setup/contribute.sh
```

If you build from source (menu Option 2 or 3) from a checkout that was cloned without
`--recurse-submodules`, the script fetches the required submodule automatically (requires
`git` plus network access). The prebuilt release path (Option 1) never clones, so this note
does not apply there.

The script presents an interactive menu:

```text
  1) Download prebuilt release          fastest
  2) Build from source with local Go    requires Go and build tools
  3) Build from source with Docker      requires Docker
```

- **Option 1** downloads a signature-verified prebuilt binary from GitHub Releases (see "Verifying Your Download" below).
- **Option 2** clones the repo and builds with your local Go toolchain.
- **Option 3** clones the repo and builds through Docker. Docker here is only a build sandbox for a source build, compiling the checked-out source inside a generic Go base image the same way Option 2 does on your host. It does not pull or verify any published container image, so it carries the same trust level as Option 2, not Option 1's verified release.

The default config is downloaded from the current public ceremony config path. Coordinators can override it with `--config` or `--config-url`.

Overrides (optional):

```bash
CEREMONY_CONFIG_URL="https://example.com/ceremony.config.json" \
bash contribute.sh --coordinator-url <COORDINATOR_URL>
```

Useful alternatives:

```bash
# Use a local config file.
bash contribute.sh \
  --coordinator-url <COORDINATOR_URL> \
  --config ./ceremony.config.json

# Pin quickstart to a specific ceremony release or source ref.
bash contribute.sh \
  --coordinator-url <COORDINATOR_URL> \
  --release-version 1.2.3

# Skip the menu and build through Docker.
bash contribute.sh \
  --coordinator-url <COORDINATOR_URL> \
  --build-mode docker
```

If you are already working from a repository checkout, the repo-local quickstart still works:

```bash
bash circuit-setup/contribute_quickstart.sh \
  --config circuit-setup/configs/production.ceremony.config.json \
  --coordinator-url <COORDINATOR_URL>
```

What to expect:

- The script prints setup milestones such as checking tools, downloading/building the CLI, and completion.
- The standalone script lets you choose prebuilt release, local build, or Docker build.
- After setup, it starts the contribution flow.
- You will see a message like: `Open https://github.com/login/device and enter code XXXX-XXXX`.
  Follow the link, enter the code, and return to the terminal.
- The CLI will proceed circuit-by-circuit until complete.
- Interrupted input downloads and output uploads resume from the last verified byte when you rerun the command.
- If local compute or verification fails, the CLI reports a safe failure reason so the coordinator can release the lease quickly.

## What Becomes Public

Before `contribute` starts the device-flow login, it prints a one-time notice: your GitHub login and the exact time of each contribution become part of the public transcript once the ceremony finalizes. This is not incidental logging. When the ceremony finalizes, the published `manifest.json` (see "Cross-checking after the ceremony finalizes" below) records every contribution's GitHub login and timestamp next to its `transcriptHash`, plus a ceremony-wide list of every participating login. Your login is cryptographically bound into that specific contribution's transcript hash, so changing it in the published `manifest.json` without also recomputing the hash chain is detectable by re-running verification. That file sits outside the ceremony's on-chain integrity anchor, which deliberately excludes it so a coordinator can correct manifest metadata after finalization, so the login's authenticity ultimately rests on trusting whoever publishes the finalized bundle, the same as any other manifest field. If you would rather not have a particular GitHub login publicly associated with this ceremony, use a different account you are comfortable disclosing.

## Requirements

- `curl` and `tar` for the recommended prebuilt release path.
- A checksum tool, such as `shasum`, `sha256sum`, or `openssl`.
- `cosign`: verifies the binary's signature before it runs. See "The download is signed, not just checksummed" below. You do not need to install it. If it is not on your PATH the script asks whether to download a pinned build for the run, about 130 MB, kept in a temp directory and removed on exit. Nothing is installed onto your system. Decline and the run continues on the published checksum alone. A signature that fails to verify still aborts the run either way.
- For local builds, Git plus Go and platform build tools.
- For Docker builds, Git plus Docker.
- The coordinator URL provided by the ceremony coordinator.

## Recommended Single-Command Flow

```bash
./bin/ceremony contribute \
  --config ./circuit-setup/configs/production.ceremony.config.json \
  --coordinator-url <COORDINATOR_URL>
```

By default, `contribute` prints step-by-step progress (claim retries, downloads, local compute, submits) and then prints contribution records. To reduce output, add `--quiet`. On a headless machine with no browser available, add `--no-browser` (or set `NO_BROWSER=1`) so the CLI only prints the device-flow URL and code instead of attempting to open one.

```bash
./bin/ceremony contribute \
  --config ./circuit-setup/configs/production.ceremony.config.json \
  --coordinator-url <COORDINATOR_URL> \
  --quiet
```

The command:

- runs GitHub Device Flow and mints a session token
- claims queue lease for each configured circuit
- downloads current input `.ph2` from coordinator
- computes phase2 locally on contributor machine
- uploads output `.ph2` to coordinator for verification and persistence
- prints contribution records

## Verifying Your Download and Your Contribution

Two things are worth verifying: that the binary you downloaded is the one this ceremony published, and that the contribution you made is the one the coordinator recorded. Both are checkable without trusting the download channel.

### The download is signed, not just checksummed

The prebuilt release (Option 1) ships three files per platform: the `.tar.gz` binary, a `.tar.gz.sha256` checksum, and a `.tar.gz.cosign.bundle` signature. A checksum only proves the file matches a hash served from the same place, so the script also verifies a keyless [cosign](https://docs.sigstore.dev) signature.

Keyless means there is no long-lived signing key. The release workflow signs with a short-lived identity issued by GitHub's OIDC provider, and the signature is recorded in the public Rekor transparency log.

The script verifies this automatically during Option 1. It pins the expected signer to the exact release tag you downloaded, so a binary served from a compromised release page, or a valid signature copied from a different tag, both fail the check and the script aborts before running it. If `cosign` is not installed, the script asks whether to fetch a pinned build for the run or to continue on the checksum alone. Pass `--skip-signature-verification` (or set `CEREMONY_SKIP_SIGNATURE_VERIFICATION=1`) to choose the second answer up front, which is also how automation opts out, since a non-interactive run is never asked and never skips the check on its own.

To verify the binary yourself, substitute the exact tag you downloaded (e.g. `ceremony/v1.2.3`) for `<tag>` below:

```bash
cosign verify-blob \
  --bundle ceremony-linux-amd64.tar.gz.cosign.bundle \
  --certificate-identity 'https://github.com/sunnyside-io/privacy-boost-backend/.github/workflows/ceremony-release.yml@refs/tags/<tag>' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ceremony-linux-amd64.tar.gz
```

Replace `ceremony-linux-amd64` with your platform (`linux-arm64`, `darwin-amd64`, or `darwin-arm64`). The pinned config is published and signed the same way, so verify it too:

```bash
cosign verify-blob \
  --bundle production.ceremony.config.json.cosign.bundle \
  --certificate-identity 'https://github.com/sunnyside-io/privacy-boost-backend/.github/workflows/ceremony-release.yml@refs/tags/<tag>' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  production.ceremony.config.json
```

The bootstrap script (`contribute.sh`) is published and signed the same way. See the pinned-and-verified flow in "Quickstart (Recommended)" above for the exact commands.

If a ceremony is published from a different repository, set `CEREMONY_RELEASE_REPO` to match its release workflow before running the script. To verify against a pattern instead of one exact tag (for example, a fork with its own tagging scheme), set `CEREMONY_SIGNER_IDENTITY_REGEXP` and pass `--certificate-identity-regexp` instead of `--certificate-identity` above.

### The binary is pinned to one config

Each release binary carries the SHA256 of the exact config it was built against. `contribute` recomputes the hash of the config you pass and refuses to start on a mismatch, so a config and binary from different releases cannot be combined by accident. To see what your binary is pinned to:

```bash
./bin/ceremony version
```

```text
ceremony CLI
version: 1.2.3
pinnedConfigSha256: 9f2c...
```

An unstamped local or dev build prints `(unstamped dev build)` and skips the pin.

### Reading your contribution attestation

After each accepted circuit, `contribute` prints one attestation line. It is printed even with `--quiet`, because it is your audit record. Save it.

```text
[ceremony][attestation] circuit=d32 name=deposit-d32 contributionIndex=3 transcriptHash=abc123... verified=true
```

| Field | What it tells you |
|---|---|
| `circuit` / `name` | which circuit you contributed to |
| `contributionIndex` | your position in that circuit's contribution chain |
| `transcriptHash` | a hash binding your input `.ph2`, your output `.ph2`, and your participant id |
| `verified` | whether the coordinator re-verified your output before recording it |

The `transcriptHash` is the value to keep. It is the fingerprint of your specific contribution and appears unchanged in the final published record.

### Your receipt file

At the end of a run, `contribute` also writes every accepted contribution to a JSON receipt so the record survives the terminal closing. The path is printed on the last line:

```text
[ceremony][contribute] receipt_written path=/.../.state/prod-ceremony-2026-02/receipts/prod-ceremony-2026-02-20260827T093000Z.json
```

The default location is a `receipts/` directory inside the `stateDir` from your config, named for the round and the completion time, so each run adds a file rather than overwriting the last one. Pass `--receipt /path/to/file.json` to choose the location yourself.

The file holds the ceremony id, the coordinator URL, your participant id, and one entry per accepted circuit with the same `transcriptHash` the attestation line printed. Keep it. It is what you compare against the published `manifest.json` below.

Writing the receipt is best effort. If the write fails the run still succeeds, because your contributions are already accepted and recorded by the coordinator, and the same data is on screen in the attestation lines and the JSON summary. You would see `receipt_write_failed` instead, and should copy the terminal output by hand.

### Cross-checking after the ceremony finalizes

When the ceremony finalizes, the coordinator publishes a public bundle. Its `manifest.json` lists every contribution with a `transcriptHash` field.

Find your record (your circuit and `contributionIndex`) and confirm its `transcriptHash` equals the one your attestation printed. A match proves the manifest you were handed lists your contribution unaltered. It does not, by itself, rule out the coordinator showing a different manifest to other verifiers. Closing that gap needs either comparing notes with other contributors and auditors, or an independent on-chain anchor, see below.

You can also re-derive and check every hash in the published bundle offline:

```bash
./bin/ceremony verify-public \
  --config ./circuit-setup/configs/production.ceremony.config.json \
  --bundle-dir /path/to/public \
  --mode integrity
```

`--mode integrity` checks the bundle's hashes, the transcript chain, and the on-chain anchor if you asked for one. That is everything the `transcriptHash` cross-check above depends on, and it finishes in seconds.

Dropping `--mode` runs the default `full` verification, which additionally re-derives every proving and verifying key from the transcript and compares them against the manifest. That is the stronger check, since it also catches a coordinator that published keys not generated from the transcript, but its cost scales with circuit size and it takes hours on a production-sized ceremony. Run `integrity` to confirm your own contribution quickly, and `full` when you are auditing the bundle as a whole.

### Independent inclusion: ask about the on-chain anchor

A coordinator can optionally publish an Ethereum transaction that anchors the exported bundle's root hash on-chain, then anyone can independently confirm the manifest they were handed matches the one anchored, without trusting the coordinator not to show different verifiers different manifests. This is the `verify-public --require-anchor` flow documented in the coordinator guide.

Anchoring is optional and coordinator-specific. Ask the coordinator whether this ceremony published an anchor, and if so, for the chain id and transaction hash so you can verify it yourself. If a ceremony did not anchor, the manifest cross-check above is still worth doing, but treat it as agreement between you and the coordinator's published manifest, not an independent, coordinator-can't-lie guarantee.

## Best Practices

- Run with a stable machine and avoid interrupting the command.
- Contribute only to assigned circuits to avoid unnecessary queue pressure.
- Keep the receipt file the run writes, and save the command output alongside it.
- If interrupted, rerun `contribute`.

## Common Errors

The CLI retries transient claim responses for you (`claim_busy` "contribution claim is
temporarily unavailable", `ceremony_paused` "ceremony is paused", and `rate_limited` "rate
limited"), so these usually clear without any action.

- `active lease limit reached` (`active_lease_limit`): another active lease exists for your
  identity. Let the current run finish, or wait for the lease to expire, then rerun.
- `session expired` or `session revoked`: rerun `contribute` to re-authenticate.
- `lease expired` or `lease not found`: your contribution slot was reclaimed. Rerun
  `contribute` to claim a fresh slot.
- phase1 or artifact read errors: verify config path and coordinator setup completion.
- verification errors: retry once. If repeated, contact the coordinator with your output log.

## Security Notes

- Do not share auth/session output with others.
- Run from a trusted environment.
- Avoid adding custom scripts that modify contribution files.
