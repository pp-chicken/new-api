# Task plugin API v1

Task plugins are single-file synchronous ECMAScript modules. The plugin contract
is currently unreleased; [`v1.schema.json`](./v1.schema.json) and
[`v1.d.ts`](./v1.d.ts) are the authoritative v1 contract.

## Contract and lifecycle

Every plugin exports `meta`, `buildSubmitRequest`, `parseSubmitResponse`, and
`parseTaskResult`. A `per_task` plugin also exports `buildQueryRequest`; a
`batch` plugin exports `buildBatchQueryRequest` and `parseBatchResult`.
`meta.author.name` is required and `meta.author.url`, when present, must be an
absolute HTTP(S) URL. This is self-declared attribution; a future marketplace's
verified publisher identity is a separate host-owned record.
Plugins may declare authenticated vendor-native `meta.routes` and claim
host-owned names through `meta.protocols`. Submit and dynamic routes name a
`native` decoder and presenter; query routes name only a presenter. Protocol
bindings are registered once by the host registry, and protocol decoders receive
the host-parsed `body` union plus the pinned model. Shared protocol hooks are
synchronous transformations; Go owns connections and wire framing.

The host selects a channel, invokes the request-building hook, validates the
returned URL against the channel host, performs HTTP, and gives the decoded
response to the matching parse hook. It owns persistence, retries, polling,
billing, and settlement. Plugins only transform data and report usage facts.
See [v1.d.ts](./v1.d.ts) for signatures and
[v1.schema.json](./v1.schema.json) for machine-readable shapes.

Plugins that expose task outputs export `listArtifacts(task)` and
`buildContentRequest(ctx)` together. Artifacts are projected on explicit reads
from persisted `Task.Data`; they are never stored as a second source of truth.
The list contains only stable `key`, `type`, and optional `mimeType` fields.
The content hook receives the selected key, raw decoded task data, the explicit
private upstream task id, the producer plugin version, channel authentication,
and a safe client Range/conditional-header subset. Its URL and headers exist
only for that proxy request.

