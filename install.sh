#!/usr/bin/env bash
set -euo pipefail

REPO="csx5/lowi"
MIN_GO_MINOR=24

MODE="auto"
VERSION="latest"
SCOPE="auto"
INSTALL_SERVICE=0
LISTEN_ADDR=":8080"
TLS_AUTO=0
CHECK_ONLY=0
ASSUME_YES=0
UNINSTALL=0

TMP_DIR=""
PKG_MGR=""
GO_BIN="go"
DL_TOOL=""
HASH_TOOL=""

log_info() {
  printf '[INFO] %s\n' "$*"
}

log_ok() {
  printf '[OK]   %s\n' "$*"
}

log_warn() {
  printf '[WARN] %s\n' "$*" >&2
}

log_err() {
  printf '[FEHLER] %s\n' "$*" >&2
}

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

usage() {
  cat <<'EOF'
IT-Wissensdatenbank (it-kb) - Installations- und Setup-Skript

Verwendung:
  ./install.sh [Optionen]
  curl -fsSL https://raw.githubusercontent.com/csx5/lowi/main/install.sh | bash -s -- [Optionen]

Optionen:
  --release         Fertiges Release-Binary von GitHub (csx5/lowi) laden und SHA-256 prüfen
  --from-source     Aus Quellcode kompilieren (Standard im geklonten Repository)
  --version <tag>   Bestimmte Release-Version oder Git-Tag verwenden (Standard: latest)
  --user            Installation für den aktuellen Benutzer (~/.local/bin, Standard ohne Root)
  --system          Systemweite Installation (/usr/local/bin, /etc/it-kb, /var/lib/it-kb)
  --service         systemd-Dienst einrichten, aktivieren und starten
  --listen <addr>   Standard-Listen-Adresse für neue config.json (Standard: :8080)
  --tls-auto        Automatisch selbstsigniertes lokales HTTPS aktivieren (neue config.json)
  --check-only      Nur System, Werkzeuge, Go-Version (>= 1.24) und Port prüfen
  --yes, -y         Alle Rückfragen automatisch bestätigen
  --uninstall       Binary und ggf. systemd-Dienst entfernen (Datenverzeichnis bleibt erhalten)
  --help, -h        Diese Hilfe anzeigen
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --release)
        MODE="release"
        shift
        ;;
      --from-source)
        MODE="source"
        shift
        ;;
      --version)
        [[ $# -lt 2 ]] && { log_err "--version benötigt ein Argument"; exit 1; }
        VERSION="$2"
        shift 2
        ;;
      --user)
        SCOPE="user"
        shift
        ;;
      --system)
        SCOPE="system"
        shift
        ;;
      --service)
        INSTALL_SERVICE=1
        shift
        ;;
      --listen)
        [[ $# -lt 2 ]] && { log_err "--listen benötigt ein Argument"; exit 1; }
        LISTEN_ADDR="$2"
        shift 2
        ;;
      --tls-auto)
        TLS_AUTO=1
        shift
        ;;
      --check-only)
        CHECK_ONLY=1
        shift
        ;;
      --yes|-y)
        ASSUME_YES=1
        shift
        ;;
      --uninstall)
        UNINSTALL=1
        shift
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        log_err "Unbekannte Option: $1"
        usage >&2
        exit 1
        ;;
    esac
  done
}

confirm_action() {
  local prompt="$1"
  if [[ "$ASSUME_YES" -eq 1 ]]; then
    return 0
  fi
  if [[ -r /dev/tty && -w /dev/tty ]]; then
    local answer=""
    printf '%s [j/N]: ' "$prompt" >/dev/tty
    read -r answer </dev/tty || answer=""
    case "$answer" in
      j|J|ja|JA|y|Y|yes|YES) return 0 ;;
      *) return 1 ;;
    esac
  fi
  return 1
}

run_priv() {
  if [[ "$(id -u)" -eq 0 ]]; then
    "$@"
  elif command -v sudo >/dev/null 2>&1; then
    sudo "$@"
  else
    log_err "Für '$*' sind Root-Rechte erforderlich (sudo nicht gefunden)."
    exit 1
  fi
}

