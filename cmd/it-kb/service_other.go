//go:build !windows

package main

import (
	"fmt"
	"os"

	"github.com/csx5/it-kb/internal/config"
)

func maybeRunAsWindowsService(_ config.Config) (bool, error) {
	return false, nil
}

func runServiceCommand(_ []string) {
	fmt.Fprintln(os.Stderr, "Der Befehl 'service' ist nur unter Windows verfügbar. Unter Linux bitte die systemd-Unit unter deploy/linux/it-kb.service nutzen.")
	os.Exit(1)
}
