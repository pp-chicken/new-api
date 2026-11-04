# Codex WebSocket 本机代理

这个小程序给 Codex 提供本机的 OpenAI Responses 地址，并把请求转发到 `new-api` 的 `/v1/responses` WebSocket。这样 Codex 即使选择普通 HTTP 请求，代理到上游仍然只使用 WebSocket。

## 启动

在 `new-api` 目录执行：

```bash
go run ./cmd/codex-ws-proxy \
  -listen 127.0.0.1:28080 \
  -target wss://ai.jizw.net/v1
```

上面两个参数就是默认值，也可以通过 `CODEX_WS_PROXY_LISTEN` 和 `NEW_API_RESPONSES_WS` 环境变量覆盖。`-target` 支持 `http(s)://host/v1`、`ws(s)://host/v1`，也兼容直接填写 `/v1/responses`。

需要排查 Codex 请求时，增加 `-verbose`：

```bash
go run ./cmd/codex-ws-proxy -verbose
```

详细日志使用 `ws_id` 关联同一条连接，时间精度为微秒。`client` 指 Codex 到代理的一端，`upstream` 指代理到 New API 的一端。

- `connection_started`：连接开始，`mode=websocket` 为长连接转发，`mode=http` 为单次 HTTP 转换。
- `dial_finished`：连接 New API 的耗时、握手状态，以及 `upstream_request_id`。优先读取 New API 的 `X-Oneapi-Request-Id`，便于查找请求监控中的父连接；兼容 `X-Request-Id`。
- `first_message`、`message_received`：每一端首条业务消息的时间、消息序号、大小与相邻消息间隔 `gap_ms`。
- `write_started`、`message_sent`：消息发送的起止与耗时；结束时汇总两端成功收发的消息数和字节数。
- `connection_closing`：最先触发关闭的一端、读写阶段、原始关闭码与错误。清理导致的另一端错误不会覆盖这个原因。
- `close_sent`、`close_write_failed`：向另一端传递的关闭码与发送结果。
- `connection_finished`：连接持续时间（`elapsed_ms`）以及两端收发汇总。

日志不输出消息正文、提示词或鉴权请求头；关闭原因属于对端提供的诊断文本，会保留在日志中。业务消息计数不包含 Ping/Pong。代理保持默认的本段心跳回复，不把心跳转发到另一段，也不额外增加空闲超时或自动重连。

WebSocket 模式允许在同一连接上连续发送多次请求。正常的 Close 帧会保留合法关闭码和原因传给另一端；网络异常等无法传递的关闭状态会转换为 `1011`，原始错误仍记录在日志中。HTTP 转换模式每次请求新建连接，响应结束后发送正常关闭帧再关闭连接。

## 实际链路测速

先部署包含本次修改的 **New API 服务端**。新接口为 `GET /v1/network-test`，通过 WebSocket 收发测试数据，沿用现有 API 令牌认证。入口反向代理需要允许这个路径进行 WebSocket Upgrade。

在代理所在电脑上运行：

```bash
./build/codex-ws-proxy -network-test
```

测速使用 `-target` / `NEW_API_RESPONSES_WS` 配置的目标，默认仍是 `wss://ai.jizw.net/v1`。认证优先读取 `CODEX_WS_PROXY_API_KEY`，未设置时读取 `OPENAI_API_KEY`。这里使用 **New API 的访问令牌**，不自动读取 Codex 登录文件，也不把令牌放进 URL 或命令行参数。

如果尚未设置令牌，可以在 macOS 默认的 zsh 中隐藏输入：

```zsh
read -rs 'CODEX_WS_PROXY_API_KEY?请输入 New API 令牌：'
export CODEX_WS_PROXY_API_KEY
printf '\n'
./build/codex-ws-proxy -network-test
unset CODEX_WS_PROXY_API_KEY
```

默认上传和下载各 **1 MiB**，各阶段超时 **60 秒**。可以复现日志中约 249 KB 的请求大小，也可以增加数据量：

```bash
./build/codex-ws-proxy -network-test -test-bytes 249027
./build/codex-ws-proxy -network-test -test-bytes 8388608 -test-timeout 120s
./build/codex-ws-proxy -network-test -target wss://example.com/v1
```

测速完成后退出，不开启监听端口，也不会重启现有代理服务。`-network-test` 不能与安装、卸载、状态查询参数同时使用。失败时以非零状态退出，不自动重试。

输出包括：