detect_platform() {
  local raw_os raw_arch
  raw_os="$(uname -s)"
  raw_arch="$(uname -m)"

  case "$raw_os" in
    Linux)  OS="linux" ;;
    Darwin) OS="darwin" ;;
    *)
      log_err "Nicht unterstütztes Betriebssystem: $raw_os"
      exit 1
      ;;
  esac

  case "$raw_arch" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
      log_err "Nicht unterstützte CPU-Architektur: $raw_arch"
      exit 1
      ;;
  esac

  IN_LOCAL_REPO=0
  LOCAL_REPO_DIR=""
  if [[ -f "./go.mod" && -f "./cmd/it-kb/main.go" ]]; then
    IN_LOCAL_REPO=1
    LOCAL_REPO_DIR="$PWD"
  elif [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
    local script_dir
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [[ -f "${script_dir}/go.mod" && -f "${script_dir}/cmd/it-kb/main.go" ]]; then
      IN_LOCAL_REPO=1
      LOCAL_REPO_DIR="$script_dir"
    fi
  fi

  HAS_RELEASE_BINARY=0
  if [[ "$OS" == "linux" && ( "$ARCH" == "amd64" || "$ARCH" == "arm64" ) ]]; then
    HAS_RELEASE_BINARY=1
  fi

  if [[ "$MODE" == "auto" ]]; then
    if [[ "$IN_LOCAL_REPO" -eq 1 ]]; then
      MODE="source"
    elif [[ "$HAS_RELEASE_BINARY" -eq 1 ]]; then
      MODE="release"
    else
      MODE="source"
    fi
  fi

  if [[ "$MODE" == "release" && "$HAS_RELEASE_BINARY" -eq 0 ]]; then
    log_err "Für ${OS}/${ARCH} existiert kein vorkompiliertes Release-Binary. Bitte mit --from-source bauen."
    exit 1
  fi

  if [[ "$SCOPE" == "auto" ]]; then
    if [[ "$(id -u)" -eq 0 ]]; then
      SCOPE="system"
    else
      SCOPE="user"
    fi
  fi

  if [[ "$SCOPE" == "system" ]]; then
    BIN_PATH="/usr/local/bin/it-kb"
    CONFIG_DIR="/etc/it-kb"
    DATA_DIR="/var/lib/it-kb"
    GO_PREFIX="/usr/local"
  else
    BIN_PATH="${HOME}/.local/bin/it-kb"
    CONFIG_DIR="${HOME}/.config/it-kb"
    DATA_DIR="${HOME}/.local/share/it-kb"
    GO_PREFIX="${HOME}/.local/share/it-kb-go"
  fi
  CONFIG_PATH="${CONFIG_DIR}/config.json"
}

detect_pkg_mgr() {
  for mgr in pacman apt-get dnf zypper apk brew; do
    if command -v "$mgr" >/dev/null 2>&1; then
      PKG_MGR="$mgr"
      return 0
    fi
  done
  PKG_MGR=""
}

pkg_name_for() {
  local tool="$1"
  case "${PKG_MGR}:${tool}" in
    pacman:curl|apt-get:curl|dnf:curl|zypper:curl|apk:curl|brew:curl) echo "curl" ;;
    pacman:git|apt-get:git|dnf:git|zypper:git|apk:git|brew:git)       echo "git" ;;
    pacman:tar|apt-get:tar|dnf:tar|zypper:tar|apk:tar|brew:gnu-tar)   echo "tar" ;;
    pacman:sha256|dnf:sha256|apt-get:sha256|zypper:sha256|apk:sha256) echo "coreutils" ;;
    pacman:go|zypper:go|apk:go|brew:go)                               echo "go" ;;
    apt-get:go)                                                       echo "golang-go" ;;
    dnf:go)                                                           echo "golang" ;;
    *) echo "" ;;
  esac
}

install_system_packages() {
  local pkgs=("$@")
  [[ "${#pkgs[@]}" -eq 0 ]] && return 0
  if [[ -z "$PKG_MGR" ]]; then
    log_err "Kein unterstützter Paketmanager (pacman, apt-get, dnf, zypper, apk, brew) gefunden."
    return 1
  fi

  log_info "Fehlende Systempakete über ${PKG_MGR} installieren: ${pkgs[*]}"
  case "$PKG_MGR" in
    pacman)  run_priv pacman -S --needed --noconfirm "${pkgs[@]}" ;;
    apt-get) run_priv apt-get update -qq && run_priv DEBIAN_FRONTEND=noninteractive apt-get install -y "${pkgs[@]}" ;;
    dnf)     run_priv dnf install -y "${pkgs[@]}" ;;
    zypper)  run_priv zypper --non-interactive install "${pkgs[@]}" ;;
    apk)     run_priv apk add --no-cache "${pkgs[@]}" ;;
    brew)    brew install "${pkgs[@]}" ;;
  esac
}

