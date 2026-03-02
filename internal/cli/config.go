package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	keysDirPerm  = 0o700
	keysFilePerm = 0o600
)

// KeyStore manages per-user API key files under ~/.config/defi/keys/.
type KeyStore struct {
	dir string
}

// NewKeyStore returns a KeyStore backed by ~/.config/defi/keys/.
func NewKeyStore() *KeyStore {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".config", "defi", "keys")
	_ = os.MkdirAll(dir, keysDirPerm)
	return &KeyStore{dir: dir}
}

// Path returns the file path for the given email's key.
func (ks *KeyStore) Path(email string) string {
	return filepath.Join(ks.dir, email)
}

// StoreKey writes a private key to keys/{email} with mode 0600.
func (ks *KeyStore) StoreKey(email, privateKey string) error {
	if err := os.WriteFile(filepath.Join(ks.dir, email), []byte(privateKey), keysFilePerm); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}
	return nil
}

// LoadKey reads the private key for the given email.
func (ks *KeyStore) LoadKey(email string) (string, error) {
	data, err := os.ReadFile(filepath.Join(ks.dir, email))
	if err != nil {
		return "", fmt.Errorf("read key file: %w", err)
	}
	return string(data), nil
}
