package tunnel

import (
	"sync"
	"time"
)

// ProxyRequestStatus 代理请求状态
type ProxyRequestStatus string

const (
	RequestStatusConnecting ProxyRequestStatus = "connecting"
	RequestStatusActive     ProxyRequestStatus = "active"
	RequestStatusCompleted  ProxyRequestStatus = "completed"
	RequestStatusFailed     ProxyRequestStatus = "failed"
)

// ProxyRequest 单条代理请求记录
type ProxyRequest struct {
	ID        uint64             `json:"id"`
	Host      string             `json:"host"`
	Port      string             `json:"port"`
	Protocol  string             `json:"protocol"` // SOCKS5 / HTTP / HTTPS
	Status    ProxyRequestStatus `json:"status"`
	StartTime time.Time          `json:"startTime"`
	EndTime   time.Time          `json:"endTime,omitempty"`
	Error     string             `json:"error,omitempty"`
	ViaSSH    bool               `json:"viaSSH"`
}

// ProxyRequestTracker 代理请求跟踪器（环形缓冲，保留最近 N 条）
type ProxyRequestTracker struct {
	mu       sync.Mutex
	requests []*ProxyRequest
	maxSize  int
	nextID   uint64
}

// NewProxyRequestTracker 创建跟踪器
func NewProxyRequestTracker(maxSize int) *ProxyRequestTracker {
	if maxSize <= 0 {
		maxSize = 200
	}
	return &ProxyRequestTracker{
		requests: make([]*ProxyRequest, 0, maxSize),
		maxSize:  maxSize,
		nextID:   1,
	}
}

// StartRequest 记录一条新请求（connecting状态），返回请求指针供后续更新
func (prt *ProxyRequestTracker) StartRequest(host, port, protocol string, viaSSH bool) *ProxyRequest {
	prt.mu.Lock()
	defer prt.mu.Unlock()

	req := &ProxyRequest{
		ID:        prt.nextID,
		Host:      host,
		Port:      port,
		Protocol:  protocol,
		Status:    RequestStatusConnecting,
		StartTime: time.Now(),
		ViaSSH:    viaSSH,
	}
	prt.nextID++

	if len(prt.requests) >= prt.maxSize {
		// 移除最旧的记录
		prt.requests = prt.requests[1:]
	}
	prt.requests = append(prt.requests, req)
	return req
}

// MarkActive 标记请求为传输中
func (prt *ProxyRequestTracker) MarkActive(req *ProxyRequest) {
	if req == nil {
		return
	}
	prt.mu.Lock()
	defer prt.mu.Unlock()
	req.Status = RequestStatusActive
}

// MarkCompleted 标记请求完成
func (prt *ProxyRequestTracker) MarkCompleted(req *ProxyRequest) {
	if req == nil {
		return
	}
	prt.mu.Lock()
	defer prt.mu.Unlock()
	req.Status = RequestStatusCompleted
	req.EndTime = time.Now()
}

// MarkFailed 标记请求失败
func (prt *ProxyRequestTracker) MarkFailed(req *ProxyRequest, errMsg string) {
	if req == nil {
		return
	}
	prt.mu.Lock()
	defer prt.mu.Unlock()
	req.Status = RequestStatusFailed
	req.EndTime = time.Now()
	req.Error = errMsg
}

// Snapshot 获取当前所有请求的快照（返回副本，倒序：最新在前）
func (prt *ProxyRequestTracker) Snapshot() []ProxyRequest {
	prt.mu.Lock()
	defer prt.mu.Unlock()

	n := len(prt.requests)
	result := make([]ProxyRequest, n)
	for i, req := range prt.requests {
		result[n-1-i] = *req
	}
	return result
}

// ActiveCount 返回活跃请求数
func (prt *ProxyRequestTracker) ActiveCount() int {
	prt.mu.Lock()
	defer prt.mu.Unlock()

	count := 0
	for _, req := range prt.requests {
		if req.Status == RequestStatusConnecting || req.Status == RequestStatusActive {
			count++
		}
	}
	return count
}
