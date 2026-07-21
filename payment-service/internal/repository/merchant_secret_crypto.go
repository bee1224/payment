package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const encryptedValuePrefix = "enc:"

type MerchantSecretCipher struct {
	key []byte
}

func NewMerchantSecretCipher(rawKey string) MerchantSecretCipher {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return MerchantSecretCipher{}
	}
	sum := sha256.Sum256([]byte(rawKey))
	return MerchantSecretCipher{key: sum[:]}
}

func (c MerchantSecretCipher) Enabled() bool {
	return len(c.key) == 32
}

func (c MerchantSecretCipher) Encrypt(secret string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("merchant secret cipher is not configured")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", errors.New("merchant secret is required")
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return encryptedValuePrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (c MerchantSecretCipher) Decrypt(ciphertext string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("merchant secret cipher is not configured")
	}
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", errors.New("merchant secret ciphertext is required")
	}
	rawValue := strings.TrimPrefix(ciphertext, encryptedValuePrefix)
	raw, err := base64.StdEncoding.DecodeString(rawValue)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("merchant secret ciphertext is too short")
	}
	nonce := raw[:gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (c MerchantSecretCipher) EncryptIfConfigured(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !c.Enabled() {
		return value, nil
	}
	return c.Encrypt(value)
}

func (c MerchantSecretCipher) DecryptIfEncrypted(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	if !c.Enabled() {
		return "", errors.New("merchant secret cipher is not configured")
	}
	return c.Decrypt(value)
}
