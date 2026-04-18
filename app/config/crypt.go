package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

// EncryptPrefix is added to encrypted values to identify them
const EncryptPrefix = "ENC:"

// Sensitive field name constants
const (
	FieldTelegramToken  = "telegram.token"
	FieldOpenAIToken    = "openai.token"
	FieldGeminiToken    = "gemini.token"
	FieldServerAuthHash = "server.auth_hash"
)

// MinKeyLength defines the minimum acceptable length for an encryption key
const MinKeyLength = 20

// Crypter handles encryption and decryption of sensitive fields
type Crypter struct {
	key []byte
	gid string // instance ID for salt generation
}

// Argon2 parameters for key derivation
const (
	argon2Time    = 3         // number of iterations (OWASP recommends at least 3)
	argon2Memory  = 64 * 1024 // memory usage in KiB (64MB)
	argon2Threads = 4         // number of threads
	argon2KeyLen  = 32        // output key length (for AES-256)
)

// NewCrypter creates a new encryption manager with the given key and instance ID
func NewCrypter(masterKey, instanceID string) (*Crypter, error) {
	if masterKey == "" {
		return nil, errors.New("empty master key")
	}

	if len(masterKey) < MinKeyLength {
		return nil, fmt.Errorf("encryption key too short, minimum length is %d characters (use a high-entropy random value)",
			MinKeyLength)
	}

	if instanceID == "" {
		return nil, errors.New("empty instance ID")
	}

	// create a salt based on instance ID for consistency across restarts
	// this ensures different instances use different keys even with the same master key
	salt := []byte("tg-spam-config-encryption-salt-" + instanceID)

	// derive a proper cryptographic key using Argon2id
	key := argon2.IDKey(
		[]byte(masterKey),
		salt,
		argon2Time,
		argon2Memory,
		argon2Threads,
		argon2KeyLen,
	)

	return &Crypter{key: key, gid: instanceID}, nil
}

// Encrypt encrypts a string value
func (c *Crypter) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	// create a new AES cipher block
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	// create the GCM mode with the default nonce size
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// create a nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// encrypt and append the nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// encode to base64 and add prefix
	return EncryptPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a string value
func (c *Crypter) Decrypt(ciphertext string) (string, error) {
	// if not encrypted or empty, return as is
	if ciphertext == "" || !strings.HasPrefix(ciphertext, EncryptPrefix) {
		return ciphertext, nil
	}

	// remove prefix and decode from base64
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, EncryptPrefix))
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 data: %w", err)
	}

	// create a new AES cipher block
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher for decryption: %w", err)
	}

	// create the GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM for decryption: %w", err)
	}

	// the nonce is at the beginning of the ciphertext
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt data: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted checks if a value is encrypted
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, EncryptPrefix)
}

// sensitiveFieldAccessors maps sensitive field names to accessors that return
// a pointer to the backing string on a Settings instance plus a human-readable
// label used in error messages. Adding a new sensitive field is a single-place
// change here; defaultSensitiveFields derives its list from these keys.
var sensitiveFieldAccessors = map[string]struct {
	label string
	get   func(*Settings) *string
}{
	FieldTelegramToken:  {"Telegram token", func(s *Settings) *string { return &s.Telegram.Token }},
	FieldOpenAIToken:    {"OpenAI token", func(s *Settings) *string { return &s.OpenAI.Token }},
	FieldGeminiToken:    {"Gemini token", func(s *Settings) *string { return &s.Gemini.Token }},
	FieldServerAuthHash: {"Server auth hash", func(s *Settings) *string { return &s.Server.AuthHash }},
}

// EncryptSensitiveFields encrypts sensitive fields in a Settings object
// It can encrypt default fields or custom fields specified in sensitiveFields.
// Returns an error on the first encryption failure or unknown field.
func (c *Crypter) EncryptSensitiveFields(settings *Settings, sensitiveFields ...string) error {
	if settings == nil {
		return nil
	}

	fieldsToEncrypt := sensitiveFields
	if len(fieldsToEncrypt) == 0 {
		fieldsToEncrypt = defaultSensitiveFields()
	}

	for _, field := range fieldsToEncrypt {
		accessor, ok := sensitiveFieldAccessors[field]
		if !ok {
			return fmt.Errorf("unknown sensitive field: %s", field)
		}
		target := accessor.get(settings)
		if *target == "" || IsEncrypted(*target) {
			continue
		}
		encrypted, err := c.Encrypt(*target)
		if err != nil {
			return fmt.Errorf("failed to encrypt %s: %w", accessor.label, err)
		}
		*target = encrypted
	}

	return nil
}

// DecryptSensitiveFields decrypts sensitive fields in a Settings object.
// It can decrypt default fields or custom fields specified in sensitiveFields.
// Returns an error on the first decryption failure or unknown field; symmetric
// with EncryptSensitiveFields.
func (c *Crypter) DecryptSensitiveFields(settings *Settings, sensitiveFields ...string) error {
	if settings == nil {
		return nil
	}

	fieldsToDecrypt := sensitiveFields
	if len(fieldsToDecrypt) == 0 {
		fieldsToDecrypt = defaultSensitiveFields()
	}

	for _, field := range fieldsToDecrypt {
		accessor, ok := sensitiveFieldAccessors[field]
		if !ok {
			return fmt.Errorf("unknown sensitive field: %s", field)
		}
		target := accessor.get(settings)
		if !IsEncrypted(*target) {
			continue
		}
		decrypted, err := c.Decrypt(*target)
		if err != nil {
			return fmt.Errorf("failed to decrypt %s: %w", accessor.label, err)
		}
		*target = decrypted
	}

	return nil
}
