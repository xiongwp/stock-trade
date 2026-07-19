package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

// listenPort 是服务端口。绑定 0.0.0.0 以便家庭局域网内的手机也能访问。
const listenPort = "9010"

// recoverMW 拦截每个请求中的 panic，避免单个请求打崩整个进程。
func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[recover] 请求 %s 发生 panic: %v\n%s", r.URL.Path, rec, debug.Stack())
				writeErr(w, http.StatusInternalServerError, "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func main() {
	log.SetFlags(log.LstdFlags)

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

	// 后台轮询：每 30 秒刷新报价并运行策略。崩溃自动重启，永不退出。
	go svc.RunForever(30 * time.Second)

	staticFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/symbols", h.symbols)
	mux.HandleFunc("/api/symbols/", h.symbol)
	mux.HandleFunc("/api/search", h.search)
	mux.HandleFunc("/api/bars/", h.bars)
	mux.HandleFunc("/api/signals/", h.signals)
	mux.HandleFunc("/api/positions", h.positions)
	mux.HandleFunc("/api/positions/", h.position)
	mux.HandleFunc("/api/pnl", h.pnl)
	mux.HandleFunc("/api/stats", h.stats)
	mux.HandleFunc("/api/server-info", h.serverInfo)
	mux.HandleFunc("/api/strategies", h.strategies)
	mux.HandleFunc("/api/alerts", h.alerts)
	mux.HandleFunc("/api/refresh", h.refresh)
	mux.HandleFunc("/api/alerts/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ack") {
			h.alertAck(w, r)
			return
		}
		writeErr(w, http.StatusNotFound, "未找到")
	})
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// 绑定 0.0.0.0，局域网内的手机等设备也能访问。
	srv := &http.Server{
		Addr:              "0.0.0.0:" + listenPort,
		Handler:           recoverMW(mux),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("股票交易终端（%s 行情）已启动，可用访问地址：", strings.ToUpper(cfg.Feed))
	for _, a := range serverAddrs(listenPort) {
		log.Printf("    %-28s %s", a.Label, a.URL)
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("服务器退出: %v", err)
	}
}
