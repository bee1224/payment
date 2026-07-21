package repository

import (
	"strings"
	"testing"
)

func TestStripLegacyDatabaseBootstrap(t *testing.T) {
	for _, input := range []string{
		"CREATE DATABASE IF NOT EXISTS payment_service;\nUSE payment_service;\nCREATE TABLE merchants (id INT);\n",
		"CREATE DATABASE IF NOT EXISTS payment_service;\r\n  USE payment_service;  \r\nCREATE TABLE merchants (id INT);\r\n",
		"\ufeff CREATE DATABASE IF NOT EXISTS payment_service;\n\tUSE payment_service\nCREATE TABLE merchants (id INT);\n",
	} {
		got := string(stripLegacyDatabaseBootstrap([]byte(input)))
		if contains := "CREATE DATABASE"; strings.Contains(strings.ToUpper(got), contains) {
			t.Fatalf("bootstrap CREATE remained: %q", got)
		}
		if strings.Contains(strings.ToUpper(got), "USE PAYMENT_SERVICE") {
			t.Fatalf("bootstrap USE remained: %q", got)
		}
		if !strings.Contains(got, "CREATE TABLE merchants") {
			t.Fatalf("schema SQL missing: %q", got)
		}
	}
}

func TestShouldBaselineLegacyDatabase(t *testing.T) {
	tests := []struct {
		name                               string
		migrationTableExists, schemaExists bool
		want                               bool
	}{
		{name: "complete legacy schema without migration history", schemaExists: true, want: true},
		{name: "already versioned schema", migrationTableExists: true, schemaExists: true},
		{name: "new empty database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBaselineLegacyDatabase(tt.migrationTableExists, tt.schemaExists); got != tt.want {
				t.Fatalf("shouldBaselineLegacyDatabase() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMigrationSourceURL(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "Linux absolute path",
			path: "/opt/payment service/migrations",
			want: "file:///opt/payment%20service/migrations",
		},
		{
			name: "Linux absolute path with non ASCII",
			path: "/opt/支付服務/migrations",
			want: "file:///opt/%E6%94%AF%E4%BB%98%E6%9C%8D%E5%8B%99/migrations",
		},
		{
			name: "Windows drive path with backslashes",
			path: `C:\payment-service\migrations`,
			want: "file://C:/payment-service/migrations",
		},
		{
			name: "Windows drive path with spaces and non ASCII",
			path: `D:\Payment Service\支付\migrations`,
			want: "file://D:/Payment%20Service/%E6%94%AF%E4%BB%98/migrations",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := migrationSourceURL(tt.path); got != tt.want {
				t.Fatalf("migrationSourceURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
