package web

import (
	"embed"
	"encoding/json"
	"net/http"

	"github.com/totootao/openlist-copy-tool/internal/job"
	"github.com/totootao/openlist-copy-tool/internal/openlist"
	"github.com/totootao/openlist-copy-tool/internal/rate"
)

//go:embed static
var assets embed.FS

// Server Web 服务
type Server struct {
	client   *openlist.Client
	manager  *job.Manager
	limiter  *rate.RateLimiter
	cooldown *rate.Cooldown
}

// NewServer 创建 Web 服务
func NewServer(client *openlist.Client, manager *job.Manager, limiter *rate.RateLimiter, cooldown *rate.Cooldown) *Server {
	return &Server{client: client, manager: manager, limiter: limiter, cooldown: cooldown}
}

// Routes 注册路由
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/list", s.handleList)
	mux.HandleFunc("/api/copy", s.handleCopy)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := assets.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})
	return mux
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	s.limiter.Acquire()
	items, err := s.client.List(path, refresh)
	if err != nil {
		if err == openlist.ErrRateLimited {
			s.cooldown.Trigger()
			writeJSON(w, 429, map[string]interface{}{"error": "触发115限流，已自动进入冷却", "cooldown": true})
			return
		}
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// 仅返回目录（复制以目录为单位）
	dirs := make([]openlist.Item, 0)
	for _, it := range items {
		if it.IsDir {
			dirs = append(dirs, it)
		}
	}
	writeJSON(w, 200, map[string]interface{}{"path": path, "dirs": dirs})
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SrcDir string `json:"src_dir"`
		DstDir string `json:"dst_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "参数错误"})
		return
	}
	if req.SrcDir == "" || req.DstDir == "" {
		writeJSON(w, 400, map[string]string{"error": "源目录和目标目录均不能为空"})
		return
	}
	j := s.manager.Submit(req.SrcDir, req.DstDir)
	writeJSON(w, 200, map[string]string{"job_id": j.ID, "status": string(j.Status)})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.manager.ListJobs())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{
		"cooldown_active": s.cooldown.Active(),
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
