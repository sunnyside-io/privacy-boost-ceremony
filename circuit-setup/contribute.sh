#!/usr/bin/env bash
set -euo pipefail

# Standalone public contributor entrypoint.
#
# This script mirrors the public ceremony UX from sunnyside-io/privacy-boost-ceremony:
# contributors can download one script, enter the coordinator URL, choose how to
# obtain the ceremony binary, and then run the existing `ceremony contribute`
# client with the current config-based backend flow.

DEFAULT_RELEASE_REPO="sunnyside-io/privacy-boost-ceremony"
# The signer is a separate identity from the asset host. Releases are built and
# signed by the backend workflow, then republished to the public ceremony repo,
# so deriving the identity from RELEASE_REPO would check the wrong signer.
DEFAULT_SIGNER_REPO="sunnyside-io/privacy-boost-backend"
DEFAULT_SOURCE_REF="main"
DEFAULT_CONFIG_RELPATH="circuit-setup/configs/production.ceremony.config.json"
# The config asset name published on every release. Round-independent on
# purpose: each release carries its OWN round's config under this one name.
DEFAULT_CONFIG_ASSET="production.ceremony.config.json"
DEFAULT_RUN_DIR="${PWD}/privacy-boost-ceremony"
# The round this script currently serves. Override per round with --coordinator-url.
DEFAULT_COORDINATOR_URL="http://68.183.252.249:8790"

RELEASE_REPO="${CEREMONY_RELEASE_REPO:-$DEFAULT_RELEASE_REPO}"
SIGNER_REPO="${CEREMONY_SIGNER_REPO:-$DEFAULT_SIGNER_REPO}"
SOURCE_REF="${CEREMONY_SOURCE_REF:-$DEFAULT_SOURCE_REF}"
CONFIG_URL="${CEREMONY_CONFIG_URL:-https://raw.githubusercontent.com/${RELEASE_REPO}/${SOURCE_REF}/${DEFAULT_CONFIG_RELPATH}}"
CONFIG_URL_EXPLICIT=0
CONFIG_EXPLICIT=0
CONFIG_PATH="${CEREMONY_CONFIG_PATH:-}"
# Which ceremony round to contribute to. Empty means the current round,
# whichever one the newest release carries.
ROUND="${CEREMONY_ROUND:-}"
COORDINATOR_URL="${CEREMONY_COORDINATOR_URL:-$DEFAULT_COORDINATOR_URL}"
RUN_DIR="${CEREMONY_WORK_DIR:-$DEFAULT_RUN_DIR}"
BUILD_MODE="${CEREMONY_BUILD_MODE:-}"
RELEASE_VERSION="${CEREMONY_RELEASE_VERSION:-}"
# Keyless cosign trust anchor for verifying the release signature bundle. The
# signer is pinned to this repo's ceremony-release workflow at a ceremony/v* tag,
# independent of the GitHub Releases channel that also serves the tarball + .sha256.
CEREMONY_OIDC_ISSUER="${CEREMONY_OIDC_ISSUER:-https://token.actions.githubusercontent.com}"
CEREMONY_SIGNER_IDENTITY_REGEXP="${CEREMONY_SIGNER_IDENTITY_REGEXP:-}"
# Pinned cosign build fetched when the contributor has none installed, so the
# published two-command flow keeps its signature check without asking anyone to
# install a signing tool first. A fetched cosign is used only after its digest
# matches the value baked in below, so it is trusted exactly as far as this
# script is, and it never lands on the contributor's PATH.
COSIGN_VERSION="${CEREMONY_COSIGN_VERSION:-v3.1.3}"
# Set to 1 to accept the published SHA256 alone and skip the signature check.
# The interactive prompt sets this when the contributor declines the fetch. A
# checksum is served from the same release page as the binary, so this is a
# real reduction in what the download proves, never a silent default.
SKIP_SIGNATURE_VERIFICATION="${CEREMONY_SKIP_SIGNATURE_VERIFICATION:-0}"
COSIGN_DOWNLOAD_BASE="${CEREMONY_COSIGN_DOWNLOAD_BASE:-https://github.com/sigstore/cosign/releases/download}"
ALLOW_INSECURE_HTTP="${CEREMONY_ALLOW_INSECURE_HTTP:-1}"
QUIET="${CEREMONY_QUIET:-}"
NO_BROWSER_OPT="${CEREMONY_NO_BROWSER:-}"
CEREMONY_BIN=""
SOURCE_DIR=""
WORK_DIR=""
COSIGN_BIN=""
COSIGN_DIR=""

