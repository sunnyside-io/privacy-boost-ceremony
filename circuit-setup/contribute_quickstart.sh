#!/usr/bin/env bash
set -euo pipefail

# Contributor quickstart for the Privacy Boost ceremony.
# - Reuses a provided or existing ceremony binary when available
# - Downloads an official ceremony GitHub release binary when available
# - Detects local Go installs (PATH, shell-managed, mise/asdf/homebrew)
# - Falls back to Docker or a repo-local Go toolchain when needed
# - Creates the configured stateDir (required for downloads)
# - Runs `ceremony contribute`

DEFAULT_CONFIG="circuit-setup/configs/production.ceremony.config.json"

CONFIG_PATH="${CEREMONY_CONFIG_PATH:-$DEFAULT_CONFIG}"
COORDINATOR_URL="${CEREMONY_COORDINATOR_URL:-}"
ALLOW_INSECURE_HTTP="${CEREMONY_ALLOW_INSECURE_HTTP:-}"
QUIET="${CEREMONY_QUIET:-}"
NO_BROWSER_OPT="${CEREMONY_NO_BROWSER:-}"
CEREMONY_BIN="${CEREMONY_BINARY_PATH:-}"
BUILD_MODE="${CEREMONY_BUILD_MODE:-auto}"
CEREMONY_RELEASE_REPO="${CEREMONY_RELEASE_REPO:-sunnyside-io/privacy-boost-ceremony}"
# The signer is a separate identity from the asset host. Releases are built and
# signed by the backend workflow, then republished to the public ceremony repo,
# so a caller that points --release-repo at the public repo must not have the
# cosign identity follow it, or verification checks the wrong signer.
CEREMONY_SIGNER_REPO="${CEREMONY_SIGNER_REPO:-sunnyside-io/privacy-boost-backend}"
CEREMONY_RELEASE_VERSION="${CEREMONY_RELEASE_VERSION:-}"
CEREMONY_OIDC_ISSUER="${CEREMONY_OIDC_ISSUER:-https://token.actions.githubusercontent.com}"
CEREMONY_SIGNER_IDENTITY_REGEXP="${CEREMONY_SIGNER_IDENTITY_REGEXP:-}"
# Pinned cosign build fetched when the contributor has none installed, so the
# release path keeps its signature check without a separate install step. A
# fetched cosign is used only after its digest matches the value baked in below.
COSIGN_VERSION="${CEREMONY_COSIGN_VERSION:-v3.1.3}"
# Set to 1 to accept the published SHA256 alone and skip the signature check.
# The interactive prompt sets this when the contributor declines the fetch.
SKIP_SIGNATURE_VERIFICATION="${CEREMONY_SKIP_SIGNATURE_VERIFICATION:-0}"
COSIGN_DOWNLOAD_BASE="${CEREMONY_COSIGN_DOWNLOAD_BASE:-https://github.com/sigstore/cosign/releases/download}"
COSIGN_BIN=""
COSIGN_DIR=""
GO_BIN=""
GO_SOURCE=""

