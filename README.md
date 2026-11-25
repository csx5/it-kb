# Lokale IT-Wissensdatenbank

Eigenständige, 100 % lokale Wissensdatenbank für IT-Teams als einzelnes Go-Binary mit eingebettetem Web-UI (`embed.FS`), Pure-Go SQLite und deterministischer Volltext-Such-Engine (ohne KI, ohne Cloud, ohne externe Laufzeit-Abhängigkeiten).

## Kernmerkmale

- **Einzelnes Binary:** Keine Laufzeit-Abhängigkeiten (kein Docker, Node, Python oder CGO). Läuft auf Linux, Windows und lokal auf `localhost`.
- **100 % lokal & deterministisch:** Keine externen CDNs, Fonts, Skripte oder Cloud-APIs.
- **IT-Such-Engine:** Eigene In-Memory-Engine mit BM25-Feldgewichtung (`Fehlercode > Titel > Tags > Symptome > Lösung`), Exakt-Boost für Fehlercodes/Registry-Pfade/Dateinamen, Trigramm-Tippfehler-Toleranz und im UI pflegbaren Team-Synonymen.
- **Browser-Workflow & Querverweise:** Teilbare Hash-Links (`#article/<id>`, `#new`, `#edit/<id>`, `#admin`), automatische lokale Entwurfsspeicherung (`localStorage`) mit Schutz vor versehentlichem Verlust, klickbare `#tag`-Filter mit Live-Vorschlägen sowie Artikel-Querverweise per `[[`.
- **Screenshot-Workflow:** Globales `Strg+V` im Editor über den integrierten Canvas-Bildeditor (Schwärzen, Verpixeln, Rahmen, Pfeil, Text-Labels, Zuschneiden, Zoom) mit clientseitiger Komprimierung.
- **Struktur, Vorlagen & Versionierung:** Standard-Vorlagen (*Problem & Lösung*, *How-To*) sowie eigene Team-Vorlagen/Textbausteine, Versionshistorie mit Diff & Restore, Konflikterkennung, Aktualitätsprüfung und Papierkorb.
- **Sichere Authentifizierung & Admin-Konsole:** Standardmäßig geschützt per Benutzer-Login (Argon2id-Passwort-Hashing, optionales 2FA per TOTP-Authenticator-App inkl. Offline-SVG-QR-Code und persistentem DB-Replay-Schutz, 256-Bit-`HttpOnly`-Sessions als SHA-256-Hash in SQLite, Brute-Force-Rate-Limiting, CSRF-Schutz, Fail-Closed für API/Medien/Exporte). Drei Rollen (*Administrator*, *Bearbeiter*, *Leser*), geführte Ersteinrichtung im Browser und Sicherheits-Audit-Log.
- **Datenschutz, Proxy & HTTPS im LAN:** 1-Flag-HTTPS (`--tls-auto` erzeugt lokal ein selbstsigniertes ECDSA-Zertifikat inkl. LAN-IPs), vertrauenswürdige Reverse-Proxy-Konfiguration (`trusted_proxies`), lokaler Sensible-Daten-Warner im Editor vor dem Speichern und AES-256-GCM-Chunked-Backup-Verschlüsselung (`ITKBENC2`, abwärtskompatibel zu `ITKBENC1`).
- **IT-Support-Alltag:** „Hat geholfen“-Zähler, angepinnte Dauerbrenner, Praxis-Kommentare unter Artikeln, algorithmisch ermittelte „Ähnliche Artikel“ und saubere Druck-/PDF-Ansicht (`@media print`).
- **Kein Vendor-Lock-in & Auto-Backup:** Portabler Markdown-Export (ZIP) **und** Markdown-/ZIP-Import inkl. Medien, konsistentes SQLite-Voll-Backup (`VACUUM INTO`) sowie eingebaute zeitgesteuerte Backup-Rotation.

## Einfache Installation (Linux & Windows)

### Linux / macOS (`install.sh`)

```bash
# Direktinstallation des aktuellen Releases inkl. Abhängigkeits- & SHA-256-Prüfung
curl -fsSL https://raw.githubusercontent.com/csx5/lowi/main/install.sh | bash

# Oder als systemd-Dienst systemweit mit automatischem LAN-HTTPS installieren:
curl -fsSL https://raw.githubusercontent.com/csx5/lowi/main/install.sh | bash -s -- --system --service --tls-auto
```

