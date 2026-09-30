# 权限系统

本文档描述 Kyvro 插件权限系统的当前实现（V1）与设计方向。设计原则源自原始需求（[docs/original/docs.md](./original/docs.md)、[docs/original/kyvro-plugin-api.md](./original/kyvro-plugin-api.md)），实现位于 `internal/plugin/permission.go`、`errors.go`、`runtime.go`。插件开发指南见 [plugins.md](./plugins.md)。

## 设计原则

权限系统长期遵循以下原则（源自原始需求）：

```text
Least Privilege
```

插件只获得完成任务所需的最小能力。

```text
Raw Data → System Broker → Structured Capability → Normal Plugin
```

用户原始输入、项目目录、剪贴板等敏感资源由受信任的 System Plugin 转换为结构化能力（Typed Match、Project Handle、Typed Result），普通插件消费结构化接口而非原始资源。

```text
Permission = Which native JS namespaces are exposed
```

权限决定插件能看到哪些 API 命名空间。未授权的 API 直接不注入，而不是全部暴露后每次调用返回 denied。

```text
Structured API > Raw Resource
```

优先 Handles（project_id / file_id）、Typed Events（TypedMatch）、域名白名单网络，而不是 Raw Path、Raw Global Input、`network.*`。

```text
High-sensitive Read + Data Exfiltration = High Risk
```

可以读取高敏感数据的组件，应尽量失去数据外传能力；Policy 必须支持权限组合限制。

```text
Simple Manifest, Complex Runtime
```

Manifest 权限保持少量、简单；Runtime 内部安全边界可以更细（scope、域名、路径白名单）。

---

## 当前实现（V1）

### 权限清单

Manifest `permissions` 声明 `<capability>` 或 `<capability>:<scope>` 两种形式（如 `storage`、`storage:cache`）。宿主按冒号前基名解析能力。

| Capability | 状态 | V1 授权策略 | 运行时行为 |
|-----|-----|-----|-----|
| storage | ✅ 已实现 | 声明即授权 | 注入 `ctx.storage`（同步 string→string KV，bucket `plugin:<id>`） |
| network | ⏳ 已解析未实现 | 暂不授权 | `ctx.network` 不注入；预留调用返回 `CAPABILITY_UNAVAILABLE` |
| filesystem | ⏳ 已解析未实现 | 暂不授权 | 同上 |
| shell | ⏳ 已解析未实现 | 暂不授权 | 同上 |
| clipboard | ⏳ 已解析未实现 | 暂不授权 | 同上 |
| secrets | ⏳ 已解析未实现 | 暂不授权 | 同上 |
| background | ⏳ 已解析未实现 | 暂不授权 | 同上 |
| system | ⏳ 已解析未实现 | 规划 SystemOnly | 规划为 System Plugin 专用，普通插件不可申请 |

未知能力名按拒绝处理（`Require` 时返回 `PERMISSION_DENIED`）。

### 授权与执行语义

权限模型为 default-deny。`PermissionSet.Require` 的三态语义：

```text
implemented + granted          → 允许
known but unimplemented (V1)   → CAPABILITY_UNAVAILABLE
unknown / not granted          → PERMISSION_DENIED
```

- V1 授权策略：manifest 声明 `storage` 即授权（`defaultGrant`，`internal/plugin/permission.go:26`）。
- 宿主预留 `GrantDecision` 回调（`permission.go:23`，注释标明 Reserved for the settings UI）：返回请求权限的授予子集。后续设置界面做 per-plugin 粒度授权时接入该接缝，无需重构。
- scoped 形式：`storage:cache` 这类请求在 base `storage` 被授予时视为已授予；`Granted` 同时支持精确名与基名匹配。

### API 注入与映射

权限决定 `activate(ctx)` 收到的能力面。未实现 / 未授权的命名空间**直接不出现在 `ctx` 上**，插件应 feature-detect（`if (ctx.storage)`），而不是假设存在后捕获错误。

