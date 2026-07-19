package main

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// openDB 打开（或创建）SQLite 数据库并建表。
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// 单文件本地库，开启 WAL 提升读写并发。
	for _, pragma := range []string{
		`PRAGMA journal_mode = WAL;`,
		`PRAGMA busy_timeout = 5000;`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, err
		}
	}

	schema := []string{
		`CREATE TABLE IF NOT EXISTS symbols (
			symbol     TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			last_price REAL NOT NULL DEFAULT 0,
			prev_close REAL NOT NULL DEFAULT 0,
			quote_at   TEXT NOT NULL DEFAULT '',
			added_at   TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS bars (
			symbol TEXT NOT NULL,
			ts     INTEGER NOT NULL,   -- unix 秒（该交易日）
			o REAL NOT NULL, h REAL NOT NULL, l REAL NOT NULL, c REAL NOT NULL,
			v INTEGER NOT NULL,
			PRIMARY KEY (symbol, ts)
		);`,
		`CREATE TABLE IF NOT EXISTS positions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol     TEXT NOT NULL,
			quantity   REAL NOT NULL,
			buy_price  REAL NOT NULL,
			buy_time   TEXT NOT NULL,
			note       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS alerts (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol       TEXT NOT NULL,
			kind         TEXT NOT NULL,
			message      TEXT NOT NULL,
			price        REAL NOT NULL,
			created_at   TEXT NOT NULL,
			dedup_key    TEXT NOT NULL DEFAULT '',
			acknowledged INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE INDEX IF NOT EXISTS idx_positions_symbol ON positions(symbol);`,
		`CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_dedup ON alerts(dedup_key) WHERE dedup_key <> '';`,
	}
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			return nil, err
		}
	}
	return db, nil
}

// ---------- symbols ----------

type Symbol struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	LastPrice float64 `json:"lastPrice"`
	PrevClose float64 `json:"prevClose"`
	QuoteAt   string  `json:"quoteAt"`
	AddedAt   string  `json:"addedAt"`
}

func addSymbol(db *sql.DB, symbol, name string) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO symbols (symbol, name, added_at) VALUES (?, ?, ?)`,
		symbol, name, time.Now().Format(time.RFC3339),
	)
	return err
}

func listSymbols(db *sql.DB) ([]Symbol, error) {
	rows, err := db.Query(`SELECT symbol, name, last_price, prev_close, quote_at, added_at
		FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Symbol{}
	for rows.Next() {
		var s Symbol
		if err := rows.Scan(&s.Symbol, &s.Name, &s.LastPrice, &s.PrevClose, &s.QuoteAt, &s.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func listSymbolNames(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT symbol FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func deleteSymbol(db *sql.DB, symbol string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM symbols WHERE symbol = ?`,
		`DELETE FROM bars WHERE symbol = ?`,
	} {
		if _, err := tx.Exec(q, symbol); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func updateQuote(db *sql.DB, symbol string, price, prevClose float64) error {
	_, err := db.Exec(
		`UPDATE symbols SET last_price = ?, prev_close = ?, quote_at = ? WHERE symbol = ?`,
		price, prevClose, time.Now().Format(time.RFC3339), symbol,
	)
	return err
}

// ---------- bars ----------

func upsertBars(db *sql.DB, symbol string, bars []Bar) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO bars (symbol, ts, o, h, l, c, v)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, b := range bars {
		if _, err := stmt.Exec(symbol, b.T.Unix(), b.O, b.H, b.L, b.C, b.V); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BarRow 是给前端图表用的一行 K 线（time 为 unix 秒）。
type BarRow struct {
	Time int64   `json:"time"`
	O    float64 `json:"open"`
	H    float64 `json:"high"`
	L    float64 `json:"low"`
	C    float64 `json:"close"`
	V    int64   `json:"volume"`
}

func getBars(db *sql.DB, symbol string) ([]BarRow, error) {
	rows, err := db.Query(`SELECT ts, o, h, l, c, v FROM bars WHERE symbol = ? ORDER BY ts`, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BarRow{}
	for rows.Next() {
		var b BarRow
		if err := rows.Scan(&b.Time, &b.O, &b.H, &b.L, &b.C, &b.V); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// latestClose 返回本地库中该股票最近一根 K 线的收盘价（无则 0）。
func latestClose(db *sql.DB, symbol string) float64 {
	var c float64
	db.QueryRow(`SELECT c FROM bars WHERE symbol = ? ORDER BY ts DESC LIMIT 1`, symbol).Scan(&c)
	return c
}

// ---------- positions ----------

type Position struct {
	ID        int64   `json:"id"`
	Symbol    string  `json:"symbol"`
	Quantity  float64 `json:"quantity"`
	BuyPrice  float64 `json:"buyPrice"`
	BuyTime   string  `json:"buyTime"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"createdAt"`
}

func addPosition(db *sql.DB, p Position) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO positions (symbol, quantity, buy_price, buy_time, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		p.Symbol, p.Quantity, p.BuyPrice, p.BuyTime, p.Note, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func listPositions(db *sql.DB) ([]Position, error) {
	rows, err := db.Query(`SELECT id, symbol, quantity, buy_price, buy_time, note, created_at
		FROM positions ORDER BY symbol, buy_time`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Position{}
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.ID, &p.Symbol, &p.Quantity, &p.BuyPrice, &p.BuyTime, &p.Note, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func deletePosition(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM positions WHERE id = ?`, id)
	return err
}

// ---------- alerts ----------

type Alert struct {
	ID           int64   `json:"id"`
	Symbol       string  `json:"symbol"`
	Kind         string  `json:"kind"`
	Message      string  `json:"message"`
	Price        float64 `json:"price"`
	CreatedAt    string  `json:"createdAt"`
	Acknowledged bool    `json:"acknowledged"`
}

// insertAlert 写入一条提醒；dedupKey 非空且已存在时静默跳过（返回 false）。
func insertAlert(db *sql.DB, a Alert, dedupKey string) (bool, error) {
	res, err := db.Exec(
		`INSERT OR IGNORE INTO alerts (symbol, kind, message, price, created_at, dedup_key)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.Symbol, a.Kind, a.Message, a.Price, time.Now().Format(time.RFC3339), dedupKey,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func listAlerts(db *sql.DB, limit int) ([]Alert, error) {
	rows, err := db.Query(`SELECT id, symbol, kind, message, price, created_at, acknowledged
		FROM alerts ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		var ack int
		if err := rows.Scan(&a.ID, &a.Symbol, &a.Kind, &a.Message, &a.Price, &a.CreatedAt, &ack); err != nil {
			return nil, err
		}
		a.Acknowledged = ack != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

func ackAlert(db *sql.DB, id int64) error {
	_, err := db.Exec(`UPDATE alerts SET acknowledged = 1 WHERE id = ?`, id)
	return err
}
