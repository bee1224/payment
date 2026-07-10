package repository

import (
	"database/sql"
	"errors"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

type DB struct {
	SQL *sql.DB
}

func NewDB(sqlDB *sql.DB) *DB {
	return &DB{SQL: sqlDB}
}

func Open(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, nil
	}
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return sqlDB, nil
}

func Migrate(dsn, path string) error {
	if dsn == "" {
		return nil
	}

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	for _, stmt := range splitSQLStatements(string(content)) {
		if stmt == "" {
			continue
		}
		if _, err := sqlDB.Exec(stmt); err != nil {
			if isIgnorableMigrationError(err) {
				continue
			}
			return err
		}
	}
	return sqlDB.Ping()
}

func splitSQLStatements(script string) []string {
	parts := strings.Split(script, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		statements = append(statements, stmt)
	}
	return statements
}

func isIgnorableMigrationError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1060: // duplicate column name
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name")
}