usage() {
  cat <<'EOF'
Usage:
  bash contribute.sh [--coordinator-url http://host] [--config path.json] [--config-url https://...] [--build-mode auto|release|local|docker] [--release-version X.Y.Z] [--source-ref git-ref] [--work-dir path] [--allow-insecure-http] [--skip-signature-verification] [--quiet] [--no-browser]

Recommended public flow (pins to a signed release tag instead of the mutable main branch):
  1) Find the latest tag at https://github.com/sunnyside-io/privacy-boost-ceremony/releases
  2) curl -fsSLO https://github.com/sunnyside-io/privacy-boost-ceremony/releases/download/<tag>/contribute.sh
     curl -fsSLO https://github.com/sunnyside-io/privacy-boost-ceremony/releases/download/<tag>/contribute.sh.cosign.bundle
  3) cosign verify-blob --bundle contribute.sh.cosign.bundle \
       --certificate-identity "https://github.com/sunnyside-io/privacy-boost-backend/.github/workflows/ceremony-release.yml@refs/tags/<tag>" \
       --certificate-oidc-issuer https://token.actions.githubusercontent.com \
       contribute.sh
  4) bash contribute.sh --release-version <tag>

Quick start without script verification (fetches contribute.sh itself from the mutable main branch,
though the downloaded ceremony binary is still signature-verified either way):
  curl -fsSLO https://raw.githubusercontent.com/sunnyside-io/privacy-boost-ceremony/main/circuit-setup/contribute.sh
  bash contribute.sh

Environment overrides:
  CEREMONY_COORDINATOR_URL=...   Coordinator server URL (defaults to the round this script serves)
  CEREMONY_CONFIG_PATH=...       Use a local config file
  CEREMONY_CONFIG_URL=...        Download config from this URL
  CEREMONY_RELEASE_REPO=...      Default: sunnyside-io/privacy-boost-ceremony
  CEREMONY_SIGNER_REPO=...       Default: sunnyside-io/privacy-boost-backend
  CEREMONY_SOURCE_REF=...        Default: main
  CEREMONY_BUILD_MODE=...        auto, release, local, or docker
  CEREMONY_RELEASE_VERSION=...   GitHub release tag or version
  CEREMONY_ROUND=...             Round to contribute to, e.g. 2026-02 (defaults to the current round)
  CEREMONY_WORK_DIR=...          Persistent local state directory
  CEREMONY_ALLOW_INSECURE_HTTP=0 Re-arm the plaintext-HTTP guard (enabled by default while the coordinator has no TLS)
  CEREMONY_QUIET=1              Pass --quiet to ceremony contribute
  CEREMONY_NO_BROWSER=1         Pass --no-browser to ceremony contribute (also honors a bare NO_BROWSER=1)

Build modes:
  release   Download a verified prebuilt binary from GitHub Releases
  local     Clone source and build with local Go through the repo quickstart
  docker    Clone source and build inside a generic Go container as a build
            sandbox (same trust level as local; not a verified release channel)
  auto      Try release, then local, then Docker
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --coordinator-url)
      COORDINATOR_URL="$2"
      shift 2
      ;;
    --config)
      CONFIG_PATH="$2"
      shift 2
      ;;
    --config-url)
      CONFIG_URL="$2"
      CONFIG_URL_EXPLICIT=1
      CONFIG_PATH=""
      shift 2
      ;;
    --build-mode)
      BUILD_MODE="$2"
      shift 2
      ;;
    --release-version)
      RELEASE_VERSION="$2"
      shift 2
      ;;
    --round)
      ROUND="$2"
      shift 2
      ;;
    --release-repo)
      RELEASE_REPO="$2"
      shift 2
      ;;
    --source-ref)
      SOURCE_REF="$2"
      shift 2
      ;;
    --work-dir)
      RUN_DIR="$2"
      shift 2
      ;;
    --allow-insecure-http)
      ALLOW_INSECURE_HTTP=1
      shift
      ;;
    --skip-signature-verification)
      SKIP_SIGNATURE_VERIFICATION=1
      shift
      ;;
    --quiet)
      QUIET=1
      shift
      ;;
    --no-browser)
      NO_BROWSER_OPT=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf '[ceremony] error: unknown arg: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -n "${CEREMONY_CONFIG_URL:-}" ]]; then
  CONFIG_URL_EXPLICIT=1
