# Kyvro Architecture

## 1. 目标

Kyvro 搜索系统围绕两类核心能力设计：

1. **Indexed Search**
2. **Prefix Command**

两者解决不同类型的搜索需求，但最终统一输出搜索结果，并通过统一的 Action 系统执行用户操作。

整体原则：

```text
静态数据 → Indexed Search
动态功能 → Prefix Command
最终操作 → Action
```

## 2. 总体架构

```text
                         Query
                           │
             ┌─────────────┴─────────────┐
             │                           │
             ▼                           ▼
      Indexed Search               Prefix Command
             │                           │
       Static Index                 Prefix Router
             │                           │
       Fuzzy Search              Provider / Plugin
             │                           │
             └─────────────┬─────────────┘
                           ▼
                     SearchResult[]
                           │
                           ▼
                         Action
                           │
                           ▼
                    Action Executor
```

Kyvro 的搜索入口只有一个。

用户输入内容后，系统根据查询类型进入不同的搜索路径。

无论结果来自静态索引还是动态 Provider，最终都转换为统一的搜索结果，并通过 Action 执行。

# 3. Indexed Search

## 3.1 定位

Indexed Search 用于搜索已经提前建立索引的数据。

主要目标是：

> 提供高频、低延迟、即时反馈的搜索体验。

这类数据通常变化频率较低，或者可以提前缓存，因此不需要在用户每次输入时实时访问外部系统。

典型场景：

```text
App
Folder / Project
Password Metadata
Clipboard History
Bookmark / URL
Command
```

未来插件也可以向静态索引中注入额外数据，例如：

```text
GitHub Repository
SSH Host
Notion Page
Jira Issue
Cloud Resource
```

## 3.2 核心职责

Indexed Search 负责：

```text
数据采集
    ↓
建立索引
    ↓
加载搜索索引
    ↓
快速模糊匹配
    ↓
生成 SearchResult
    ↓
执行 Action
```

重点是搜索速度。

用户输入过程中不应该执行复杂业务逻辑、网络请求或高延迟操作。

## 3.3 数据来源

静态索引可以来自两类来源。

### Core 数据源

由 Kyvro Core 自己维护。

例如：

```text
App
Folder / Project
Password
Clipboard
Bookmark
Built-in Command
```

这些通常属于系统级、高频或敏感能力。

### Plugin Index Extension

插件可以向 Kyvro 的统一索引中注入额外数据。

例如：

```text
GitHub Plugin
    ↓
GitHub Repository Index

Jira Plugin
    ↓
Issue Index

SSH Plugin
    ↓
Server Index
```

插件负责提供数据，Kyvro Core 负责统一搜索。

## 3.4 用户体验

用户不需要知道数据来自哪个 Provider。

例如输入：

```text
kyvro
```

可能同时匹配：

```text
Kyvro Project
Kyvro GitHub Repository
Kyvro Documentation
Kyvro Bookmark
```

搜索系统统一排序后展示。

## 3.5 Action

Indexed Search 返回的结果必须能够执行 Action。

例如：

```text
Project
→ Open

App
→ Launch

Bookmark
→ Open URL

Password
→ Copy Username / Password

Clipboard
→ Copy
```

后续插件可以继续为这些结果扩展更多 Action。

例如：

```text
Project
    Core Action:
    → Open

    Plugin Action:
    → Open in VS Code
    → Open in Cursor
    → Git Status
```

因此 Indexed Search 的完整职责可以概括为：

```text
Index
    ↓
Search
    ↓
Result
    ↓
Action
```

# 4. Prefix Command

## 4.1 定位

Prefix Command 用于处理需要实时执行逻辑的搜索功能。

它不是从已有静态索引中查找数据，而是在特定前缀命中后，将查询交给对应的 Provider 或 Plugin 处理。

例如：

```text
gh kyvro
ai explain this
dns github.com
translate hello
weather singapore
calc 10*20
```

Prefix Command 更接近：

> 搜索框中的动态功能入口。

## 4.2 核心流程

```text
Query
    ↓
Prefix Router
    ↓
匹配 Prefix
    ↓
Provider / Plugin
    ↓
执行动态逻辑
    ↓
返回 SearchResult[]
    ↓
Action
```

例如：

```text
gh kyvro
```

命中：

```text
gh 
```

然后调用 GitHub Provider。

GitHub Provider 可以实时访问 API、处理参数、查询数据并生成结果。

最终仍然返回标准搜索结果。

## 4.3 适用场景

Prefix Command 适用于：

### 实时网络查询

```text
GitHub
天气
汇率
在线文档
API 查询
```

### 动态计算

```text
Calculator
Unit Conversion
Timestamp
Hash
Encoding
```

### AI 功能

```text
AI Explain
AI Translate
AI Summarize
AI Generate
```

### 开发者工具

```text
DNS Lookup
HTTP Request
JSON Transform
Regex
JWT Decode
```

### 系统或自动化命令

```text
Docker
Git
SSH
Workflow
Shell Command
```

这些能力通常不适合提前建立完整静态索引。

# 5. Prefix Router

Prefix Router 的作用是根据用户输入决定是否进入动态功能。

例如：

```text
gh 
→ GitHub Provider

ai 
→ AI Provider

dns 
→ DNS Provider

calc 
→ Calculator Provider
```

Prefix 的核心作用是：

> 避免所有插件在每一次搜索输入时都执行动态逻辑。

只有匹配到对应前缀后，Provider 才会被调用。

因此普通搜索：

```text
kyvro
```

不会触发 GitHub API、AI、DNS 或其他动态插件。

