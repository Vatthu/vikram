package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/logger"
	"golang.org/x/crypto/pbkdf2"
)

// MasterKeyEnvVar is the environment variable where the master encryption key is expected.
const MasterKeyEnvVar = "VIKRAM_AUTH_MASTER_KEY"

// storeMu protects concurrent access to the auth store file.
var storeMu sync.Mutex

// AuthCredential represents a stored authentication credential.
// AccessToken and RefreshToken are encrypted at rest.
type AuthCredential struct {
	AccessToken  string    `json:"access_token_enc"`            // Encrypted
	RefreshToken string    `json:"refresh_token_enc,omitempty"` // Encrypted
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Provider     string    `json:"provider"`
	AuthMethod   string    `json:"auth_method"`
}

// IsExpired checks if the access token has expired.
func (c *AuthCredential) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

// NeedsRefresh checks if the access token needs to be refreshed soon.
func (c *AuthCredential) NeedsRefresh() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

// AuthStore holds all authentication credentials.
type AuthStore struct {
	Credentials map[string]*AuthCredential `json:"credentials"`
	KDFSalt     string                     `json:"kdf_salt,omitempty"` // Per-install random salt (base64)
}

// authFilePath returns the path to the auth store file.
func authFilePath() string {
	return filepath.Join(config.HomeDir(), "auth.json")
}

// loadStoreLocked loads the authentication store from disk, decrypting credentials. Requires storeMu lock.
func loadStoreLocked() (*AuthStore, error) {

	path := authFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AuthStore{Credentials: make(map[string]*AuthCredential), KDFSalt: generateSalt()}, nil
		}
		return nil, fmt.Errorf("failed to read auth store: %w", err)
	}

	var encryptedStore struct {
		Credentials map[string]*AuthCredential `json:"credentials"`
		KDFSalt     string                     `json:"kdf_salt,omitempty"`
	}
	if err := json.Unmarshal(data, &encryptedStore); err != nil {
		return nil, fmt.Errorf("failed to unmarshal encrypted auth store: %w", err)
	}

	// Use the store's per-install salt, falling back to legacy static salt
	// for stores created before the per-install salt was introduced.
	salt := encryptedStore.KDFSalt
	key, err := getMasterKeyWithSalt(salt)
	if err != nil {
		return nil, fmt.Errorf("master encryption key not found: %w", err)
	}

	decryptedStore := &AuthStore{
		Credentials: make(map[string]*AuthCredential),
		KDFSalt:     salt,
	}
	// Auto-migrate: assign a new random salt for legacy stores (will be
	// persisted on the next save).
	if decryptedStore.KDFSalt == "" {
		decryptedStore.KDFSalt = generateSalt()
	}
	for provider, encCred := range encryptedStore.Credentials {
		decryptedCred := &AuthCredential{
			AccountID:  encCred.AccountID,
			ExpiresAt:  encCred.ExpiresAt,
			Provider:   encCred.Provider,
			AuthMethod: encCred.AuthMethod,
		}

		if encCred.AccessToken != "" {
			decryptedAccessToken, decErr := decrypt(encCred.AccessToken, key)
			if decErr != nil {
				logger.ErrorC("auth", fmt.Sprintf("Failed to decrypt access token for %s: %v", provider, decErr))
				continue // Skip this credential if decryption fails
			}
			decryptedCred.AccessToken = decryptedAccessToken
		}

		if encCred.RefreshToken != "" {
			decryptedRefreshToken, decErr := decrypt(encCred.RefreshToken, key)
			if decErr != nil {
				logger.ErrorC("auth", fmt.Sprintf("Failed to decrypt refresh token for %s: %v", provider, decErr))
				continue // Skip this credential if decryption fails
			}
			decryptedCred.RefreshToken = decryptedRefreshToken
		}
		decryptedStore.Credentials[provider] = decryptedCred
	}

	return decryptedStore, nil
}

// LoadStore loads the authentication store from disk, decrypting credentials.
func LoadStore() (*AuthStore, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return loadStoreLocked()
}

// saveStoreLocked saves the authentication store to disk, encrypting credentials atomically. Requires storeMu lock.
func saveStoreLocked(store *AuthStore) error {

	// Use the store's per-install salt for key derivation.
	// If the store was migrated from legacy (no salt), one was assigned
	// during load; it gets persisted here.
	key, err := getMasterKeyWithSalt(store.KDFSalt)
	if err != nil {
		return fmt.Errorf("master encryption key not found: %w", err)
	}

	encryptedStore := &AuthStore{
		Credentials: make(map[string]*AuthCredential),
		KDFSalt:     store.KDFSalt,
	}
	for provider, cred := range store.Credentials {
		encryptedCred := &AuthCredential{
			AccountID:  cred.AccountID,
			ExpiresAt:  cred.ExpiresAt,
			Provider:   cred.Provider,
			AuthMethod: cred.AuthMethod,
		}

		if cred.AccessToken != "" {
			encryptedAccessToken, encErr := encrypt(cred.AccessToken, key)
			if encErr != nil {
				return fmt.Errorf("failed to encrypt access token for %s: %w", provider, encErr)
			}
			encryptedCred.AccessToken = encryptedAccessToken
		}

		if cred.RefreshToken != "" {
			encryptedRefreshToken, encErr := encrypt(cred.RefreshToken, key)
			if encErr != nil {
				return fmt.Errorf("failed to encrypt refresh token for %s: %w", provider, encErr)
			}
			encryptedCred.RefreshToken = encryptedRefreshToken
		}
		encryptedStore.Credentials[provider] = encryptedCred
	}

	data, err := json.MarshalIndent(encryptedStore, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth store: %w", err)
	}

	path := authFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create auth directory: %w", err)
	}

	// Atomic write using temp file + rename
	tempFile := path + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp auth file: %w", err)
	}
	if err := os.Rename(tempFile, path); err != nil {
		os.Remove(tempFile) // Clean up temp file on failure
		return fmt.Errorf("failed to rename temp auth file: %w", err)
	}
	return nil
}