fi
if [[ -z "${CONFIG_PATH}" && "${CONFIG_URL_EXPLICIT}" == "0" ]]; then
  CONFIG_URL="https://raw.githubusercontent.com/${RELEASE_REPO}/${SOURCE_REF}/${DEFAULT_CONFIG_RELPATH}"
fi
if [[ -n "${CONFIG_PATH}" || "${CONFIG_URL_EXPLICIT}" == "1" ]]; then
  CONFIG_EXPLICIT=1
fi
if [[ -n "${ALLOW_INSECURE_HTTP}" && "${ALLOW_INSECURE_HTTP}" != "0" ]]; then
  # Both run_binary's direct invocation and run_repo_quickstart's delegated
  # contribute_quickstart.sh read this same env var, so exporting it once
  # here covers either downstream path without per-call-site flag-forwarding.
  export CEREMONY_ALLOW_INSECURE_HTTP=1
fi

log() {
  printf '[ceremony] %s\n' "$*"
}

warn() {
  printf '[ceremony] warning: %s\n' "$*" >&2
}

fail() {
  printf '[ceremony] error: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n "${WORK_DIR}" && -d "${WORK_DIR}" ]]; then
    rm -rf "${WORK_DIR}"
  fi
  if [[ -n "${COSIGN_DIR}" && -d "${COSIGN_DIR}" ]]; then
    rm -rf "${COSIGN_DIR}"
  fi
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    return 1
  fi
}

abs_path() {
  local path="$1"
  local dir base
  dir="$(cd "$(dirname "${path}")" && pwd)"
  base="$(basename "${path}")"
  printf '%s/%s\n' "${dir}" "${base}"
}

normalize_release_tag() {
  local version="$1"
  if [[ -z "${version}" ]]; then
    return 1
  fi
  if [[ "${version}" == ceremony/v* ]]; then
    printf '%s\n' "${version}"
    return 0
  fi
  if [[ "${version}" == v* ]]; then
    printf 'ceremony/%s\n' "${version}"
    return 0
  fi
  printf 'ceremony/v%s\n' "${version}"
}

# Accepts either the full round id or just its date suffix, so --round 2026-01
# and --round prod-ceremony-2026-01 both resolve to the same round.
normalize_round_id() {
  local round="$1"
  if [[ "${round}" == prod-ceremony-* ]]; then
    printf '%s\n' "${round}"
    return 0
  fi
  printf 'prod-ceremony-%s\n' "${round}"
}

normalize_build_mode() {
  case "${BUILD_MODE}" in
    ""|menu)
      BUILD_MODE=""
      ;;
    prebuilt)
      BUILD_MODE="release"
      ;;
    release|local|docker|auto)
      ;;
    *)
      fail "unsupported build mode: ${BUILD_MODE}"
      ;;
  esac
}

# Set when preflight_check_cosign has already warned about a missing cosign, so
# verify_release_signature doesn't print the same warning a second time when the
# preflight's prediction plays out.
COSIGN_MISSING_WARNED=0

# Resolves cosign immediately after the build mode is known, before any release
# download begins, so the fetch happens up front and a machine that cannot get
# cosign at all says so before other setup work runs rather than failing deep
# inside verify_release_signature. Only runs for modes that can reach the
# prebuilt release path.
preflight_check_cosign() {
  case "${BUILD_MODE}" in
    release|auto) ;;
    *) return 0 ;;
  esac
  if ! signature_verification_enabled; then
    warn "signature verification is disabled, the download will be checked against its published checksum only."
    return 0
  fi
  if command -v cosign >/dev/null 2>&1; then
    COSIGN_BIN="cosign"
    return 0
  fi
  prompt_cosign_choice
  if ! signature_verification_enabled; then
    return 0
  fi
  if ensure_cosign; then
    return 0
  fi
  COSIGN_MISSING_WARNED=1
  if [[ "${BUILD_MODE}" == "release" ]]; then
    warn "no usable cosign: --build-mode release verifies the downloaded binary's signature before running it, and this run will abort rather than use an unverified download."
  else
    warn "no usable cosign: the release path cannot verify the downloaded binary's signature, so this run will fall back to a source build."
  fi
  warn "Install cosign: https://docs.sigstore.dev/system_config/installation"
}

show_banner() {
  cat <<'EOF'

  Privacy Boost trusted setup ceremony

  This script will prepare the ceremony client, open GitHub device auth,
  and submit your contribution to the coordinator.

EOF
}

