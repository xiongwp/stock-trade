package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("配置错误: %v", err)
	}

	db, err := openDB("stock-trade.db")
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	svc := newService(db, newAlpaca(cfg))
	h := &api{svc: svc}

	// 后台轮询：每 30 秒刷新报价并运行策略。
	go svc.Run(30 * time.Second)

	staticFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/symbols", h.symbols)
	mux.HandleFunc("/api/symbols/", h.symbol)
	mux.HandleFunc("/api/search", h.search)
	mux.HandleFunc("/api/bars/", h.bars)
	mux.HandleFunc("/api/positions", h.positions)
	mux.HandleFunc("/api/positions/", h.position)
	mux.HandleFunc("/api/pnl", h.pnl)
	mux.HandleFunc("/api/alerts", h.alerts)
	mux.HandleFunc("/api/refresh", h.refresh)
	// /api/alerts/{id}/ack 需要子路径路由。
	mux.HandleFunc("/api/alerts/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ack") {
			h.alertAck(w, r)
			return
		}
		writeErr(w, http.StatusNotFound, "未找到")
	})
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	addr := "localhost:8080"
	log.Printf("股票交易（%s 行情）已启动 → http://%s", strings.ToUpper(cfg.Feed), addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务器退出: %v", err)
	}
}
