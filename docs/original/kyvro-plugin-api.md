# Kyvro Plugin API 设计

## 1. 目标

Kyvro Plugin API 分成两类完全不同的接口：

1. **Native Capability API**
   - 对应插件权限。
   - 用于访问 Kyvro 提供的原生能力。
   - 例如文件、网络、剪贴板、后台服务、系统能力。

2. **Extension Management API**
   - 用于插件向 Kyvro 注册、反注册扩展。
   - 例如 Command、Search Provider、Match Handler、Action Provider、Agent Tool。
   - 属于插件运行时基础能力，不视为高权限能力。

核心原则：

> `extensions` 决定插件可以为 Kyvro 增加什么能力。  
> `Native Capability API` 决定插件可以访问哪些系统资源。

两者必须严格分离。

---

## 2. 顶层 JS API

统一通过：

```js
kyvro
```

暴露 API。

建议第一版结构：

```text
kyvro
├── runtime
├── extensions
├── files
├── network
├── clipboard
├── background
└── system
```

其中：

```text
runtime
extensions
```

属于 Plugin Runtime 基础接口。

以下接口属于权限接口：

```text
files
network
clipboard
background
system
```

---

## 3. Native Capability API

Native Capability API 与插件 Manifest 权限对应。

建议第一版只保留少量权限：

```text
files
network
clipboard
background
system
```

不要一开始设计几十个细粒度权限。

核心原则：

> Manifest 权限保持简单，Runtime 内部安全边界可以更细。

### 3.1 files

Manifest：

```json
{
  "permissions": ["files"]
}
```

插件获得：

```js
kyvro.files
```

例如：

```js
await kyvro.files.read(...)
await kyvro.files.write(...)
await kyvro.files.list(...)
```

`files` 不代表全磁盘权限。Runtime 可以在内部限制为用户选择的文件、目录、Project Handle 对应目录或临时授权范围。

### 3.2 network

Manifest：

```json
{
  "permissions": ["network"]
}
```

插件获得：

```js
kyvro.network
```

例如：

```js
const response = await kyvro.network.fetch("https://api.github.com/repos/...");
```

插件不应直接绕过 Runtime 使用宿主原始网络 API。

### 3.3 clipboard

Manifest：

```json
{
  "permissions": ["clipboard"]
}
```

插件获得：

```js
kyvro.clipboard
```

例如：

```js
const text = await kyvro.clipboard.readText();
await kyvro.clipboard.writeText("hello");
```

### 3.4 background

Manifest：

```json
{
  "permissions": ["background"]
}
```

插件获得：

```js
kyvro.background
```

主要用于 Persistent Background Service、Watcher、Indexer、Sync 等长期能力。

### 3.5 system

`system` 是特殊权限，普通 Marketplace Plugin 不允许直接申请。

主要用于官方 System Plugin，例如：

```text
Input Matcher
Project Scanner
Clipboard Broker
Browser Broker
Credential Broker
```

Manifest：

```json
{
  "permissions": ["system"]
}
```

插件获得：

```js
kyvro.system
```

`system` 权限要求：

```text
官方签名
可信 Publisher
Runtime 校验
普通插件无法伪造
```

---

## 4. API 暴露方式

权限决定对应 JS Namespace 是否存在。

例如插件声明：

```json
{
  "permissions": ["files", "network"]
}
```

Runtime 注入：

```js
kyvro.files
kyvro.network
```

但不注入：

```js
kyvro.clipboard
kyvro.background
kyvro.system
```

推荐：

> 没有权限的 API 不暴露，而不是全部暴露后每次调用再返回 Permission Denied。

---

## 5. 禁止插件直接访问宿主能力

为了让权限系统真正有效，插件不能直接使用宿主语言提供的高权限 API。

例如禁止：

```js
require("fs")
require("child_process")
require("net")
```

也不应直接获得：

```text
Node.js native APIs
Deno.*
Bun.*
```

插件只能通过：

```text
kyvro.files
kyvro.network
kyvro.clipboard
kyvro.background
kyvro.system
```

