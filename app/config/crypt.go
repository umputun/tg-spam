package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"golang.org/x/crypto/argon2"
)

// EncryptPrefix is added to encrypted values to identify them
const EncryptPrefix = "ENC:"

// Sensitive field name constants
const (
	FieldTelegramToken  = "telegram.token"
	FieldOpenAIToken    = "openai.token"
	FieldServerAuthHash = "server.auth_hash"
	FieldServerAuthUser = "server.auth_user"
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
	argon2Time    = 1         // number of iterations
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
		return nil, fmt.Errorf("encryption key too short, minimum length is %d characters", MinKeyLength)
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

// EncryptSensitiveFields encrypts sensitive fields in a Settings object
// It can encrypt default fields or custom fields specified in sensitiveFields
func (c *Crypter) EncryptSensitiveFields(settings *Settings, sensitiveFields ...string) error {
	if settings == nil {
		return nil
	}

	// if no custom fields provided, use the defaults
	fieldsToEncrypt := sensitiveFields
	if len(fieldsToEncrypt) == 0 {
		fieldsToEncrypt = defaultSensitiveFields()
	}

	// process each sensitive field
	for _, field := range fieldsToEncrypt {
		switch field {
		case FieldTelegramToken:
			if settings.Telegram.Token != "" && !IsEncrypted(settings.Telegram.Token) {
				encrypted, err := c.Encrypt(settings.Telegram.Token)
				if err != nil {
					return fmt.Errorf("failed to encrypt Telegram token: %w", err)
				}
				settings.Telegram.Token = encrypted
			}
		case FieldOpenAIToken:
			if settings.OpenAI.Token != "" && !IsEncrypted(settings.OpenAI.Token) {
				encrypted, err := c.Encrypt(settings.OpenAI.Token)
				if err != nil {
					return fmt.Errorf("failed to encrypt OpenAI token: %w", err)
				}
				settings.OpenAI.Token = encrypted
			}
		case FieldServerAuthHash:
			if settings.Server.AuthHash != "" && !IsEncrypted(settings.Server.AuthHash) {
				encrypted, err := c.Encrypt(settings.Server.AuthHash)
				if err != nil {
					return fmt.Errorf("failed to encrypt Server auth hash: %w", err)
				}
				settings.Server.AuthHash = encrypted
			}
		case FieldServerAuthUser:
			if settings.Server.AuthUser != "" && !IsEncrypted(settings.Server.AuthUser) {
				encrypted, err := c.Encrypt(settings.Server.AuthUser)
				if err != nil {
					return fmt.Errorf("failed to encrypt Server auth user: %w", err)
				}
				settings.Server.AuthUser = encrypted
			}
		default:
			log.Printf("[WARN] unknown sensitive field: %s", field)
		}
	}

	return nil
}

// DecryptSensitiveFields decrypts sensitive fields in a Settings object
// It can decrypt default fields or custom fields specified in sensitiveFields
func (c *Crypter) DecryptSensitiveFields(settings *Settings, sensitiveFields ...string) error {
	if settings == nil {
		return nil
	}

	// if no custom fields provided, use the defaults
	fieldsToDecrypt := sensitiveFields
	if len(fieldsToDecrypt) == 0 {
		fieldsToDecrypt = defaultSensitiveFields()
	}

	// process each sensitive field
	var decryptErrs []error

	for _, field := range fieldsToDecrypt {
		switch field {
		case FieldTelegramToken:
			if IsEncrypted(settings.Telegram.Token) {
				decrypted, err := c.Decrypt(settings.Telegram.Token)
				if err != nil {
					decryptErrs = append(decryptErrs, fmt.Errorf("failed to decrypt Telegram token: %w", err))
					log.Printf("[WARN] failed to decrypt Telegram token: %v", err)
				} else {
					settings.Telegram.Token = decrypted
				}
			}
		case FieldOpenAIToken:
			if IsEncrypted(settings.OpenAI.Token) {
				decrypted, err := c.Decrypt(settings.OpenAI.Token)
				if err != nil {
					decryptErrs = append(decryptErrs, fmt.Errorf("failed to decrypt OpenAI token: %w", err))
					log.Printf("[WARN] failed to decrypt OpenAI token: %v", err)
				} else {
					settings.OpenAI.Token = decrypted
				}
			}
		case FieldServerAuthHash:
			if IsEncrypted(settings.Server.AuthHash) {
				decrypted, err := c.Decrypt(settings.Server.AuthHash)
				if err != nil {
					decryptErrs = append(decryptErrs, fmt.Errorf("failed to decrypt Server auth hash: %w", err))
					log.Printf("[WARN] failed to decrypt Server auth hash: %v", err)
				} else {
					settings.Server.AuthHash = decrypted
				}
			}
		case FieldServerAuthUser:
			if IsEncrypted(settings.Server.AuthUser) {
				decrypted, err := c.Decrypt(settings.Server.AuthUser)
				if err != nil {
					decryptErrs = append(decryptErrs, fmt.Errorf("failed to decrypt Server auth user: %w", err))
					log.Printf("[WARN] failed to decrypt Server auth user: %v", err)
				} else {
					settings.Server.AuthUser = decrypted
				}
			}
		default:
			log.Printf("[WARN] unknown sensitive field: %s", field)
		}
	}

	// return a combined error if any decryption failures occurred
	if len(decryptErrs) > 0 {
		var combinedErr error
		for _, err := range decryptErrs {
			if combinedErr == nil {
				combinedErr = err
			} else {
				combinedErr = fmt.Errorf("%v; %w", combinedErr, err)
			}
		}
		return combinedErr
	}

	return nil
}
