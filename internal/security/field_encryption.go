package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"minIODB/config"

	"go.uber.org/zap"
)

var (
	// ErrInvalidKey indicates the encryption key is invalid
	ErrInvalidKey = errors.New("invalid encryption key: must be 16, 24, or 32 bytes")
	// ErrInvalidCiphertext indicates the ciphertext is invalid or corrupted
	ErrInvalidCiphertext = errors.New("invalid ciphertext: too short or corrupted")
	// ErrEncryptionFailed indicates encryption failed
	ErrEncryptionFailed = errors.New("encryption failed")
	// ErrDecryptionFailed indicates decryption failed
	ErrDecryptionFailed = errors.New("decryption failed")
)

// FieldEncryptor handles AES-GCM encryption/decryption for sensitive fields
type FieldEncryptor struct {
	key []byte
	gcm cipher.AEAD
	mu  sync.RWMutex
}

// NewFieldEncryptor creates a new FieldEncryptor with the given key
// Key must be 16, 24, or 32 bytes (AES-128, AES-192, or AES-256)
func NewFieldEncryptor(key []byte) (*FieldEncryptor, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &FieldEncryptor{
		key: key,
		gcm: gcm,
	}, nil
}

// Encrypt encrypts a plaintext string and returns base64-encoded ciphertext
func (e *FieldEncryptor) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("%w: %v", ErrEncryptionFailed, err)
	}

	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext string
func (e *FieldEncryptor) Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: base64 decode failed", ErrDecryptionFailed)
	}

	nonceSize := e.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", fmt.Errorf("%w: GCM open failed", ErrDecryptionFailed)
	}

	return string(plaintext), nil
}

