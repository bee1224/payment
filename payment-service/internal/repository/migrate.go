package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const legacyBaselineVersion uint = 1

// Migrate applies only migration versions not recorded in schema_migrations.
// Legacy databases created before version tracking are baselined at version 1
// only after the sentinel tables prove that the complete original schema ran.
func Migrate(dsn, path string) error {
	if dsn == "" {
		return nil
	}

	migrationTableExists, legacySchemaAtBaseline, err := migrationState(dsn)
	if err != nil {
		return err
	}

	absPath, cleanup, err := isolatedMigrationPath(path)
	if err != nil {
		return fmt.Errorf("resolve migration path: %w", err)
	}
	defer cleanup()
	m, err := migrate.New(migrationSourceURL(absPath), "mysql://"+dsn)
	if err != nil {
		return fmt.Errorf("initialize versioned migrations: %w", err)
	}
	defer m.Close()

	if shouldBaselineLegacyDatabase(migrationTableExists, legacySchemaAtBaseline) {
		if err := m.Force(int(legacyBaselineVersion)); err != nil {
			return fmt.Errorf("baseline legacy database at version %d: %w", legacyBaselineVersion, err)
		}
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply versioned migrations: %w", err)
	}
	return nil
}

// isolatedMigrationPath keeps legacy bootstrap SQL from selecting a database
// other than the one explicitly named in the DSN. The source migrations remain
// immutable; only the execution copy is normalized.
func isolatedMigrationPath(path string) (string, func(), error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", func() {}, err
	}
	tempDir, err := os.MkdirTemp("", "payment-migrations-")
	if err != nil {
		return "", func() {}, err
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", func() {}, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(absPath, entry.Name()))
		if err != nil {
			os.RemoveAll(tempDir)
			return "", func() {}, err
		}
		if entry.Name() == "000001_init.up.sql" {
			data = stripLegacyDatabaseBootstrap(data)
		}
		if err := os.WriteFile(filepath.Join(tempDir, entry.Name()), data, 0600); err != nil {
			os.RemoveAll(tempDir)
			return "", func() {}, err
		}
	}
	return tempDir, func() { _ = os.RemoveAll(tempDir) }, nil
}

func stripLegacyDatabaseBootstrap(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "CREATE DATABASE") || upper == "USE PAYMENT_SERVICE;" || upper == "USE PAYMENT_SERVICE" {
			continue
		}
		kept = append(kept, line)
	}
	return []byte(strings.Join(kept, "\n"))
}

func migrationState(dsn string) (migrationTableExists, legacySchemaAtBaseline bool, err error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return false, false, fmt.Errorf("open database for migration state: %w", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		return false, false, fmt.Errorf("ping database for migration state: %w", err)
	}

	for _, table := range []string{"schema_migrations", "merchants", "admin_users", "manual_payout_cases", "worker_leases"} {
		var found int
		err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&found)
		if err != nil {
			return false, false, fmt.Errorf("inspect %s table: %w", table, err)
		}
		switch table {
		case "schema_migrations":
			migrationTableExists = found > 0
		case "merchants", "admin_users", "manual_payout_cases", "worker_leases":
			if !migrationTableExists && found == 0 {
				return migrationTableExists, false, nil
			}
			if table == "worker_leases" {
				legacySchemaAtBaseline = true
			}
		}
	}
	return migrationTableExists, legacySchemaAtBaseline, nil
}

func shouldBaselineLegacyDatabase(migrationTableExists, legacySchemaExists bool) bool {
	return !migrationTableExists && legacySchemaExists
}

func migrationSourceURL(path string) string {
	path = strings.ReplaceAll(path, `\`, "/")
	if isWindowsDrivePath(path) {
		// golang-migrate's file driver concatenates URL host and path.  Using
		// file:///C:/... therefore becomes /C:/... on Windows, which is not a
		// valid filesystem path. Keeping the drive in the host restores C:/.
		// Do not use filepath.VolumeName here: it intentionally follows the
		// current OS and therefore cannot recognize a Windows path on WSL/Linux.
		return (&url.URL{Scheme: "file", Host: path[:2], Path: path[2:]}).String()
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func isWindowsDrivePath(path string) bool {
	return len(path) >= 3 &&
		((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && path[2] == '/'
}
