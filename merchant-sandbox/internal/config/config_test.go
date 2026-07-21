package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvOnlyFillsUnsetVariables(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("MERCHANT_SANDBOX_TEST_VALUE=from-file\nMERCHANT_SANDBOX_TEST_QUOTED=\"quoted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MERCHANT_SANDBOX_TEST_VALUE", "from-environment")
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("MERCHANT_SANDBOX_TEST_VALUE"); got != "from-environment" {
		t.Fatalf("value=%q", got)
	}
	if got := os.Getenv("MERCHANT_SANDBOX_TEST_QUOTED"); got != "quoted" {
		t.Fatalf("quoted=%q", got)
	}
}