| 权限 | V1 注入的 ctx API | 规划 API（原始需求 `kyvro.*` 命名空间） |
|-----|-----|-----|
| 无（基础接口） | `ctx.log` | `kyvro.runtime`、`kyvro.extensions` |
| storage | `ctx.storage.get/set/delete` | 保持独立命名空间 |
| network | — | `kyvro.network.fetch(url)`，域名白名单 |
| filesystem | — | `kyvro.files.read/write/list`，范围受限 |
| clipboard | — | `kyvro.clipboard.readText/writeText` |
| background | — | `kyvro.background`（持久服务） |
| system | — | `kyvro.system`（仅 System Plugin） |

说明：

- `ctx.log`（宿主日志）不需要权限，属于运行时基础接口（对应原始需求中的 **Extension Management API**），不属于敏感资源访问。
- 插件禁止直接访问宿主高权限 API（原始需求：no direct OS access）。当前 goja 沙箱不提供 `require`，未来引入宿主语言 API（Node/Deno/Bun 原生模块）时同样只允许经 `kyvro.*` 受控命名空间访问原生资源。

### 错误码

错误码定义于 `internal/plugin/errors.go`，每次 manifest 加载、搜索、action 失败都携带 `PluginError{PluginID, Code, Message}`。

| 错误码 | 含义 | V1 是否产生 |
|-----|-----|-----|
| `PERMISSION_DENIED` | 权限未授予 | ✅ |
| `TIMEOUT` | 150ms 搜索 / 2s activate / 5s action 超时 | ✅ |
| `CAPABILITY_UNAVAILABLE` | 宿主版本未提供该能力 | ✅ |
| `INVALID_ARGUMENT` | manifest / 参数 / 返回值非法 | ✅ |
| `PLUGIN_EXCEPTION` | JS 异常或宿主 panic | ✅ |
| `INCOMPATIBLE_VERSION` | schemaVersion / minHostVersion / platform 不兼容 | ✅ |
| `NETWORK_BLOCKED` | 网络请求被域名策略拦截 | ⏳ 预留（spec 保真定义） |
| `HOST_API_ERROR` | 宿主 API 内部错误 | ⏳ 预留（spec 保真定义） |

---

## 设计方向（与原始需求对齐）

以下为原始需求规划的分级与策略模型，当前尚未实现。落地时保持现有 default-deny 语义与错误码契约不变。

### Trust Level

| Level | 说明 | 可申请 | 不可申请 |
|-----|-----|-----|-----|
| Normal | Marketplace 普通插件 | 命令 scope、结构化 Match、结构化 Project API、受限 network / filesystem、Action API | `input.raw_global`、`vault.raw`、`system.unrestricted` |
| Trusted | 官方审核 / 签名 / 特殊授权插件 | `browser.raw_tabs`、`clipboard.read`、`input.selected_text`、`filesystem.project_roots` | `input.raw_global`、`project.scan` |
| System | Kyvro 官方系统级组件 | `input.raw_global`、`project.scan`、`clipboard.raw`、`credential.broker` | （权限组合仍受 Policy 限制） |

System Plugin 仍运行在 Plugin Runtime 中，但要求：官方签名、固定 Publisher、Runtime 校验签名、无法被普通插件伪装或覆盖、默认禁止 Network。典型 System Plugin：Input Matcher（原始输入 → TypedMatch）、Project Scanner（目录扫描 → Project Handle / Metadata）、Clipboard / Browser / Credential Broker。System Plugin 可独立升级、回滚、版本管理，不随 Core 发布节奏。

Trusted Plugin 即使获得高风险 Capability，仍遵循最小权限原则。

### Permission Policy

权限除名称外还应定义申请策略：

| Policy | 含义 |
|-----|-----|
| Normal | 默认，静默拒绝 |
| UserGrantable | 安装 / 设置界面由用户授予 |
| TrustedOnly | 仅 Trusted 及以上 Trust Level 可申请 |
| SystemOnly | 仅 System Plugin 可申请 |

