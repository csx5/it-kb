package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

var (
	encryptedBackupMagic = []byte("ITKBENC1")
	ErrPassphraseNeeded  = errors.New("dieses backup ist verschlüsselt – bitte backup-passphrase angeben")
	ErrDecryptFailed     = errors.New("entschlüsselung fehlgeschlagen (falsche passphrase oder beschädigte datei)")
)

// IsEncryptedBackup erkennt anhand des Magic-Headers, ob ein Backup AES-256-GCM-verschlüsselt ist.
func IsEncryptedBackup(data []byte) bool {
	return len(data) >= len(encryptedBackupMagic) && bytes.Equal(data[:len(encryptedBackupMagic)], encryptedBackupMagic)
}

// EncryptBytes verschlüsselt beliebige Backup-Daten lokal mit Argon2id + AES-256-GCM.
// Format: "ITKBENC1" (8B) || Salt (16B) || Nonce (12B) || AES-GCM Ciphertext+Tag
func EncryptBytes(plaintext []byte, passphrase string) ([]byte, error) {
	if len(passphrase) < 8 {
		return nil, errors.New("backup-passphrase muss mindestens 8 zeichen lang sein")
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 2, 32)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, encryptedBackupMagic)

	out := make([]byte, 0, len(encryptedBackupMagic)+len(salt)+len(nonce)+len(ciphertext))
	out = append(out, encryptedBackupMagic...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// DecryptBytes entschlüsselt ein mit EncryptBytes erzeugtes Backup.
func DecryptBytes(blob []byte, passphrase string) ([]byte, error) {
	if !IsEncryptedBackup(blob) {
		return nil, ErrDecryptFailed
	}
	if passphrase == "" {
		return nil, ErrPassphraseNeeded
	}
	headerLen := len(encryptedBackupMagic) + 16 + 12
	if len(blob) <= headerLen {
		return nil, ErrDecryptFailed
	}

	salt := blob[len(encryptedBackupMagic) : len(encryptedBackupMagic)+16]
	nonce := blob[len(encryptedBackupMagic)+16 : headerLen]
	ciphertext := blob[headerLen:]

	key := argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 2, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plain, err := gcm.Open(nil, nonce, ciphertext, encryptedBackupMagic)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plain, nil
}