usage() {
  cat <<'EOF'
Usage:
  bash circuit-setup/contribute_quickstart.sh --coordinator-url http://host [--config path.json] [--binary path] [--release-version X.Y.Z] [--release-repo owner/repo] [--signer-repo owner/repo] [--build-mode auto|local|docker] [--allow-insecure-http] [--skip-signature-verification] [--quiet] [--no-browser]

Environment overrides:
  CEREMONY_CONFIG_PATH=...       (default: circuit-setup/configs/production.ceremony.config.json)
  CEREMONY_COORDINATOR_URL=...   (required; no default, must be set via this or --coordinator-url)
  CEREMONY_ALLOW_INSECURE_HTTP=1 (allow plaintext HTTP to a non-loopback coordinator; same as --allow-insecure-http)
  CEREMONY_SKIP_SIGNATURE_VERIFICATION=1 (accept the published checksum alone; same as --skip-signature-verification)
  CEREMONY_BINARY_PATH=...       (use an existing ceremony binary and skip build)
  CEREMONY_RELEASE_VERSION=...   (download ceremony/v<version> before building)
  CEREMONY_RELEASE_REPO=...      (default: sunnyside-io/privacy-boost-ceremony)
  CEREMONY_SIGNER_REPO=...       (default: sunnyside-io/privacy-boost-backend; whose workflow signed the release)
  CEREMONY_BUILD_MODE=auto       (auto, local, or docker; default: auto)
  CEREMONY_GO_VERSION=...        (default: derived from this repo's go.mod; used for local Go fallback / Docker image tag)
  CEREMONY_DOCKER_IMAGE=...      (default: golang:${CEREMONY_GO_VERSION}-bookworm)
  CEREMONY_QUIET=1               (same as --quiet)
  CEREMONY_NO_BROWSER=1          (same as --no-browser; also honors a bare NO_BROWSER=1)

Notes:
  - A plain `git clone` is enough. This repository has no submodules.
  - For Linux builds, you may need a C compiler + sqlite3 dev headers (for go-sqlite3).
  - Auto mode prefers an existing/prebuilt binary first. It will try an official GitHub release before
    building from source. If it needs to build, it tries your local Go (including shell-managed installs
    such as mise/homebrew), then Docker, then downloads repo-local Go.
  - Docker mode is a build sandbox only: it compiles the checked-out source inside a generic Go base
    image, the same way local mode builds on your host. It does not pull or verify any published
    ceremony container image, so it carries the same trust level as local mode, not a verified release.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      CONFIG_PATH="$2"
      shift 2
      ;;
    --coordinator-url)
      COORDINATOR_URL="$2"
      shift 2
      ;;
    --binary)
      CEREMONY_BIN="$2"
      shift 2
      ;;
    --release-version)
      CEREMONY_RELEASE_VERSION="$2"
      shift 2
      ;;
    --release-repo)
      CEREMONY_RELEASE_REPO="$2"
      shift 2
      ;;
    --signer-repo)
      CEREMONY_SIGNER_REPO="$2"
      shift 2
      ;;
    --build-mode)
      BUILD_MODE="$2"
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
      printf 'Unknown arg: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

log() {
  printf '[quickstart] %s\n' "$*"
}

log_warn() {
  printf '[quickstart] warning: %s\n' "$*" >&2
}

log_error() {
  printf '[quickstart] error: %s\n' "$*" >&2
}

# GO_VERSION tracks this repo's own go.mod so the local-Go-fallback and Docker-image
# versions track the module's actual toolchain instead of a literal that goes stale
# the next time go.mod bumps. Resolved directly in the main shell (not inside a
# command-substituted function) so a missing go.mod can exit the script, not just
# a throwaway subshell.
GO_VERSION="${CEREMONY_GO_VERSION:-}"
if [[ -z "${GO_VERSION}" ]]; then
  GO_MOD_PATH="${REPO_ROOT}/go.mod"
  if [[ -f "${GO_MOD_PATH}" ]]; then
    GO_VERSION="$(grep -m1 '^go ' "${GO_MOD_PATH}" | awk '{print $2}' || true)"
  fi
  if [[ -z "${GO_VERSION}" ]]; then
    log_error "could not read the Go version from ${GO_MOD_PATH}; set CEREMONY_GO_VERSION explicitly"
    exit 1
  fi
fi
DOCKER_IMAGE="${CEREMONY_DOCKER_IMAGE:-golang:${GO_VERSION}-bookworm}"

log_done() {
  log "$* completed."
}

# Set when preflight_check_cosign has already warned about a missing cosign, so
# verify_release_signature doesn't print the same warning a second time when the
# preflight's prediction plays out.
COSIGN_MISSING_WARNED=0

# Resolves cosign immediately before a release download is actually attempted
# (called from within prepare_ceremony_binary, after the provided-binary and
# existing-binary short-circuits), so the fetch happens up front and a machine
# that cannot get cosign at all says so before other setup work runs rather
# than failing deep inside verify_release_signature.
preflight_check_cosign() {
  if [[ -z "${CEREMONY_RELEASE_VERSION}" && "${BUILD_MODE}" != "auto" ]]; then
    return 0
  fi
  if ! signature_verification_enabled; then
    log_warn "signature verification is disabled, the download will be checked against its published checksum only."
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
  if [[ -n "${CEREMONY_RELEASE_VERSION}" ]]; then
    log_warn "no usable cosign: CEREMONY_RELEASE_VERSION is set, so this run will abort rather than use an unverified download."
  else
    log_warn "no usable cosign: the release path cannot verify the downloaded binary's signature, so this run will fall back to a source build."
  fi
  log_warn "Install cosign: https://docs.sigstore.dev/system_config/installation"
}