// SaveStore saves the authentication store to disk, encrypting credentials atomically.
func SaveStore(store *AuthStore) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	return saveStoreLocked(store)
}

// GetCredential retrieves a decrypted credential for a provider.
func GetCredential(provider string) (*AuthCredential, error) {
	store, err := LoadStore()
	if err != nil {
		return nil, err
	}
	cred, ok := store.Credentials[provider]
	if !ok {
		return nil, nil // Not an error if credential not found
	}
	return cred, nil
}

// SetCredential sets an encrypted credential for a provider.
func SetCredential(provider string, cred *AuthCredential) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	store, err := loadStoreLocked() // Load existing (and decrypted) store
	if err != nil {
		return err
	}
	store.Credentials[provider] = cred // Replace with new decrypted credential
	return saveStoreLocked(store)      // Save (which will encrypt it)
}

// DeleteCredential deletes a credential for a provider.
func DeleteCredential(provider string) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	store, err := loadStoreLocked()
	if err != nil {
		return err
	}
	delete(store.Credentials, provider)
	return saveStoreLocked(store)
}

// DeleteAllCredentials deletes the entire auth store file.
func DeleteAllCredentials() error {
	path := authFilePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete auth file: %w", err)
	}
	return nil
}

// legacyKDFSalt is the original static salt, kept only for backward compatibility
// with auth.json files created before per-install salts were introduced.
const legacyKDFSalt = "vikram-auth-kdf-v1"

// cachedMasterPassphrase holds the passphrase after initial read, allowing us
// to clear it from the process environment to prevent leakage to child processes.
var cachedMasterPassphrase string
var masterPassphraseOnce sync.Once

// generateSalt creates a random 16-byte salt encoded as base64.
func generateSalt() string {
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		// Fallback: this should never happen with crypto/rand
		return legacyKDFSalt
	}
	return base64.StdEncoding.EncodeToString(salt)
}

// getMasterKey derives a 32-byte AES-256 key from the master passphrase using
// PBKDF2-SHA256 (100 000 iterations).  The passphrase is read from the
// VIKRAM_AUTH_MASTER_KEY environment variable and can be any non-empty string.
// Using PBKDF2 provides two guarantees over raw key material:
//   - Any length passphrase is accepted — no awkward "must be 32 bytes" constraint.
//   - An attacker who steals auth.json must run 100 000 SHA-256 rounds per guess,
//     making brute-force of weak passphrases significantly more expensive.
func getMasterKey() ([]byte, error) {
	masterPassphraseOnce.Do(func() {
		cachedMasterPassphrase = os.Getenv(MasterKeyEnvVar)
		// Security (SEC-AUTH-02): Clear the master key from the process
		// environment so it is not inherited by child processes (shell
		// commands, subagents, etc.).
		os.Unsetenv(MasterKeyEnvVar)
	})
	if cachedMasterPassphrase == "" {
		return nil, fmt.Errorf("environment variable %s is not set", MasterKeyEnvVar)
	}
	return pbkdf2.Key([]byte(cachedMasterPassphrase), []byte(legacyKDFSalt), 100_000, 32, sha256.New), nil
}

// getMasterKeyWithSalt derives the key using a per-install salt (preferred)
// or falls back to the legacy static salt for backward compatibility.
func getMasterKeyWithSalt(salt string) ([]byte, error) {
	masterPassphraseOnce.Do(func() {
		cachedMasterPassphrase = os.Getenv(MasterKeyEnvVar)
		os.Unsetenv(MasterKeyEnvVar)
	})
	if cachedMasterPassphrase == "" {
		return nil, fmt.Errorf("environment variable %s is not set", MasterKeyEnvVar)
	}
	if salt == "" {
		salt = legacyKDFSalt
	}
	return pbkdf2.Key([]byte(cachedMasterPassphrase), []byte(salt), 100_000, 32, sha256.New), nil
}

// SanitizedEnv returns a copy of the current environment with security-sensitive
// variables (master key) removed. Use this for child process Cmd.Env to prevent
// credential leakage.
func SanitizedEnv() []string {
	var clean []string
	prefix := MasterKeyEnvVar + "="
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, prefix) {
			clean = append(clean, e)
		}
	}
	return clean
}

// encrypt encrypts plaintext using AES-GCM.
func encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts ciphertext using AES-GCM.
func decrypt(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintextBytes, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}
	return string(plaintextBytes), nil
}
