export const meta = {
  apiVersion: 1,
  key: "openai-image",
  name: "OpenAI 异步生图",
  version: "1.1.1",
  author: { name: "QuantumNous" },
  description: { en: "Background tasks over streaming OpenAI image generation", zh: "下游异步任务，上游 OpenAI 流式生图" },
  submitMode: "background",
  submitResponseTypes: ["sse"],
  // 预设列表用于渠道选择；自定义模型名由渠道配置和 model_mapping 决定。
  models: ["gpt-image-1.5", "gpt-image-2", "gpt-image-2.5", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst"],
  fetchMode: "per_task",
  usageSchema: { images: { type: "number", unit: "count", description: { en: "Image generation unit price", zh: "图片生成单价" } } },
};

function imageCount(request) {
  const n = request.n === undefined ? 1 : request.n;
  if (!Number.isInteger(n) || n < 1 || n > 10) throw new Error("n 必须为 1 到 10 的整数");
  return n;
}

export function buildSubmitRequest(ctx) {
  const request = ctx.requestBody || {};
  if (typeof request.prompt !== "string" || !request.prompt.trim()) throw new Error("prompt 不能为空");
  imageCount(request);
  const body = Object.assign({}, request, { model: ctx.upstreamModel || ctx.model, stream: true });
  delete body.action;
  // 不声明同步 Images 协议，防止接管已有的同步图片渠道。
  return {
    url: ctx.baseUrl.replace(/\/$/, "") + "/v1/images/generations",
    method: "POST",
    responseType: "sse",
    headers: { "Authorization": "Bearer " + ctx.apiKey, "Content-Type": "application/json", "Accept": "text/event-stream" },
    body: body,
    action: "text_to_image",
  };
}

export function parseSubmitEventDelta(ctx, event, previous) {
  const state = previous || { count: 0 };
  state.events = (state.events || 0) + 1;
  if (event.data === "[DONE]") {
    console.log("image_stream done events=" + state.events + " completed=" + state.count);
    if (state.count !== imageCount(ctx.requestBody || {})) throw new Error("图片流未返回全部完整图片");
    return { changes: [], state: state, done: true };
  }
  let value;
  try { value = JSON.parse(event.data); }
  catch (_) {
    console.log("image_stream failure=invalid_json event=" + state.events + " bytes=" + event.data.length);
    throw new Error("图片流事件不是有效 JSON");
  }
  if (!value || typeof value !== "object") {
    console.log("image_stream failure=invalid_event_shape event=" + state.events);
    throw new Error("图片流事件必须是对象");
  }
  const type = value.type || event.event;
  const category = ["image_generation.completed", "image_generation.partial_image", "error", "upstream_error"].includes(type) ? type : "other";
  // 限制预览日志量；不记录原始事件、上游错误消息、URL 或图片数据。
  if (state.events <= 3 || category === "image_generation.completed" || value.error || category === "error" || category === "upstream_error") {
    console.log("image_stream event=" + state.events + " category=" + category + " completed=" + state.count + " has_b64=" + !!value.b64_json + " has_url=" + !!value.url + " has_error=" + !!value.error);
  }
  if (value.error || type === "error" || type === "upstream_error") throw new Error("上游生图失败");
  // 中间预览不持久化，也不作为最终计费图片。
  if (type !== "image_generation.completed") return { changes: [], state: state, done: false };
  const image = {};
  if (typeof value.b64_json === "string" && value.b64_json) image.b64_json = value.b64_json;
  else if (typeof value.url === "string" && /^https?:\/\//.test(value.url)) image.url = value.url;
  else {
    console.log("image_stream failure=missing_image_payload");
    throw new Error("完成事件缺少图片内容");
  }
  const format = value.output_format || (ctx.requestBody || {}).output_format || "png";
  if (!["png", "jpeg", "webp"].includes(format)) {
    console.log("image_stream failure=unsupported_image_format");
    throw new Error("不支持的图片格式");
  }
  image.mime_type = "image/" + format;
  if (typeof value.revised_prompt === "string") image.revised_prompt = value.revised_prompt;
  const changes = state.count ? [] : [{ op: "set", path: [], value: { data: [] } }];
  changes.push({ op: "append", path: ["data"], value: image });
  state.count += 1;
  if (state.count > imageCount(ctx.requestBody || {})) throw new Error("图片数量超过请求数量");
  // 完整图片事件已经包含最终内容，不要求上游额外发送 [DONE]。
  return { changes: changes, state: state, done: state.count === imageCount(ctx.requestBody || {}) };
}

export function parseSubmitResponse(ctx, response) {
  const body = response.body;
  if (!body || !Array.isArray(body.data) || !body.data.length) throw new Error("缺少图片结果");
  return { taskId: ctx.publicTaskId, taskData: body, immediate: { status: "SUCCESS", progress: "100%" } };
}

export function extractUsage(ctx) { return { images: imageCount(ctx.requestBody || {}) }; }
export function extractUsageOnComplete(ctx, result, body) { return { images: body.data.length }; }
export function buildQueryRequest() { throw new Error("后台任务由宿主执行，不查询上游"); }
export function parseTaskResult() { throw new Error("后台任务由宿主完成"); }
export function listArtifacts(task) {
  if (task.status !== "SUCCESS") return [];
  return task.data.data.map((image, index) => ({ key: "image-" + (index + 1), type: "image", mimeType: image.mime_type }));
}
export function buildContentRequest(ctx) {
  const match = /^image-([1-9][0-9]*)$/.exec(ctx.artifactKey);
  const image = match && ctx.data.data[Number(match[1]) - 1];
  if (!image) throw new Error("图片不存在");
  return { url: image.b64_json ? "data:" + image.mime_type + ";base64," + image.b64_json : image.url, method: ctx.clientRequest.method, credentialless: true };
}