访问原生资源。

---

## 6. Extension Management API

所有插件默认可以访问：

```js
kyvro.extensions
```

它不需要 Manifest 权限。

基础接口：

```js
kyvro.extensions.register(...)
kyvro.extensions.unregister(...)
kyvro.extensions.unregisterAll(...)
```

职责：

```text
注册 Extension
反注册 Extension
查询当前插件注册状态
```

---

## 7. Extension 类型

当前建议支持：

```text
command
match_handler
search_provider
action_provider
background_service
agent_tool
```

一个 Plugin Package 可以注册多个 Extension。

---

## 8. 注册 Command

```js
kyvro.extensions.register({
  type: "command",
  id: "uuid",
  title: "UUID",
  keywords: ["uuid", "guid"],

  async handler(ctx) {
    return {
      title: crypto.randomUUID()
    };
  }
});
```

用户输入 `uuid` 后，Kyvro Router 命中该 Command 才执行 Handler。

---

## 9. 注册 Match Handler

System Input Matcher 将原始输入转换成 Typed Match。

例如：

```text
1 + 2 * 3
```

产生：

```json
{
  "type": "math.expression",
  "value": "1 + 2 * 3",
  "confidence": 1.0
}
```

Calculator Plugin：

```js
kyvro.extensions.register({
  type: "match_handler",
  id: "calculator",
  handles: ["math.expression"],

  async handler(match) {
    return {
      title: evaluate(match.value)
    };
  }
});
```

Calculator Plugin 不需要读取 `raw_global`，只消费结构化 Match。

---

## 10. 注册 Search Provider

```js
kyvro.extensions.register({
  type: "search_provider",
  id: "github-search",
  keywords: ["github", "repo", "repository"],

  async search(query, ctx) {
    return [];
  }
});
```

Search Provider 不应默认收到每一个用户输入。

正确流程：

```text
Input
  ↓
Router
  ↓
Candidate Selection
  ↓
Relevant Search Providers
  ↓
search()
```

---

## 11. 注册 Action Provider

```js
kyvro.extensions.register({
  type: "action_provider",
  id: "project-actions",
  supports: ["project"],

  async getActions(result) {
    return [
      { id: "open", title: "Open Project" },
      { id: "terminal", title: "Open Terminal" }
    ];
  }
});
```

Action Provider 应优先消费 `Result Handle`，例如：

```text
project_id
file_id
tab_id
```

而不是直接获得原始底层路径。

---

## 12. 注册 Background Service

Background Service 必须拥有：

```text
background
```

权限。

```js
kyvro.extensions.register({
  type: "background_service",
  id: "project-watcher",

  async start(ctx) {},
  async stop(ctx) {}
});
```

Runtime 负责启动、停止、异常恢复、资源限制、插件禁用时清理和退出时停止。

---

## 13. 注册 Agent Tool

未来 Kyvro Agent 可以通过 Agent Tool 使用插件能力。

```js
kyvro.extensions.register({
  type: "agent_tool",
  id: "github.search_pr",
  description: "Search GitHub pull requests",

  inputSchema: {
    type: "object",
    properties: {
      query: { type: "string" }
    }
  },

  async execute(input) {
    return {};
  }
});
```

Agent Tool 的权限仍继承插件自身权限，Agent 不能绕过 Permission Engine。

---

## 14. unregister

动态删除某个 Extension：

```js
kyvro.extensions.unregister("uuid");
```

删除当前插件全部动态注册：

```js
kyvro.extensions.unregisterAll();
```

但 Runtime 不应依赖插件主动调用 `unregister()` 完成生命周期清理。

---

## 15. Runtime 自动生命周期管理

Kyvro Runtime 必须跟踪：

```text
plugin_id
    ↓
registered extensions[]
```

当发生：

```text
Plugin Disable
Plugin Upgrade
Plugin Crash
Plugin Uninstall
Kyvro Shutdown
```

Runtime 自动：

```text
Unregister All Extensions
Release Resources
Stop Background Services
Cancel Pending Tasks
```

