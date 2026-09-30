# Kyvro Plugin & Permission System

## 1. 目标

Kyvro 的插件系统需要同时满足以下目标：

- Core 尽可能小，具体能力尽量插件化。
- 官方能力与第三方能力使用同一套插件运行时。
- 敏感原始数据不直接暴露给普通第三方插件。
- 插件只获得完成任务所需的最小权限。
- 插件数量增加时，不显著影响搜索性能。
- 插件可以独立安装、升级、禁用、回滚。
- 后续可以平滑扩展到 Agent、Workflow、Marketplace。

核心原则：

> Raw data is a system capability. Normal plugins should consume structured capabilities, not unrestricted raw resources.

也就是说：

- 用户原始输入由受信任的 System Plugin 处理。
- 原始项目目录由受信任的 Project Scanner 处理。
- 普通插件优先接收结构化事件、Project Handle、Typed Match，而不是直接读取所有原始数据。

---

## 2. 插件包与插件类型

一个 Plugin Package 可以同时注册多个 Extension。

例如 GitHub Plugin 可以同时提供：

- Command
- Search Provider
- Action Provider
- Agent Tool

因此：

> Plugin Package != Plugin Type

示例：

```json
{
  "id": "com.kyvro.github",
  "name": "GitHub",
  "extensions": [
    {
      "type": "command"
    },
    {
      "type": "search_provider"
    },
    {
      "type": "action_provider"
    },
    {
      "type": "agent_tool"
    }
  ]
}
```

---

# 3. 插件分类

Kyvro 第一阶段定义 6 类普通 Extension。

## 3.1 Command Plugin

用户显式调用的能力。

示例：

```text
uuid
date utc
github pr
docker ps
base64 hello
```

特点：

- 由明确前缀、命令名或 Alias 触发。
- 不需要读取全局搜索输入。
- 安全边界清晰。
- 适合第三方插件。

典型权限：

```text
input.command
```

插件只应收到属于自己的 Command Payload。

例如：

```text
github kyvro
```

GitHub Plugin 可以收到：

```text
kyvro
```

而不应该收到用户此前输入的其他搜索内容。

---

## 3.2 Detector Plugin

Detector 是事件驱动的识别或处理能力。

典型场景：

```text
math.expression
structured.json
token.jwt
datetime.timestamp
url
```

Detector 不等于常驻后台进程。

生命周期通常是：

```text
Event
  ↓
Runtime invokes Detector
  ↓
Detector returns Match
  ↓
Idle
```

普通 Detector 默认不能读取 `raw_global`。

推荐模型：

```text
Raw Input
  ↓
System Input Matcher
  ↓
Typed Match
  ↓
Detector / Handler Plugin
```

例如：

```text
1 + 2 * 3
```

System Input Matcher 输出：

```json
{
  "type": "math.expression",
  "value": "1 + 2 * 3",
  "confidence": 1.0
}
```

Calculator Plugin 只订阅：

```text
math.expression
```

---

## 3.3 Search Provider Plugin

向 Kyvro 搜索结果提供数据。

例如：

- App Search
- Project Search
- File Search
- Browser Tabs
- GitHub Repository
- Notion Page
- Window Search

特点：

- 不应默认在每次输入时全部执行。
- 应通过 Router、索引、关键字、类型等机制筛选候选 Provider。
- Remote Provider 必须异步执行，并使用 debounce。

目标：

> Search cost 与本次命中的 Provider 数量相关，而不是与已安装插件总数相关。

---

## 3.4 Action Provider Plugin

为已有 Result 增加可执行操作。

例如 File Result：

```text
Open
Reveal in Finder
Copy Path
Open in VS Code
```

Project Result：

```text
Open
Open Terminal
Run Tests
Build
Git Status
```

GitHub Result：

```text
Open PR
Copy URL
Checkout
```

Action Provider 应优先操作结构化 Handle，而不是直接获得底层原始权限。

例如：

```text
project_id = project_123
```

优于：

```text
/Users/user/private/project
```

---

## 3.5 Background Service Plugin

真正长期运行的后台服务。

例如：

- Project Indexer
- Filesystem Watcher
- Clipboard History
- Browser Tab Sync
- Git Watcher
- SSH Tunnel
- Local Sync Service

生命周期：

```text
Kyvro Start
  ↓
Service Start
  ↓
Persistent Runtime
  ↓
Kyvro Exit
  ↓
Service Stop
```

Background Service 与 Detector 必须严格区分。

Detector：

```text
Event-driven
```

Background Service：

```text
Persistent
```

Background Service 需要额外的资源权限，例如：

```text
background.execution
```

同时 Runtime 应允许：

- 启用 / 禁用
- Health Check
- CPU / Memory 监控
- Crash Restart
- Rate Limit
- Stop

