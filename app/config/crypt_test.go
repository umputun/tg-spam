package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrypter_EncryptDecrypt(t *testing.T) {
	crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "normal string",
			input:    "test-value",
			expected: "test-value",
		},
		{
			name:     "with special chars",
			input:    "test@#$!*&^%value",
			expected: "test@#$!*&^%value",
		},
		{
			name:     "long string",
			input:    "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nullam euismod, nisl eget ultricies ultrices, nunc nisl ultricies nunc.",
			expected: "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nullam euismod, nisl eget ultricies ultrices, nunc nisl ultricies nunc.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// encrypt
			encrypted, err := crypter.Encrypt(tc.input)
			require.NoError(t, err)

			// skip empty string test
			if tc.input != "" {
				// verify encrypted value has prefix and is different from input
				assert.True(t, IsEncrypted(encrypted))
				assert.NotEqual(t, tc.input, encrypted)
			} else {
				assert.Equal(t, tc.input, encrypted)
			}

			// decrypt
			decrypted, err := crypter.Decrypt(encrypted)
			require.NoError(t, err)

			// verify decrypted value matches the original
			assert.Equal(t, tc.expected, decrypted)
		})
	}
}

func TestCrypter_IsEncrypted(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"", false},
		{"test", false},
		{EncryptPrefix, true},
		{EncryptPrefix + "data", true},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.expected, IsEncrypted(tc.value))
	}
}

func TestCrypter_EncryptDecryptSensitiveFields(t *testing.T) {
	crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
	require.NoError(t, err)

	// create a settings object with sensitive data
	settings := &Settings{
		Telegram: TelegramSettings{
			Token: "telegram-token-secret",
			Group: "public-group-name",
		},
		OpenAI: OpenAISettings{
			Token:  "openai-token-secret",
			Model:  "gpt-4",
			Prompt: "public-prompt",
		},
		Server: ServerSettings{
			AuthHash: "server-auth-hash-secret",
			AuthUser: "public-username",
		},
	}

	// encrypt sensitive fields
	err = crypter.EncryptSensitiveFields(settings)
	require.NoError(t, err)

	// verify fields are encrypted
	assert.True(t, IsEncrypted(settings.Telegram.Token))
	assert.True(t, IsEncrypted(settings.OpenAI.Token))
	assert.True(t, IsEncrypted(settings.Server.AuthHash))

	// verify non-sensitive fields are not encrypted
	assert.Equal(t, "public-group-name", settings.Telegram.Group)
	assert.Equal(t, "gpt-4", settings.OpenAI.Model)
	assert.Equal(t, "public-prompt", settings.OpenAI.Prompt)
	assert.Equal(t, "public-username", settings.Server.AuthUser)

	// create a new crypter with the same key
	decrypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
	require.NoError(t, err)

	// decrypt the fields
	err = decrypter.DecryptSensitiveFields(settings)
	require.NoError(t, err)

	// verify original values are restored
	assert.Equal(t, "telegram-token-secret", settings.Telegram.Token)
	assert.Equal(t, "openai-token-secret", settings.OpenAI.Token)
	assert.Equal(t, "server-auth-hash-secret", settings.Server.AuthHash)

	// verify non-sensitive fields are unchanged
	assert.Equal(t, "public-group-name", settings.Telegram.Group)
	assert.Equal(t, "gpt-4", settings.OpenAI.Model)
	assert.Equal(t, "public-prompt", settings.OpenAI.Prompt)
	assert.Equal(t, "public-username", settings.Server.AuthUser)
}

func TestCrypter_EncryptWithInvalidKey(t *testing.T) {
	// test with empty key
	_, err := NewCrypter("", "test-instance")
	assert.Error(t, err)
	
	// test with empty instance ID
	_, err = NewCrypter("test-master-key-with-sufficient-length", "")
	assert.Error(t, err)
	
	// test with key too short
	_, err = NewCrypter("short", "test-instance")
	assert.Error(t, err)
}

func TestCrypter_DecryptInvalidData(t *testing.T) {
	crypter, err := NewCrypter("test-master-key-20-chars", "test-instance")
	require.NoError(t, err)

	// test with invalid base64
	_, err = crypter.Decrypt(EncryptPrefix + "invalid-base64")
	assert.Error(t, err)

	// test with valid base64 but invalid ciphertext
	_, err = crypter.Decrypt(EncryptPrefix + "aW52YWxpZC1jaXBoZXJ0ZXh0") // "invalid-ciphertext" in base64
	assert.Error(t, err)
}

func TestCrypter_DifferentKeys(t *testing.T) {
	// create two crypters with different keys
	crypter1, err := NewCrypter("test-master-key-1-20chars", "test-instance")
	require.NoError(t, err)

	crypter2, err := NewCrypter("test-master-key-2-20chars", "test-instance")
	require.NoError(t, err)

	// encrypt with first key
	original := "sensitive-data"
	encrypted, err := crypter1.Encrypt(original)
	require.NoError(t, err)

	// try to decrypt with second key (should fail)
	_, err = crypter2.Decrypt(encrypted)
	assert.Error(t, err)

	// decrypt with correct key (should succeed)
	decrypted, err := crypter1.Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, original, decrypted)
}
