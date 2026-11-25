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

## Schnellstart (Lokal)

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