detect_tools() {
  DL_TOOL=""
  if command -v curl >/dev/null 2>&1; then
    DL_TOOL="curl"
  elif command -v wget >/dev/null 2>&1; then
    DL_TOOL="wget"
  fi

  HASH_TOOL=""
  if command -v sha256sum >/dev/null 2>&1; then
    HASH_TOOL="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    HASH_TOOL="shasum"
  fi

  GO_BIN=""
  if command -v go >/dev/null 2>&1; then
    GO_BIN="$(command -v go)"
  elif [[ -x "${HOME}/.local/share/it-kb-go/go/bin/go" ]]; then
    GO_BIN="${HOME}/.local/share/it-kb-go/go/bin/go"
  elif [[ -x "/usr/local/go/bin/go" ]]; then
    GO_BIN="/usr/local/go/bin/go"
  fi
}

go_version_string() {
  if [[ -z "$GO_BIN" ]]; then
    echo ""
    return
  fi
  "$GO_BIN" version 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i ~ /^go[0-9]+\.[0-9]+/) {print $i; exit}}'
}

go_version_ok() {
  local v="$1"
  [[ -z "$v" ]] && return 1
  v="${v#go}"
  local major minor
  major="${v%%.*}"
  local rest="${v#*.}"
  minor="${rest%%.*}"
  if [[ "$major" -gt 1 ]]; then
    return 0
  fi
  if [[ "$major" -eq 1 && "$minor" -ge "$MIN_GO_MINOR" ]]; then
    return 0
  fi
  return 1
}

download_file() {
  local url="$1"
  local dest="$2"
  if [[ "$DL_TOOL" == "curl" ]]; then
    curl -fsSL "$url" -o "$dest"
  elif [[ "$DL_TOOL" == "wget" ]]; then
    wget -qO "$dest" "$url"
  else
    log_err "Weder curl noch wget verfügbar."
    return 1
  fi
}

download_text() {
  local url="$1"
  if [[ "$DL_TOOL" == "curl" ]]; then
    curl -fsSL "$url"
  elif [[ "$DL_TOOL" == "wget" ]]; then
    wget -qO- "$url"
  else
    return 1
  fi
}

compute_sha256() {
  local file="$1"
  if [[ "$HASH_TOOL" == "sha256sum" ]]; then
    sha256sum "$file" | awk '{print $1}'
  elif [[ "$HASH_TOOL" == "shasum" ]]; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    log_err "Kein SHA-256-Werkzeug gefunden."
    return 1
  fi
}

install_official_go() {
  local go_ver archive_url sha_url tmp_dl expected_hash actual_hash
  if [[ -z "$DL_TOOL" ]]; then
    log_err "Für den Download der Go-Toolchain wird curl oder wget benötigt."
    return 1
  fi
  go_ver="$(download_text "https://go.dev/VERSION?m=text" | head -n 1 | tr -d '[:space:]')"
  if [[ -z "$go_ver" || "$go_ver" != go* ]]; then
    log_err "Konnte aktuelle Go-Version von https://go.dev/VERSION nicht ermitteln."
    return 1
  fi

  archive_url="https://go.dev/dl/${go_ver}.${OS}-${ARCH}.tar.gz"
  sha_url="${archive_url}.sha256"
  tmp_dl="${TMP_DIR}/${go_ver}.tar.gz"

  log_info "Lade offizielle Go-Toolchain ${go_ver} (${OS}/${ARCH}) …"
  download_file "$archive_url" "$tmp_dl"

  if expected_hash="$(download_text "$sha_url" 2>/dev/null | tr -d '[:space:]')" && [[ -n "$expected_hash" && -n "$HASH_TOOL" ]]; then
    actual_hash="$(compute_sha256 "$tmp_dl")"
    if [[ "$expected_hash" != "$actual_hash" ]]; then
      log_err "SHA-256-Prüfung für ${go_ver} fehlgeschlagen (erwartet ${expected_hash}, erhalten ${actual_hash})."
      return 1
    fi
    log_ok "SHA-256 für ${go_ver} verifiziert"
  fi

  if [[ "$SCOPE" == "system" ]]; then
    run_priv rm -rf /usr/local/go
    run_priv mkdir -p /usr/local
    run_priv tar -C /usr/local -xzf "$tmp_dl"
    GO_BIN="/usr/local/go/bin/go"
  else
    mkdir -p "$GO_PREFIX"
    rm -rf "${GO_PREFIX}/go"
    tar -C "$GO_PREFIX" -xzf "$tmp_dl"
    GO_BIN="${GO_PREFIX}/go/bin/go"
  fi
  export PATH="$(dirname "$GO_BIN"):$PATH"
  log_ok "Go-Toolchain installiert: $(go_version_string)"
}

