package main

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// Stock 表示一只自选股及其最新行情。
type Stock struct {
	ID        int64   `json:"id"`
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	PrevClose float64 `json:"prevClose"`
	UpdatedAt string  `json:"updatedAt"`
}

// openDB 打开（或创建）SQLite 数据库并建表。
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS stocks (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol     TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL DEFAULT '',
			price      REAL NOT NULL DEFAULT 0,
			prev_close REAL NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);
	`); err != nil {
		return nil, err
	}
	return db, nil
}

func listStocks(db *sql.DB) ([]Stock, error) {
	rows, err := db.Query(`SELECT id, symbol, name, price, prev_close, updated_at
		FROM stocks ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stocks := []Stock{}
	for rows.Next() {
		var s Stock
		if err := rows.Scan(&s.ID, &s.Symbol, &s.Name, &s.Price, &s.PrevClose, &s.UpdatedAt); err != nil {
			return nil, err
		}
		stocks = append(stocks, s)
	}
	return stocks, rows.Err()
}

func addStock(db *sql.DB, symbol, name string, price float64) (Stock, error) {
	now := time.Now().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO stocks (symbol, name, price, prev_close, updated_at) VALUES (?, ?, ?, ?, ?)`,
		symbol, name, price, price, now,
	)
	if err != nil {
		return Stock{}, err
	}
	id, _ := res.LastInsertId()
	return Stock{ID: id, Symbol: symbol, Name: name, Price: price, PrevClose: price, UpdatedAt: now}, nil
}

// updatePrice 只改当前价，prev_close 保持不变，作为涨跌幅基准。
func updatePrice(db *sql.DB, id int64, price float64) error {
	now := time.Now().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE stocks SET price = ?, updated_at = ? WHERE id = ?`, price, now, id)
	return err
}

func deleteStock(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM stocks WHERE id = ?`, id)
	return err
}