- 建连总耗时、DNS+TCP、TLS 和 WebSocket Upgrade 耗时，以及 New API 请求 ID。DNS 和 TCP 合并统计；本机明文测试没有 TLS。
- 单次应用层往返 RTT，用于观察空载时一次小消息的往返时间。
- 上传本机写入耗时，以及收到服务端完整接收确认后的有效上传速度。
- 下载完整接收并校验后的有效下载速度；两个方向同时显示 MiB/s 和 Mbps。
- 失败阶段。旧服务端、认证失败、限流、超时、连接中断、字节数或 SHA-256 不匹配均不会被报告为测速成功；已完成的方向仍会输出结果。

测试使用新连接和随机二进制数据，关闭 WebSocket 压缩。上传计时包含服务端确认的往返，下载计时包含请求发送到收到全部数据及校验摘要，两个方向都包含数据生成/校验的开销。因此输出是这次样本的**应用层有效吞吐**，小样本会明显受 RTT 影响，不等于网卡峰值带宽。

覆盖范围是 **运行代理的电脑 → 配置目标的入口反向代理/CDN（如有）→ New API**。测试不进入 CPA，也不调用模型或产生模型计费。连接方式沿用当前代理的直连行为，不读取 `HTTP_PROXY` / `HTTPS_PROXY`；系统 VPN、透明网络代理仍可能影响实际链路。

服务端约束：每方向 64 KiB～32 MiB，每用户每 60 秒最多 3 次尝试；配置 Redis 时复用共享限流，否则在实例内限流。每个实例最多同时 8 条测速连接，同用户在该实例最多 1 条。控制消息限 128 字节，上传和下载阶段分别最多 120 秒，连接总时长最多 270 秒。数据流式处理，不保存正文。只有正常启用、未过期且通过 IP/用户状态检查的 API 令牌才能进入测速。

远程目标要求 HTTPS/WSS 并验证证书；仅本机回环地址允许 HTTP/WS。服务端使用默认同源 Origin 校验并在 Upgrade 前完成认证；无 Origin 的命令行客户端可正常使用。公网 TLS 终止与路径转发需要由部署入口提供。

安全实现参考 [OWASP ASVS 5.0.0](https://asvs.owasp.org/)、[Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)、[Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) 和 [WebSocket Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security_Cheat_Sheet.html)。定向回归覆盖认证拒绝、Origin、频率/并发、大小/时间边界、数据完整性和传输失败；这不表示对整个项目进行了 ASVS 认证，公网部署仍需验证实际入口的 TLS 和 WebSocket 路由。

## macOS 后台服务

可以把代理注册为当前用户的 `launchd` 服务。先构建二进制，然后执行：

```bash
./build/codex-ws-proxy -install-service -verbose
```

服务会写入 `~/Library/LaunchAgents/com.codex.ws-proxy.plist`，登录后自动启动，异常退出会自动重启。日志写入二进制旁边的 `logs/` 目录。

管理命令：

```bash
./build/codex-ws-proxy -service-status
./build/codex-ws-proxy -uninstall-service
```

安装服务时也可以显式覆盖监听和上游地址：

```bash
./build/codex-ws-proxy \
  -install-service \
  -listen 127.0.0.1:28080 \
  -target wss://ai.jizw.net/v1 \
  -verbose
```

然后把 Codex 的 API Base URL 设置为 `http://127.0.0.1:28080/v1`，API Key 仍使用 new-api 的访问令牌。代理会转发 `Authorization`、`User-Agent`、`OpenAI-Organization` 和 `OpenAI-Project` 请求头。

代理支持以下入口：

- `POST /v1/responses`：接收普通 Responses JSON，通过上游 WebSocket 发送 `response.create`；`stream: true` 时转换为 SSE，普通请求则返回最终 `response` 对象。
- `GET /v1/responses` 的 WebSocket Upgrade：双向透传 WebSocket 消息，适合 Codex 本身使用 Responses WebSocket 的情况。
- 其他 `/v1/*` 路由：按原 HTTP 方法和请求体转发到上游，例如 `/v1/models`、`/v1/responses/{id}`、`/v1/responses/{id}/cancel`、`/v1/responses/compact`。

健康检查地址为 `GET /healthz`。

## 构建

```bash
go build -o ./build/codex-ws-proxy ./cmd/codex-ws-proxy
```

代理只监听回环地址是默认行为；如果改成其他监听地址，请自行确保端口不会暴露给不可信网络。