show_menu() {
  cat <<'EOF'
  How would you like to obtain the ceremony binary?

    1) Download prebuilt release          fastest
    2) Build from source with local Go    requires Go and build tools
    3) Build from source with Docker      requires Docker

EOF
}

prompt_coordinator_url() {
  if [[ -n "${COORDINATOR_URL}" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    fail "coordinator URL is required, pass --coordinator-url or CEREMONY_COORDINATOR_URL"
  fi
  printf '  Enter the coordinator URL: '
  read -r COORDINATOR_URL
  if [[ -z "${COORDINATOR_URL}" ]]; then
    fail "coordinator URL is required"
  fi
}

prompt_build_mode() {
  normalize_build_mode
  if [[ -n "${BUILD_MODE}" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    BUILD_MODE="auto"
    return 0
  fi
  show_menu
  local choice
  while true; do
    printf '  Enter your choice [1-3]: '
    read -r choice
    case "${choice}" in
      1)
        BUILD_MODE="release"
        return 0
        ;;
      2)
        BUILD_MODE="local"
        return 0
        ;;
      3)
        BUILD_MODE="docker"
        return 0
        ;;
      *)
        warn "invalid choice, enter 1, 2, or 3"
        ;;
    esac
  done
}

prepare_run_dir() {
  mkdir -p "${RUN_DIR}"
  RUN_DIR="$(abs_path "${RUN_DIR}")"
}

prepare_config() {
  prepare_run_dir
  if [[ -n "${CONFIG_PATH}" ]]; then
    if [[ ! -f "${CONFIG_PATH}" ]]; then
      fail "config not found: ${CONFIG_PATH}"
    fi
    CONFIG_PATH="$(abs_path "${CONFIG_PATH}")"
    log "Using local config: ${CONFIG_PATH}"
    return 0
  fi

  require_cmd curl || fail "curl is required to download the ceremony config"
  CONFIG_PATH="${RUN_DIR}/ceremony.config.json"

  # Prefer a round-scoped config over whatever main currently holds. That is
  # what keeps a build-from-source contributor on the same round as everyone
  # else instead of on the moving target main represents.
  if [[ "${CONFIG_URL_EXPLICIT}" == "0" ]]; then
    if resolve_round_config; then
      return 0
    fi
    warn "falling back to the config on main, which tracks the CURRENT round only and carries no signature"
  fi

  log "Downloading ceremony config from ${CONFIG_URL}"
  curl -fL --progress-bar "${CONFIG_URL}" -o "${CONFIG_PATH}"
}

detect_platform() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "${os}" in
    Linux)
      GOOS="linux"
      ;;
    Darwin)
      GOOS="darwin"
      ;;
    *)
      fail "unsupported OS: ${os}"
      ;;
  esac
  case "${arch}" in
    x86_64|amd64)
      GOARCH="amd64"
      ;;
    arm64|aarch64)
      GOARCH="arm64"
      ;;
    *)
      fail "unsupported architecture: ${arch}"
      ;;
  esac
}

file_sha256() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${path}" | awk '{print $1}'
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk '{print $1}'
    return 0
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "${path}" | awk '{print $NF}'
    return 0
  fi
  return 1
}

# Resolve the cosign identity flag/value pair used to verify release artifacts.
# Defaults to an exact match on the resolved release tag so a signature from a
# different ceremony release cannot verify. CEREMONY_SIGNER_IDENTITY_REGEXP
# overrides to pattern matching for callers that need it (e.g. a fork).
resolve_signer_identity() {
  local release_tag="$1"
  if [[ -n "${CEREMONY_SIGNER_IDENTITY_REGEXP:-}" ]]; then
    SIGNER_IDENTITY_FLAG="--certificate-identity-regexp"
    SIGNER_IDENTITY_VALUE="${CEREMONY_SIGNER_IDENTITY_REGEXP}"
  else
    SIGNER_IDENTITY_FLAG="--certificate-identity"
    SIGNER_IDENTITY_VALUE="https://github.com/${SIGNER_REPO}/.github/workflows/ceremony-release.yml@refs/tags/${release_tag}"
  fi
}