check_port_status() {
  local port="${LISTEN_ADDR##*:}"
  [[ -z "$port" ]] && return 0
  if command -v ss >/dev/null 2>&1; then
    if ss -tlnH 2>/dev/null | awk '{print $4}' | grep -Eq "(:|\\])${port}\$"; then
      log_warn "Port ${port} ist aktuell bereits belegt."
    else
      log_ok "Port ${port} ist frei"
    fi
  elif command -v lsof >/dev/null 2>&1; then
    if lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      log_warn "Port ${port} ist aktuell bereits belegt."
    else
      log_ok "Port ${port} ist frei"
    fi
  fi
}

check_and_install_deps() {
  detect_pkg_mgr
  detect_tools

  local missing_tools=()
  local missing_pkgs=()

  if [[ -z "$DL_TOOL" ]]; then
    missing_tools+=("curl/wget")
    local p; p="$(pkg_name_for curl)"; [[ -n "$p" ]] && missing_pkgs+=("$p")
  fi
  if [[ -z "$HASH_TOOL" ]]; then
    missing_tools+=("sha256sum")
    local p; p="$(pkg_name_for sha256)"; [[ -n "$p" ]] && missing_pkgs+=("$p")
  fi
  if [[ "$MODE" == "source" ]]; then
    if [[ "$IN_LOCAL_REPO" -eq 0 ]] && ! command -v git >/dev/null 2>&1; then
      missing_tools+=("git")
      local p; p="$(pkg_name_for git)"; [[ -n "$p" ]] && missing_pkgs+=("$p")
    fi
    if ! command -v tar >/dev/null 2>&1; then
      missing_tools+=("tar")
      local p; p="$(pkg_name_for tar)"; [[ -n "$p" ]] && missing_pkgs+=("$p")
    fi
    local go_ver; go_ver="$(go_version_string)"
    if ! go_version_ok "$go_ver"; then
      missing_tools+=("go>=1.${MIN_GO_MINOR} (aktuell: ${go_ver:-nicht installiert})")
      if [[ -z "$go_ver" ]]; then
        local p; p="$(pkg_name_for go)"; [[ -n "$p" ]] && missing_pkgs+=("$p")
      fi
    fi
  fi

  if [[ "$CHECK_ONLY" -eq 1 ]]; then
    log_info "System: ${OS}/${ARCH} | Modus: ${MODE} | Ziel: ${SCOPE} (${BIN_PATH})"
    log_info "Paketmanager: ${PKG_MGR:-nicht erkannt}"
    [[ -n "$DL_TOOL" ]] && log_ok "Downloader: $DL_TOOL" || log_warn "Downloader fehlt (curl/wget)"
    [[ -n "$HASH_TOOL" ]] && log_ok "Prüfsummen-Tool: $HASH_TOOL" || log_warn "Prüfsummen-Tool fehlt (sha256sum)"
    if [[ "$MODE" == "source" ]]; then
      if [[ "$IN_LOCAL_REPO" -eq 1 ]]; then
        log_ok "Quellcode: lokales Repository ($LOCAL_REPO_DIR)"
      elif command -v git >/dev/null 2>&1; then
        log_ok "git: $(git --version)"
      else
        log_warn "git fehlt (für Repo-Klon)"
      fi
      local gv; gv="$(go_version_string)"
      if go_version_ok "$gv"; then
        log_ok "Go-Version: $gv (>= 1.${MIN_GO_MINOR})"
      else
        log_warn "Go >= 1.${MIN_GO_MINOR} fehlt (aktuell: ${gv:-nicht installiert})"
      fi
    fi
    check_port_status
    if [[ "${#missing_tools[@]}" -eq 0 ]]; then
      log_ok "Alle benötigten Abhängigkeiten sind vorhanden."
    else
      log_info "Fehlende Abhängigkeiten (${missing_tools[*]}) können ohne --check-only automatisch installiert werden."
    fi
    return 0
  fi

  if [[ "${#missing_pkgs[@]}" -gt 0 ]]; then
    # Doppelte Paketnamen entfernen
    local unique_pkgs=()
    local seen=""
    for pkg in "${missing_pkgs[@]}"; do
      if [[ " $seen " != *" $pkg "* ]]; then
        unique_pkgs+=("$pkg")
        seen+=" $pkg"
      fi
    done

    if ! confirm_action "Fehlende Abhängigkeiten (${missing_tools[*]}) automatisch installieren?"; then
      log_err "Abgebrochen. Zum automatischen Installieren bitte mit --yes erneut starten."
      exit 1
    fi
    install_system_packages "${unique_pkgs[@]}"
    detect_tools
  fi

  if [[ "$MODE" == "source" ]]; then
    local go_ver; go_ver="$(go_version_string)"
    if ! go_version_ok "$go_ver"; then
      if [[ "${#missing_pkgs[@]}" -eq 0 ]]; then
        if ! confirm_action "Go >= 1.${MIN_GO_MINOR} wird benötigt (aktuell: ${go_ver:-nicht installiert}). Offizielle Go-Toolchain laden?"; then
          log_err "Abgebrochen. Bitte Go >= 1.${MIN_GO_MINOR} bereitstellen oder --yes verwenden."
          exit 1
        fi
      fi
      install_official_go
    else
      log_ok "Go-Toolchain geprüft: $go_ver"
    fi
  fi
}