---

## 3.6 Agent Tool Plugin

向未来 Kyvro Agent 暴露可调用能力。

例如：

```text
git.status
git.diff

github.search_pr
github.create_issue

docker.logs

redis.get

project.test
filesystem.read
```

Agent Tool 不代表拥有无限底层权限。

推荐：

```text
Agent
  ↓
Structured Tool API
  ↓
Permission Engine
  ↓
Plugin / System Broker
```

例如优先提供：

```text
project.test(project_id)
```

而不是：

```text
shell.exec(cwd, arbitrary_command)
```

---

# 4. System Plugin

System Plugin 不是第 7 种普通插件类型，而是一个更高的 Trust Level。

它仍然运行在 Plugin Runtime 中，但可以获得普通 Marketplace 插件无法申请的敏感 Capability。

典型 System Plugin：

```text
Input Matcher
Project Scanner
Clipboard Broker
Browser Broker
Credential Broker
```

System Plugin 的目的：

> 将敏感原始资源转换成普通插件可以安全消费的结构化能力。

---

# 5. System Input Matcher

System Input Matcher 是唯一允许实时读取 Kyvro 全局输入的组件之一。

数据流：

```text
User Input
    ↓
Kyvro Core
    ↓
System Input Matcher
    ↓
[]TypedMatch
    ↓
Match Router
    ↓
Handler Plugin
```

例如：

```text
100 * (20 + 5)
```

Matcher 输出：

```json
{
  "type": "math.expression",
  "value": "100 * (20 + 5)",
  "confidence": 1.0
}
```

Calculator Plugin 不拥有：

```text
input.raw_global
```

它只拥有：

```text
match.receive:math.expression
```

---

## 5.1 Matcher 只负责识别

Matcher 应负责：

```text
Classification
Validation
Minimal Extraction
```

不负责具体业务逻辑。

例如：

```text
Matcher
→ math.expression
```

Calculator Plugin：

```text
Evaluate Expression
Format Result
Provide Actions
```

JSON Matcher：

```text
Matcher
→ structured.json
```

JSON Plugin：

```text
Format
Minify
Escape
Inspect
```

---

## 5.2 Match 不与具体 Plugin 绑定

错误设计：

```json
{
  "plugin": "com.kyvro.calculator"
}
```

推荐：

```json
{
  "type": "math.expression"
}
```

由 Registry 决定 Handler：

```text
math.expression
    ↓
Calculator
```

以后允许：

```text
math.expression
    ↓
Advanced Calculator
```

用户可以选择默认 Handler。

---

## 5.3 一个输入允许多个 Match

例如：

```text
1234567890
```

可能同时为：

```text
number
datetime.timestamp
```

因此 Matcher 应返回：

```go
[]Match
```

而不是单个 Match。

推荐结构：

```go
type Match struct {
    Type       string
    Value      string
    Confidence float64
    Metadata   map[string]any
}
```

---

# 6. System Project Scanner

项目扫描不应由所有第三方插件自行遍历用户目录。

推荐：

```text
Configured Project Roots
        ↓
System Project Scanner
        ↓
Project Detection
        ↓
Project Registry
        ↓
Structured Project Metadata
        ↓
Normal Plugins
```

用户配置扫描目录：

```text
~/Projects
~/Developer
~/Workspace
```

Project Scanner 可以识别：

```text
.git
go.mod
Cargo.toml
package.json
settings.gradle
pom.xml
```

---

## 6.1 普通插件优先使用 Project Handle

例如 Project Scanner 建立：

```json
{
  "id": "project_123",
  "name": "kyvro",
  "type": "go",
  "git": true,
  "capabilities": [
    "build",
    "test",
    "git"
  ]
}
```

普通插件优先只获得：

```text
project_id
name
type
capabilities
```

而不是完整底层访问权限。

例如：

```text
project.test(project_123)
```

优于：

```text
shell.exec(
    cwd="/Users/user/Projects/kyvro",
    command="go test ./..."
)
```

---

## 6.2 Project Scanner 权限

推荐：

```text
filesystem.read.project_roots
filesystem.watch.project_roots
```

默认禁止：

```text
network
vault
raw_input
arbitrary_shell
```

System Project Scanner 只负责：

```text
scan
detect
index
watch
metadata extraction
```

不要承担：

```text
AI
upload
build
deploy
arbitrary command execution
```

---

# 7. 插件 Trust Level

建议定义 3 个 Trust Level。

## 7.1 Normal Plugin

普通 Marketplace 插件。

可以申请：

- Command Scope
- Structured Match
- Structured Project API
- Limited Network
- Limited Filesystem
- Action APIs

不能申请：

