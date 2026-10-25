let ctx;
let state = null;
let error = "";
function refresh() {
  if (!ctx.filesystem || !ctx.network) {
    error = "当前 Kyvro 不支持文件读取或网络能力，请更新宿主或检查插件权限";
    return;
  }
  try {
    let auth;
    try { auth = JSON.parse(ctx.filesystem.readText("~/.codex/auth.json")); }
    catch (_) { throw new Error("无法读取 Codex 登录态，请先运行 codex login（仅支持 auth.json 登录）"); }
    const tokens = auth && auth.tokens;
    if (!tokens || typeof tokens.access_token !== "string" || !tokens.access_token) {
      throw new Error("未找到 Codex OAuth 登录态，请运行 codex login；API Key 登录不支持订阅额度查询");
    }
    const headers = {Authorization: "Bearer " + tokens.access_token, "User-Agent": "codex-cli"};
    if (typeof tokens.account_id === "string" && tokens.account_id) headers["ChatGPT-Account-Id"] = tokens.account_id;
    let response;
    try { response = ctx.network.request("https://chatgpt.com/backend-api/wham/usage", {headers: headers, followRedirects: false}); }
    catch (_) { throw new Error("Codex 用量请求失败或超时，请稍后重试"); }
    if (response.status === 401 || response.status === 403) throw new Error("Codex 登录已过期或无权限，请在 Codex 中重新登录后刷新");
    if (!response.ok) throw new Error("Codex 用量服务返回 HTTP " + response.status + "，请稍后重试");
    let data;
    try { data = JSON.parse(response.body); } catch (_) { throw new Error("Codex 用量响应格式不受支持"); }
    if (!data || typeof data !== "object" || (!data.rate_limit && !data.credits)) throw new Error("Codex 用量响应格式不受支持");
    // Cache only display fields. Never persist credentials or raw API responses.
    function quota(w) {
      if (w == null) return null;
      if (typeof w.used_percent !== "number" || !Number.isFinite(w.used_percent) || w.used_percent < 0 || w.used_percent > 100) throw new Error("Codex 用量百分比无效");
      return {used_percent: w.used_percent, reset_at: typeof w.reset_at === "number" ? w.reset_at : 0, limit_window_seconds: typeof w.limit_window_seconds === "number" ? w.limit_window_seconds : 0};
    }
    const usage = {
      plan_type: typeof data.plan_type === "string" ? data.plan_type : "未知",
      rate_limit: data.rate_limit ? {primary_window: quota(data.rate_limit.primary_window), secondary_window: quota(data.rate_limit.secondary_window)} : null,
      credits: data.credits ? {unlimited: data.credits.unlimited === true, balance: typeof data.credits.balance === "number" || typeof data.credits.balance === "string" ? data.credits.balance : null} : null
    };
    state = { usage: usage, updated: new Date().toISOString() };
    error = "";
    ctx.storage.set("usage", JSON.stringify(state));
  } catch (_) {
    // Only our sanitized diagnostics reach the UI; raw network/auth errors are discarded.
    error = String(_);
  }
}
module.exports.activate = function (context) {
  ctx = context;
  try { state = JSON.parse(ctx.storage.get("usage") || "null"); } catch (_) { state = null; }
  ctx.background.every(300000, refresh);
};
module.exports.onAction = function (id) {
  if (id !== "codex.usage") return [];
  return [{id: "usage", title: "Codex 用量", subtitle: state ? "Enter 查看 · " + new Date(state.updated).toLocaleTimeString([], {hour: "2-digit", minute: "2-digit"}) + " 更新" : "Enter 查看剩余额度", actions: [{type: "open-view", id: "usage"}]}];
};
function text(value, tone) { return {type: "text", text: value, tone: tone || "default"}; }
function resetCountdown(resetAt) {
  if (!resetAt) return "重置时间未知";
  const minutes = Math.ceil((resetAt * 1000 - Date.now()) / 60000);
  if (minutes <= 0) return "等待额度重置";
  const days = Math.floor(minutes / 1440);
  const hours = Math.floor(minutes % 1440 / 60);
  const rest = minutes % 60;
  if (days) return days + " 天 " + hours + " 小时后重置";
  if (hours) return hours + " 小时 " + rest + " 分后重置";
  return rest + " 分后重置";
}
function windowSummary(window, fallback) {
  const hours = window.limit_window_seconds / 3600;
  const label = hours === 5 ? "会话" : hours === 168 ? "每周" : hours > 0 ? hours + " 小时" : fallback;
  const remaining = Math.round((100 - window.used_percent) * 10) / 10;
  return {type: "progress", title: label + " " + remaining + "% 剩余", text: resetCountdown(window.reset_at), value: remaining, max: 100, tone: remaining <= 10 ? "warning" : "default"};
}
module.exports.renderView = function (id) {
  if (id !== "usage") throw new Error("Unknown view");
  if (!state && !error) refresh();
  const nodes = [];
  if (error) nodes.push(text(error + (state ? "（显示上次成功数据）" : ""), "warning"));
  if (state) {
    const usage = state.usage;
    const limit = usage.rate_limit || {};
    if (limit.primary_window) nodes.push(windowSummary(limit.primary_window, "主额度"));
    if (limit.secondary_window) nodes.push(windowSummary(limit.secondary_window, "次额度"));
    if (!limit.primary_window && !limit.secondary_window) nodes.push(text("暂无额度信息", "muted"));
    if (usage.credits) nodes.push(text(usage.credits.unlimited ? "Credits ∞" : "Credits " + (usage.credits.balance == null ? "未知" : usage.credits.balance), "muted"));
  }
  const footer = [];
  if (state) {
    const stale = Date.now() - new Date(state.updated).getTime() > 600000;
    footer.push(text(new Date(state.updated).toLocaleTimeString([], {hour: "2-digit", minute: "2-digit"}) + " 更新" + (stale ? " · 已过期" : ""), stale ? "warning" : "muted"));
  }
  footer.push({type: "button", text: "刷新", event: "refresh"});
  nodes.push({type: "row", gap: "small", children: footer});
  return {title: "Codex" + (state && state.usage.plan_type ? " · " + state.usage.plan_type : " 用量"), shortcuts: {enter: "refresh"}, root: {type: "column", gap: "medium", children: nodes}};
};
module.exports.onViewEvent = function (id, event) { if (id === "usage" && event === "refresh") refresh(); };
