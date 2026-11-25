package auth

import (
	"strings"
	"testing"
	"time"
)

func TestArgon2idHashAndVerify(t *testing.T) {
	if _, err := HashPassword("kurz"); err == nil {
		t.Fatalf("zu kurzes passwort hätte abgelehnt werden müssen")
	}

	h, err := HashPassword("Sicheres-IT-Passwort-2026!")
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}

	ok, err := VerifyPassword("Sicheres-IT-Passwort-2026!", h)
	if err != nil || !ok {
		t.Fatalf("korrektes passwort nicht verifiziert: ok=%v err=%v", ok, err)
	}

	ok, err = VerifyPassword("Falsches-Passwort-2026!", h)
	if err != nil || ok {
		t.Fatalf("falsches passwort hätte abgelehnt werden müssen")
	}
}

func TestLoginRateLimiter(t *testing.T) {
	rl := NewLoginRateLimiter(3, 1*time.Minute, 2*time.Minute)
	key := "192.168.1.10|admin"

	for i := 0; i < 3; i++ {
		if ok, _ := rl.Allow(key); !ok {
			t.Fatalf("versuch %d sollte erlaubt sein", i+1)
		}
		rl.RecordFailure(key)
	}

	if ok, wait := rl.Allow(key); ok || wait <= 0 {
		t.Fatalf("nach 3 fehlversuchen sollte gesperrt sein: ok=%v wait=%v", ok, wait)
	}

	rl.Reset(key)
	if ok, _ := rl.Allow(key); !ok {
		t.Fatalf("nach reset sollte wieder erlaubt sein")
	}
}

func TestTOTPGenerateAndVerify(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil || len(secret) < 20 {
		t.Fatalf("totp secret fehler: %v", err)
	}

	now := time.Now().UTC()
	code, err := GenerateTOTPCode(secret, now)
	if err != nil || len(code) != 6 {
		t.Fatalf("totp code fehler: %v %q", err, code)
	}

	if !VerifyTOTPCode(secret, code, now) {
		t.Fatalf("gültiger totp-code abgelehnt")
	}
	if !VerifyTOTPCode(secret, code, now.Add(29*time.Second)) {
		t.Fatalf("totp-code innerhalb ±1 fenster abgelehnt")
	}
	if VerifyTOTPCode(secret, "000000", now) && code != "000000" {
		t.Fatalf("falscher totp-code akzeptiert")
	}

	uri := BuildTOTPURI("IT-KB", "admin", secret)
	dataURI, err := BuildTOTPQRDataURI(uri)
	if err != nil || !strings.HasPrefix(dataURI, "data:image/svg+xml;base64,") {
		t.Fatalf("qr data uri ungültig: err=%v prefix=%q", err, dataURI)
	}
}
