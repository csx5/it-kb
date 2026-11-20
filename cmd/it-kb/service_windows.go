//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/csx5/it-kb/internal/config"
)

const windowsServiceName = "it-kb"

type kbWindowsService struct {
	cfg config.Config
}

func (s *kbWindowsService) Execute(_ []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- startServer(ctx, s.cfg)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-errCh:
			cancel()
			if err != nil {
				log.Printf("Windows-Dienst beendet mit Fehler: %v", err)
				return false, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
			}
		}
	}
}

func maybeRunAsWindowsService(cfg config.Config) (bool, error) {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return false, err
	}
	return true, svc.Run(windowsServiceName, &kbWindowsService{cfg: cfg})
}

func runServiceCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Verwendung: it-kb.exe service [install|uninstall] [--config pfad.json]")
		os.Exit(1)
	}
	switch args[0] {
	case "install":
		exePath, err := os.Executable()
		if err != nil {
			log.Fatalf("Executable-Pfad ermitteln: %v", err)
		}
		exePath, _ = filepath.Abs(exePath)

		m, err := mgr.Connect()
		if err != nil {
			log.Fatalf("Service Manager öffnen (Administratorrechte erforderlich): %v", err)
		}
		defer m.Disconnect()

		svcArgs := append([]string{"serve"}, args[1:]...)
		s, err := m.CreateService(windowsServiceName, exePath, mgr.Config{
			DisplayName: "IT-Wissensdatenbank (it-kb)",
			Description: "Lokale IT-Wissensdatenbank im internen LAN",
			StartType:   mgr.StartAutomatic,
		}, svcArgs...)
		if err != nil {
			log.Fatalf("Dienst installieren: %v", err)
		}
		defer s.Close()
		fmt.Println("Windows-Dienst 'it-kb' erfolgreich installiert. Starten mit: sc start it-kb")

	case "uninstall":
		m, err := mgr.Connect()
		if err != nil {
			log.Fatalf("Service Manager öffnen (Administratorrechte erforderlich): %v", err)
		}
		defer m.Disconnect()

		s, err := m.OpenService(windowsServiceName)
		if err != nil {
			log.Fatalf("Dienst öffnen: %v", err)
		}
		defer s.Close()
		if err := s.Delete(); err != nil {
			log.Fatalf("Dienst entfernen: %v", err)
		}
		fmt.Println("Windows-Dienst 'it-kb' erfolgreich entfernt.")

	default:
		fmt.Fprintln(os.Stderr, "Unbekannter Unterbefehl. Erlaubt: install, uninstall")
		os.Exit(1)
	}
}
