package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"payment-service/internal/domain"
)

type LocalReceiptStorage struct {
	root     string
	maxBytes int64
}

func NewLocalReceiptStorage(root string, maxMB int64) (*LocalReceiptStorage, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("receipt storage path is required")
	}
	if maxMB <= 0 {
		maxMB = 10
	}
	if maxMB > 10 {
		return nil, errors.New("receipt max size must not exceed 10 MB")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &LocalReceiptStorage{root: root, maxBytes: maxMB * 1024 * 1024}, nil
}

func (s *LocalReceiptStorage) Save(originalFilename, declaredType, uploadedBy string, source io.Reader) (domain.PayoutReceipt, error) {
	if source == nil || strings.TrimSpace(uploadedBy) == "" {
		return domain.PayoutReceipt{}, errors.New("receipt and uploader are required")
	}
	// Read at most one byte beyond the permitted size; never trust multipart's Content-Type.
	data, err := io.ReadAll(io.LimitReader(source, s.maxBytes+1))
	if err != nil {
		return domain.PayoutReceipt{}, err
	}
	if int64(len(data)) > s.maxBytes {
		return domain.PayoutReceipt{}, fmt.Errorf("receipt exceeds %d MB", s.maxBytes/(1024*1024))
	}
	contentType, extension, ok := receiptMagicType(data)
	if !ok {
		return domain.PayoutReceipt{}, errors.New("unsupported or invalid receipt file")
	}
	if declaredType = strings.TrimSpace(strings.ToLower(strings.Split(declaredType, ";")[0])); declaredType != "" && declaredType != contentType {
		return domain.PayoutReceipt{}, errors.New("receipt MIME type does not match file content")
	}
	key, err := receiptStorageKey()
	if err != nil {
		return domain.PayoutReceipt{}, err
	}
	key += extension
	path := filepath.Join(s.root, key)
	if filepath.Dir(path) != filepath.Clean(s.root) {
		return domain.PayoutReceipt{}, errors.New("invalid receipt storage path")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return domain.PayoutReceipt{}, err
	}
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return domain.PayoutReceipt{}, err
	}
	if err = file.Close(); err != nil {
		_ = os.Remove(path)
		return domain.PayoutReceipt{}, err
	}
	sum := sha256.Sum256(data)
	return domain.PayoutReceipt{StorageKey: key, OriginalFilename: filepath.Base(strings.TrimSpace(originalFilename)), ContentType: contentType, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), UploadedBy: strings.TrimSpace(uploadedBy)}, nil
}

func (s *LocalReceiptStorage) Delete(storageKey string) error {
	if strings.Contains(storageKey, "..") || filepath.Base(storageKey) != storageKey {
		return errors.New("invalid storage key")
	}
	return os.Remove(filepath.Join(s.root, storageKey))
}
func (s *LocalReceiptStorage) Open(storageKey string) (*os.File, error) {
	if strings.Contains(storageKey, "..") || filepath.Base(storageKey) != storageKey {
		return nil, errors.New("invalid storage key")
	}
	return os.Open(filepath.Join(s.root, storageKey))
}

func receiptStorageKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func receiptMagicType(b []byte) (string, string, bool) {
	switch {
	case len(b) >= 5 && string(b[:5]) == "%PDF-":
		return "application/pdf", ".pdf", true
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "image/jpeg", ".jpg", true
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png", ".png", true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp", ".webp", true
	default:
		return "", "", false
	}
}
