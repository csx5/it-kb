package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/csx5/it-kb/internal/backup"
	"github.com/csx5/it-kb/internal/config"
	"github.com/csx5/it-kb/internal/db"
	"github.com/csx5/it-kb/internal/search"
	"github.com/csx5/it-kb/internal/web"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			runServe(os.Args[2:])
			return
		case "backup":
			runBackup(os.Args[2:])
			return
		case "service":
			runServiceCommand(os.Args[2:])
			return
		case "version", "-v", "--version":
			fmt.Printf("it-kb %s\n", version)
			return
		}
	}

	// Zero-Config-Default: direkt starten (oder als Windows-Dienst)
	runServe(os.Args[1:])
}

func parseConfigFlags(args []string) (config.Config, error) {
	fs := flag.NewFlagSet("it-kb", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Pfad zur optionalen JSON-Konfigurationsdatei")
	listen := fs.String("listen", "", "HTTP/HTTPS-Adresse (Standard :8080)")
	dataDir := fs.String("data", "", "Datenverzeichnis für SQLite und Medien (Standard ./data)")
	title := fs.String("title", "", "Seitentitel in der Web-Oberfläche")
	adminPw := fs.String("admin-password", "", "Optionales Admin-Passwort für kritische Aktionen")
	tlsCert := fs.String("tls-cert", "", "Pfad zum TLS-Zertifikat (PEM)")
	tlsKey := fs.String("tls-key", "", "Pfad zum privaten TLS-Schlüssel (PEM)")

	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return cfg, err
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *title != "" {
		cfg.SiteTitle = *title
	}
	if *adminPw != "" {
		cfg.AdminPassword = *adminPw
	}
	if *tlsCert != "" {
		cfg.TLSCert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKey = *tlsKey
	}
	return cfg, cfg.Validate()
}

func runServe(args []string) {
	cfg, err := parseConfigFlags(args)
	if err != nil {
		log.Fatalf("Konfiguration ungültig: %v", err)
	}

	// Falls unter Windows als Systemdienst gestartet, übernimmt der Windows-Service-Runner
	if handled, err := maybeRunAsWindowsService(cfg); handled {
		if err != nil {
			log.Fatalf("Windows-Dienst fehlgeschlagen: %v", err)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := startServer(ctx, cfg); err != nil {
		log.Fatalf("Server-Fehler: %v", err)
	}
}

func startServer(ctx context.Context, cfg config.Config) error {
	store, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("datenbank öffnen: %w", err)
	}
	defer store.Close()

	if err := store.SeedInitialData(ctx, cfg.SeedDemoArticles); err != nil {
		return fmt.Errorf("initialdaten anlegen: %w", err)
	}

	articles, err := store.ListAllArticles(ctx)
	if err != nil {
		return fmt.Errorf("artikel laden: %w", err)
	}
	synonyms, err := store.ListSynonymGroups(ctx)
	if err != nil {
		return fmt.Errorf("synonyme laden: %w", err)
	}

	engine := search.NewEngine(cfg.ReviewReminderDays)
	engine.Rebuild(articles, synonyms)

	srv, err := web.NewServer(cfg, store, engine, version)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			log.Printf("it-kb (%s) gestartet auf https://%s (Daten: %s)", version, cfg.Listen, cfg.DataDir)
			errCh <- httpServer.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			log.Printf("it-kb (%s) gestartet auf http://%s (Daten: %s)", version, cfg.Listen, cfg.DataDir)
			errCh <- httpServer.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	cfgPath := fs.String("config", "", "Pfad zur optionalen JSON-Konfigurationsdatei")
	dataDir := fs.String("data", "", "Datenverzeichnis")
	out := fs.String("out", "", "Zielpfad für das Backup-ZIP")
	_ = fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("Konfiguration laden: %v", err)
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(".", fmt.Sprintf("it-kb-backup-%s.zip", time.Now().Format("20060102-150405")))
	}

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("Datenbank öffnen: %v", err)
	}
	defer store.Close()

	if err := backup.CreateFullBackup(context.Background(), store, cfg.MediaDir(), outPath); err != nil {
		log.Fatalf("Backup fehlgeschlagen: %v", err)
	}
	log.Printf("Voll-Backup erfolgreich erstellt: %s", outPath)
}