When a Responses observation reaches persisted `SUCCESS`, the host also runs
the pinned plugin's `listArtifacts` and injects a read-only
`ctx.artifacts[key] = {key, type, mimeType?, url}` map into `renderEvents` or
`renderFinal`. Each `url` is a long-lived host-signed capability URL, never the
provider URL from `Task.Data`. Nonterminal and failed tasks receive no artifact
map. Capability construction or rendering failure fails only that Responses
observation; it cannot change the task, billing settlement, or refunds.
The absolute URL uses `TaskPublicAddress`, falling back only to
`ServerAddress`; multi-node deployments must share the effective
`CRYPTO_SECRET`.
Dashboard artifact reads return each `content_url` (or the legacy
`legacy_content_url`) directly, without a temporary URL exchange. Capability
generation and verification are stateless and have no expiry; after
verification the host still loads the task, owner, and plugin needed to serve
the artifact. Rotating `CRYPTO_SECRET` invalidates issued URLs. The `access`
query is redacted before request logging.
Deployment boundaries and concurrency environment variables are documented in
[v1.md](./v1.md#generic-task-management-api).

The host requires `protocols.openai_video.render` to return a JSON object and
preserves provider extensions, including output URLs and metadata. Plugins may
return `task.data` to expose the latest persisted upstream snapshot. The host
still overwrites `id`, `object`, `model`, `status`, `progress`, `created_at`, and
`completed_at`, and removes legacy `task_id`. This is field-level projection,
not byte-for-byte HTTP forwarding: known private task IDs are replaced before
rendering, polling may redact inline video data, and retrieval reads the latest
saved snapshot rather than issuing a new upstream request. Artifact content
endpoints remain available for proxied downloads.

Provider-authenticated content URLs must use the channel base host or a
plugin-declared `meta.allowedHosts` entry. A public dynamic CDN URL may instead
set `credentialless: true`; the host then permits only GET/HEAD with no
plugin-supplied headers or body and applies SSRF checks to the initial URL and
every redirect.

Registry publication is generation-atomic. A request pins one plugin generation
for its full lifetime, while background polling may use a later active plugin
version. New versions must continue parsing responses for in-flight tasks.
Root administrators can inspect the local node with
`GET /api/plugin/task/runtime/status`. The response includes the node-local generation,
a deterministic revision of the active database overrides, the latest rebuild
outcome, and plugin-level compile or routing errors. Generation numbers are
local to a node; compare database revisions when diagnosing rollout lag between
nodes. If the database snapshot is temporarily unavailable, the endpoint keeps
serving node-local state and the last known revision with `database_error` set.

For live diagnosis, start the process with `DEBUG=true` and filter logs on
`task_plugin`. Plugin registry, routing, endpoint ownership, channel selection,
submit durability, polling adapters, and protocol observation emit safe
key/value lifecycle events. Request-context events carry the request id;
scheduled, background, and context-less work is labeled `SYSTEM`. Plugin
`console.log` output is also forwarded in DEBUG mode. Hook-time output is
prefixed with plugin key/version; module-initialization output may have an empty
identity during initial upload validation. Do not print credentials, headers,
request bodies, upstream payloads, or private URLs from plugin code; free-form
console output cannot be redacted by the host.

## Fixtures and dry runs

A fixture case is `{name?, hook, member?, args, expected?, expectedError?}`.
Keep deterministic cases for every exported hook, its main error branch, batch
behavior, renderers, usage, and content requests. Run a fixture locally with:

```sh
new-api plugin lint plugin.js
new-api plugin test plugin.js --fixture golden.json
```

Root administrators can open the plugin detail Sandbox tab, choose a hook, and
submit an `args` JSON array. `POST /api/plugin/task/:key/dryrun` compiles the
active database source or factory source in a temporary registry and invokes
only that synchronous function. Dry runs never execute a request descriptor and
therefore never contact an upstream service.

## Upload and release

Upload from the root-only task plugin page or `POST /api/plugin/task` with
`{"source":"...","remark":"..."}`. The server compiles the module, validates
v1 metadata and required exports, and rejects invalid source before saving it.
Use semantic plugin versions. Reusing a key/version with different source is
rejected; activate or roll back a stored version through the management page.

For a third-party platform, create a channel of type `Task Plugin`, select the
plugin key, provide an explicit base URL, and configure models. Clients may use
the plugin's declared native routes. The generic management surface remains
`POST /v1/tasks/:pluginKey`, `GET /v1/tasks/:taskId`, and
`GET /v1/tasks/:taskId/artifacts` plus
`GET|HEAD /v1/tasks/:taskId/artifacts/:key/content`.

## Security boundary

Plugins have no global `fetch`, filesystem, `require`, imports, async
functions, code compiled from strings (`eval` and the `Function` constructors
throw an `EvalError`), or environment access. A few driver hooks may read
upstream data with [`utils.fetch`](./v1.md#reading-upstream-data-with-utilsfetch):
`GET` or `HEAD` only, to the hosts a request descriptor may address. The host
limits execution time, concurrency, input size, allowed request hosts, and
resolves OAuth credentials outside JavaScript.
Multipart files enter JavaScript only as opaque references.

This is not a hard memory-isolation boundary. A plugin sees data needed for the
current request and can influence an authenticated upstream request. Uploading a
plugin is an administrator-level trust decision equivalent to configuring a
channel credential. Review source and version diffs before activation. Never run
untrusted plugins merely because they compile.

Usage hooks may return facts such as seconds, resolution, or upstream units, but
must never calculate prices or attempt quota settlement. The host owns all
pricing and clamps billing conversions.

## 后台流式生图

内置 `openai-image` 插件通过 `POST /v1/tasks/openai-image` 接收 JSON 图片请求，
预设模型包括 `gpt-image-1.5`、`gpt-image-2`、`gpt-image-2.5`、
`gpt-image-2.5-flare` 和 `gpt-image-2.5-sunburst`。创建“任务插件”渠道，选择此插件，填写上游站点
Base URL（例如 `https://provider.example`，不要重复添加 `/v1`）、密钥与模型，并配置按张价格
或基于 `u("images")` 的任务用量表达式。

预设模型列表不是调用白名单。可在渠道模型列表中手动添加自定义模型名，并为该名称配置价格和令牌访问权限。
不配置模型映射时，自定义名称原样发送给上游；配置 `model_mapping`（例如
`{"my-image":"gpt-image-2.5-flare"}`）后，客户端请求 `my-image`，上游收到
`gpt-image-2.5-flare`，任务展示与计费仍使用客户端名称。上游自定义部署名称也可作为映射目标。
以上名称都要求上游实际提供对应模型，并支持下述 Images SSE 完成事件；插件不推断上游模型能力。

提交时使用 `{"model":"gpt-image-2.5-sunburst","prompt":"一只猫","n":1}`。
宿主预扣额度并保存任务后立即返回 `task_id`，后台向上游 `/v1/images/generations`
发送 `stream: true`。使用 `GET /v1/tasks/:taskId` 查询；`SUCCESS` 后通过
`GET /v1/tasks/:taskId/artifacts` 获取图片内容链接。查询和下载沿用现有用户隔离和令牌认证。

上游必须返回 OpenAI Images SSE 的 `image_generation.completed` 事件，携带
`b64_json` 或 HTTP(S) `url`；预览事件不保存。此插件只处理 JSON 文生图，不接管同步图片接口。
单任务最终 JSON 图片结果最多 32 MiB，请求体最多 4 MiB。

`submitMode: "background"` 仅允许通用任务接口及 `per_task` 模式，不可声明
`routes` 或 `protocols`。`TASK_BACKGROUND_CONCURRENCY` 默认 8，表示每进程同时运行上限；
超出返回 429。`TASK_BACKGROUND_TIMEOUT_SECONDS` 默认 900 秒；上游 SSE 的空闲读取仍受
`STREAMING_TIMEOUT` 约束，HTTP 总时长仍受 `RELAY_TIMEOUT` 约束。

客户端断开不会取消已落库任务；已完成结果保存在现有任务数据库中。节点重启会中断正在执行的
上游流，宿主不会自动重发生图。遗留任务在截止时间后由任务轮询器标记失败并按现有规则退款，
因此需保持异步任务轮询启用。上游已经发生的费用可能无法撤销。

### 后台生图诊断

宿主在后台流结束时输出 `background_image_stream` 摘要，失败为 WARN、成功为 INFO，
包含任务 ID、HTTP 状态、响应类型分类、失败阶段、事件数、读取字节数及耗时。
`content_type` 表示响应不是 SSE，`eof_before_completion` 表示完成事件前流已结束，
`plugin_hook` 表示插件事件校验失败，`accumulation` 表示结果累积校验失败。
`reason` 进一步区分空闲超时、无效 JSON、上游错误事件和图片字段缺失等固定类别。
日志不包含请求体、响应体、原始异常、凭证或图片内容。

`openai-image` v1.1.1 还通过 `console.log` 输出有数量限制的事件摘要；该日志遵循宿主
`DEBUG=true` 开关。通常先查看无需 DEBUG 的宿主摘要；全局 DEBUG 会同时启用其他模块的调试日志。