Im geklonten Repository:

```bash
./install.sh --check-only      # Prüft System, Abhängigkeiten, Go (>= 1.24) und Port
./install.sh                   # Baut und installiert lokal unter ~/.local/bin/it-kb
./install.sh --system --service # Systemweite Installation mit systemd-Dienst
./install.sh --uninstall       # Entfernt Binary/Dienst (Datenverzeichnis bleibt erhalten)
```

Fehlende Abhängigkeiten (`curl`, `git`, `sha256sum`, Go >= 1.24) erkennt `install.sh` automatisch und installiert sie auf Wunsch über `pacman`, `apt-get`, `dnf`, `zypper`, `apk`, `brew` oder als offizielle Go-Toolchain. Bei privaten Repositories nutzen beide Skripte automatisch `gh auth login`, `GITHUB_TOKEN` bzw. das lokale geklonte Repository.

### Windows (`install.ps1`)

```powershell
# Benutzer-Installation aus dem aktuellen GitHub-Release inkl. SHA-256-Prüfung
irm https://raw.githubusercontent.com/csx5/lowi/main/install.ps1 | iex

# Im geklonten Repo als nativer Windows-Dienst + LAN-Firewall (PowerShell als Administrator):
.\install.ps1 -FromSource -Service -OpenFirewall -TlsAuto
```

Optionen für `install.ps1`: `-FromSource`, `-Version <tag>`, `-InstallDir <pfad>`, `-Service`, `-OpenFirewall`, `-TlsAuto`, `-CheckOnly`, `-Yes`, `-Uninstall`.

## Schnellstart (Manuell)

```bash
make build
./bin/it-kb serve
```

Danach im Browser öffnen: `http://localhost:8080`

Standardmäßig legt das Programm seine Daten unter `./data` an (`kb.sqlite` und `media/`).

## Konfiguration

Reihenfolge der Priorität: `CLI-Flags > Umgebungsvariablen (ITKB_*) > config.json > Defaults`.

| Option (JSON) | Flag | Env-Variable | Standard |
| --- | --- | --- | --- |
| `listen` | `--listen` | `ITKB_LISTEN` | `:8080` |
| `data_dir` | `--data` | `ITKB_DATA_DIR` | `./data` |
| `site_title` | `--title` | `ITKB_SITE_TITLE` | `IT-Wissensdatenbank` |
| `auth_required` | `--auth` | `ITKB_AUTH_REQUIRED` | `true` |
| `session_hours` | – | `ITKB_SESSION_HOURS` | `12` |
| `bootstrap_admin` | – | `ITKB_BOOTSTRAP_ADMIN` | _(leer = Ersteinrichtung im Browser)_ |
| `bootstrap_password` | – | `ITKB_BOOTSTRAP_PASSWORD` | _(leer)_ |
| `tls_auto` | `--tls-auto` | `ITKB_TLS_AUTO` | `false` |
| `tls_cert` | `--tls-cert` | `ITKB_TLS_CERT` | _(leer = HTTP)_ |
| `tls_key` | `--tls-key` | `ITKB_TLS_KEY` | _(leer = HTTP)_ |
| `trusted_proxies` | – | `ITKB_TRUSTED_PROXIES` | `[]` (kommagetrennte IPs/CIDRs) |
| `auto_backup_dir` | – | `ITKB_AUTO_BACKUP_DIR` | _(leer = aus)_ |
| `auto_backup_interval_hours` | – | `ITKB_AUTO_BACKUP_INTERVAL_HOURS` | `0` (Stunden, z. B. `24`) |
| `auto_backup_keep` | – | `ITKB_AUTO_BACKUP_KEEP` | `7` |
| `auto_backup_passphrase` | – | `ITKB_AUTO_BACKUP_PASSPHRASE` | _(leer = ZIP, sonst `.itkb.enc`)_ |

Beispielkonfiguration siehe [config.example.json](config.example.json).

## Cross-Compiling (Linux & Windows)

```bash
make build-linux
make build-windows
```

## Deployment

- **Linux (`systemd`):** Vorlage unter `deploy/linux/it-kb.service`
- **Windows (nativer Dienst):** Anleitung unter `deploy/windows/DEPLOYMENT.md`

## Architektur & Entwicklungsstatus

Siehe [SPEC.md](SPEC.md) für die vollständige Architektur-Spezifikation und den aktuellen Umsetzungsstand.