示例规划：

| Permission | Policy |
|---|---|
| `network.github.com` | UserGrantable |
| `filesystem.project` | UserGrantable |
| `clipboard.read` | UserGrantable / Trusted |
| `input.selected_text` | UserGrantable |
| `input.raw_global` | SystemOnly |
| `vault.raw` | SystemOnly |
| `background.execution` | UserGrantable |
| `project.scan` | SystemOnly |

### Capability 优于字符串权限

长期从字符串权限升级为结构化 Capability 声明。示例（原始需求）：

```json
{
  "id": "com.kyvro.github",
  "capabilities": {
    "input": { "scope": "command" },
    "network": { "domains": ["api.github.com"] }
  }
}
```

```json
{
  "id": "com.kyvro.system.input-matcher",
  "capabilities": {
    "input": { "scope": "global", "lifecycle": "realtime" },
    "network": { "enabled": false }
  }
}
```

### Filesystem 分层

不提供单一 `filesystem.read/write`，按范围分层（风险递增）：

```text
filesystem.selected_file.read
filesystem.user_selected_directory.read
filesystem.project.read / write
filesystem.project_roots.scan
filesystem.full_disk.read        ← 高风险，普通插件默认不可申请
```

普通插件优先通过 Project API（结构化 Project Handle）访问项目，而不是自行扫描目录。

### Network 分层

网络权限至少限制到域名，不提供 `network: *`：

```json
{ "network": { "domains": ["api.github.com"] } }
```

进一步可限制方法（GET only / POST）、WebSocket、localhost、私有网段。Remote Search Provider 必须支持 debounce、timeout、cancellation、rate limit，避免每次 keystroke 都外发请求；被域名策略拦截时返回 `NETWORK_BLOCKED`。

### 高风险权限组合

权限不能只单独判断，需考虑组合风险。核心规则：

```text
Marketplace Plugin: input.raw_global + network.* = prohibited
System Input Matcher: input.raw_global ✓ 且 network ✗ / filesystem ✗ / vault ✗ / shell ✗
```

Policy Engine 必须支持权限组合限制：读取高敏感数据的组件应失去数据外传能力。

### Sensitive Input

宿主识别疑似敏感输入（API Key、Access Token、Private Key、JWT、Password-like、Recovery Phrase-like），命中后进入 Sensitive Mode：

```text
Third-party Plugin Dispatch ↓
Telemetry ↓
History Storage ↓
Network Plugin Invocation ↓
```

> Secret-like data should not be broadcast to third-party plugins.

具体策略后续可配置。

---

## 运行时注入流程

当前 V1（`internal/plugin/runtime.go`，与原始需求的注入模型一致）：

```text
Read Manifest (ParseManifest)
   ↓
Verify schema / host version / platform (INCOMPATIBLE_VERSION)
   ↓
Resolve Permissions (ParsePermissions + GrantDecision)
   ↓
Create Sandbox (独立 goja VM + 专用 worker goroutine，无 require)
   ↓
Inject Base APIs（无需权限：ctx.log）
   ↓
Inject Permission APIs（ctx.storage 仅当 storage 被授予）
   ↓
Load Plugin Code (module.exports：onAction / activate)
   ↓
Run activate(ctx)（2s 预算，支持 Promise）
```

只注入已授权命名空间；未实现能力的 API 不存在，而非存在后报错。

---

## 路线图

- **M2**：设置界面接入 `GrantDecision`（per-plugin 权限授予 UI）；实现 `network` API（域名白名单，拦截返回 `NETWORK_BLOCKED`）；实现 `filesystem` API（用户选定目录 scope）
- **M3**：Trust Level 判定与签名校验；Manifest 升级为 Capability 声明；Policy Engine 支持权限组合限制
- **M4+**：System Plugin 体系（Input Matcher / Project Scanner / Credential Broker，独立升级）；Sensitive Mode；Agent Tool（权限继承插件自身，Agent 不能绕过 Permission Engine）
