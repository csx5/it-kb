package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SeedInitialData legt bei einer frischen Datenbank Standard-Kategorien und Basis-Synonyme an.
func (s *Store) SeedInitialData(ctx context.Context) error {
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
		{"outlook-exchange", "Outlook / Exchange", "E-Mail, Exchange, Kalender", 10},
		{"active-directory", "Active Directory", "Konten, GPO, LDAP", 20},
		{"netzwerk-vpn", "Netzwerk / VPN", "DNS, DHCP, VPN, Firewall, WLAN", 30},
		{"drucker", "Drucker", "Netzwerkdrucker, Spooler, Treiber", 40},
		{"windows-clients", "Windows / Clients", "Windows, Updates, Office", 50},
		{"server-infrastruktur", "Server / Infrastruktur", "Server, Virtualisierung, Backups", 60},
		{"sonstiges", "Sonstiges", "Allgemeine Themen", 90},
	}

	for _, c := range categories {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO categories (slug, name, description, sort_order, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, c.slug, c.name, c.desc, c.order, now); err != nil {
			return fmt.Errorf("kategorie %s anlegen: %w", c.slug, err)
		}
	}

	synonymGroups := [][]string{
		{"o365", "m365", "microsoft 365", "outlook", "exchange"},
		{"pw", "passwort", "kennwort", "password"},
		{"rdp", "remotedesktop", "remote desktop", "mstsc"},
		{"ad", "active directory", "domäne", "domain"},
		{"drucker", "printer", "spooler", "druckwarteschlange"},
		{"wlan", "wifi", "wi-fi"},
		{"vpn", "tunnel"},
	}
	for _, group := range synonymGroups {
		raw, _ := json.Marshal(group)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO synonym_groups (terms_json, updated_at) VALUES (?, ?)
		`, string(raw), now); err != nil {
			return fmt.Errorf("synonyme anlegen: %w", err)
		}
	}

	return tx.Commit()
}