# Asks whether to fetch cosign when the contributor has none. Interactive input
# only: a non-interactive run has nobody to ask, so it leaves the fetch in place
# rather than silently dropping verification. Use --skip-signature-verification
# to opt out there.
prompt_cosign_choice() {
  if [[ ! -t 0 ]]; then
    return 0
  fi
  local choice
  cat <<'EOF'

  cosign is not installed, so this run cannot verify the ceremony binary's signature.

  The download is also checked against a published SHA256, but that checksum comes from
  the same release page as the binary, so only the signature proves the binary was built
  by the release workflow.

  1) Download cosign for this run (about 130 MB, kept in a temp dir, removed on exit)
  2) Continue without verifying the signature

EOF
  while true; do
    printf '  Enter your choice [1-2]: '
    read -r choice
    case "${choice}" in
      1)
        return 0
        ;;
      2)
        SKIP_SIGNATURE_VERIFICATION=1
        warn "continuing without signature verification at your request, the download is checksum-only."
        return 0
        ;;
      *)
        warn "invalid choice, enter 1 or 2"
        ;;
    esac
  done
}

# Whether this run should verify the release signature at all.
signature_verification_enabled() {
  [[ "${SKIP_SIGNATURE_VERIFICATION}" != "1" ]]
}

# Upstream's published checksums for COSIGN_VERSION, one per platform this
# script supports. macOS ships bash 3.2, which has no associative arrays, so
# this is a case rather than a map.
cosign_expected_sha256() {
  case "$1-$2" in
    darwin-amd64) printf '%s\n' '2347488e5d5b25336644024dfeca5601b190e91197a71a917bda44744aff106c' ;;
    darwin-arm64) printf '%s\n' '5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76' ;;
    linux-amd64) printf '%s\n' '4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71' ;;
    linux-arm64) printf '%s\n' 'c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a' ;;
    *) return 1 ;;
  esac
}

# Resolves the cosign used to verify the release signature, preferring one the
# contributor already has and otherwise fetching the pinned build. Sets
# COSIGN_BIN and returns 0 on success, returns 1 (after warning) when no usable
# cosign could be obtained. Memoized, so repeated calls cost nothing.
#
# The fetched binary is checked against cosign_expected_sha256 before it runs.
# That digest lives in this script, so a fetched cosign is no more trusted than
# the script itself: an attacker who could substitute the script could equally
# well skip the verification altogether, so fetching adds no new trust root.
ensure_cosign() {
  if [[ -n "${COSIGN_BIN}" ]]; then
    return 0
  fi
  if command -v cosign >/dev/null 2>&1; then
    COSIGN_BIN="cosign"
    return 0
  fi
  if ! require_cmd curl; then
    warn "curl is required to fetch cosign, cannot verify the release signature."
    return 1
  fi
  detect_platform
  local want
  if ! want="$(cosign_expected_sha256 "${GOOS}" "${GOARCH}")"; then
    warn "no pinned cosign checksum for ${GOOS}/${GOARCH}, cannot verify the release signature."
    return 1
  fi
  COSIGN_DIR="$(mktemp -d)"
  trap cleanup EXIT
  local dest="${COSIGN_DIR}/cosign"
  log "cosign not installed, fetching the pinned ${COSIGN_VERSION} build to verify the release signature"
  if ! curl -fL --progress-bar "${COSIGN_DOWNLOAD_BASE}/${COSIGN_VERSION}/cosign-${GOOS}-${GOARCH}" -o "${dest}"; then
    warn "could not download cosign ${COSIGN_VERSION} for ${GOOS}/${GOARCH}."
    return 1
  fi
  local got
  got="$(file_sha256 "${dest}" | tr '[:upper:]' '[:lower:]')"
  # A mismatch means the fetched cosign is not the pinned build, so treat it the
  # same as a tampered release artifact and stop rather than falling back.
  if [[ -z "${got}" || "${got}" != "${want}" ]]; then
    fail "fetched cosign ${COSIGN_VERSION} does not match its pinned checksum, refusing to use it"
  fi
  chmod 0755 "${dest}"
  COSIGN_BIN="${dest}"
  return 0
}

# Verify a detached keyless-cosign signature bundle for a release artifact.
# Returns 0 on success, 1 if cosign is unavailable (caller may fall back to a
# source build), 2 if verification fails (a tampering signal, caller must abort).
verify_release_signature() {
  local bundle="$1" artifact="$2" identity_flag="$3" identity_value="$4"
  if ! ensure_cosign; then
    if [[ "${COSIGN_MISSING_WARNED}" -eq 0 ]]; then
      warn "no usable cosign, cannot verify the release signature."
      warn "Install cosign (https://docs.sigstore.dev/system_config/installation) or rerun with --build-mode local."
    fi
    return 1
  fi
  if ! "${COSIGN_BIN}" verify-blob \
    --bundle "${bundle}" \
    "${identity_flag}" "${identity_value}" \
    --certificate-oidc-issuer "${CEREMONY_OIDC_ISSUER}" \
    "${artifact}" >/dev/null 2>&1; then
    return 2
  fi
  return 0
}

