// Package backup implements a bounded, authenticated local database backup
// envelope. Database dumps remain in memory; only ciphertext is written to disk.
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const MaxBytes = 256 * 1024 * 1024
const KeyBytes = 32

var header = []byte("BWB-DB-BACKUP\x00\x01")

func aead(key []byte) (cipher.AEAD, error) {
	if len(key) != KeyBytes {
		return nil, errors.New("backup key must contain exactly 32 random bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("cannot initialize backup encryption")
	}
	return cipher.NewGCM(block)
}

func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 || len(plaintext) > MaxBytes {
		return nil, errors.New("database dump is empty or exceeds the 256 MiB backup limit")
	}
	box, err := aead(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, errors.New("cannot generate backup nonce")
	}
	result := append([]byte(nil), header...)
	result = append(result, nonce...)
	return box.Seal(result, nonce, plaintext, header), nil
}

func Decrypt(key, encrypted []byte) ([]byte, error) {
	box, err := aead(key)
	if err != nil {
		return nil, err
	}
	minimum := len(header) + box.NonceSize() + box.Overhead() + 1
	if len(encrypted) < minimum || len(encrypted) > MaxBytes+minimum-1 || string(encrypted[:len(header)]) != string(header) {
		return nil, errors.New("invalid encrypted database backup")
	}
	nonce := encrypted[len(header) : len(header)+box.NonceSize()]
	plaintext, err := box.Open(nil, nonce, encrypted[len(header)+box.NonceSize():], header)
	if err != nil {
		return nil, errors.New("backup authentication failed; key or ciphertext does not match")
	}
	return plaintext, nil
}

func ReadBounded(input io.Reader, maximum int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(input, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		return nil, errors.New("backup input is unreadable or exceeds its size limit")
	}
	return data, nil
}

// PrivateDirectory refuses symbolic directories and group/world access. It
// never broadens or silently changes the permissions of an existing location.
func PrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err = os.Mkdir(path, 0700); err != nil {
			return errors.New("cannot create restricted backup directory")
		}
		info, err = os.Lstat(path)
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("backup directory must be a private regular directory with permissions 0700")
	}
	return nil
}

func privateRead(path string, maximum int64) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return nil, errors.New("backup input must be a regular file with permissions 0600 or 0400")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open restricted backup file")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Mode().Perm()&0077 != 0 {
		return nil, errors.New("backup file changed while opening")
	}
	return ReadBounded(file, maximum)
}

func ReadKey(path string) ([]byte, error) {
	key, err := privateRead(path, KeyBytes)
	if err != nil || len(key) != KeyBytes {
		return nil, errors.New("backup key must be a restricted regular file containing exactly 32 random bytes")
	}
	return key, nil
}

func writeExclusive(path string, data []byte) error {
	if err := PrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("backup output already exists or cannot be created")
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			os.Remove(path)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return errors.New("cannot write encrypted backup")
	}
	if err = file.Sync(); err != nil {
		return errors.New("cannot sync encrypted backup")
	}
	if err = file.Close(); err != nil {
		return errors.New("cannot close encrypted backup")
	}
	complete = true
	return nil
}

func GenerateKey(path string) error {
	key := make([]byte, KeyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return errors.New("cannot generate backup key")
	}
	defer Clear(key)
	return writeExclusive(path, key)
}

func EncryptFile(input io.Reader, keyPath, outputPath string) (int, error) {
	if filepath.Clean(keyPath) == filepath.Clean(outputPath) {
		return 0, errors.New("backup ciphertext and encryption key require separate files")
	}
	key, err := ReadKey(keyPath)
	if err != nil {
		return 0, err
	}
	defer Clear(key)
	plaintext, err := ReadBounded(input, MaxBytes)
	if err != nil {
		return 0, err
	}
	defer Clear(plaintext)
	encrypted, err := Encrypt(key, plaintext)
	if err != nil {
		return 0, err
	}
	if err = writeExclusive(outputPath, encrypted); err != nil {
		return 0, err
	}
	return len(encrypted), nil
}

func DecryptFile(keyPath, inputPath string, output io.Writer) error {
	key, err := ReadKey(keyPath)
	if err != nil {
		return err
	}
	defer Clear(key)
	encrypted, err := privateRead(inputPath, MaxBytes+1024)
	if err != nil {
		return err
	}
	plaintext, err := Decrypt(key, encrypted)
	if err != nil {
		return err
	}
	defer Clear(plaintext)
	if _, err = output.Write(plaintext); err != nil {
		return errors.New("cannot write decrypted backup to the restore pipe")
	}
	return nil
}

// Clear limits the lifetime of retained buffers. It is not a claim that the Go
// runtime, OS, or subprocess machinery can guarantee complete memory erasure.
func Clear(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
