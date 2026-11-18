package main

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const modelCatalogRefreshInterval = time.Minute

// 缓存仅驻留内存；不同认证、项目及查询参数的目录不能共用。
type modelCatalogCache struct {
	mu         sync.RWMutex
	body       []byte
	status     int
	updated    time.Time
	lastAccess time.Time
	ready      chan struct{}
	header     http.Header
	query      string
}

func (p *proxy) cachedModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		p.forwardHTTP(w, r)
		return
	}
	if r.Header.Get("Authorization") == "" {
		p.forwardHTTP(w, r)
		return
	}
	header := forwardedHeaders(r)
	// 由 Transport 协商解压，缓存始终保存普通 JSON。
	header.Del("Accept-Encoding")
	header.Set("Accept", "application/json")
	encoded, err := common.Marshal(struct {
		Header http.Header
		Query  string
	}{header, r.URL.RawQuery})
	if err != nil {
		http.Error(w, "无法构建模型目录请求", http.StatusInternalServerError)
		return
	}
	key := sha256.Sum256(encoded)
	p.modelsMu.Lock()
	if p.models == nil {
		p.models = make(map[[32]byte]*modelCatalogCache)
	}
	entry := p.models[key]
	if entry == nil {
		entry = &modelCatalogCache{ready: make(chan struct{}), header: header, query: r.URL.RawQuery, lastAccess: time.Now()}
		p.models[key] = entry
		ctx := p.cacheContext
		if ctx == nil {
			ctx = context.Background()
		}
		go p.syncModelCatalog(ctx, key, entry)
	}
	entry.mu.Lock()
	entry.lastAccess = time.Now()
	entry.mu.Unlock()
	p.modelsMu.Unlock()
	select {
	case <-entry.ready:
	case <-r.Context().Done():
		return
	}
	entry.mu.RLock()
	body, status, updated := entry.body, entry.status, entry.updated
	entry.mu.RUnlock()
	w.Header().Set("Cache-Control", "private, no-store")
	// 同步异常只短期保留旧目录，防止撤权后无限期继续提供缓存。
	if len(body) > 0 && time.Since(updated) <= 5*time.Minute {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Retry-After", "60")
	http.Error(w, "模型目录暂不可用，请稍后重试", status)
}

func (p *proxy) syncModelCatalog(ctx context.Context, key [32]byte, entry *modelCatalogCache) {
	defer func() {
		p.modelsMu.Lock()
		if p.models[key] == entry {
			delete(p.models, key)
		}
		p.modelsMu.Unlock()
	}()
	p.refreshModelCatalog(ctx, entry)
	close(entry.ready)
	ticker := time.NewTicker(modelCatalogRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.modelsMu.Lock()
			entry.mu.RLock()
			idle := time.Since(entry.lastAccess) > 10*time.Minute
			entry.mu.RUnlock()
			if idle {
				delete(p.models, key)
			}
			p.modelsMu.Unlock()
			if idle {
				return
			}
			p.refreshModelCatalog(ctx, entry)
		}
	}
}

func (p *proxy) refreshModelCatalog(parent context.Context, entry *modelCatalogCache) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	target := *p.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/codex/models"
	target.RawQuery = entry.query
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return
	}
	request.Header = entry.header.Clone()
	started := time.Now()
	response, err := p.client.Do(request)
	if err != nil {
		p.logf("模型目录同步失败：连接错误，耗时=%s", time.Since(started))
		return
	}
	defer response.Body.Close()
	entry.mu.Lock()
	entry.status = response.StatusCode
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		entry.body = nil
		entry.updated = time.Time{}
	}
	entry.mu.Unlock()
	if response.StatusCode != http.StatusOK {
		p.logf("模型目录同步失败：HTTP %d", response.StatusCode)
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	if err != nil || len(body) > 16*1024*1024 || common.Unmarshal(body, &catalog) != nil || catalog.Models == nil {
		p.logf("模型目录同步失败：响应不完整或格式错误")
		return
	}
	entry.mu.Lock()
	entry.body = body
	entry.updated = time.Now()
	entry.mu.Unlock()
	p.logf("模型目录同步成功：模型=%d 字节=%d 耗时=%s", len(catalog.Models), len(body), time.Since(started))
}
