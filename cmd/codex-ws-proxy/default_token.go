package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func localHost(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func (p *proxy) withDefaultToken(next http.Handler, listen, tokenFile string) (http.Handler, error) {
	token := os.Getenv("CODEX_WS_PROXY_DEFAULT_TOKEN")
	if tokenFile != "" {
		f, err := os.Open(tokenFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("令牌文件必须是仅当前用户可读写的普通文件")
		}
		data, err := io.ReadAll(io.LimitReader(f, 8193))
		if err != nil {
			return nil, err
		}
		if len(data) > 8192 {
			return nil, fmt.Errorf("令牌文件过大")
		}
		token = strings.TrimSpace(string(data))
		if token == "" {
			return nil, fmt.Errorf("令牌文件为空")
		}
	}
	if token == "" {
		return next, nil
	}
	if strings.ContainsAny(token, " \t\n\r") {
		return nil, fmt.Errorf("默认令牌不可包含空白字符")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil || !localHost(host) {
		return nil, fmt.Errorf("默认令牌仅支持回环监听地址")
	}
	if p.baseURL.Scheme != "https" && !(p.baseURL.Scheme == "http" && localHost(p.baseURL.Hostname())) {
		return nil, fmt.Errorf("使用默认令牌时，远程上游必须采用 HTTPS/WSS")
	}
	// 带默认凭据的请求只发往已配置的上游。
	p.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if via[0].Header.Get("Authorization") == "Bearer "+token {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return fmt.Errorf("重定向次数过多")
		}
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") && len(r.Header.Values("Authorization")) == 0 {
			peer, _, _ := net.SplitHostPort(r.RemoteAddr)
			if !net.ParseIP(peer).IsLoopback() || !localHost((&url.URL{Host: r.Host}).Hostname()) || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
				http.Error(w, "此请求不能使用本机默认令牌", http.StatusForbidden)
				return
			}
			r.Header.Set("Authorization", "Bearer "+token)
		}
		next.ServeHTTP(w, r)
	}), nil
}
