// Package secretvault stores bounded supplier credentials or session documents
// in a local authenticated encryption envelope. It neither contacts suppliers
// nor interprets the stored content. References and metadata are safe to persist
// in connection configuration; plaintext must never be placed in jobs or logs.
package secretvault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	KeyBytes   = 32
	MaxBytes   = 1024 * 1024
	markerName = ".vault-key-check"
)

var (
	header               = []byte("BWB-SUPPLIER-SECRET\x00\x01")
	markerValue          = []byte("supplier-vault-key-check-v1")
	ErrNotFound          = errors.New("supplier secret is not configured")
	ErrInvalidConnection = errors.New("supplier secret requires a connection UUID")
	ErrUnavailable       = errors.New("supplier secret vault is unavailable or authentication failed")
	ErrUnsafeFile        = errors.New("supplier secret files require regular files with permissions 0600 or 0400 and no symbolic links")
	ErrInputSize         = errors.New("supplier secret must contain between 1 byte and 1 MiB")
)

type Metadata struct {
	ConnectionID string    `json:"connection_id"`
	SecretRef    string    `json:"secret_ref"`
	Bytes        int       `json:"bytes"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Vault pins a restricted directory for its lifetime. A connection's reference
// is an opaque handle, not a credential. Close releases the directory handle.
// Get callers own their returned buffer and must clear it when finished.
type Vault struct {
	mu   sync.Mutex
	root *os.Root
	box  cipher.AEAD
}

// Reference validates and canonicalizes the connection UUID. A reference grants
// no access without the vault, its key, and the matching connection binding.
func Reference(connectionID string) (string, error) {
	id, err := canonicalID(connectionID)
	if err != nil {
		return "", err
	}
	return "vault:v1:" + id, nil
}

func canonicalID(value string) (string, error) {
	if len(value) != 36 {
		return "", ErrInvalidConnection
	}
	for i := range len(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if value[i] != '-' {
				return "", ErrInvalidConnection
			}
			continue
		}
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return "", ErrInvalidConnection
		}
	}
	if value == "00000000-0000-0000-0000-000000000000" {
		return "", ErrInvalidConnection
	}
	return strings.ToLower(value), nil
}

// Open creates the final vault directory with mode 0700 if absent. Existing
// unsafe locations are refused, never repaired. Key files contain exactly 32
// raw bytes, with no hex decoding, newline trimming, or default key fallback.
func Open(vaultDir, keyFile string) (*Vault, error) {
	key, err := ReadValueFile(keyFile)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer Clear(key)
	if len(key) != KeyBytes {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnavailable
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	root, err := privateDirectory(vaultDir, true)
	if err != nil {
		return nil, err
	}
	v := &Vault{root: root, box: box}
	if err = v.checkKey(); err != nil {
		v.Close()
		return nil, err
	}
	return v, nil
}

func (v *Vault) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.root == nil {
		return nil
	}
	err := v.root.Close()
	v.root, v.box = nil, nil
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (v *Vault) ready() error {
	if v.root == nil || v.box == nil {
		return ErrUnavailable
	}
	info, err := v.root.Stat(".")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return ErrUnavailable
	}
	return nil
}

func (v *Vault) checkKey() error {
	encrypted, _, err := privateRead(v.root, markerName, int64(MaxBytes+128))
	if errors.Is(err, ErrNotFound) {
		// A missing marker on a populated directory is damage, not a new vault.
		// Do not let a different key silently claim an existing vault.
		dir, openErr := v.root.Open(".")
		if openErr != nil {
			return ErrUnavailable
		}
		entries, readErr := dir.ReadDir(1)
		dir.Close()
		if len(entries) != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
			return ErrUnavailable
		}
		encrypted, err = v.encrypt("vault-key-check", markerValue)
		if err != nil {
			return err
		}
		if err = writeExclusive(v.root, markerName, encrypted); err != nil {
			// A second opener may have initialized the same directory.
			encrypted, _, err = privateRead(v.root, markerName, int64(MaxBytes+128))
		}
	}
	if err != nil {
		return ErrUnavailable
	}
	value, err := v.decrypt("vault-key-check", encrypted)
	defer Clear(value)
	if err != nil || !bytes.Equal(value, markerValue) {
		return ErrUnavailable
	}
	return nil
}

func (v *Vault) encrypt(binding string, value []byte) ([]byte, error) {
	nonce := make([]byte, v.box.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, ErrUnavailable
	}
	encrypted := append([]byte(nil), header...)
	encrypted = append(encrypted, nonce...)
	return v.box.Seal(encrypted, nonce, value, append(append([]byte(nil), header...), []byte(binding)...)), nil
}

func (v *Vault) decrypt(binding string, encrypted []byte) ([]byte, error) {
	minimum := len(header) + v.box.NonceSize() + v.box.Overhead() + 1
	if len(encrypted) < minimum || len(encrypted) > MaxBytes+minimum-1 || !bytes.Equal(encrypted[:len(header)], header) {
		return nil, ErrUnavailable
	}
	nonce := encrypted[len(header) : len(header)+v.box.NonceSize()]
	value, err := v.box.Open(nil, nonce, encrypted[len(header)+v.box.NonceSize():], append(append([]byte(nil), header...), []byte(binding)...))
	if err != nil {
		return nil, ErrUnavailable
	}
	return value, nil
}

func (v *Vault) read(id string) ([]byte, os.FileInfo, error) {
	if err := v.ready(); err != nil {
		return nil, nil, err
	}
	encrypted, info, err := privateRead(v.root, id+".vault", int64(MaxBytes+128))
	if err != nil {
		return nil, nil, err
	}
	value, err := v.decrypt(id, encrypted)
	if err != nil {
		return nil, nil, err
	}
	return value, info, nil
}

func metadata(id string, value []byte, info os.FileInfo) Metadata {
	return Metadata{ConnectionID: id, SecretRef: "vault:v1:" + id, Bytes: len(value), UpdatedAt: info.ModTime().UTC()}
}

// Put atomically replaces one connection's encrypted value after authenticating
// any existing entry. It preserves the old value if preparing the replacement
// fails. Concurrent operators must coordinate; the last completed write wins.
func (v *Vault) Put(connectionID string, value []byte) (Metadata, error) {
	id, err := canonicalID(connectionID)
	if err != nil {
		return Metadata{}, err
	}
	if len(value) == 0 || len(value) > MaxBytes {
		return Metadata{}, ErrInputSize
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err = v.ready(); err != nil {
		return Metadata{}, err
	}
	existing, _, err := v.read(id)
	Clear(existing)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Metadata{}, err
	}
	encrypted, err := v.encrypt(id, value)
	if err != nil {
		return Metadata{}, err
	}
	token := make([]byte, 16)
	if _, err = io.ReadFull(rand.Reader, token); err != nil {
		return Metadata{}, ErrUnavailable
	}
	tempName := ".pending-" + hex.EncodeToString(token)
	if err = writeExclusive(v.root, tempName, encrypted); err != nil {
		return Metadata{}, err
	}
	defer v.root.Remove(tempName)
	// Refuse an unsafe destination even when a different operator changed it
	// while the replacement was being prepared.
	if info, statErr := v.root.Lstat(id + ".vault"); statErr == nil {
		if !privateFile(info) {
			return Metadata{}, ErrUnsafeFile
		}
	} else if !os.IsNotExist(statErr) {
		return Metadata{}, ErrUnavailable
	}
	if err = v.root.Rename(tempName, id+".vault"); err != nil {
		return Metadata{}, ErrUnavailable
	}
	if err = syncRoot(v.root); err != nil {
		return Metadata{}, err
	}
	info, err := v.root.Lstat(id + ".vault")
	if err != nil || !privateFile(info) {
		return Metadata{}, ErrUnavailable
	}
	return metadata(id, value, info), nil
}

func (v *Vault) Get(connectionID string) ([]byte, error) {
	id, err := canonicalID(connectionID)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	value, _, err := v.read(id)
	return value, err
}

// Status authenticates the stored entry and returns no plaintext.
func (v *Vault) Status(connectionID string) (Metadata, error) {
	id, err := canonicalID(connectionID)
	if err != nil {
		return Metadata{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	value, info, err := v.read(id)
	defer Clear(value)
	if err != nil {
		return Metadata{}, err
	}
	return metadata(id, value, info), nil
}

// Delete revokes one stored connection value. Already decrypted buffers or
// supplier-side sessions require their own revocation; no supplier is contacted.
func (v *Vault) Delete(connectionID string) error {
	id, err := canonicalID(connectionID)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	value, _, err := v.read(id)
	Clear(value)
	if err != nil {
		return err
	}
	if err = v.root.Remove(id + ".vault"); err != nil {
		return ErrUnavailable
	}
	return syncRoot(v.root)
}

func privateFile(info os.FileInfo) bool {
	return info.Mode().IsRegular() && (info.Mode().Perm() == 0600 || info.Mode().Perm() == 0400)
}

// noLinks rejects symbolic path components before opening a pinned directory.
func noLinks(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil || path == "" {
		return "", ErrUnavailable
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", ErrUnavailable
		}
	}
	return abs, nil
}

func privateDirectory(path string, create bool) (*os.Root, error) {
	if path == "" {
		return nil, ErrUnavailable
	}
	if create {
		// Only the requested final directory is created; its parent must exist.
		if _, err := noLinks(filepath.Dir(path)); err != nil {
			return nil, err
		}
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return nil, ErrUnavailable
		}
	}
	abs, err := noLinks(path)
	if err != nil {
		return nil, err
	}
	before, err := os.Lstat(abs)
	if err != nil || !before.IsDir() || (create && before.Mode().Perm() != 0700) {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, ErrUnavailable
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) || !after.IsDir() || (create && after.Mode().Perm() != 0700) {
		root.Close()
		return nil, ErrUnavailable
	}
	return root, nil
}

func privateRead(root *os.Root, name string, maximum int64) ([]byte, os.FileInfo, error) {
	before, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil, ErrNotFound
	}
	if err != nil || !privateFile(before) {
		return nil, nil, ErrUnsafeFile
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, ErrUnavailable
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !privateFile(after) {
		return nil, nil, ErrUnsafeFile
	}
	value, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(value)) > maximum {
		Clear(value)
		return nil, nil, ErrUnavailable
	}
	return value, after, nil
}

// ReadValueFile accepts a bounded, restricted, non-symlink local file. Values
// are preserved byte for byte, including whitespace and line endings.
func ReadValueFile(path string) ([]byte, error) {
	if _, err := noLinks(path); err != nil {
		return nil, ErrUnsafeFile
	}
	root, err := privateDirectory(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	value, _, err := privateRead(root, filepath.Base(path), MaxBytes)
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		return nil, ErrInputSize
	}
	return value, nil
}

func writeExclusive(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrUnavailable
	}
	complete := false
	defer func() {
		file.Close()
		if !complete {
			root.Remove(name)
		}
	}()
	if _, err = file.Write(data); err != nil {
		return ErrUnavailable
	}
	if err = file.Sync(); err != nil {
		return ErrUnavailable
	}
	if err = file.Close(); err != nil {
		return ErrUnavailable
	}
	if err = syncRoot(root); err != nil {
		return err
	}
	complete = true
	return nil
}

func syncRoot(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return ErrUnavailable
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return ErrUnavailable
	}
	return nil
}

// GenerateKey writes a fresh raw 32-byte key using exclusive creation. It does
// not overwrite keys, silently fix permissions, or generate default credentials.
func GenerateKey(path string) error {
	root, err := privateDirectory(filepath.Dir(path), true)
	if err != nil {
		return err
	}
	defer root.Close()
	key := make([]byte, KeyBytes)
	defer Clear(key)
	if _, err = io.ReadFull(rand.Reader, key); err != nil {
		return ErrUnavailable
	}
	return writeExclusive(root, filepath.Base(path), key)
}

// Clear is best-effort buffer lifetime reduction, not a guarantee of erasure by
// the Go runtime or operating system. Cipher internals can retain expanded keys.
func Clear(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
