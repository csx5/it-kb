package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

const streamChunkSize = 64 * 1024

var (
	encryptedBackupMagic   = []byte("ITKBENC1")
	encryptedBackupV2Magic = []byte("ITKBENC2")
	ErrPassphraseNeeded    = errors.New("dieses backup ist verschlüsselt – bitte backup-passphrase angeben")
	ErrDecryptFailed       = errors.New("entschlüsselung fehlgeschlagen (falsche passphrase oder beschädigte datei)")
)

// IsEncryptedBackup erkennt anhand des Magic-Headers (ITKBENC1 oder ITKBENC2),
// ob ein Backup AES-256-GCM-verschlüsselt ist.
func IsEncryptedBackup(data []byte) bool {
	return len(data) >= len(encryptedBackupMagic) &&
		(bytes.Equal(data[:len(encryptedBackupMagic)], encryptedBackupMagic) ||
			bytes.Equal(data[:len(encryptedBackupV2Magic)], encryptedBackupV2Magic))
}

// EncryptBytes verschlüsselt ein Byte-Slice lokal über EncryptStream (ITKBENC2).
func EncryptBytes(plaintext []byte, passphrase string) ([]byte, error) {
	var out bytes.Buffer
	if err := EncryptStream(bytes.NewReader(plaintext), &out, passphrase); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// DecryptBytes entschlüsselt sowohl ITKBENC1- als auch ITKBENC2-Backups im Arbeitsspeicher.
func DecryptBytes(blob []byte, passphrase string) ([]byte, error) {
	if !IsEncryptedBackup(blob) {
		return nil, ErrDecryptFailed
	}
	if passphrase == "" {
		return nil, ErrPassphraseNeeded
	}
	if len(blob) >= len(encryptedBackupV2Magic) && bytes.Equal(blob[:len(encryptedBackupV2Magic)], encryptedBackupV2Magic) {
		var out bytes.Buffer
		if err := DecryptStream(bytes.NewReader(blob), &out, passphrase); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}
	return decryptLegacyV1(blob, passphrase)
}

func decryptLegacyV1(blob []byte, passphrase string) ([]byte, error) {
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

// EncryptStream verschlüsselt einen beliebigen Eingabestrom in 64-KiB-Chunks mit Argon2id + AES-256-GCM (ITKBENC2).
// Header: "ITKBENC2" (8B) || Salt (16B) || NonceBase (8B)
// Pro Chunk: Flags (1B: 0=weiter, 1=letzter Chunk) || CipherLen (4B BigEndian) || AES-GCM-Ciphertext+Tag
func EncryptStream(src io.Reader, dst io.Writer, passphrase string) error {
	if len(passphrase) < 8 {
		return errors.New("backup-passphrase muss mindestens 8 zeichen lang sein")
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	nonceBase := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, nonceBase); err != nil {
		return err
	}

	key := argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 2, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	var header [32]byte
	copy(header[0:8], encryptedBackupV2Magic)
	copy(header[8:24], salt)
	copy(header[24:32], nonceBase)
	if _, err := dst.Write(header[:]); err != nil {
		return err
	}

	currBuf := make([]byte, streamChunkSize)
	nextBuf := make([]byte, streamChunkSize)
	currN, currErr := io.ReadFull(src, currBuf)
	if currErr != nil && !errors.Is(currErr, io.EOF) && !errors.Is(currErr, io.ErrUnexpectedEOF) {
		return currErr
	}

	var chunkIdx uint32
	writeChunk := func(plain []byte, isFinal bool) error {
		var nonce [12]byte
		copy(nonce[0:8], nonceBase)
		binary.BigEndian.PutUint32(nonce[8:12], chunkIdx)

		var flagByte byte
		if isFinal {
			flagByte = 1
		}
		var aad [13]byte
		copy(aad[0:8], encryptedBackupV2Magic)
		binary.BigEndian.PutUint32(aad[8:12], chunkIdx)
		aad[12] = flagByte

		ct := gcm.Seal(nil, nonce[:], plain, aad[:])
		var frameHdr [5]byte
		frameHdr[0] = flagByte
		binary.BigEndian.PutUint32(frameHdr[1:5], uint32(len(ct)))
		if _, err := dst.Write(frameHdr[:]); err != nil {
			return err
		}
		_, err := dst.Write(ct)
		return err
	}

	if currN == 0 && errors.Is(currErr, io.EOF) {
		return writeChunk(nil, true)
	}

	for {
		if currErr != nil { // ErrUnexpectedEOF -> letzter Chunk
			return writeChunk(currBuf[:currN], true)
		}

		nextN, nextErr := io.ReadFull(src, nextBuf)
		if nextErr != nil && !errors.Is(nextErr, io.EOF) && !errors.Is(nextErr, io.ErrUnexpectedEOF) {
			return nextErr
		}
		if nextN == 0 && errors.Is(nextErr, io.EOF) {
			return writeChunk(currBuf[:currN], true)
		}

		if err := writeChunk(currBuf[:currN], false); err != nil {
			return err
		}
		chunkIdx++
		currBuf, nextBuf = nextBuf, currBuf
		currN, currErr = nextN, nextErr
	}
}

// DecryptStream entschlüsselt einen Backup-Strom (ITKBENC2 oder Legacy-ITKBENC1) speicherschonend.
func DecryptStream(src io.Reader, dst io.Writer, passphrase string) error {
	var magic [8]byte
	if _, err := io.ReadFull(src, magic[:]); err != nil {
		return ErrDecryptFailed
	}
	if !IsEncryptedBackup(magic[:]) {
		return ErrDecryptFailed
	}
	if passphrase == "" {
		return ErrPassphraseNeeded
	}

	if bytes.Equal(magic[:], encryptedBackupMagic) {
		rest, err := io.ReadAll(io.LimitReader(src, (512<<20)+1))
		if err != nil || len(rest) > 512<<20 {
			return ErrDecryptFailed
		}
		full := make([]byte, 8+len(rest))
		copy(full, magic[:])
		copy(full[8:], rest)
		plain, err := decryptLegacyV1(full, passphrase)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, bytes.NewReader(plain))
		return err
	}

	var restHdr [24]byte // 16B Salt + 8B NonceBase
	if _, err := io.ReadFull(src, restHdr[:]); err != nil {
		return ErrDecryptFailed
	}
	salt := restHdr[0:16]
	nonceBase := restHdr[16:24]

	key := argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 2, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	maxCipherChunk := uint32(streamChunkSize + gcm.Overhead())
	buf := make([]byte, maxCipherChunk)
	var chunkIdx uint32

	for {
		var frameHdr [5]byte
		if _, err := io.ReadFull(src, frameHdr[:]); err != nil {
			return ErrDecryptFailed
		}
		flagByte := frameHdr[0]
		if flagByte != 0 && flagByte != 1 {
			return ErrDecryptFailed
		}
		cipherLen := binary.BigEndian.Uint32(frameHdr[1:5])
		if cipherLen < uint32(gcm.Overhead()) || cipherLen > maxCipherChunk {
			return ErrDecryptFailed
		}

		ct := buf[:cipherLen]
		if _, err := io.ReadFull(src, ct); err != nil {
			return ErrDecryptFailed
		}

		var nonce [12]byte
		copy(nonce[0:8], nonceBase)
		binary.BigEndian.PutUint32(nonce[8:12], chunkIdx)

		var aad [13]byte
		copy(aad[0:8], encryptedBackupV2Magic)
		binary.BigEndian.PutUint32(aad[8:12], chunkIdx)
		aad[12] = flagByte

		plain, err := gcm.Open(nil, nonce[:], ct, aad[:])
		if err != nil {
			return ErrDecryptFailed
		}
		if len(plain) > 0 {
			if _, err := dst.Write(plain); err != nil {
				return err
			}
		}
		if flagByte == 1 {
			// Sicherstellen, dass keine manipulierten Zusatzdaten nach dem End-Chunk folgen
			var trailing [1]byte
			n, err := src.Read(trailing[:])
			if n > 0 || !errors.Is(err, io.EOF) {
				return ErrDecryptFailed
			}
			return nil
		}
		chunkIdx++
	}
}