```text
input.raw_global
vault.raw
system.unrestricted
```

---

## 7.2 Trusted Plugin

经过官方审核、签名或特殊授权的插件。

可以获得某些高风险 Capability。

例如：

```text
browser.raw_tabs
clipboard.read
selected_text
filesystem.project_roots
```

仍然遵循最小权限原则。

---

## 7.3 System Plugin

Kyvro 官方系统级组件。

可以获得 Restricted Capability。

例如：

```text
input.raw_global
project.scan
clipboard.raw
credential.broker
```

要求：

- 官方签名
- 固定 Publisher
- Runtime 验证签名
- 无法被普通插件伪装或覆盖
- 默认尽可能禁止 Network
- 权限组合受到额外 Policy 限制

---

# 8. Permission Policy

权限除了名称，还应定义申请策略。

```go
type PermissionPolicy int

const (
    PermissionNormal PermissionPolicy = iota
    PermissionUserGrantable
    PermissionTrustedOnly
    PermissionSystemOnly
)
```

示例：

| Permission | Policy |
|---|---|
| network.github.com | UserGrantable |
| filesystem.project | UserGrantable |
| clipboard.read | UserGrantable / Trusted |
| input.selected_text | UserGrantable |
| input.raw_global | SystemOnly |
| vault.raw | SystemOnly |
| background.execution | UserGrantable |
| project.scan | SystemOnly |

---

# 9. Capability 优于简单 Permission

长期建议从字符串权限升级为 Capability。

例如 Calculator：

```json
{
  "id": "com.kyvro.calculator",
  "capabilities": {
    "match": {
      "receive": [
        "math.expression"
      ]
    },
    "network": {
      "enabled": false
    }
  }
}
```

GitHub：

```json
{
  "id": "com.kyvro.github",
  "capabilities": {
    "input": {
      "scope": "command"
    },
    "network": {
      "domains": [
        "api.github.com"
      ]
    }
  }
}
```

System Input Matcher：

```json
{
  "id": "com.kyvro.system.input-matcher",
  "capabilities": {
    "input": {
      "scope": "global",
      "lifecycle": "realtime"
    },
    "network": {
      "enabled": false
    }
  }
}
```

---

# 10. 高风险权限组合

权限不能只单独判断，也需要考虑组合风险。

例如：

```text
input.raw_global
+
network.*
```

意味着插件拥有：

```text
Read Everything User Types
+
Potential Data Exfiltration
```

这是极高风险组合。

默认 Policy：

```text
Marketplace Plugin:
input.raw_global + network = prohibited
```

System Input Matcher 推荐：

```text
input.raw_global ✓

network ✗
filesystem ✗
vault ✗
shell ✗
```

核心原则：

> 可以读取高敏感数据的组件，应尽量失去数据外传能力。

---

# 11. Filesystem 权限

不要只提供：

```text
filesystem.read
filesystem.write
```

推荐进一步分层：

```text
filesystem.project.read
filesystem.project.write

filesystem.selected_file.read

filesystem.user_selected_directory.read

filesystem.project_roots.scan

filesystem.full_disk.read
```

其中：

```text
filesystem.full_disk.read
```

属于高风险权限。

普通 Project Plugin 应优先通过：

```text
Project API
```

而不是直接获得目录扫描能力。

---

# 12. Network 权限

Network 应至少限制到 Domain。

不推荐：

```text
network: *
```

推荐：

```json
{
  "network": {
    "domains": [
      "api.github.com"
    ]
  }
}
```

进一步可以限制：

```text
GET only
POST allowed
WebSocket
Localhost
Private LAN
```

Remote Search Provider 需要：

- debounce
- timeout
- cancellation
- rate limit

避免每次 keystroke 都上传。

---

# 13. Sensitive Input

Kyvro 应识别疑似敏感输入。

例如：

```text
API Key
Access Token
Private Key
Password-like Value
JWT
Recovery Phrase-like Content
```

当检测到高风险输入时，可以进入 Sensitive Mode：

```text
Third-party Plugin Dispatch ↓
Telemetry ↓
History Storage ↓
Network Plugin Invocation ↓
```

具体策略后续可配置。

重要原则：

> Secret-like data should not be broadcast to third-party plugins.

---

# 14. 插件生命周期

建议定义三种生命周期。

## Foreground

用户主动触发。

包括：

```text
Command
Search UI
Explicit Action
```

---

## Event-driven Background

收到事件才执行。

包括：

```text
Detector
Action Provider
Agent Tool
Typed Match Handler
```

平时不需要保持独立常驻进程。

---

## Persistent Background

长期运行。

包括：

```text
Indexer
Watcher
Sync
Service
```

需要：

```text
background.execution
```

并受到资源管理。

---

# 15. 插件运行与性能

