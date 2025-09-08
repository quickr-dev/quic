package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DBPath = "/etc/quic/db.sqlite"
)

type DB struct {
	*sql.DB
}

type User struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func InitDB() (*DB, error) {
	db, err := sql.Open("sqlite", DBPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	dbWrapper := &DB{DB: db}

	if err := dbWrapper.createTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating tables: %w", err)
	}

	return dbWrapper, nil
}

func (db *DB) createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		token TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("creating users table: %w", err)
	}

	return nil
}

func (db *DB) GetUserByToken(token string) (*User, error) {
	query := `SELECT id, name, token, created_at FROM users WHERE token = ?`

	var user User
	err := db.QueryRow(query, token).Scan(&user.ID, &user.Name, &user.Token, &user.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("querying user: %w", err)
	}

	return &user, nil
}