resolve_release_tag() {
  if [[ -n "${RELEASE_VERSION}" ]]; then
    normalize_release_tag "${RELEASE_VERSION}"
    return 0
  fi

  require_cmd curl || return 1
  local releases_json tag
  releases_json="$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -H "User-Agent: privacy-boost-ceremony" \
    "https://api.github.com/repos/${RELEASE_REPO}/releases?per_page=20" 2>/dev/null || true)"
  if [[ -z "${releases_json}" ]]; then
    return 1
  fi

  if command -v python3 >/dev/null 2>&1; then
    tag="$(printf '%s' "${releases_json}" | python3 -c '
import json
import sys

for release in json.load(sys.stdin):
    if release.get("draft") or release.get("prerelease"):
        continue
    tag = release.get("tag_name", "")
    if tag.startswith("ceremony/v"):
        print(tag)
        break
' 2>/dev/null || true)"
  else
    tag="$(printf '%s' "${releases_json}" | grep -oE '"tag_name":[[:space:]]*"ceremony/v[^"]*"' | head -n 1 | grep -oE 'ceremony/v[^"]*' || true)"
  fi

  if [[ -z "${tag}" ]]; then
    return 1
  fi
  printf '%s\n' "${tag}"
}

# Downloads a round's ceremony config from its release tag and verifies it
# against the signer identity pinned for that tag.
#
# The release, not main, is the source of truth for a round's config. main
# carries only the CURRENT round, so resolving from main hands a past-round
# contributor the wrong circuit shapes, and it leaves a closed round's config
# mutable. Writes the verified config to $2.
#
# Returns 1 when the tag publishes no signed config or the signature could not
# be checked, so each caller decides whether to fall back or refuse. A signature
# that is present and BAD is fatal here rather than a return, because that is
# tampering, not a missing artifact.
fetch_pinned_config() {
  local release_tag="$1" dest="$2"
  local base_url tmp rc=0
  base_url="https://github.com/${RELEASE_REPO}/releases/download/${release_tag}"
  tmp="$(mktemp -d)"

  log "Downloading signed config from ${release_tag}"
  if ! curl -fL --progress-bar "${base_url}/${DEFAULT_CONFIG_ASSET}" -o "${tmp}/ceremony.config.json"; then
    warn "no signed config published with ${release_tag}"
    rm -rf "${tmp}"
    return 1
  fi
  if ! curl -fL --progress-bar "${base_url}/${DEFAULT_CONFIG_ASSET}.cosign.bundle" -o "${tmp}/ceremony.config.json.cosign.bundle"; then
    warn "no signed config bundle published with ${release_tag}"
    rm -rf "${tmp}"
    return 1
  fi

  # Resolved here rather than inherited: the binary path sets these only when
  # signature verification is enabled, and this helper is also reached from the
  # build-from-source path where that block never ran.
  resolve_signer_identity "${release_tag}"
  verify_release_signature "${tmp}/ceremony.config.json.cosign.bundle" "${tmp}/ceremony.config.json" "${SIGNER_IDENTITY_FLAG}" "${SIGNER_IDENTITY_VALUE}" || rc=$?
  if [[ "${rc}" -eq 2 ]]; then
    rm -rf "${tmp}"
    fail "release config signature verification FAILED for ${release_tag}, the config may be tampered with, aborting"
  elif [[ "${rc}" -ne 0 ]]; then
    warn "could not verify the signed release config for ${release_tag}"
    rm -rf "${tmp}"
    return 1
  fi

  cp "${tmp}/ceremony.config.json" "${dest}"
  rm -rf "${tmp}"
  log "Using pinned config published with ${release_tag}"
}

# Reads the round id out of a ceremony config. Mirrors resolve_release_tag's
# python3-preferred, grep-fallback shape so a host without python3 still works.
config_round_id() {
  local file="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c '
import json
import sys

with open(sys.argv[1]) as handle:
    print(json.load(handle).get("id", ""))
' "${file}" 2>/dev/null || true
    return 0
  fi
  grep -oE '"id":[[:space:]]*"[^"]*"' "${file}" 2>/dev/null | head -n 1 |
    sed -E 's/.*"id":[[:space:]]*"([^"]*)".*/\1/' || true
}

# Resolves the config for the requested round, or for the current round when
# --round was not given.
#
# Signed release asset first. A past round then falls back to its round-scoped
# file in the tree, because only the round that was CURRENT at release time was
# ever published as an asset, so an older round is reachable only from the tree
# and only unsigned. Returns 1 so the caller can apply its own fallback.
resolve_round_config() {
  local pinned_tag="" carried="" round_url=""

  if [[ -n "${ROUND}" ]]; then
    ROUND="$(normalize_round_id "${ROUND}")"
  fi

  if pinned_tag="$(resolve_release_tag)" && [[ -n "${pinned_tag}" ]] &&
    fetch_pinned_config "${pinned_tag}" "${CONFIG_PATH}"; then
    carried="$(config_round_id "${CONFIG_PATH}")"
    if [[ -z "${ROUND}" || "${ROUND}" == "${carried}" ]]; then
      return 0
    fi
    warn "release ${pinned_tag} carries round ${carried:-unknown}, not ${ROUND}"
  fi

  if [[ -z "${ROUND}" ]]; then
    return 1
  fi

  round_url="https://raw.githubusercontent.com/${RELEASE_REPO}/${SOURCE_REF}/circuit-setup/configs/${ROUND}.config.json"
  log "Downloading round config from ${round_url}"
  if ! curl -fL --progress-bar "${round_url}" -o "${CONFIG_PATH}"; then
    fail "no config found for round ${ROUND}, neither a signed release asset nor a round-scoped file in the tree"
  fi
  warn "round ${ROUND} config came from the tree and carries NO signature"
}

download_prebuilt_release() {
  require_cmd curl || return 1
  require_cmd tar || return 1
  detect_platform
  local release_tag asset base_url expected actual
  release_tag="$(resolve_release_tag)" || return 1
  asset="ceremony-${GOOS}-${GOARCH}.tar.gz"
  base_url="https://github.com/${RELEASE_REPO}/releases/download/${release_tag}"
  WORK_DIR="$(mktemp -d)"
  trap cleanup EXIT

  log "Downloading ${asset} from ${release_tag}"
  curl -fL --progress-bar "${base_url}/${asset}" -o "${WORK_DIR}/${asset}" || return 1
  curl -fL --progress-bar "${base_url}/${asset}.sha256" -o "${WORK_DIR}/${asset}.sha256" || return 1

  expected="$(awk 'NR==1 {print $1}' "${WORK_DIR}/${asset}.sha256" | tr '[:upper:]' '[:lower:]')"
  actual="$(file_sha256 "${WORK_DIR}/${asset}" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "${expected}" || -z "${actual}" || "${expected}" != "${actual}" ]]; then
    fail "checksum verification failed for ${asset}"
  fi

  if signature_verification_enabled; then
    resolve_signer_identity "${release_tag}"
    log "Downloading ${asset}.cosign.bundle from ${release_tag}"
    if ! curl -fL --progress-bar "${base_url}/${asset}.cosign.bundle" -o "${WORK_DIR}/${asset}.cosign.bundle"; then
      warn "no cosign signature bundle published for ${asset}, refusing the prebuilt path"
      return 1
    fi
    local vrc=0
    verify_release_signature "${WORK_DIR}/${asset}.cosign.bundle" "${WORK_DIR}/${asset}" "${SIGNER_IDENTITY_FLAG}" "${SIGNER_IDENTITY_VALUE}" || vrc=$?
    if [[ "${vrc}" -eq 2 ]]; then
      fail "release signature verification FAILED for ${asset}, the download may be tampered with, aborting"
    elif [[ "${vrc}" -ne 0 ]]; then
      return 1
    fi
    log "Verified release signature for ${asset}"
  else
    warn "SIGNATURE NOT VERIFIED for ${asset}. The checksum matched, but a checksum served alongside the binary does not prove who built it."
  fi

  # When the contributor did not choose a config, pair the binary with the config
  # published alongside it. Never leave a prebuilt release paired with the
  # mutable config from main.
  if [[ "${CONFIG_EXPLICIT}" == "0" ]]; then
    if ! fetch_pinned_config "${release_tag}" "${RUN_DIR}/ceremony.config.json"; then
      warn "refusing the prebuilt path without a verified config"
      return 1
    fi
    CONFIG_PATH="${RUN_DIR}/ceremony.config.json"
  fi

  tar -xzf "${WORK_DIR}/${asset}" -C "${WORK_DIR}"
  if [[ ! -f "${WORK_DIR}/ceremony" ]]; then
    fail "release archive did not contain a ceremony binary"
  fi
  cp "${WORK_DIR}/ceremony" "${RUN_DIR}/ceremony"
  chmod 0755 "${RUN_DIR}/ceremony"
  CEREMONY_BIN="${RUN_DIR}/ceremony"
  if ! "${CEREMONY_BIN}" version --expect "${release_tag}"; then
    fail "downloaded binary's stamped version does not match release ${release_tag}, the archive may be tampered with or mismatched"
  fi
  log "Prebuilt binary ready: ${CEREMONY_BIN}"
}

clone_source_repo() {
  require_cmd git || fail "git is required to build from source"
  mkdir -p "${RUN_DIR}"
  SOURCE_DIR="${RUN_DIR}/source"
  local ref
  if [[ -n "${RELEASE_VERSION}" ]]; then
    ref="$(normalize_release_tag "${RELEASE_VERSION}")"
  else
    ref="${SOURCE_REF}"
  fi

  if [[ -d "${SOURCE_DIR}/.git" ]]; then
    log "Updating source checkout at ${SOURCE_DIR}"
    git -C "${SOURCE_DIR}" fetch --depth 1 origin "${ref}"
    git -C "${SOURCE_DIR}" checkout --detach FETCH_HEAD
    return 0
  fi
  if [[ -e "${SOURCE_DIR}" ]]; then
    fail "${SOURCE_DIR} exists but is not a git checkout, pass --work-dir to use a clean directory"
  fi
  log "Cloning ${RELEASE_REPO} at ${ref}"
  git clone --depth 1 --branch "${ref}" "https://github.com/${RELEASE_REPO}.git" "${SOURCE_DIR}"
}

run_binary() {
  local args=("${CEREMONY_BIN}" contribute --config "${CONFIG_PATH}" --coordinator-url "${COORDINATOR_URL}")
  if [[ -n "${QUIET}" ]]; then
    args+=(--quiet)
  fi
  if [[ -n "${NO_BROWSER_OPT}" || -n "${NO_BROWSER:-}" ]]; then
    args+=(--no-browser)
  fi
  log "Starting contribution against ${COORDINATOR_URL}"
  (cd "${RUN_DIR}" && "${args[@]}")
}

run_repo_quickstart() {
  local mode="$1"
  if [[ -z "${CONFIG_PATH}" ]]; then
    prepare_config
  fi
  clone_source_repo
  local args=(circuit-setup/contribute_quickstart.sh --config "${CONFIG_PATH}" --coordinator-url "${COORDINATOR_URL}" --build-mode "${mode}" --release-repo "${RELEASE_REPO}" --signer-repo "${SIGNER_REPO}")
  if [[ -n "${QUIET}" ]]; then
    args+=(--quiet)
  fi
  if [[ -n "${NO_BROWSER_OPT}" || -n "${NO_BROWSER:-}" ]]; then
    args+=(--no-browser)
  fi
  (cd "${SOURCE_DIR}" && bash "${args[@]}")
}

run_auto() {
  if download_prebuilt_release; then
    run_binary
    return 0
  fi
  warn "prebuilt release unavailable, trying local build"
  if run_repo_quickstart local; then
    return 0
  fi
  warn "local build unavailable, trying Docker"
  run_repo_quickstart docker
}

main() {
  show_banner
  prompt_coordinator_url
  prompt_build_mode
  preflight_check_cosign
  prepare_run_dir

  case "${BUILD_MODE}" in
    release)
      if [[ "${CONFIG_EXPLICIT}" == "1" ]]; then
        prepare_config
      fi
      download_prebuilt_release || fail "could not obtain a signature-verified ceremony binary, see the warnings above for the cause"
      run_binary
      ;;
    local)
      prepare_config
      run_repo_quickstart local
      ;;
    docker)
      prepare_config
      run_repo_quickstart docker
      ;;
    auto)
      if [[ "${CONFIG_EXPLICIT}" == "1" ]]; then
        prepare_config
      fi
      run_auto
      ;;
    *)
      fail "unsupported build mode: ${BUILD_MODE}"
      ;;
  esac
}

main "$@"
