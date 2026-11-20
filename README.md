# it-kb – Lokale IT-Wissensdatenbank

Eigenständige, 100 % lokale Wissensdatenbank für IT-Teams als einzelnes Go-Binary mit eingebettetem Web-UI (`embed.FS`), Pure-Go SQLite und deterministischer Volltext-Such-Engine (ohne KI, ohne Cloud, ohne externe Laufzeit-Abhängigkeiten).

## Kernmerkmale

- **Einzelnes Binary:** Keine Laufzeit-Abhängigkeiten (kein Docker, Node, Python oder CGO). Läuft auf Linux, Windows und lokal auf `localhost`.
- **100 % lokal & deterministisch:** Keine externen CDNs, Fonts, Skripte oder Cloud-APIs.
- **IT-Such-Engine:** Eigene In-Memory-Engine mit BM25-Feldgewichtung (`Fehlercode > Titel > Tags > Symptome > Lösung`), Exakt-Boost für Fehlercodes/Registry-Pfade/Dateinamen, Trigramm-Tippfehler-Toleranz und im UI pflegbaren Team-Synonymen.
- **Screenshot-Workflow:** Einfügen per `Strg+V` über einen integrierten Canvas-Bildeditor (Rahmen, Pfeil, Schwärzen/Verpixeln) mit clientseitiger Komprimierung.
- **Struktur & Versionierung:** Vorlagen (*Problem & Lösung*, *How-To*), Versionshistorie mit Diff & Restore, Konflikterkennung, Aktualitätsprüfung und Papierkorb.
- **Kein Vendor-Lock-in:** Portabler Markdown-Export (ZIP) sowie konsistentes SQLite-Voll-Backup inkl. Medien.

## Schnellstart (Lokal)

```bash
go mod tidy
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
| `admin_password` | `--admin-password` | `ITKB_ADMIN_PASSWORD` | _(leer = offen im LAN)_ |
| `tls_cert` | `--tls-cert` | `ITKB_TLS_CERT` | _(leer = HTTP)_ |
| `tls_key` | `--tls-key` | `ITKB_TLS_KEY` | _(leer = HTTP)_ |

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
