package service

import (
	"bytes"
	"net"
	"os"
	"runtime"
	"testing"
)

func TestLocalReceiptStorageValidatesMagicAndWritesPrivateFile(t *testing.T) {
	storage, err := NewLocalReceiptStorage(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.7\nexample")
	receipt, err := storage.Save("proof.pdf", "application/pdf", "operator", bytes.NewReader(pdf))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ContentType != "application/pdf" || receipt.SHA256 == "" {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	file, err := storage.Open(receipt.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	info, err := os.Stat(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("receipt permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestLocalReceiptStorageRejectsMIMEAndTraversal(t *testing.T) {
	storage, err := NewLocalReceiptStorage(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Save("proof.png", "image/png", "operator", bytes.NewReader([]byte("%PDF-1.7"))); err == nil {
		t.Fatal("expected MIME mismatch rejection")
	}
	if _, err := storage.Open("../secret"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestManualCallbackIPPolicy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1"} {
		if publicCallbackIP(net.ParseIP(raw)) {
			t.Fatalf("%s must be blocked", raw)
		}
	}
	if !publicCallbackIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP must be accepted")
	}
}
