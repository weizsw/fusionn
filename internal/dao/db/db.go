package db

import (
	"database/sql"
	"fusionn/configs"

	_ "github.com/mattn/go-sqlite3"
)

type IDatabase interface {
}

type database struct {
	db *sql.DB
}

func NewDatabase() (*database, error) {
	dbPath := configs.C.GetString("sqlite.path")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	return &database{
		db: db,
	}, nil
}