插件多不应该直接导致搜索变慢。

禁止：

```text
Input
  ↓
Broadcast to 200 Plugins
  ↓
200 x Match()
```

推荐：

```text
Input
  ↓
Fast Router
  ↓
Candidate Selection
  ↓
Only Relevant Plugins
  ↓
Parallel Execution
  ↓
Ranker
```

原则：

> Search cost should depend on matched plugins, not installed plugins.

---

## 15.1 插件 Manifest 索引

安装时将轻量 metadata 注册到 Core。

例如：

```json
{
  "id": "com.kyvro.docker",
  "commands": [
    "docker"
  ],
  "handles": [
    "docker.container"
  ],
  "keywords": [
    "container",
    "compose"
  ]
}
```

Core 可以加载 Manifest Index，而无需启动插件代码。

只有真正命中后才加载插件。

---

## 15.2 搜索性能预算

建议目标：

```text
Input Routing          < 5ms
Core / Local Result    < 20ms
Local Plugin Result    < 50ms
UI First Result        < 100ms
Remote Provider        Async
```

单个插件不能阻塞主搜索线程。

---

# 16. Plugin Runtime 安全原则

Plugin Runtime 应至少保证：

- Permission Check
- Capability Check
- Signature Verification
- Timeout
- Cancellation
- Resource Limit
- Crash Isolation
- Audit Log
- Network Scope
- Filesystem Scope

高风险动作必须通过 Runtime，而不是插件直接绕过 Runtime。

---

# 17. System Plugin 更新

System Plugin 仍然是插件，因此可以：

```text
独立升级
独立回滚
独立版本管理
```

例如：

```text
Kyvro Core         1.2.0
Input Matcher      1.5.2
Project Scanner    1.3.1
Calculator         2.0.0
```

这样新增 Match Rule 或 Project Detector 不需要每次升级 Core。

---

# 18. 推荐的系统结构

```text
                         Kyvro Core
                             │
          ┌──────────────────┴──────────────────┐
          │                                     │
    System Plugins                        Normal Plugins
          │                                     │
 ┌────────┼──────────┐                          │
 │        │          │                          │
 ↓        ↓          ↓                          │
Input   Project   Clipboard                     │
Broker  Scanner    Broker                       │
 │        │          │                          │
 └────────┴──────────┴──────────┐               │
                                ↓               │
                         Structured APIs        │
                                │               │
                     ┌──────────┴───────────────┘
                     ↓
                Permission Engine
                     ↓
                Plugin Runtime
                     ↓
              Result / Action / Tool
```

---

# 19. 当前建议的第一阶段实现

第一阶段不需要一次实现完整 Capability 系统。

先实现以下核心结构：

```text
Plugin Manifest
Plugin Registry
Extension Type
Trust Level
Permission
System Plugin Signature Check
Input Matcher
Project Scanner
Search Provider
Command
Result
Action
```

第一阶段重点保证：

1. 普通插件不能读取 `raw_global`。
2. 普通插件不能任意扫描 Project Root。
3. System Plugin 可以独立升级。
4. Input Matcher 输出 Typed Match。
5. Project Scanner 输出 Project Metadata / Handle。
6. 搜索通过 Registry 选择候选插件。
7. 权限由 Runtime 强制检查，而不是依赖插件自觉。

---

# 20. 核心安全原则

Kyvro 插件体系长期遵循以下原则：

### Principle 1

```text
Everything can be a Plugin.
Not every Plugin is equally trusted.
```

### Principle 2

```text
Raw Data
   ↓
System Broker
   ↓
Structured Capability
   ↓
Normal Plugin
```

### Principle 3

```text
Least Privilege
```

插件只获得完成任务所需的最小能力。

### Principle 4

```text
Handles > Raw Paths
Typed Events > Raw Global Input
Structured APIs > Unrestricted Shell
Domain Network > network.*
```

### Principle 5

```text
High-sensitive Read Capability
+
Data Exfiltration Capability
=
High Risk
```

Policy Engine 必须支持权限组合限制。

### Principle 6

```text
Plugin count should not determine search latency.
```

所有插件通过索引、Router 和 Candidate Selection 按需执行。

---

# 21. 总结

Kyvro 的插件体系不是简单的：

```text
Plugin → Permission
```

而应该是：

```text
Plugin Package
      ↓
Extensions
      ↓
Trust Level
      ↓
Capability Request
      ↓
Permission / Policy Engine
      ↓
Structured System API
      ↓
Runtime Execution
```

最终目标：

> Core 保持小而稳定，System Plugin 负责敏感原始能力，普通插件消费安全的结构化接口，第三方生态可以快速扩展但无法默认获取用户全部输入、项目目录、剪贴板和凭据。