// RotateKey rotates the encryption key
// Note: Data encrypted with the old key must be re-encrypted
func (e *FieldEncryptor) RotateKey(newKey []byte) error {
	if len(newKey) != 16 && len(newKey) != 24 && len(newKey) != 32 {
		return ErrInvalidKey
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	block, err := aes.NewCipher(newKey)
	if err != nil {
		return fmt.Errorf("failed to create cipher with new key: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM with new key: %w", err)
	}

	e.key = newKey
	e.gcm = gcm
	return nil
}

// FieldEncryptionManager manages field-level encryption across tables
type FieldEncryptionManager struct {
	encryptor      *FieldEncryptor
	config         *config.FieldEncryptionConfig
	encryptedField map[string]bool // set of encrypted field names for fast lookup
	logger         *zap.Logger
	mu             sync.RWMutex
}

// NewFieldEncryptionManager creates a new FieldEncryptionManager
func NewFieldEncryptionManager(cfg *config.FieldEncryptionConfig, logger *zap.Logger) (*FieldEncryptionManager, error) {
	if cfg == nil || !cfg.Enabled {
		return &FieldEncryptionManager{
			encryptor:      nil,
			config:         cfg,
			encryptedField: make(map[string]bool),
			logger:         logger,
		}, nil
	}

	// Get encryption key
	key, err := getEncryptionKey(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	encryptor, err := NewFieldEncryptor(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	// Build lookup map
	encryptedField := make(map[string]bool)
	for _, field := range cfg.EncryptedFields {
		encryptedField[field] = true
	}

	if logger != nil {
		logger.Info("Field encryption manager initialized",
			zap.Strings("encrypted_fields", cfg.EncryptedFields),
			zap.String("key_id", cfg.KeyID))
	}

	return &FieldEncryptionManager{
		encryptor:      encryptor,
		config:         cfg,
		encryptedField: encryptedField,
		logger:         logger,
	}, nil
}

// getEncryptionKey retrieves the encryption key from config
func getEncryptionKey(cfg *config.FieldEncryptionConfig) ([]byte, error) {
	// Priority: KeyEnvVar > KeyFile
	if cfg.KeyEnvVar != "" {
		keyStr := os.Getenv(cfg.KeyEnvVar)
		if keyStr == "" {
			return nil, fmt.Errorf("encryption key environment variable %s is not set", cfg.KeyEnvVar)
		}
		return []byte(keyStr), nil
	}

	if cfg.KeyFile != "" {
		key, err := os.ReadFile(cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %s: %w", cfg.KeyFile, err)
		}
		// Trim whitespace/newline
		key = []byte(strings.TrimSpace(string(key)))
		return key, nil
	}

	return nil, errors.New("no encryption key configured (set key_env_var or key_file)")
}

// IsEnabled returns whether encryption is enabled
func (m *FieldEncryptionManager) IsEnabled() bool {
	return m != nil && m.config != nil && m.config.Enabled && m.encryptor != nil
}

// IsFieldEncrypted checks if a field should be encrypted
func (m *FieldEncryptionManager) IsFieldEncrypted(fieldName string) bool {
	if m == nil || m.encryptedField == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.encryptedField[fieldName]
}

// EncryptFields encrypts specified fields in the payload map
// Returns a new map with encrypted values for configured fields
func (m *FieldEncryptionManager) EncryptFields(payload map[string]interface{}) (map[string]interface{}, error) {
	if !m.IsEnabled() {
		return payload, nil
	}

	result := make(map[string]interface{})
	for k, v := range payload {
		if m.IsFieldEncrypted(k) {
			// Convert value to string for encryption
			strValue := fmt.Sprintf("%v", v)
			encrypted, err := m.encryptor.Encrypt(strValue)
			if err != nil {
				if m.logger != nil {
					m.logger.Error("Failed to encrypt field",
						zap.String("field", k),
						zap.Error(err))
				}
				return nil, fmt.Errorf("failed to encrypt field %s: %w", k, err)
			}
			// Store encrypted value
			result[k] = encrypted
		} else {
			result[k] = v
		}
	}
	return result, nil
}

// DecryptFields decrypts specified fields in the payload map
// Returns a new map with decrypted values
func (m *FieldEncryptionManager) DecryptFields(payload map[string]interface{}) (map[string]interface{}, error) {
	if !m.IsEnabled() {
		return payload, nil
	}

	result := make(map[string]interface{})
	for k, v := range payload {
		if m.IsFieldEncrypted(k) {
			// Value should be an encrypted string
			strValue, ok := v.(string)
			if !ok {
				// Not a string, keep as-is (might not have been encrypted)
				result[k] = v
				continue
			}

			decrypted, err := m.encryptor.Decrypt(strValue)
			if err != nil {
				if m.logger != nil {
					m.logger.Warn("Failed to decrypt field, keeping original",
						zap.String("field", k),
						zap.Error(err))
				}
				// Keep original value on decryption failure
				result[k] = v
				continue
			}
			result[k] = decrypted
		} else {
			result[k] = v
		}
	}
	return result, nil
}

// GetEncryptedFields returns the list of encrypted field names
func (m *FieldEncryptionManager) GetEncryptedFields() []string {
	if m == nil || m.config == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string{}, m.config.EncryptedFields...)
}

// UpdateConfig updates the encryption configuration
func (m *FieldEncryptionManager) UpdateConfig(cfg *config.FieldEncryptionConfig) error {
	if cfg == nil {
		return errors.New("config cannot be nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Rebuild lookup map
	m.encryptedField = make(map[string]bool)
	for _, field := range cfg.EncryptedFields {
		m.encryptedField[field] = true
	}
	m.config = cfg

	return nil
}

// Global default encryption manager (can be overridden per-table)
var defaultEncryptionManager *FieldEncryptionManager
var defaultEncryptionManagerMu sync.RWMutex

// SetDefaultEncryptionManager sets the global default encryption manager
func SetDefaultEncryptionManager(m *FieldEncryptionManager) {
	defaultEncryptionManagerMu.Lock()
	defer defaultEncryptionManagerMu.Unlock()
	defaultEncryptionManager = m
}

// GetDefaultEncryptionManager returns the global default encryption manager
func GetDefaultEncryptionManager() *FieldEncryptionManager {
	defaultEncryptionManagerMu.RLock()
	defer defaultEncryptionManagerMu.RUnlock()
	return defaultEncryptionManager
}
