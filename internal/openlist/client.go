package openlist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrRateLimited 表示被 115/OpenList 限流（405/429 等）
var ErrRateLimited = fmt.Errorf("rate limited")

// Client 封装 OpenList 的 REST API
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// Item 目录项
type Item struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Modified string `json:"modified"`
}

// ListResponse 列目录响应
type ListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Content []Item `json:"content"`
		Total   int    `json:"total"`
	} `json:"data"`
}

// NewClient 创建客户端并登录获取 token
func NewClient(baseURL, user, password string) (*Client, error) {
	c := &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
	if err := c.login(user, password); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) login(user, password string) error {
	body, _ := json.Marshal(map[string]string{"username": user, "password": password})
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("解析登录响应失败: %w", err)
	}
	if r.Code != 200 {
		return fmt.Errorf("登录失败: %s", r.Message)
	}
	c.Token = r.Data.Token
	return nil
}

// List 列出目录内容
func (c *Client) List(path string, refresh bool) ([]Item, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"path":     path,
		"page":     1,
		"per_page": 500,
		"refresh":  refresh,
	})
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/api/fs/list", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}
	data, _ := io.ReadAll(resp.Body)
	var r ListResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("列目录失败: %s", r.Message)
	}
	return r.Data.Content, nil
}

// Copy 复制整个目录（115->115 走秒传）
// 返回 code, message, error
func (c *Client) Copy(srcDir, dstDir string) (int, string, error) {
	body, _ := json.Marshal(map[string]string{
		"src_dir": srcDir,
		"dst_dir": dstDir,
	})
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/api/fs/copy", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusTooManyRequests {
		return resp.StatusCode, "", ErrRateLimited
	}
	data, _ := io.ReadAll(resp.Body)
	var r struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &r)
	if r.Code == 405 || r.Code == 429 {
		return r.Code, r.Message, ErrRateLimited
	}
	return r.Code, r.Message, nil
}
