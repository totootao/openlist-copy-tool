package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/totootao/openlist-copy-tool/internal/job"
	"github.com/totootao/openlist-copy-tool/internal/openlist"
	"github.com/totootao/openlist-copy-tool/internal/rate"
	"github.com/totootao/openlist-copy-tool/internal/web"
)

func main() {
	var (
		baseURL  = flag.String("url", envOr("OPENLIST_URL", "http://127.0.0.1:5244"), "OpenList 地址")
		user     = flag.String("user", envOr("OPENLIST_USER", "admin"), "OpenList 用户名")
		password = flag.String("pass", envOr("OPENLIST_PASSWORD", ""), "OpenList 密码")
		listen   = flag.String("listen", envOr("LISTEN_ADDR", ":8080"), "Web 监听地址")
		qps      = flag.Int("qps", 1, "每秒最大请求数（对 115 后端）")
		qpm      = flag.Int("qpm", 40, "每分钟最大请求数")
		qph      = flag.Int("qph", 300, "每小时最大请求数")
		batch    = flag.Int("batch", 10, "每批目录数")
		batchGap = flag.Int("batch-gap", 60, "批间间隔(秒)")
		cooldown = flag.Int("cooldown", 1800, "限流冷却时长(秒)")
		ckpt     = flag.String("checkpoint", "checkpoint.json", "断点续传文件路径")
	)
	flag.Parse()

	client, err := openlist.NewClient(*baseURL, *user, *password)
	if err != nil {
		log.Fatalf("连接 OpenList 失败: %v", err)
	}
	log.Println("OpenList 登录成功")

	limiter := rate.NewRateLimiter(*qps, *qpm, *qph)
	cd := rate.NewCooldown(time.Duration(*cooldown) * time.Second)
	manager := job.NewManager(client, limiter, cd, *batch, time.Duration(*batchGap)*time.Second, *ckpt)

	srv := web.NewServer(client, manager, limiter, cd)
	log.Printf("Web 服务启动于 %s （源/目标目录选择后即可复制）", *listen)
	if err := http.ListenAndServe(*listen, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
