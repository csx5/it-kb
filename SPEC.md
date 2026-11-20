# Architektur-Spezifikation & Roadmap (`it-kb`)

Dieses Dokument hält alle abgestimmten Design-Entscheidungen und den Umsetzungsstand fest, damit nahtlos weiterentwickelt werden kann.

## 1. Abgestimmte Architektur-Entscheidungen

- **Datenbank:** Pure-Go SQLite (`modernc.org/sqlite`) im WAL-Modus + Medien auf Disk (`data/media/YYYY/MM/<uuid>.<ext>`). Schema enthält bereits `users` und `sessions` für v2 (RBAC: `admin`, `editor`, `reader`).
- **Suche:** Eigene Pure-Go In-Memory-Engine (`internal/search`), beim Start aus SQLite geladen und bei jeder Änderung live synchronisiert:
  - IT-Tokenizer (erhält `0x...`, `HKLM\...`, Dateinamen, Bindestrich-/Unterstrich-Bezeichner + Teil-Tokens + Umlaut-Normalisierung).
  - BM25 mit Feldgewichten: `Fehlercode (5.0) > Titel (3.5) > Tags (2.5) > Symptome (1.5) > Lösung (1.0)`.
  - Exakt-Boost für Fehlercodes und Phrasen.
  - Bidirektionale Team-Synonyme (inkl. Mehrwort-Begriffe).
  - Trigramm-/Damerau-Levenshtein-Tippfehler-Toleranz.
  - Snippets mit `<mark>`-Highlighting + Filter (`category`, `tag`, `status`, `author`, `template`, `needs_review`).
- **Frontend:** Zero-Build Vanilla JS (ES-Module) + modernes natives CSS via `embed.FS`, 100 % Deutsch mit echten Umlauten, ohne Emojis, Dark & Light Theme.
- **Screenshot-Workflow:** `Strg+V` öffnet direkt ein Canvas-Bildeditor-Modal (Rahmen, Pfeil, Schwärzen, Verpixeln, Zuschneiden, Undo + `Enter`/1-Klick „Direkt einfügen“), clientseitige WebP/PNG-Komprimierung vor dem Upload.
- **Medien & Sicherheit:** Konfigurierbare Whitelist + Größenlimits (15 MB Bild / 100 MB Video / 25 MB Anhang), `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff` für Anhänge, serverseitiges Markdown-Rendering (`goldmark` + `bluemonday`), CSP, revisionssichere Medienaufbewahrung bis zur expliziten Orphan-Bereinigung.
- **Team-Funktionen:** Optimistic Locking (`version`) mit Diff-Konfliktdialog + informativer In-Memory-Soft-Lock (Heartbeat), Versionshistorie (Diff + Restore), „Zuletzt geprüft“ mit konfigurierbarer Frist (Default 180 Tage).
- **Schutz in v1:** Soft-Delete (Papierkorb) + optionales Admin-Passwort (`ITKB_ADMIN_PASSWORD`) für endgültiges Löschen, Papierkorb leeren, Orphan-Bereinigung und Backup-Restore.
- **Backup & Export:** Portabler Markdown-ZIP-Export (`.md` mit YAML-Frontmatter + `media/` + JSON-Metadaten) + Voll-Backup-ZIP (`VACUUM INTO` + `media/`) inkl. Restore und CLI `it-kb backup`.
- **Deployment:** Zero-Config-Start, optionales natives TLS, nativer Windows-Dienst im Binary + `systemd`-Unit für Linux.

## 2. Umsetzungsstatus (Checkliste)

### Fertig (Phase 1 & Phase 2 – Grundstruktur, Artikel-CRUD, Editor & Konflikte)
- [x] Projektlayout, `Makefile`, `.gitignore`, `LICENSE`, `config.example.json`, `README.md`
- [x] Konfigurationssystem (`internal/config`: Flags + Env + JSON + Defaults)
- [x] Domain-Modelle (`internal/model`)
- [x] Pure-Go SQLite-Layer, Migration `001_init.sql` (inkl. v2 `users`/`sessions`) & Seeding (`internal/db`)
- [x] Pure-Go In-Memory-Such-Engine (`internal/search`: IT-Tokenizer, BM25, Exakt-Boost, Synonyme, Trigramm/Fuzzy, Snippets)
- [x] Markdown-Renderer mit HTML-Sanitizing (`internal/markdown`)
- [x] Medien-Storage & Sicherheits-Whitelist (`internal/media`)
- [x] In-Memory Soft-Lock Manager (`internal/web/locks.go`)
- [x] HTTP/TLS-Server, Security-Header/CSP, Admin-Middleware, `embed.FS`, Read/Search-API (`internal/web/server.go`)
- [x] CLI-Entrypoint inkl. Windows-Service-Integration (`cmd/it-kb`) & Deployment-Dateien (`deploy/`)
- [x] **Phase 2 – Artikel-CRUD, Editor & Konflikte:**
  - [x] REST-Endpunkte `POST /api/articles`, `PUT /api/articles/{id}`, `DELETE /api/articles/{id}` (Soft-Delete), `POST /api/articles/{id}/restore`, `POST /api/articles/{id}/verify`, `DELETE /api/articles/{id}/permanent` (Admin-geschützt), `POST /api/preview`
  - [x] Optimistic Locking (`409 Conflict` inkl. Server-Snapshot) + Soft-Lock-Heartbeat (`POST`/`DELETE /api/articles/{id}/lock`)
  - [x] Frontend: Detailansicht mit Copy-Button & Aktionen, Papierkorb-Ansicht, Vorlagen-Editor (*Problem & Lösung* / *How-To*) mit Markdown-Toolbar & Split-Vorschau, Konfliktdialog mit Feldvergleich & Soft-Lock-Banner
  - [x] Unit- und HTTP-Integrationstests (`internal/db/articles_test.go`, `internal/web/server_test.go`)

### Nächste Ausbaustufen (für die nächsten Sessions)
- [ ] **Phase 3 – Screenshot-Bildeditor & Medien-Upload:**
  - `POST /api/media` (Upload mit Whitelist, SHA256, Größenlimit) & `GET /media/{id}` (Streaming mit korrekten Sicherheits-Headern)
  - Vollständiger Canvas-Editor in `editor-canvas.js` (Rahmen, Pfeil, Schwärzen, Verpixeln, Zuschneiden, Undo/Redo, `Enter`-Schnellpfad, WebP/PNG-Komprimierung)
  - Drag-and-Drop für Videos & IT-Anhänge + Orphan-Bereinigung im Admin-Bereich
- [ ] **Phase 4 – Revisionen, Synonyme, Kategorien & Papierkorb-UI:**
  - Endpunkte & UI für Revisionsliste, Zeilen-Diff und 1-Klick-Restore
  - Einstellungs-UI für Kategorien, Synonym-Gruppen, Seitentitel, Prüf-Intervall und Papierkorb
- [ ] **Phase 5 – Backup, Portabler Export & Restore:**
  - Portabler Markdown-ZIP-Export (`.md` + YAML-Frontmatter + `media/` + JSON)
  - Konsistentes Voll-Backup-ZIP (`VACUUM INTO` + `media/`), UI-Restore & CLI `it-kb backup`
- [ ] **Phase 6 – Tests & Release-Feinschliff:**
  - Unit-Tests für Tokenizer/BM25/Fuzzy/Synonyme, Optimistic Locking, Whitelist/Path-Traversal und Export/Backup
  - Cross-Compile-Check Linux & Windows
