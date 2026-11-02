package service

import (
	"container/list"
	"context"
	"encoding/base64"
	"io"
	"sync"
	"time"
	"unicode/utf8"
)

const RequestMonitorRetention = 30 * time.Minute
const RequestMonitorBodyLimit = 1 << 20
const RequestMonitorBodyBudget = 64 << 20

// MonitoredBody 保存转发过程中实际读取或写出的正文；WebSocket 消息以换行分隔。
type MonitoredBody struct {
	Direction   string `json:"direction"`
	Attempt     int    `json:"attempt,omitempty"`
	ChannelID   int    `json:"channel_id,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
	TotalBytes  int64  `json:"total_bytes"`
	Truncated   bool   `json:"truncated"`
}

type monitorBodyBuffer struct {
	MonitoredBody
	data []byte
}

type monitorBodyRecord struct {
	store     *monitorBodyStore
	element   *list.Element
	expiresAt time.Time
	bodies    []*monitorBodyBuffer
	bytes     int
	released  string
}

type monitorBodyStore struct {
	mu      sync.Mutex
	records list.List
	bytes   int
	budget  int
	limit   int
}

var requestMonitorBodies = monitorBodyStore{budget: RequestMonitorBodyBudget, limit: RequestMonitorBodyLimit}

func (s *monitorBodyStore) start(expiresAt time.Time) *monitorBodyRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &monitorBodyRecord{store: s, expiresAt: expiresAt}
	r.element = s.records.PushBack(r)
	return r
}

// releaseLocked 也使仍在运行的旧请求停止采集，防止淘汰后重新占用缓存。
func (s *monitorBodyStore) releaseLocked(r *monitorBodyRecord, reason string) {
	if r.released != "" {
		return
	}
	s.bytes -= r.bytes
	r.bytes, r.bodies, r.released = 0, nil, reason
	s.records.Remove(r.element)
}

func (r *monitorBodyRecord) release(reason string) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	r.store.releaseLocked(r, reason)
}

func (r *monitorBodyRecord) append(now time.Time, direction string, attempt, channelID int, contentType string, data []byte) {
	s := r.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if !now.Before(r.expiresAt) {
		s.releaseLocked(r, "expired")
	}
	if r.released != "" {
		return
	}
	var body *monitorBodyBuffer
	for _, candidate := range r.bodies {
		if candidate.Direction == direction && candidate.Attempt == attempt {
			body = candidate
			break
		}
	}
	if body == nil {
		// 上游最多 64 次，另加客户端请求和响应，限制零字节记录的开销。
		if len(r.bodies) >= 2*maxMonitorUpstreams+2 {
			return
		}
		body = &monitorBodyBuffer{MonitoredBody: MonitoredBody{Direction: direction, Attempt: attempt, ChannelID: channelID, ContentType: contentType}}
		r.bodies = append(r.bodies, body)
	}
	if contentType != "" {
		body.ContentType = contentType
	}
	body.TotalBytes += int64(len(data))
	n := min(len(data), s.limit-len(body.data))
	capacity := cap(body.data)
	if len(body.data)+n > capacity {
		capacity = min(s.limit, max(len(body.data)+n, capacity*2))
	}
	allocated := capacity - cap(body.data)
	for s.bytes+allocated > s.budget {
		oldest := s.records.Front()
		if oldest == nil {
			return
		}
		s.releaseLocked(oldest.Value.(*monitorBodyRecord), "evicted")
		if r.released != "" {
			return
		}
	}
	// 按实际缓冲区容量计预算，避免 append 扩容使常驻正文超出预算。
	if allocated > 0 {
		grown := make([]byte, len(body.data), capacity)
		copy(grown, body.data)
		body.data = grown
	}
	body.data = append(body.data, data[:n]...)
	body.Truncated = body.Truncated || n < len(data)
	r.bytes += allocated
	s.bytes += allocated
}

func (r *monitorBodyRecord) snapshot(now time.Time) ([]MonitoredBody, string) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if !now.Before(r.expiresAt) {
		r.store.releaseLocked(r, "expired")
	}
	bodies := make([]MonitoredBody, 0, len(r.bodies))
	for _, captured := range r.bodies {
		body := captured.MonitoredBody
		body.Encoding = "utf-8"
		data := captured.data
		if body.Truncated && !utf8.Valid(data) {
			// 截断点可能落在 UTF-8 字符中间，不因此把整段文本误判为二进制。
			// 只有不完整的字符才可裁剪，非法字节仍按二进制保留。
			for start := len(data) - 1; start >= max(0, len(data)-utf8.UTFMax+1); start-- {
				if utf8.RuneStart(data[start]) {
					if !utf8.FullRune(data[start:]) && utf8.Valid(data[:start]) {
						data = data[:start]
					}
					break
				}
			}
		}
		if utf8.Valid(data) {
			body.Content = string(data)
		} else {
			body.Encoding = "base64"
			body.Content = base64.StdEncoding.EncodeToString(data)
		}
		bodies = append(bodies, body)
	}
	return bodies, r.released
}

// RecordMonitorBody 不读取额外数据，不改变响应分块、刷新时机或转发内容。
func RecordMonitorBody(ctx context.Context, direction string, attempt, channelID int, contentType string, data []byte) {
	if ctx == nil {
		return
	}
	trace, _ := ctx.Value(monitorContextKey{}).(*requestMonitorTrace)
	if trace != nil {
		trace.bodies.append(time.Now(), direction, attempt, channelID, contentType, data)
	}
}

func RecordMonitorWSBody(ctx context.Context, direction string, attempt, channelID int, data []byte) {
	RecordMonitorBody(ctx, direction, attempt, channelID, "application/x-ndjson", data)
	RecordMonitorBody(ctx, direction, attempt, channelID, "application/x-ndjson", []byte("\n"))
}

// RecordMonitorWSBodyByID 用于单次生成结束后写出的最终帧，此时 Gin 上下文已释放。
func RecordMonitorWSBodyByID(id, direction string, data []byte) {
	requestMonitor.mu.Lock()
	var trace *requestMonitorTrace
	for _, candidate := range requestMonitor.rows {
		if candidate != nil && candidate.data.ID == id {
			trace = candidate
			break
		}
	}
	requestMonitor.mu.Unlock()
	if trace != nil {
		trace.bodies.append(time.Now(), direction, 0, 0, "application/x-ndjson", data)
		trace.bodies.append(time.Now(), direction, 0, 0, "application/x-ndjson", []byte("\n"))
	}
}

type monitorRequestBody struct {
	io.ReadCloser
	record      *monitorBodyRecord
	direction   string
	attempt     int
	channelID   int
	contentType string
}

func (b *monitorRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.record.append(time.Now(), b.direction, b.attempt, b.channelID, b.contentType, p[:n])
	return n, err
}