这可以保证主搜索路径足够轻量。

# 6. Dynamic Provider / Plugin

Prefix Command 的实际功能由 Provider 或 Plugin 提供。

Provider 可以执行：

```text
网络访问
数据转换
计算
业务逻辑
外部 API
系统能力
```

然后把结果转换为统一 SearchResult。

Kyvro Core 不需要理解 Provider 内部业务。

例如：

```text
GitHub Provider
AI Provider
DNS Provider
Translate Provider
Docker Provider
```

对于 Search Engine 来说，它们最终只是：

```text
Query
    ↓
SearchResult[]
```

# 7. Prefix Command 的 Action

Prefix Command 不应该以“执行完成”作为终点。

动态 Provider 返回结果后，仍然进入统一 Action 系统。

完整链路：

```text
Prefix
    ↓
Provider / Plugin
    ↓
Dynamic Result
    ↓
SearchResult[]
    ↓
Action
```

例如：

```text
dns github.com
```

返回一个 IP 后，可以继续提供：

```text
Copy IP
Copy Full Result
```

再例如：

```text
gh kyvro
```

返回 Repository 后，可以继续提供：

```text
Open Repository
Copy URL
Clone Repository
```

因此 Prefix Command 和 Indexed Search 最终拥有一致的交互模型。

# 8. 两类搜索的区别

| 能力 | Indexed Search | Prefix Command |
|---|---|---|
| 数据来源 | 已建立索引的数据 | 实时 Provider / Plugin |
| 搜索方式 | 快速模糊匹配 | Prefix 路由 |
| 是否提前准备数据 | 是 | 通常否 |
| 是否适合网络请求 | 否 | 是 |
| 是否适合高频搜索 | 非常适合 | 取决于 Provider |
| 是否可以由插件扩展 | 可以 | 可以 |
| 最终输出 | SearchResult | SearchResult |
| 最终操作 | Action | Action |

# 9. 两条路径的统一

虽然 Indexed Search 和 Prefix Command 的数据来源完全不同，但 Kyvro 不应该为它们设计两套 UI 和 Action 系统。

统一架构：

```text
Indexed Search
      │
      ▼
 SearchResult
      │
      ▼
    Action


Prefix Command
      │
      ▼
 SearchResult
      │
      ▼
    Action
```

最终：

```text
                 SearchResult
                      │
                      ▼
                 Action System
                      │
                      ▼
                Action Executor
```

UI 不需要知道结果来自：

```text
Core Index
Plugin Index
Dynamic Plugin
Network API
Built-in Provider
```

UI 只需要处理：

```text
展示结果
选择结果
执行 Action
```

# 10. 插件扩展模型

插件未来主要可以从三个方向扩展 Kyvro。

## 10.1 Index Extension

向静态索引中注入新的可搜索数据。

```text
Plugin
    ↓
Index Data
    ↓
Indexed Search
```

适合：

```text
GitHub Repository
Jira Issue
SSH Host
Notion Page
Cloud Resource
```

## 10.2 Prefix Provider

注册新的动态 Prefix 功能。

```text
Plugin
    ↓
Prefix
    ↓
Dynamic Provider
```

适合：

```text
AI
GitHub Search
DNS
Translate
Weather
API Tools
```

## 10.3 Action Extension

为 Core 或其他插件产生的 SearchResult 增加额外操作。

```text
SearchResult
    ↓
Plugin Action Extension
    ↓
More Actions
```

例如：

```text
Folder / Project
    ↓
VS Code Plugin
    → Open in VS Code

Folder / Project
    ↓
Git Plugin
    → Git Status
```

因此插件体系最终形成：

```text
Plugin
├── Index Extension
├── Prefix Provider
└── Action Extension
```

# 11. Core 与 Plugin 的边界

## Core

Core 负责：

```text
统一搜索入口
静态索引框架
Indexed Search
Prefix Router
SearchResult
Action System
Action Executor
排序
历史权重
基础数据源
```

## Plugin

Plugin 负责：

```text
额外索引数据
动态 Prefix 功能
扩展 Action
第三方服务集成
业务功能
```

Core 不应该包含大量第三方业务逻辑。

# 12. 最终架构

```text
                              Query
                                │
                  ┌─────────────┴─────────────┐
                  │                           │
                  ▼                           ▼
           Indexed Search               Prefix Command
                  │                           │
            Unified Index                Prefix Router
                  │                           │
             Fuzzy Search              Provider / Plugin
                  │                           │
                  └─────────────┬─────────────┘
                                ▼
                          SearchResult[]
                                │
                                ▼
                         Action Extension
                                │
                                ▼
                            Actions
                                │
                                ▼
                         Action Executor
```

插件扩展：

```text
                       Plugin System
                    ┌───────┼────────┐
                    ▼       ▼        ▼
                  Index   Prefix   Action
                Extension Provider Extension
```

# 13. 架构原则

Kyvro 搜索系统最终遵循三个核心原则：

### 静态数据走 Index

```text
Indexed Search
```

用于快速找到已经存在的数据。

### 动态功能走 Prefix

```text
Prefix Command
```

用于执行实时查询、计算和功能。

### 所有结果最终走 Action

```text
SearchResult
    ↓
Action
```

无论结果来自 Core、索引、插件还是动态 Provider，最终交互方式保持一致。

因此 Kyvro 的核心搜索模型可以概括为：

```text
Search = Indexed Search + Prefix Command

Indexed Search
    = Index → Result → Action

Prefix Command
    = Prefix → Provider → Result → Action
```
