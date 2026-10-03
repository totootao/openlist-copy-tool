package job

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/totootao/openlist-copy-tool/internal/openlist"
	"github.com/totootao/openlist-copy-tool/internal/rate"
)

// JobStatus 任务状态
type JobStatus string

const (
	StatusPending  JobStatus = "pending"
	StatusRunning  JobStatus = "running"
	StatusDone     JobStatus = "done"
	StatusError    JobStatus = "error"
	StatusCooldown JobStatus = "cooldown"
)

// Job 复制任务
type Job struct {
	ID         string    `json:"id"`
	SrcDir     string    `json:"src_dir"`
	DstDir     string    `json:"dst_dir"`
	Status     JobStatus `json:"status"`
	Total      int       `json:"total"`
	Processed  int       `json:"processed"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Log        []string  `json:"log"`
}

// Manager 任务管理器
type Manager struct {
	client    *openlist.Client
	limiter   *rate.RateLimiter
	cooldown  *rate.Cooldown
	batch     int
	batchGap  time.Duration
	checkpoint string
	done      map[string]bool

	mu   sync.Mutex
	jobs map[string]*Job
}

// NewManager 创建管理器
func NewManager(client *openlist.Client, limiter *rate.RateLimiter, cooldown *rate.Cooldown, batch int, batchGap time.Duration, checkpoint string) *Manager {
	return &Manager{
		client:     client,
		limiter:    limiter,
		cooldown:   cooldown,
		batch:      batch,
		batchGap:   batchGap,
		checkpoint: checkpoint,
		done:       loadCheckpoint(checkpoint),
		jobs:       make(map[string]*Job),
	}
}

func loadCheckpoint(path string) map[string]bool {
	m := make(map[string]bool)
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var arr []string
	if json.Unmarshal(data, &arr) == nil {
		for _, s := range arr {
			m[s] = true
		}
	}
	return m
}

func saveCheckpoint(path string, m map[string]bool) {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	data, _ := json.MarshalIndent(arr, "", "  ")
	_ = os.WriteFile(path, data, 0644)
}

// Submit 提交一个复制任务（异步执行）
func (m *Manager) Submit(src, dst string) *Job {
	m.mu.Lock()
	job := &Job{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		SrcDir:    src,
		DstDir:    dst,
		Status:    StatusPending,
		StartedAt: time.Now(),
	}
	m.jobs[job.ID] = job
	m.mu.Unlock()
	go m.execute(job)
	return job
}

// ListJobs 返回全部任务
func (m *Manager) ListJobs() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out
}

// GetJob 按 ID 获取任务
func (m *Manager) GetJob(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

func (m *Manager) appendLog(job *Job, msg string) {
	m.mu.Lock()
	job.Log = append(job.Log, time.Now().Format("15:04:05")+" "+msg)
	if len(job.Log) > 500 {
		job.Log = job.Log[len(job.Log)-500:]
	}
	m.mu.Unlock()
}

func (m *Manager) setStatus(job *Job, s JobStatus) {
	m.mu.Lock()
	job.Status = s
	m.mu.Unlock()
}

// avoidPeak 判断当前是否处于中国晚高峰(20:00-22:00)，使用 UTC+8 计算避免依赖时区数据库
func avoidPeak() bool {
	t := time.Now().UTC().Add(8 * time.Hour)
	h := t.Hour()
	return h >= 20 && h < 22
}

// execute 执行复制（分批、限速、冷却、断点续传、错峰）
func (m *Manager) execute(job *Job) {
	m.setStatus(job, StatusRunning)
	defer func() {
		m.mu.Lock()
		job.FinishedAt = time.Now()
		if job.Status == StatusRunning {
			job.Status = StatusDone
		}
		m.mu.Unlock()
	}()

	items, err := m.client.List(job.SrcDir, false)
	if err != nil {
		m.appendLog(job, "列源目录失败: "+err.Error())
		m.setStatus(job, StatusError)
		return
	}
	var subdirs []string
	for _, it := range items {
		if it.IsDir {
			subdirs = append(subdirs, it.Name)
		}
	}

	m.mu.Lock()
	job.Total = len(subdirs)
	m.mu.Unlock()

	// 过滤已完成
	pending := make([]string, 0, len(subdirs))
	for _, n := range subdirs {
		key := job.SrcDir + "/" + n
		m.mu.Lock()
		skip := m.done[key]
		m.mu.Unlock()
		if !skip {
			pending = append(pending, n)
		}
	}

	for i := 0; i < len(pending); {
		// 错峰：晚高峰暂停
		for avoidPeak() {
			m.appendLog(job, "处于晚高峰(20:00-22:00)，暂停")
			m.setStatus(job, StatusCooldown)
			time.Sleep(5 * time.Minute)
		}
		m.setStatus(job, StatusRunning)

		end := i + m.batch
		if end > len(pending) {
			end = len(pending)
		}
		chunk := pending[i:end]

		limited := false
		for _, name := range chunk {
			m.cooldown.WaitIfNeeded()
			m.limiter.Acquire()
			src := job.SrcDir + "/" + name
			dst := job.DstDir + "/" + name
			code, msg, cerr := m.client.Copy(src, dst)
			if cerr != nil {
				if cerr == openlist.ErrRateLimited {
					m.cooldown.Trigger()
					m.appendLog(job, "触发限流，进入冷却")
					limited = true
					break
				}
				m.appendLog(job, "复制异常 "+src+": "+cerr.Error())
				continue
			}
			switch {
			case code == 200:
				m.appendLog(job, "已入队: "+src)
				key := job.SrcDir + "/" + name
				m.mu.Lock()
				m.done[key] = true
				saveCheckpoint(m.checkpoint, m.done)
				job.Processed++
				m.mu.Unlock()
			case code == 405 || code == 429:
				m.cooldown.Trigger()
				m.appendLog(job, "触发限流("+msg+")，进入冷却")
				limited = true
				break
			default:
				m.appendLog(job, "复制失败 "+src+": "+msg)
				m.mu.Lock()
				job.Processed++
				m.mu.Unlock()
			}
		}

		if limited {
			// 冷却后重试本批，不前进
			m.cooldown.WaitIfNeeded()
			continue
		}
		i += m.batch
		time.Sleep(m.batchGap)
	}

	m.appendLog(job, "全部批次已提交")
}
