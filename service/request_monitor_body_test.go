package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorBodiesRetentionAndBudget(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	t.Run("正文截断不丢失实际字节计数且支持二进制", func(t *testing.T) {
		store := &monitorBodyStore{budget: 64, limit: 4}
		record := store.start(now.Add(RequestMonitorRetention))
		record.append(now, "request", 0, 0, "application/json", []byte("你好吗"))
		record.append(now, "response", 0, 0, "application/octet-stream", []byte{0xff, 0xfe})
		record.append(now, "upstream_response", 1, 5, "application/octet-stream", []byte{'a', 'b', 'c', 0xff, 'd'})
		bodies, reason := record.snapshot(now)
		require.Len(t, bodies, 3)
		assert.Empty(t, reason)
		assert.Equal(t, "你", bodies[0].Content)
		assert.Equal(t, int64(9), bodies[0].TotalBytes)
		assert.True(t, bodies[0].Truncated)
		assert.Equal(t, "utf-8", bodies[0].Encoding)
		assert.Equal(t, "//4=", bodies[1].Content)
		assert.Equal(t, "base64", bodies[1].Encoding)
		assert.Equal(t, "YWJj/w==", bodies[2].Content)
		assert.Equal(t, "base64", bodies[2].Encoding)
		bodies[0].Content = "changed"
		again, _ := record.snapshot(now)
		assert.Equal(t, "你", again[0].Content)
	})
	t.Run("到期清除正文且运行中的请求不能重新写入", func(t *testing.T) {
		store := &monitorBodyStore{budget: 64, limit: 16}
		expires := now.Add(RequestMonitorRetention)
		record := store.start(expires)
		record.append(now, "request", 0, 0, "text/plain", []byte("private body"))
		bodies, reason := record.snapshot(expires.Add(-time.Nanosecond))
		require.Len(t, bodies, 1)
		assert.Empty(t, reason)
		record.append(expires, "response", 0, 0, "text/plain", []byte("late body"))
		bodies, reason = record.snapshot(expires)
		assert.Empty(t, bodies)
		assert.Equal(t, "expired", reason)
		assert.Zero(t, store.bytes)
		assert.Zero(t, store.records.Len())
	})
	t.Run("总预算优先淘汰旧正文并禁止旧请求复活", func(t *testing.T) {
		store := &monitorBodyStore{budget: 10, limit: 10}
		old := store.start(now.Add(RequestMonitorRetention))
		old.append(now, "request", 0, 0, "text/plain", []byte("123456"))
		latest := store.start(now.Add(RequestMonitorRetention))
		latest.append(now, "request", 0, 0, "text/plain", []byte("abcdef"))
		bodies, reason := old.snapshot(now)
		assert.Empty(t, bodies)
		assert.Equal(t, "evicted", reason)
		old.append(now, "response", 0, 0, "text/plain", []byte("late"))
		assert.Equal(t, 6, store.bytes)
		bodies, reason = latest.snapshot(now)
		require.Len(t, bodies, 1)
		assert.Equal(t, "abcdef", bodies[0].Content)
		assert.Empty(t, reason)
	})
	t.Run("后台清理同时删除元数据和正文", func(t *testing.T) {
		store := &monitorBodyStore{budget: 64, limit: 16}
		expires := now.Add(RequestMonitorRetention)
		trace := &requestMonitorTrace{data: MonitoredRequest{ID: "expired", ExpiresAt: expires}, bodies: store.start(expires)}
		trace.bodies.append(now, "request", 0, 0, "text/plain", []byte("private body"))
		monitor := &requestMonitorStore{}
		monitor.rows[0] = trace
		monitor.expire(expires)
		assert.Nil(t, monitor.rows[0])
		assert.Zero(t, store.bytes)
	})
}
