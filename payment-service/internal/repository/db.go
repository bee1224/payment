package repository

import (
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
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
