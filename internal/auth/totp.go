package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const totpPeriodSeconds = 30

// GenerateTOTPSecret erzeugt ein 160-Bit Base32-Secret (ohne Padding) nach RFC 6238.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// BuildTOTPURI erstellt eine otpauth://-URI für gängige lokale Authenticator-Apps.
func BuildTOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	return fmt.Sprintf("otpauth://totp/%s?%s", label, q.Encode())
}

// GenerateTOTPCode berechnet den 6-stelligen TOTP-Code für einen bestimmten Zeitpunkt.
func GenerateTOTPCode(secret string, at time.Time) (string, error) {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(clean)
	if err != nil {
		return "", fmt.Errorf("ungültiges totp-secret: %w", err)
	}

	counter := uint64(at.UTC().Unix() / totpPeriodSeconds)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	codeInt := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", codeInt), nil
}

// VerifyTOTPCode prüft einen 6-stelligen Code mit ±1 Zeitschritt (30s) Toleranz.
func VerifyTOTPCode(secret, code string, now time.Time) bool {
	ok, _ := VerifyTOTPCodeStep(secret, code, now)
	return ok
}

// VerifyTOTPCodeStep prüft den Code und liefert zusätzlich den gematchten 30s-Zeitschritt für Replay-Schutz.
func VerifyTOTPCodeStep(secret, code string, now time.Time) (bool, int64) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return false, 0
	}
	baseStep := now.UTC().Unix() / totpPeriodSeconds
	for _, offset := range []int64{-1, 0, 1} {
		step := baseStep + offset
		expected, err := GenerateTOTPCode(secret, time.Unix(step*totpPeriodSeconds, 0).UTC())
		if err != nil {
			return false, 0
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return true, step
		}
	}
	return false, 0
}
