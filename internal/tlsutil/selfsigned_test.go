package tlsutil

import (
	"crypto/tls"
	"testing"
)

func TestEnsureSelfSignedCert(t *testing.T) {
	dir := t.TempDir()

	cert1, key1, gen1, err := EnsureSelfSignedCert(dir)
	if err != nil || !gen1 {
		t.Fatalf("erstes zertifikat fehlgeschlagen: gen=%v err=%v", gen1, err)
	}
	if _, err := tls.LoadX509KeyPair(cert1, key1); err != nil {
		t.Fatalf("zertifikat nicht ladbar: %v", err)
	}

	// Zweiter Aufruf muss bestehendes Zertifikat wiederverwenden
	_, _, gen2, err := EnsureSelfSignedCert(dir)
	if err != nil || gen2 {
		t.Fatalf("bestehendes zertifikat hätte wiederverwendet werden müssen: gen=%v err=%v", gen2, err)
	}
}
