package newebpay

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

func EncryptDepositTradeInfo(plainText, hashKey, hashIV string) (string, error) {
	block, err := aes.NewCipher([]byte(hashKey))
	if err != nil {
		return "", err
	}

	padded := pkcs7Pad([]byte(plainText), block.BlockSize())
	cipherText := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, []byte(hashIV))
	mode.CryptBlocks(cipherText, padded)
	return hex.EncodeToString(cipherText), nil
}

func DecryptDepositTradeInfo(cipherText, hashKey, hashIV string) (string, error) {
	plainText, _, err := decryptTradeInfo(cipherText, hashKey, hashIV)
	return plainText, err
}

func decryptTradeInfo(cipherText, hashKey, hashIV string) (string, string, error) {
	raw, err := hex.DecodeString(cipherText)
	if err != nil {
		return "", "", err
	}

	block, err := aes.NewCipher([]byte(hashKey))
	if err != nil {
		return "", "", err
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return "", "", errors.New("invalid trade info block size")
	}

	plain := make([]byte, len(raw))
	mode := cipher.NewCBCDecrypter(block, []byte(hashIV))
	mode.CryptBlocks(plain, raw)

	unpadded, err := pkcs7Unpad(plain, block.BlockSize())
	if err == nil {
		return string(unpadded), "pkcs7", nil
	}

	// NewebPay may return legacy CBC payloads with zero padding. TradeSha is
	// verified before this compatibility path, and the plaintext must still be
	// a recognizable JSON or query-string notification.
	zeroUnpadded := bytes.TrimRight(plain, "\x00")
	if len(zeroUnpadded) < len(plain) && isTradeInfoPlaintext(zeroUnpadded) {
		return string(zeroUnpadded), "zero", nil
	}
	if isTradeInfoPlaintext(plain) {
		return string(plain), "none", nil
	}

	return "", "", fmt.Errorf("%w (%s)", err, tradeInfoDiagnostic(plain))
}

func tradeInfoDiagnostic(data []byte) string {
	lastByte := -1
	if len(data) > 0 {
		lastByte = int(data[len(data)-1])
	}
	printable := 0
	for _, value := range data {
		if (value >= 32 && value <= 126) || value == '\r' || value == '\n' || value == '\t' {
			printable++
		}
	}
	printablePercent := 0
	if len(data) > 0 {
		printablePercent = printable * 100 / len(data)
	}
	trailingZeros := len(data) - len(bytes.TrimRight(data, "\x00"))
	return fmt.Sprintf(
		"cipher_plain_bytes=%d last_byte=%d trailing_zeros=%d printable_percent=%d",
		len(data), lastByte, trailingZeros, printablePercent,
	)
}

func isTradeInfoPlaintext(data []byte) bool {
	if json.Valid(data) {
		return true
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return false
	}
	return values.Get("MerchantOrderNo") != "" || values.Get("Status") != ""
}

func GenerateDepositTradeSHA(tradeInfo, hashKey, hashIV string) string {
	source := fmt.Sprintf("HashKey=%s&%s&HashIV=%s", hashKey, tradeInfo, hashIV)
	sum := sha256.Sum256([]byte(source))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid PKCS7 data")
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errors.New("invalid PKCS7 padding")
	}
	for _, v := range data[len(data)-padding:] {
		if int(v) != padding {
			return nil, errors.New("invalid PKCS7 padding bytes")
		}
	}
	return data[:len(data)-padding], nil
}
