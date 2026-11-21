package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config enthält alle Laufzeiteinstellungen des Servers.
// Priorität: CLI-Flags > Umgebungsvariablen (ITKB_*) > JSON-Config > Defaults.
type Config struct {
	Listen             string `json:"listen"`
	DataDir            string `json:"data_dir"`
	SiteTitle          string `json:"site_title"`
	AuthRequired       bool   `json:"auth_required"`
	SessionHours       int    `json:"session_hours"`
	BootstrapAdmin     string `json:"bootstrap_admin"`
	BootstrapPassword  string `json:"bootstrap_password"`
	AdminPassword      string `json:"admin_password"`
	TLSAuto            bool   `json:"tls_auto"`
	TLSCert            string `json:"tls_cert"`
	TLSKey             string `json:"tls_key"`
	ReviewReminderDays int    `json:"review_reminder_days"`
	MaxImageMB         int64  `json:"max_image_mb"`
	MaxVideoMB         int64  `json:"max_video_mb"`
	MaxAttachmentMB    int64  `json:"max_attachment_mb"`
}

func Default() Config {
	return Config{
		Listen:             ":8080",
		DataDir:            "./data",
		SiteTitle:          "IT-Wissensdatenbank",
		AuthRequired:       true,
		SessionHours:       12,
		ReviewReminderDays: 180,
		MaxImageMB:         15,
		MaxVideoMB:         100,
		MaxAttachmentMB:    25,
	}
}

// Load liest optional eine JSON-Konfigurationsdatei ein und überlagert sie mit ITKB_*-Env-Vars.
func Load(configPath string) (Config, error) {
	cfg := Default()

	if configPath != "" {
		raw, err := os.ReadFile(configPath)
		if err != nil {
			return cfg, fmt.Errorf("konfigurationsdatei lesen: %w", err)
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("konfigurationsdatei parsen: %w", err)
		}
	}

	applyEnv(&cfg)
	return cfg, nil
}

func (c Config) DBPath() string {
	return filepath.Join(c.DataDir, "kb.sqlite")
}

func (c Config) MediaDir() string {
	return filepath.Join(c.DataDir, "media")
}

func (c Config) CertsDir() string {
	return filepath.Join(c.DataDir, "certs")
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Listen) == "" {
		return errors.New("listen-adresse darf nicht leer sein")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("data_dir darf nicht leer sein")
	}
	if (c.TLSCert != "") != (c.TLSKey != "") {
		return errors.New("tls_cert und tls_key müssen gemeinsam gesetzt werden")
	}
	if c.ReviewReminderDays < 0 {
		return errors.New("review_reminder_days darf nicht negativ sein")
	}
	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("ITKB_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("ITKB_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("ITKB_SITE_TITLE"); v != "" {
		cfg.SiteTitle = v
	}
	if v := os.Getenv("ITKB_AUTH_REQUIRED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.AuthRequired = b
		}
	}
	if v := os.Getenv("ITKB_SESSION_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.SessionHours = n
		}
	}
	if v := os.Getenv("ITKB_BOOTSTRAP_ADMIN"); v != "" {
		cfg.BootstrapAdmin = v
	}
	if v := os.Getenv("ITKB_BOOTSTRAP_PASSWORD"); v != "" {
		cfg.BootstrapPassword = v
	}
	if v := os.Getenv("ITKB_ADMIN_PASSWORD"); v != "" {
		cfg.AdminPassword = v
	}
	if v := os.Getenv("ITKB_TLS_AUTO"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.TLSAuto = b
		}
	}
	if v := os.Getenv("ITKB_TLS_CERT"); v != "" {
		cfg.TLSCert = v
	}
	if v := os.Getenv("ITKB_TLS_KEY"); v != "" {
		cfg.TLSKey = v
	}
	if v := os.Getenv("ITKB_REVIEW_REMINDER_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.ReviewReminderDays = n
		}
	}
	if v := os.Getenv("ITKB_MAX_IMAGE_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxImageMB = n
		}
	}
	if v := os.Getenv("ITKB_MAX_VIDEO_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxVideoMB = n
		}
	}
	if v := os.Getenv("ITKB_MAX_ATTACHMENT_MB"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			cfg.MaxAttachmentMB = n
		}
	}
}
