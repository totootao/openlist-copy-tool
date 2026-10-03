package rate

import (
	"sync"
	"time"
)

// RateLimiter 三维滑动窗口限速器（秒/分/时）
type RateLimiter struct {
	mu      sync.Mutex
	limits  map[int]int       // 窗口(秒) -> 最大次数
	buckets map[int][]time.Time
}

// NewRateLimiter 创建限速器，qps/qpm/qph 分别为每秒/每分钟/每小时最大请求数
func NewRateLimiter(qps, qpm, qph int) *RateLimiter {
	return &RateLimiter{
		limits:  map[int]int{1: qps, 60: qpm, 3600: qph},
		buckets: map[int][]time.Time{1: nil, 60: nil, 3600: nil},
	}
}

// Acquire 阻塞直到本次请求被允许（满足三个维度均未满）
func (r *RateLimiter) Acquire() {
	for {
		r.mu.Lock()
		now := time.Now()
		wait := time.Duration(0)
		for window, limit := range r.limits {
			b := r.buckets[window]
			cutoff := now.Add(-time.Duration(window) * time.Second)
			i := 0
			for i < len(b) && b[i].Before(cutoff) {
				i++
			}
			r.buckets[window] = b[i:]
			b = r.buckets[window]
			if len(b) >= limit {
				earliest := b[0]
				need := time.Until(earliest.Add(time.Duration(window) * time.Second)) + 50*time.Millisecond
				if need > wait {
					wait = need
				}
			}
		}
		if wait > 0 {
			r.mu.Unlock()
			time.Sleep(wait)
			continue
		}
		for window := range r.limits {
			r.buckets[window] = append(r.buckets[window], now)
		}
		r.mu.Unlock()
		return
	}
}

// Cooldown 风控冷却管理：触发后暂停一段时间
type Cooldown struct {
	mu      sync.Mutex
	seconds time.Duration
	until   time.Time
}

// NewCooldown 创建冷却器，seconds 为触发后休眠时长
func NewCooldown(seconds time.Duration) *Cooldown {
	return &Cooldown{seconds: seconds}
}

// Trigger 进入冷却期（取较晚的时间，避免被覆盖）
func (c *Cooldown) Trigger() {
	c.mu.Lock()
	defer c.mu.Unlock()
	u := time.Now().Add(c.seconds)
	if u.After(c.until) {
		c.until = u
	}
}

// WaitIfNeeded 若处于冷却期则阻塞等待
func (c *Cooldown) WaitIfNeeded() {
	c.mu.Lock()
	remaining := time.Until(c.until)
	c.mu.Unlock()
	if remaining > 0 {
		time.Sleep(remaining)
	}
}

// Active 当前是否处于冷却期
func (c *Cooldown) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.until)
}