# Rejects a plaintext-HTTP coordinator URL for a non-loopback host, mirroring
# the Go CLI's validateCoordinatorURL policy (circuit-setup/app/coordinator_url.go).
# Loopback hosts always pass regardless of scheme; anything else requires
# https:// unless the caller opts in via --allow-insecure-http or
# CEREMONY_ALLOW_INSECURE_HTTP=1. This is a fast-fail UX check only: the
# compiled ceremony binary re-validates the same URL before any network call.
validate_coordinator_url() {
  local url="$1" host

  host="${url#*://}"
  host="${host%%/*}"
  host="${host%%\?*}"
  host="${host%%#*}"
  host="${host##*@}"
  if [[ "${host}" == \[*\]* ]]; then
    host="${host%%]*}]"
  else
    host="${host%%:*}"
  fi

  case "${host}" in
    localhost|::1|\[::1\])
      return 0
      ;;
  esac
  if [[ "${host}" =~ ^127(\.[0-9]{1,3}){3}$ ]]; then
    return 0
  fi

  if [[ "${url}" == https://* ]]; then
    return 0
  fi
  if [[ -n "${ALLOW_INSECURE_HTTP}" || "${CEREMONY_ALLOW_INSECURE_HTTP:-}" == "1" ]]; then
    return 0
  fi

  log_error "coordinator URL ${url} sends credentials over plaintext HTTP to a non-loopback host"
  log_error "use https://, or pass --allow-insecure-http / set CEREMONY_ALLOW_INSECURE_HTTP=1 if this is intentional"
  exit 1
}

case "${BUILD_MODE}" in
  auto|local|docker)
    ;;
  *)
    log_error "unsupported build mode: ${BUILD_MODE}"
    usage >&2
    exit 2
    ;;
esac

if [[ -z "${COORDINATOR_URL}" ]]; then
  log_error "--coordinator-url (or CEREMONY_COORDINATOR_URL) is required"
  usage >&2
  exit 2
fi
validate_coordinator_url "${COORDINATOR_URL}"

log "Preparing contributor quickstart."
log "Using config: ${CONFIG_PATH}"
log "Using coordinator: ${COORDINATOR_URL}"
log "Build mode: ${BUILD_MODE}"
log "Release repo: ${CEREMONY_RELEASE_REPO}"

if [[ ! -f "${CONFIG_PATH}" ]]; then
  log_error "config not found: ${CONFIG_PATH}"
  log_error "tip: run from repo root, or pass --config <path>."
  exit 1
fi

require_cmd() {
  local c="$1"
  if ! command -v "$c" >/dev/null 2>&1; then
    log_error "missing dependency: $c"
    return 1
  fi
}

APT_UPDATED=""

apt_install() {
  if ! command -v apt-get >/dev/null 2>&1; then
    return 1
  fi
  if [[ -z "${APT_UPDATED}" ]]; then
    log "Updating apt package index."
    if command -v sudo >/dev/null 2>&1; then
      sudo apt-get update
    else
      apt-get update
    fi
    APT_UPDATED=1
  fi
  log "Installing packages via apt: $*"
  if command -v sudo >/dev/null 2>&1; then
    sudo apt-get install -y "$@"
  else
    apt-get install -y "$@"
  fi
}

ensure_cmd_or_install_apt() {
  local cmd="$1"
  shift
  local pkgs=("$@")
  if command -v "${cmd}" >/dev/null 2>&1; then
    return 0
  fi
  if command -v apt-get >/dev/null 2>&1; then
    if command -v sudo >/dev/null 2>&1; then
      log "Installing ${cmd} with apt (requires sudo)."
    else
      log "Installing ${cmd} with apt."
    fi
    apt_install "${pkgs[@]}"
    command -v "${cmd}" >/dev/null 2>&1
    return
  fi
  log_error "missing dependency: ${cmd}"
  return 1
}

detect_goos_goarch() {
  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os" in
    Linux) os="linux" ;;
    Darwin) os="darwin" ;;
    *)
      log_error "unsupported OS: $(uname -s)"
      return 1
      ;;
  esac
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      log_error "unsupported arch: $(uname -m)"
      return 1
      ;;
  esac
  echo "${os}" "${arch}"
}