find_gh_asset_api_url() {
  local json="$1"
  local target="$2"
  printf '%s' "$json" | tr ',{' '\n\n' | awk -v target="\"${target}\"" '
    /https:\/\/api\.github\.com\/repos\/[^"]+\/releases\/assets\/[0-9]+/ {
      if (match($0, /https:\/\/api\.github\.com\/repos\/[^"]+\/releases\/assets\/[0-9]+/)) {
        url = substr($0, RSTART, RLENGTH)
      }
    }
    $0 ~ /"name"[[:space:]]*:/ && index($0, target) > 0 && url != "" {
      print url
      exit
    }'
}

download_release_with_token() {
  local token="$1"
  local asset_name="$2"
  local bin_dl="$3"
  local sums_dl="$4"
  local api_url rel_json asset_url sums_url

  if [[ "$VERSION" == "latest" ]]; then
    api_url="https://api.github.com/repos/${REPO}/releases/latest"
  else
    api_url="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
  fi

  if [[ "$DL_TOOL" == "curl" ]]; then
    rel_json="$(curl -fsSL -H "Authorization: Bearer ${token}" -H "Accept: application/vnd.github+json" "$api_url")" || return 1
  else
    rel_json="$(wget -qO- --header="Authorization: Bearer ${token}" --header="Accept: application/vnd.github+json" "$api_url")" || return 1
  fi

  asset_url="$(find_gh_asset_api_url "$rel_json" "$asset_name")"
  sums_url="$(find_gh_asset_api_url "$rel_json" "SHA256SUMS.txt")"
  [[ -z "$asset_url" || -z "$sums_url" ]] && return 1

  if [[ "$DL_TOOL" == "curl" ]]; then
    curl -fsSL -H "Authorization: Bearer ${token}" -H "Accept: application/octet-stream" "$asset_url" -o "$bin_dl" || return 1
    curl -fsSL -H "Authorization: Bearer ${token}" -H "Accept: application/octet-stream" "$sums_url" -o "$sums_dl" || return 1
  else
    wget -qO "$bin_dl" --header="Authorization: Bearer ${token}" --header="Accept: application/octet-stream" "$asset_url" || return 1
    wget -qO "$sums_dl" --header="Authorization: Bearer ${token}" --header="Accept: application/octet-stream" "$sums_url" || return 1
  fi
}

install_from_release() {
  local asset_name base_url bin_dl sums_dl expected_hash actual_hash token
  asset_name="it-kb-${OS}-${ARCH}"
  if [[ "$VERSION" == "latest" ]]; then
    base_url="https://github.com/${REPO}/releases/latest/download"
  else
    base_url="https://github.com/${REPO}/releases/download/${VERSION}"
  fi

  bin_dl="${TMP_DIR}/it-kb"
  sums_dl="${TMP_DIR}/SHA256SUMS.txt"

  log_info "Lade Release-Binary (${asset_name}) von ${REPO} …"
  if ! (download_file "${base_url}/${asset_name}" "$bin_dl" 2>/dev/null && download_file "${base_url}/SHA256SUMS.txt" "$sums_dl" 2>/dev/null); then
    token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
    if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
      log_info "Öffentlicher Download nicht verfügbar – verwende authentifizierte GitHub CLI (gh) …"
      if [[ "$VERSION" == "latest" ]]; then
        gh release download --repo "$REPO" --pattern "$asset_name" --pattern "SHA256SUMS.txt" --dir "$TMP_DIR" --clobber
      else
        gh release download "$VERSION" --repo "$REPO" --pattern "$asset_name" --pattern "SHA256SUMS.txt" --dir "$TMP_DIR" --clobber
      fi
      mv -f "${TMP_DIR}/${asset_name}" "$bin_dl"
    elif [[ -n "$token" ]]; then
      log_info "Öffentlicher Download nicht verfügbar – verwende GITHUB_TOKEN …"
      download_release_with_token "$token" "$asset_name" "$bin_dl" "$sums_dl" || {
        log_err "Download über GitHub API mit Token fehlgeschlagen."
        exit 1
      }
    else
      log_err "Release-Download fehlgeschlagen (bei privatem Repository bitte 'gh auth login', GITHUB_TOKEN setzen oder im geklonten Ordner './install.sh --from-source' nutzen)."
      exit 1
    fi
  fi

  expected_hash="$(awk -v asset="$asset_name" '
    {
      name = $2
      sub(/^\*/, "", name)
      if (name == asset || name == "dist/" asset || name ~ "/" asset "$") {
        print tolower($1)
        exit
      }
    }' "$sums_dl")"

  if [[ -z "$expected_hash" ]]; then
    log_err "Kein SHA-256-Eintrag für '${asset_name}' in SHA256SUMS.txt gefunden."
    exit 1
  fi

  actual_hash="$(compute_sha256 "$bin_dl" | tr '[:upper:]' '[:lower:]')"
  if [[ "$expected_hash" != "$actual_hash" ]]; then
    log_err "SHA-256-Prüfung fehlgeschlagen! Erwartet: ${expected_hash}, erhalten: ${actual_hash}"
    exit 1
  fi
  log_ok "SHA-256-Prüfsumme verifiziert (${actual_hash})"
  chmod 0755 "$bin_dl"
  BUILT_BIN="$bin_dl"
}

install_from_source() {
  local src_dir build_ver clone_ok=0
  if [[ "$IN_LOCAL_REPO" -eq 1 ]]; then
    src_dir="$LOCAL_REPO_DIR"
    log_info "Kompiliere aus lokalem Repository (${src_dir}) …"
  else
    src_dir="${TMP_DIR}/src"
    log_info "Klone Repository https://github.com/${REPO}.git …"
    if [[ "$VERSION" != "latest" ]]; then
      if GIT_TERMINAL_PROMPT=0 git clone --depth 1 --branch "$VERSION" "https://github.com/${REPO}.git" "$src_dir" 2>/dev/null; then
        clone_ok=1
      fi
    else
      if GIT_TERMINAL_PROMPT=0 git clone --depth 1 "https://github.com/${REPO}.git" "$src_dir" 2>/dev/null; then
        clone_ok=1
      fi
    fi
    if [[ "$clone_ok" -eq 0 ]] && command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
      log_info "Öffentlicher git clone fehlgeschlagen – nutze authentifizierte GitHub CLI (gh) …"
      rm -rf "$src_dir"
      if [[ "$VERSION" != "latest" ]]; then
        gh repo clone "$REPO" "$src_dir" -- --depth 1 --branch "$VERSION"
      else
        gh repo clone "$REPO" "$src_dir" -- --depth 1
      fi
      clone_ok=1
    fi
    if [[ "$clone_ok" -eq 0 ]]; then
      log_err "Repository-Klon fehlgeschlagen (bei privatem Repo bitte 'gh auth login' durchführen oder lokal im Repository './install.sh --from-source' aufrufen)."
      exit 1
    fi
  fi

  build_ver="$VERSION"
  if [[ -d "${src_dir}/.git" ]] && command -v git >/dev/null 2>&1; then
    build_ver="$(git -C "$src_dir" describe --tags --always 2>/dev/null || echo "$VERSION")"
  fi
  [[ "$build_ver" == "latest" ]] && build_ver="dev"

  log_info "Baue it-kb (${build_ver}) mit CGO_ENABLED=0 …"
  (
    cd "$src_dir"
    CGO_ENABLED=0 "$GO_BIN" build -ldflags "-s -w -X main.version=${build_ver}" -o "${TMP_DIR}/it-kb" ./cmd/it-kb
  )
  chmod 0755 "${TMP_DIR}/it-kb"
  BUILT_BIN="${TMP_DIR}/it-kb"
  log_ok "Build erfolgreich abgeschlossen"
}

service_is_active() {
  command -v systemctl >/dev/null 2>&1 || return 1
  if [[ "$SCOPE" == "system" ]]; then
    systemctl is-active --quiet it-kb 2>/dev/null
  else
    systemctl --user is-active --quiet it-kb 2>/dev/null
  fi
}

write_default_config_if_missing() {
  local tls_json="false"
  [[ "$TLS_AUTO" -eq 1 ]] && tls_json="true"

  local cfg_tmp="${TMP_DIR}/config.json"
  cat >"$cfg_tmp" <<EOF
{
  "listen": "${LISTEN_ADDR}",
  "data_dir": "${DATA_DIR}",
  "site_title": "IT-Wissensdatenbank",
  "auth_required": true,
  "session_hours": 12,
  "bootstrap_admin": "",
  "bootstrap_password": "",
  "tls_auto": ${tls_json},
  "tls_cert": "",
  "tls_key": "",
  "trusted_proxies": [],
  "auto_backup_dir": "",
  "auto_backup_interval_hours": 0,
  "auto_backup_keep": 7,
  "auto_backup_passphrase": "",
  "review_reminder_days": 180,
  "max_image_mb": 15,
  "max_video_mb": 100,
  "max_attachment_mb": 25
}
EOF

  if [[ "$SCOPE" == "system" ]]; then
    run_priv mkdir -p "$CONFIG_DIR" "$DATA_DIR"
    if run_priv test -f "$CONFIG_PATH"; then
      log_info "Bestehende Konfiguration bleibt unverändert: ${CONFIG_PATH}"
    else
      run_priv install -m 0640 "$cfg_tmp" "$CONFIG_PATH"
      log_ok "Standard-Konfiguration angelegt: ${CONFIG_PATH}"
    fi
  else
    mkdir -p "$CONFIG_DIR" "$DATA_DIR"
    if [[ -f "$CONFIG_PATH" ]]; then
      log_info "Bestehende Konfiguration bleibt unverändert: ${CONFIG_PATH}"
    else
      install -m 0640 "$cfg_tmp" "$CONFIG_PATH"
      log_ok "Standard-Konfiguration angelegt: ${CONFIG_PATH}"
    fi
  fi
}

setup_systemd_service() {
  if ! command -v systemctl >/dev/null 2>&1; then
    log_warn "systemctl wurde nicht gefunden – systemd-Dienst konnte nicht eingerichtet werden."
    return 0
  fi

  local unit_tmp="${TMP_DIR}/it-kb.service"
  if [[ "$SCOPE" == "system" ]]; then
    if ! id -u itkb >/dev/null 2>&1; then
      local nologin_shell="/usr/sbin/nologin"
      [[ -x /sbin/nologin ]] && nologin_shell="/sbin/nologin"
      run_priv useradd --system --no-create-home --shell "$nologin_shell" itkb
    fi
    run_priv chown -R itkb:itkb "$DATA_DIR"
    run_priv chown root:itkb "$CONFIG_PATH"
    run_priv chmod 0640 "$CONFIG_PATH"

    cat >"$unit_tmp" <<EOF
[Unit]
Description=IT-Wissensdatenbank (it-kb)
After=network.target

[Service]
Type=simple
User=itkb
Group=itkb
WorkingDirectory=${DATA_DIR}
ExecStart=${BIN_PATH} serve --config ${CONFIG_PATH}
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
EOF
    run_priv install -m 0644 "$unit_tmp" /etc/systemd/system/it-kb.service
    run_priv systemctl daemon-reload
    run_priv systemctl enable --now it-kb
    log_ok "Systemweiter systemd-Dienst 'it-kb' aktiviert und gestartet"
  else
    local user_unit_dir="${HOME}/.config/systemd/user"
    mkdir -p "$user_unit_dir"
    cat >"$unit_tmp" <<EOF
[Unit]
Description=IT-Wissensdatenbank (it-kb)
After=network.target

[Service]
Type=simple
WorkingDirectory=${DATA_DIR}
ExecStart=${BIN_PATH} serve --config ${CONFIG_PATH}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
    install -m 0644 "$unit_tmp" "${user_unit_dir}/it-kb.service"
    if systemctl --user daemon-reload >/dev/null 2>&1; then
      systemctl --user enable --now it-kb
      log_ok "Benutzer-systemd-Dienst 'it-kb' aktiviert und gestartet"
    else
      log_warn "systemd --user nicht verfügbar; Unit-Datei liegt unter ${user_unit_dir}/it-kb.service."
    fi
  fi
}

do_uninstall() {
  if ! confirm_action "Binary (${BIN_PATH}) und ggf. Dienst 'it-kb' entfernen? (Daten unter ${DATA_DIR} bleiben erhalten)"; then
    log_info "Deinstallation abgebrochen."
    exit 0
  fi

  if command -v systemctl >/dev/null 2>&1; then
    if [[ "$SCOPE" == "system" && -f /etc/systemd/system/it-kb.service ]]; then
      run_priv systemctl disable --now it-kb >/dev/null 2>&1 || true
      run_priv rm -f /etc/systemd/system/it-kb.service
      run_priv systemctl daemon-reload || true
      log_ok "Systemdienst 'it-kb' entfernt"
    elif [[ "$SCOPE" == "user" && -f "${HOME}/.config/systemd/user/it-kb.service" ]]; then
      systemctl --user disable --now it-kb >/dev/null 2>&1 || true
      rm -f "${HOME}/.config/systemd/user/it-kb.service"
      systemctl --user daemon-reload >/dev/null 2>&1 || true
      log_ok "Benutzerdienst 'it-kb' entfernt"
    fi
  fi

  if [[ "$SCOPE" == "system" ]]; then
    run_priv rm -f "$BIN_PATH"
  else
    rm -f "$BIN_PATH"
  fi
  log_ok "Binary entfernt: ${BIN_PATH}"
  log_info "Datenverzeichnis (${DATA_DIR}) und Konfiguration (${CONFIG_PATH}) blieben unangetastet."
}

main() {
  parse_args "$@"
  TMP_DIR="$(mktemp -d)"
  detect_platform

  if [[ "$UNINSTALL" -eq 1 ]]; then
    do_uninstall
    exit 0
  fi

  check_and_install_deps
  if [[ "$CHECK_ONLY" -eq 1 ]]; then
    exit 0
  fi

  if [[ "$MODE" == "release" ]]; then
    install_from_release
  else
    install_from_source
  fi

  local was_active=0
  if service_is_active; then
    was_active=1
    log_info "Stoppe laufenden Dienst 'it-kb' für Update …"
    if [[ "$SCOPE" == "system" ]]; then
      run_priv systemctl stop it-kb
    else
      systemctl --user stop it-kb
    fi
  fi

  local bin_dir
  bin_dir="$(dirname "$BIN_PATH")"
  if [[ "$SCOPE" == "system" ]]; then
    run_priv mkdir -p "$bin_dir"
    run_priv install -m 0755 "$BUILT_BIN" "${BIN_PATH}.tmp"
    run_priv mv -f "${BIN_PATH}.tmp" "$BIN_PATH"
  else
    mkdir -p "$bin_dir"
    install -m 0755 "$BUILT_BIN" "${BIN_PATH}.tmp"
    mv -f "${BIN_PATH}.tmp" "$BIN_PATH"
  fi
  log_ok "Binary installiert: ${BIN_PATH}"

  write_default_config_if_missing

  if [[ "$INSTALL_SERVICE" -eq 1 ]]; then
    setup_systemd_service
  elif [[ "$was_active" -eq 1 ]]; then
    if [[ "$SCOPE" == "system" ]]; then
      run_priv systemctl start it-kb
    else
      systemctl --user start it-kb
    fi
    log_ok "Dienst 'it-kb' nach Update wieder gestartet"
  fi

  local installed_ver proto port
  installed_ver="$("$BIN_PATH" version)"
  log_ok "Installiert: ${installed_ver}"

  if [[ "$SCOPE" == "user" && ":$PATH:" != *":${bin_dir}:"* ]]; then
    log_warn "Hinweis: '${bin_dir}' liegt aktuell nicht in deinem \$PATH (z. B. export PATH=\"${bin_dir}:\$PATH\")."
  fi

  proto="http"
  [[ "$TLS_AUTO" -eq 1 ]] && proto="https"
  port="${LISTEN_ADDR##*:}"
  [[ -z "$port" ]] && port="8080"

  printf '\nStarten:  %s serve --config %s\n' "$BIN_PATH" "$CONFIG_PATH"
  printf 'Web-UI:   %s://localhost:%s\n' "$proto" "$port"
}

main "$@"
