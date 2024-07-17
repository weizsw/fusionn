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

	// Initialize schema
	err = initSchema(db)
	if err != nil {
		db.Close()
		return nil, err
	}

	return &database{
		db: db,
	}, nil
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS series_overview_tab (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            imdb_id TEXT UNIQUE NOT NULL,
            overview TEXT NOT NULL
        )
    `)
	return err
}