---

## 16. runtime API

增加一个无需权限的只读接口：

```js
kyvro.runtime
```

例如：

```js
kyvro.runtime.pluginId
kyvro.runtime.pluginVersion
kyvro.runtime.kyvroVersion
kyvro.runtime.platform
kyvro.runtime.locale
```

`runtime` 只提供运行环境信息，不允许访问敏感系统资源。

---

## 17. Manifest 示例

### Calculator

```json
{
  "id": "com.kyvro.calculator",
  "name": "Calculator",
  "version": "1.0.0",
  "permissions": []
}
```

### GitHub

```json
{
  "id": "com.kyvro.github",
  "name": "GitHub",
  "version": "1.0.0",
  "permissions": ["network"]
}
```

### Project Utility

```json
{
  "id": "com.example.project-tools",
  "name": "Project Tools",
  "version": "1.0.0",
  "permissions": ["files"]
}
```

### System Project Scanner

```json
{
  "id": "com.kyvro.system.project-scanner",
  "name": "Project Scanner",
  "version": "1.0.0",
  "permissions": ["files", "background", "system"]
}
```

---

## 18. 权限与 API 映射

| Permission | JS API |
|---|---|
| 无 | `kyvro.runtime` |
| 无 | `kyvro.extensions` |
| `files` | `kyvro.files` |
| `network` | `kyvro.network` |
| `clipboard` | `kyvro.clipboard` |
| `background` | `kyvro.background` |
| `system` | `kyvro.system` |

第一版不要过早增加：

```text
files.read
files.write
network.http
network.websocket
clipboard.read
clipboard.write
```

这类 Manifest 权限。

如果需要更细安全控制，由 Runtime 内部处理 Scope。

---

## 19. Runtime 注入流程

```text
Read Manifest
   ↓
Verify Plugin
   ↓
Resolve Trust Level
   ↓
Resolve Permissions
   ↓
Create Sandbox
   ↓
Inject Base APIs
   ├── kyvro.runtime
   └── kyvro.extensions
   ↓
Inject Permission APIs
   ├── kyvro.files
   ├── kyvro.network
   ├── kyvro.clipboard
   ├── kyvro.background
   └── kyvro.system
   ↓
Load Plugin Code
```

只注入已授权 Namespace。

---

## 20. 推荐架构

```text
                       Plugin JavaScript
                              │
                 ┌────────────┴────────────┐
                 │                         │
          Extension API              Native APIs
                 │                         │
      kyvro.extensions.*             kyvro.files.*
                                    kyvro.network.*
                                    kyvro.clipboard.*
                                    kyvro.background.*
                                    kyvro.system.*
                 │                         │
                 └────────────┬────────────┘
                              ↓
                        Plugin Runtime
                              ↓
                       Permission Engine
                              ↓
                         Native Core
```

---

## 21. 设计原则

### Principle 1

```text
Extension API
!=
Native Capability API
```

### Principle 2

```text
Permission
=
Which native JS namespaces are exposed
```

### Principle 3

```text
Simple Manifest
Complex Runtime
```

### Principle 4

```text
No direct OS access
```

### Principle 5

```text
Runtime owns lifecycle
```

### Principle 6

```text
Structured API > Raw Resource
```

优先：

```text
ProjectHandle
TypedMatch
ResultHandle
```

而不是：

```text
Raw Path
Raw Input
Arbitrary Shell
```

---

## 22. 第一版建议

第一版实现：

```text
kyvro.runtime

kyvro.extensions.register
kyvro.extensions.unregister
kyvro.extensions.unregisterAll

kyvro.files
kyvro.network
kyvro.clipboard
kyvro.background
kyvro.system
```

Extension 第一批支持：

```text
command
match_handler
search_provider
action_provider
background_service
```

`agent_tool` 可以提前保留类型定义，但等 Agent 阶段再正式开放。

整体结构：

```text
插件向上：
extensions.register()

插件向下：
Permission-controlled Native API
```

后续新增能力时，不需要改变整体架构。