release_requested_explicitly() {
  [[ -n "${CEREMONY_RELEASE_VERSION}" ]]
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

ensure_release_download_deps() {
  if [[ "$(uname -s)" == "Linux" ]]; then
    ensure_cmd_or_install_apt curl curl ca-certificates
    ensure_cmd_or_install_apt tar tar
  else
    require_cmd curl
    require_cmd tar
  fi
  if command -v shasum >/dev/null 2>&1 || command -v sha256sum >/dev/null 2>&1 || command -v openssl >/dev/null 2>&1; then
    return 0
  fi
  log_error "missing checksum tool: need shasum, sha256sum, or openssl"
  return 1
}

resolve_latest_release_tag() {
  local releases_json
  if ! command -v python3 >/dev/null 2>&1; then
    log_warn "python3 not found. Skipping automatic GitHub release discovery."
    return 1
  fi
  releases_json="$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    -H "User-Agent: privacy-boost-ceremony-quickstart" \
    "https://api.github.com/repos/${CEREMONY_RELEASE_REPO}/releases?per_page=20" || true)"
  if [[ -z "${releases_json}" ]]; then
    log_warn "Could not query GitHub releases from ${CEREMONY_RELEASE_REPO}."
    return 1
  fi
  printf '%s' "${releases_json}" | python3 -c '
import json
import sys

releases = json.load(sys.stdin)
for release in releases:
    if release.get("draft") or release.get("prerelease"):
        continue
    tag = release.get("tag_name", "")
    if tag.startswith("ceremony/v"):
        print(tag)
        break
'
}

file_sha256() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${path}" | awk "{print \$1}"
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${path}" | awk "{print \$1}"
    return 0
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "${path}" | awk "{print \$NF}"
    return 0
  fi
  return 1
}

verify_release_checksum() {
  local checksum_path="$1"
  local artifact_path="$2"
  local expected actual
  # Lowercase at assignment with tr rather than bash 4's ${var,,}: macOS ships
  # bash 3.2, where that expansion is not a parse error but a runtime "bad
  # substitution" that yields an empty string and lets the script continue.
  # Inside [[ ]] both sides then compare equal, so a mismatched digest would
  # have passed verification instead of failing it.
  expected="$(awk 'NR==1 {print $1}' "${checksum_path}" | tr '[:upper:]' '[:lower:]')"
  actual="$(file_sha256 "${artifact_path}" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "${expected}" || -z "${actual}" ]]; then
    log_error "checksum verification failed: missing digest"
    return 1
  fi
  if [[ "${expected}" != "${actual}" ]]; then
    log_error "checksum verification failed for $(basename "${artifact_path}")"
    return 1
  fi
  return 0
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
    SIGNER_IDENTITY_VALUE="https://github.com/${CEREMONY_SIGNER_REPO}/.github/workflows/ceremony-release.yml@refs/tags/${release_tag}"
  fi
}

# Asks whether to fetch cosign when the contributor has none. Interactive input
# only: a non-interactive run has nobody to ask, so it leaves the fetch in place
# rather than silently dropping verification.
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
        log_warn "continuing without signature verification at your request, the download is checksum-only."
        return 0
        ;;
      *)
        log_warn "invalid choice, enter 1 or 2"
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

cleanup_fetched_cosign() {
  if [[ -n "${COSIGN_DIR}" && -d "${COSIGN_DIR}" ]]; then
    rm -rf "${COSIGN_DIR}"
  fi
}

