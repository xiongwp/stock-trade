package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
)

//go:embed web
var webFS embed.FS

func main() {
	db, err := openDB("stock-trade.db")
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	srv := &server{db: db}

	staticFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/stocks", srv.handleStocks)
	mux.HandleFunc("/api/stocks/", srv.handleStock)
	mux.HandleFunc("/api/tick", srv.handleTick)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	addr := "localhost:8080"
	log.Printf("股票交易（模拟盘）已启动 → http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务器退出: %v", err)
	}
}
