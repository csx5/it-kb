package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/csx5/it-kb/internal/model"
)

// SeedInitialData legt bei einer frischen Datenbank Standard-Kategorien,
// Starter-Synonyme sowie optional 3 löschbare IT-Beispielartikel an.
func (s *Store) SeedInitialData(ctx context.Context, seedDemoArticles bool) error {
	var catCount int
	if err := s.sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories`).Scan(&catCount); err != nil {
		return err
	}
	if catCount > 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	categories := []struct {
		slug, name, desc string
		order            int
	}{
		{"outlook-exchange", "Outlook / Exchange", "E-Mail-Clients, Exchange Online/On-Prem, OST/PST, Kalender", 10},
		{"active-directory", "Active Directory", "Benutzerkonten, Gruppenrichtlinien (GPO), Kerberos, LDAP", 20},
		{"netzwerk-vpn", "Netzwerk / VPN", "DNS, DHCP, VPN-Tunnel, Firewall, WLAN, Remote-Zugriff", 30},
		{"drucker", "Drucker", "Netzwerkdrucker, Print-Spooler, Treiber, Scan-to-Mail", 40},
		{"windows-clients", "Windows / Clients", "Windows 10/11, Updates, Profile, Treiber, Office", 50},
		{"server-infrastruktur", "Server / Infrastruktur", "Windows Server, Linux, Hyper-V, Backups, Zertifikate", 60},
		{"sonstiges", "Sonstiges", "Allgemeine IT-Anleitungen und sonstige Themen", 90},
	}

	catIDs := make(map[string]int64, len(categories))
	for _, c := range categories {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO categories (slug, name, description, sort_order, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, c.slug, c.name, c.desc, c.order, now)
		if err != nil {
			return fmt.Errorf("kategorie %s anlegen: %w", c.slug, err)
		}
		id, _ := res.LastInsertId()
		catIDs[c.slug] = id
	}

	synonymGroups := [][]string{
		{"o365", "m365", "microsoft 365", "outlook", "exchange"},
		{"pw", "passwort", "kennwort", "password"},
		{"rdp", "remotedesktop", "remote desktop", "mstsc"},
		{"ad", "active directory", "domäne", "domain"},
		{"drucker", "printer", "spooler", "druckwarteschlange"},
		{"wlan", "wifi", "wi-fi", "funknetz"},
		{"vpn", "tunnel", "wireguard", "openvpn"},
	}
	for _, group := range synonymGroups {
		raw, _ := json.Marshal(group)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO synonym_groups (terms_json, updated_at) VALUES (?, ?)
		`, string(raw), now); err != nil {
			return fmt.Errorf("synonyme anlegen: %w", err)
		}
	}

	if seedDemoArticles {
		demos := []struct {
			slug, tmpl, title, catSlug, errCodes, symptoms, solution string
			tags                                                     []string
		}{
			{
				slug:     "outlook-fehler-0x8004010f-datendatei",
				tmpl:     model.TemplateProblemSolution,
				title:    "Outlook: Fehler 0x8004010F beim Senden und Empfangen",
				catSlug:  "outlook-exchange",
				errCodes: "0x8004010F, Auf die Outlook-Datendatei kann nicht zugegriffen werden",
				symptoms: "Beim Klick auf **Senden/Empfangen** meldet Outlook den Fehler `0x8004010F`. E-Mails verbleiben im Postausgang. Häufig nach einem Profilwechsel oder einer beschädigten `.ost`-Datei.",
				solution: "### Lösungsschritte\n\n1. Outlook vollständig schließen (im Task-Manager prüfen, ob `OUTLOOK.EXE` beendet ist).\n2. Systemsteuerung öffnen: `Mail (Microsoft Outlook)` -> **Profile anzeigen**.\n3. Neues Profil anlegen und das Exchange-/Microsoft-365-Konto neu einbinden.\n4. Das neue Profil als Standard auswählen und Outlook neu starten.\n\n```powershell\n# Optional: Prüfen, ob noch Outlook-Prozesse laufen\nGet-Process outlook -ErrorAction SilentlyContinue | Stop-Process -Force\n```",
				tags:     []string{"outlook", "ost", "profil", "exchange"},
			},
			{
				slug:     "netzwerkdrucker-warteschlange-haengt",
				tmpl:     model.TemplateProblemSolution,
				title:    "Netzwerkdrucker druckt nicht – Druckwarteschlange hängt",
				catSlug:  "drucker",
				errCodes: "0x0000011b, Spooler hängt, Druckauftrag wird gelöscht",
				symptoms: "Druckaufträge bleiben dauerhaft im Status *Wird gedruckt* oder *Wird gelöscht* stehen. Neustart des Druckers allein behebt das Problem nicht.",
				solution: "### Druckwarteschlange (Spooler) sauber zurücksetzen\n\nPowerShell als Administrator ausführen:\n\n```powershell\nStop-Service -Name Spooler -Force\nRemove-Item -Path \"C:\\Windows\\System32\\spool\\PRINTERS\\*\" -Force -ErrorAction SilentlyContinue\nStart-Service -Name Spooler\n```\n\nDanach Testseite drucken. Falls Fehler `0x0000011b` auf einem Druckserver auftritt, Patchstand bzw. `RpcAuthnLevelPrivacyEnabled` prüfen.",
				tags:     []string{"drucker", "spooler", "windows", "powershell"},
			},
			{
				slug:     "ad-benutzerkonto-sperrung-pruefen",
				tmpl:     model.TemplateHowTo,
				title:    "Active Directory: Gesperrtes Benutzerkonto prüfen und entsperren",
				catSlug:  "active-directory",
				errCodes: "Event-ID 4740, Konto gesperrt",
				symptoms: "",
				solution: "### Vorgehen bei wiederholter Kontosperrung\n\n1. Sperrstatus und auslösendes Gerät über PowerShell (mit RSAT/AD-Modul) prüfen:\n\n```powershell\nGet-ADUser -Identity \"max.mustermann\" -Properties LockedOut, AccountLockoutTime, BadPwdCount |\n  Select-Object SamAccountName, LockedOut, AccountLockoutTime, BadPwdCount\n```\n\n2. Typische Ursachen für sofortige Neusperrung:\n   - Altes gespeichertes Kennwort in der Windows-Anmeldeinformationsverwaltung\n   - Mobilgerät (Mail-App / ActiveSync) mit altem Passwort\n   - Getrennte RDP-Sitzung auf einem Terminalserver\n\n3. Konto entsperren:\n\n```powershell\nUnlock-ADAccount -Identity \"max.mustermann\"\n```",
				tags:     []string{"active-directory", "passwort", "lockout", "event-4740"},
			},
		}

		for _, d := range demos {
			tagsRaw, _ := json.Marshal(d.tags)
			catID := catIDs[d.catSlug]
			res, err := tx.ExecContext(ctx, `
				INSERT INTO articles (
					slug, template_type, title, category_id, status,
					error_codes, symptoms, solution_body, tags_json,
					author_name, version, last_verified_at, last_verified_by,
					created_at, updated_at
				) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?, 'System', 1, ?, 'System', ?, ?)
			`, d.slug, d.tmpl, d.title, catID, d.errCodes, d.symptoms, d.solution, string(tagsRaw), now, now, now)
			if err != nil {
				return fmt.Errorf("demo-artikel %s anlegen: %w", d.slug, err)
			}
			artID, _ := res.LastInsertId()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO article_revisions (
					article_id, version, template_type, title, category_id, status,
					error_codes, symptoms, solution_body, tags_json,
					editor_name, change_summary, created_at
				) VALUES (?, 1, ?, ?, ?, 'active', ?, ?, ?, ?, 'System', 'Initialer Beispielartikel', ?)
			`, artID, d.tmpl, d.title, catID, d.errCodes, d.symptoms, d.solution, string(tagsRaw), now); err != nil {
				return fmt.Errorf("demo-revision %s anlegen: %w", d.slug, err)
			}
		}
	}

	return tx.Commit()
}