# Resolves the cosign used to verify the release signature, preferring one the
# contributor already has and otherwise fetching the pinned build into a temp
# dir that is removed on exit. Sets COSIGN_BIN and returns 0 on success, returns
# 1 (after warning) when no usable cosign could be obtained. Memoized.
#
# The fetched binary is checked against cosign_expected_sha256 before it runs,
# and that digest lives in this script, so a fetched cosign is no more trusted
# than the script itself and adds no new trust root.
ensure_cosign() {
  if [[ -n "${COSIGN_BIN}" ]]; then
    return 0
  fi
  if command -v cosign >/dev/null 2>&1; then
    COSIGN_BIN="cosign"
    return 0
  fi
  if ! command -v curl >/dev/null 2>&1; then
    log_warn "curl is required to fetch cosign, cannot verify the release signature."
    return 1
  fi
  local goos goarch want
  if ! read -r goos goarch < <(detect_goos_goarch); then
    return 1
  fi
  if ! want="$(cosign_expected_sha256 "${goos}" "${goarch}")"; then
    log_warn "no pinned cosign checksum for ${goos}/${goarch}, cannot verify the release signature."
    return 1
  fi
  COSIGN_DIR="$(mktemp -d)"
  trap cleanup_fetched_cosign EXIT
  local dest="${COSIGN_DIR}/cosign"
  log "cosign not installed, fetching the pinned ${COSIGN_VERSION} build to verify the release signature"
  if ! curl -fL --progress-bar "${COSIGN_DOWNLOAD_BASE}/${COSIGN_VERSION}/cosign-${goos}-${goarch}" -o "${dest}"; then
    log_warn "could not download cosign ${COSIGN_VERSION} for ${goos}/${goarch}."
    return 1
  fi
  local got
  got="$(file_sha256 "${dest}" | tr '[:upper:]' '[:lower:]')"
  # A mismatch means the fetched cosign is not the pinned build, so treat it the
  # same as a tampered release artifact and stop rather than falling back.
  if [[ -z "${got}" || "${got}" != "${want}" ]]; then
    log_error "fetched cosign ${COSIGN_VERSION} does not match its pinned checksum, refusing to use it"
    exit 1
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
      log_warn "no usable cosign, cannot verify the release signature. Install cosign or build from source."
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

download_ceremony_binary_release() {
  local release_tag goos goarch asset_name base_url tmpdir
  ensure_release_download_deps || return 1

  if release_requested_explicitly; then
    release_tag="$(normalize_release_tag "${CEREMONY_RELEASE_VERSION}")" || return 1
    log "Using requested ceremony release: ${release_tag}"
  else
    log "Checking GitHub for a prebuilt ceremony release."
    release_tag="$(resolve_latest_release_tag || true)"
    if [[ -z "${release_tag}" ]]; then
      log_warn "No stable ceremony release found in ${CEREMONY_RELEASE_REPO}. Continuing with local setup."
      return 1
    fi
    log "Found ceremony release: ${release_tag}"
  fi

  read -r goos goarch < <(detect_goos_goarch)
  asset_name="ceremony-${goos}-${goarch}.tar.gz"
  base_url="https://github.com/${CEREMONY_RELEASE_REPO}/releases/download/${release_tag}"
  tmpdir="$(mktemp -d)"

  log "Downloading ${asset_name}."
  if ! curl -fL --progress-bar "${base_url}/${asset_name}" -o "${tmpdir}/${asset_name}"; then
    rm -rf "${tmpdir}"
    log_warn "Could not download ${asset_name} from ${release_tag}."
    return 1
  fi

  log "Downloading ${asset_name}.sha256."
  if ! curl -fL --progress-bar "${base_url}/${asset_name}.sha256" -o "${tmpdir}/${asset_name}.sha256"; then
    rm -rf "${tmpdir}"
    log_warn "Could not download checksum for ${asset_name}."
    return 1
  fi

  log "Verifying ${asset_name} checksum."
  if ! verify_release_checksum "${tmpdir}/${asset_name}.sha256" "${tmpdir}/${asset_name}"; then
    rm -rf "${tmpdir}"
    return 1
  fi

  if signature_verification_enabled; then
    log "Downloading ${asset_name}.cosign.bundle."
    if ! curl -fL --progress-bar "${base_url}/${asset_name}.cosign.bundle" -o "${tmpdir}/${asset_name}.cosign.bundle"; then
      rm -rf "${tmpdir}"
      log_warn "No signature bundle published for ${asset_name}, refusing the prebuilt path."
      return 1
    fi
    resolve_signer_identity "${release_tag}"
    log "Verifying ${asset_name} signature."
    local vrc=0
    verify_release_signature "${tmpdir}/${asset_name}.cosign.bundle" "${tmpdir}/${asset_name}" "${SIGNER_IDENTITY_FLAG}" "${SIGNER_IDENTITY_VALUE}" || vrc=$?
    if [[ "${vrc}" -eq 2 ]]; then
      rm -rf "${tmpdir}"
      log_error "release signature verification FAILED for ${asset_name}, the download may be tampered with, aborting"
      exit 1
    elif [[ "${vrc}" -ne 0 ]]; then
      rm -rf "${tmpdir}"
      return 1
    fi
  else
    log_warn "SIGNATURE NOT VERIFIED for ${asset_name}. The checksum matched, but a checksum served alongside the binary does not prove who built it."
  fi

  log "Extracting ${asset_name} into ./bin."
  mkdir -p bin
  tar -xzf "${tmpdir}/${asset_name}" -C "${tmpdir}"
  if [[ ! -f "${tmpdir}/ceremony" ]]; then
    rm -rf "${tmpdir}"
    log_error "release asset ${asset_name} did not contain a ceremony binary"
    return 1
  fi
  cp "${tmpdir}/ceremony" "${REPO_ROOT}/bin/ceremony"
  chmod 0755 "${REPO_ROOT}/bin/ceremony"
  CEREMONY_BIN="${REPO_ROOT}/bin/ceremony"
  rm -rf "${tmpdir}"
  if ! "${CEREMONY_BIN}" version --expect "${release_tag}"; then
    log_error "downloaded binary's stamped version does not match release ${release_tag}, the archive may be tampered with or mismatched"
    exit 1
  fi
  log_done "Prebuilt ceremony binary download"
  return 0
}

last_glob_match() {
  local pattern="$1"
  local candidate=""
  local match
  while IFS= read -r match; do
    candidate="${match}"
  done < <(compgen -G "${pattern}" || true)
  if [[ -n "${candidate}" && -x "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi
  return 1
}

resolve_go_from_user_shell() {
  local shell_path candidate line
  shell_path="${SHELL:-}"
  if [[ -z "${shell_path}" || ! -x "${shell_path}" ]]; then
    return 1
  fi
  candidate=""
  while IFS= read -r line; do
    line="${line%$'\r'}"
    if [[ -n "${line}" && -x "${line}" ]]; then
      candidate="${line}"
    fi
  done < <("${shell_path}" -ilc 'command -v go 2>/dev/null || true' 2>/dev/null || true)
  if [[ -n "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi
  return 1
}

resolve_go_from_mise() {
  local mise_bin candidate
  mise_bin=""
  if command -v mise >/dev/null 2>&1; then
    mise_bin="$(command -v mise)"
  elif [[ -x "${HOME}/.local/bin/mise" ]]; then
    mise_bin="${HOME}/.local/bin/mise"
  fi
  if [[ -n "${mise_bin}" ]]; then
    candidate="$("${mise_bin}" which go 2>/dev/null || true)"
    if [[ -n "${candidate}" && -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  fi
  last_glob_match "${HOME}/.local/share/mise/installs/go/*/bin/go"
}

resolve_go_from_asdf() {
  local candidate
  if command -v asdf >/dev/null 2>&1; then
    candidate="$(asdf which golang 2>/dev/null || true)"
    if [[ -z "${candidate}" ]]; then
      candidate="$(asdf which go 2>/dev/null || true)"
    fi
    if [[ -n "${candidate}" && -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  fi
  last_glob_match "${HOME}/.asdf/installs/golang/*/bin/go" || \
    last_glob_match "${HOME}/.asdf/installs/go/*/bin/go"
}

resolve_go_from_homebrew() {
  local prefix candidate
  if command -v brew >/dev/null 2>&1; then
    prefix="$(brew --prefix go 2>/dev/null || true)"
    for candidate in "${prefix}/libexec/bin/go" "${prefix}/bin/go"; do
      if [[ -x "${candidate}" ]]; then
        printf '%s\n' "${candidate}"
        return 0
      fi
    done
  fi
  for candidate in \
    /opt/homebrew/opt/go/libexec/bin/go \
    /usr/local/opt/go/libexec/bin/go \
    /usr/local/go/bin/go; do
    if [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  return 1
}

discover_go() {
  local candidate=""
  candidate="$(command -v go 2>/dev/null || true)"
  if [[ -n "${candidate}" && -x "${candidate}" ]]; then
    GO_BIN="${candidate}"
    GO_SOURCE="PATH"
    return 0
  fi

  candidate="$(resolve_go_from_mise || true)"
  if [[ -n "${candidate}" ]]; then
    GO_BIN="${candidate}"
    GO_SOURCE="mise"
    return 0
  fi

  candidate="$(resolve_go_from_asdf || true)"
  if [[ -n "${candidate}" ]]; then
    GO_BIN="${candidate}"
    GO_SOURCE="asdf"
    return 0
  fi

  candidate="$(resolve_go_from_homebrew || true)"
  if [[ -n "${candidate}" ]]; then
    GO_BIN="${candidate}"
    GO_SOURCE="homebrew"
    return 0
  fi

  candidate="$(resolve_go_from_user_shell || true)"
  if [[ -n "${candidate}" ]]; then
    GO_BIN="${candidate}"
    GO_SOURCE="user shell"
    return 0
  fi

  return 1
}

use_go_bin() {
  local go_dir
  local version
  go_dir="$(dirname "${GO_BIN}")"
  export PATH="${go_dir}:${PATH}"
  version="$("${GO_BIN}" version 2>/dev/null || true)"
  if [[ -n "${version}" ]]; then
    log "Using Go from ${GO_SOURCE}: ${version}"
  else
    log "Using Go from ${GO_SOURCE}: ${GO_BIN}"
  fi
}

ensure_local_go() {
  local tools_dir="${REPO_ROOT}/.tools"
  local go_root="${tools_dir}/go${GO_VERSION}"
  local local_go_bin="${go_root}/go/bin/go"

  if [[ -x "${local_go_bin}" ]]; then
    GO_BIN="${local_go_bin}"
    GO_SOURCE="repo-local cache"
    use_go_bin
    return 0
  fi

  if [[ "$(uname -s)" == "Linux" ]]; then
    ensure_cmd_or_install_apt curl curl ca-certificates
    ensure_cmd_or_install_apt tar tar
  else
    require_cmd curl
    require_cmd tar
  fi

  mkdir -p "${tools_dir}"
  read -r goos goarch < <(detect_goos_goarch)
  local tarball="go${GO_VERSION}.${goos}-${goarch}.tar.gz"
  local url="https://go.dev/dl/${tarball}"
  log "Downloading Go ${GO_VERSION} (${goos}-${goarch}) into ./.tools."
  curl -fL --progress-bar "${url}" -o "${tools_dir}/${tarball}"
  log "Extracting Go ${GO_VERSION}."
  rm -rf "${tools_dir}/go" "${go_root}"
  tar -C "${tools_dir}" -xzf "${tools_dir}/${tarball}"
  mv "${tools_dir}/go" "${go_root}"
  GO_BIN="${local_go_bin}"
  GO_SOURCE="repo-local download"
  use_go_bin
  log_done "Local Go installation"
}

ensure_build_deps_linux() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    return 0
  fi

  # ceremony builds coordinator code too; go-sqlite3 needs a C toolchain + sqlite headers.
  if command -v gcc >/dev/null 2>&1 && command -v pkg-config >/dev/null 2>&1 && pkg-config --exists sqlite3; then
    return 0
  fi

  if command -v apt-get >/dev/null 2>&1; then
    log "Installing Linux build dependencies (requires sudo)."
    apt_install build-essential pkg-config libsqlite3-dev
    return 0
  fi

  log_error "missing build deps for Linux (need gcc/pkg-config/sqlite3 headers)."
  log_error "install them via your distro package manager, then re-run this script."
  exit 1
}

ensure_build_deps_darwin() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    return 0
  fi
  # go-sqlite3 needs a working compiler toolchain.
  if xcode-select -p >/dev/null 2>&1; then
    return 0
  fi
  log_error "Xcode Command Line Tools are required to build (compiler toolchain)."
  log "Launching the macOS Command Line Tools installer dialog."
  xcode-select --install >/dev/null 2>&1 || true
  log_error "after Command Line Tools installation completes, re-run this script."
  exit 1
}

ensure_executable_file() {
  local path="$1"
  if [[ -f "${path}" && ! -x "${path}" ]]; then
    chmod +x "${path}" 2>/dev/null || true
  fi
  [[ -x "${path}" ]]
}

use_provided_binary() {
  if [[ -z "${CEREMONY_BIN}" ]]; then
    return 1
  fi
  if ! ensure_executable_file "${CEREMONY_BIN}"; then
    log_error "ceremony binary is not executable: ${CEREMONY_BIN}"
    exit 1
  fi
  log "Using provided ceremony binary: ${CEREMONY_BIN}"
  return 0
}

# binary_is_stale reports (via exit code) whether any path the local build
# actually compiles has a newer mtime than the given binary. -quit stops at the
# first match; supported by both GNU find and macOS/BSD find. This path list
# names only first-party source that lives in THIS repository. The circuit
# definitions are not among them: they resolve from the pinned
# privacy-boost-protocol module, so a circuit change reaches this binary through
# a pin bump, which moves go.mod and go.sum and is already covered here. Listing
# a path that does not exist would be worse than useless, since find's stderr is
# discarded and the entry would silently contribute nothing.
binary_is_stale() {
  local binary_path="$1"
  find "${REPO_ROOT}/go.mod" "${REPO_ROOT}/go.sum" "${REPO_ROOT}/cmd/ceremony" \
    "${REPO_ROOT}/circuit-setup/app" "${REPO_ROOT}/circuit-setup/internal" \
    "${REPO_ROOT}/prover/compile" \
    -newer "${binary_path}" -print -quit 2>/dev/null | grep -q .
}

use_existing_repo_binary() {
  local candidate="${REPO_ROOT}/bin/ceremony"
  if ! ensure_executable_file "${candidate}"; then
    return 1
  fi
  if binary_is_stale "${candidate}"; then
    log_warn "existing ceremony binary at ${candidate} is older than the current source, rebuilding"
    return 1
  fi
  CEREMONY_BIN="${candidate}"
  log "Using existing ceremony binary: ${CEREMONY_BIN}"
  return 0
}

build_ceremony_binary_local() {
  if [[ -z "${GO_BIN}" ]]; then
    log_error "internal error: GO_BIN is empty before local build"
    exit 1
  fi
  ensure_build_deps_linux
  ensure_build_deps_darwin
  mkdir -p bin
  log "Building ceremony CLI with local Go."
  "${GO_BIN}" build -o ./bin/ceremony ./cmd/ceremony
  CEREMONY_BIN="${REPO_ROOT}/bin/ceremony"
  if ! ensure_executable_file "${CEREMONY_BIN}"; then
    log_error "local build did not produce an executable binary at ${CEREMONY_BIN}"
    exit 1
  fi
  log_done "Local ceremony build"
}

build_ceremony_binary_docker() {
  require_cmd docker
  mkdir -p bin
  log "Building ceremony CLI with Docker image ${DOCKER_IMAGE}."
  docker run --rm \
    -u "$(id -u):$(id -g)" \
    -v "${REPO_ROOT}:/work" \
    -w /work \
    "${DOCKER_IMAGE}" \
    sh -lc 'if command -v apt-get >/dev/null 2>&1; then apt-get update >/dev/null && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends build-essential pkg-config libsqlite3-dev ca-certificates >/dev/null; fi && go build -o ./bin/ceremony ./cmd/ceremony'
  CEREMONY_BIN="${REPO_ROOT}/bin/ceremony"
  if ! ensure_executable_file "${CEREMONY_BIN}"; then
    log_error "Docker build did not produce an executable binary at ${CEREMONY_BIN}"
    exit 1
  fi
  log_done "Docker ceremony build"
}

prepare_ceremony_binary() {
  if use_provided_binary; then
    return 0
  fi

  if release_requested_explicitly; then
    preflight_check_cosign
    if download_ceremony_binary_release; then
      return 0
    fi
    log_error "failed to download requested ceremony release: ${CEREMONY_RELEASE_VERSION}"
    exit 1
  fi

  if [[ "${BUILD_MODE}" == "auto" ]] && use_existing_repo_binary; then
    return 0
  fi

  preflight_check_cosign

  case "${BUILD_MODE}" in
    auto)
      if download_ceremony_binary_release; then
        return 0
      fi
      if discover_go; then
        use_go_bin
        build_ceremony_binary_local
        return 0
      fi
      if command -v docker >/dev/null 2>&1; then
        log "No local Go detected. Falling back to Docker build."
        build_ceremony_binary_docker
        return 0
      fi
      log_warn "No local Go or Docker detected. Falling back to a repo-local Go download."
      ensure_local_go
      build_ceremony_binary_local
      ;;
    local)
      if discover_go; then
        use_go_bin
      else
        log_warn "No local Go detected. Falling back to a repo-local Go download."
        ensure_local_go
      fi
      build_ceremony_binary_local
      ;;
    docker)
      build_ceremony_binary_docker
      ;;
  esac
}

extract_state_dir() {
  # Prefer python3 for correct JSON parsing.
  if command -v python3 >/dev/null 2>&1; then
    python3 - "${CONFIG_PATH}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    cfg = json.load(f)
print(cfg.get("stateDir", ""))
PY
    return 0
  fi

  # Fallback: best-effort extraction for our known, formatted config files.
  local line
  line="$(grep -E '"stateDir"\\s*:' "${CONFIG_PATH}" | head -n 1 || true)"
  if [[ -z "${line}" ]]; then
    echo ""
    return 0
  fi
  echo "${line}" | sed -E 's/.*"stateDir"\\s*:\\s*"([^"]+)".*/\\1/'
}

prepare_ceremony_binary

STATE_DIR="$(extract_state_dir)"
if [[ -z "${STATE_DIR}" ]]; then
  log_error "could not read stateDir from config: ${CONFIG_PATH}"
  exit 1
fi
log "Ensuring state directory exists: ${STATE_DIR}"
mkdir -p "${STATE_DIR}"
log_done "State directory setup"

log "Starting contribution against ${COORDINATOR_URL}."
ARGS=("${CEREMONY_BIN}" contribute --config "${CONFIG_PATH}" --coordinator-url "${COORDINATOR_URL}")
if [[ -n "${QUIET}" ]]; then
  ARGS+=(--quiet)
fi
if [[ -n "${NO_BROWSER_OPT}" || -n "${NO_BROWSER:-}" ]]; then
  ARGS+=(--no-browser)
fi
"${ARGS[@]}"
log_done "Contribution flow"
