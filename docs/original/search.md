# Kyvro Search Core Spec

## 1. 文档目标

本文档定义 Kyvro 搜索核心的第一阶段设计，以及未来插件系统对搜索结果的扩展方式。

当前阶段重点：

- 提供统一搜索入口。
- 支持本地应用程序搜索。
- 支持用户配置项目/目录根路径，并将目录写入索引后进行快速搜索。
- 定义统一的搜索结果模型。
- 定义统一 Action 模型，为后续插件扩展点击行为、快捷键行为预留能力。
- 暂不引入 Capability / Trait 模型。
- 暂不实现 URL、Command、Text 类型的实际 Provider，仅预留 ResultKind。

---

## 2. 设计原则

### 2.1 Core 负责搜索基础设施

Kyvro Core 负责：

- 接收用户搜索输入。
- 调用多个 Provider。
- 合并搜索结果。
- 排序与去重。
- 维护本地索引。
- 提供统一的 Result 数据结构。
- 提供统一的 Action 执行机制。
- 提供默认主操作（Primary Action）。
- 提供基础快捷操作。

Core 不负责：

- Git 项目管理。
- VS Code / Cursor / IDEA 等 IDE 的具体业务集成。
- GitHub / Docker / SSH / AI 等高级项目能力。
- 复杂业务工作流。

这些能力未来由插件扩展。

---

## 3. 搜索整体架构

```text
Search Input
    │
    ▼
SearchService
    │
    ▼
Search Engine
    │
    ├── AppProvider
    │      └── App Index
    │
    ├── FolderProvider
    │      └── Folder Index
    │
    └── PluginProvider
           └── Future Plugin Results
    │
    ▼
Result Merge
    │
    ├── Score
    ├── Deduplicate
    └── Limit
    │
    ▼
SearchResult[]
    │
    ▼
UI
    │
    ├── Enter
    ├── Shortcut
    └── Action Menu
    │
    ▼
ActionExecutor
```

第一阶段主要 Provider：

```text
AppProvider
FolderProvider
```

未来：

```text
PluginProvider
WebProvider
CommandProvider
...
```

---

## 4. ResultKind

第一阶段只定义以下 ResultKind：

```go
type ResultKind string

const (
    KindFolder  ResultKind = "folder"
    KindApp     ResultKind = "app"
    KindURL     ResultKind = "url"
    KindCommand ResultKind = "command"
    KindText    ResultKind = "text"
)
```

说明：

| Kind | 当前状态 | 说明 |
|---|---|---|
| `folder` | ✅ 支持 | 用户配置目录后，将目标文件夹加入本地索引并搜索 |
| `app` | ✅ 支持 | 自动扫描操作系统中的本地应用程序 |
| `url` | ⏳ 预留 | 未来用于书签、网页搜索、GitHub Repo、插件网络结果等 |
| `command` | ⏳ 预留 | 未来用于插件命令、系统命令、工具命令 |
| `text` | ⏳ 预留 | 未来用于计算结果、转换结果、Snippet、纯文本结果 |

当前阶段不实现 Capability。

例如“项目”暂时表现为：

```text
KindFolder
```

Core 不需要区分：

```text
普通目录
Git 项目
Node 项目
Go 项目
Rust 项目
```

后续如果需要更强语义能力，再考虑 Capability / Metadata 扩展。

---

## 5. SearchResult

建议统一模型：

```go
type SearchResult struct {
    ID       string
    Kind     ResultKind

    Title    string
    Subtitle string

    Score    float64

    Data     map[string]any

    PrimaryAction Action
    Actions       []ActionItem
}
```

### 5.1 字段说明

#### ID

结果唯一 ID。

示例：

```text
app:com.microsoft.VSCode
folder:/Users/user/Code/kyvro
```

要求：

- 同一个对象 ID 稳定。
- Provider 多次搜索返回同一对象时 ID 不变化。
- 用于去重、历史记录、使用频率排序。

---

#### Kind

结果基础类型。

例如：

```text
KindApp
KindFolder
```

---

#### Title

主要展示文本。

App：

```text
Visual Studio Code
```

Folder：

```text
kyvro
```

---

#### Subtitle

辅助信息。

App：

```text
/Applications/Visual Studio Code.app
```

Folder：

```text
~/Code/kyvro
```

---

#### Score

Provider 给出的基础搜索评分。

最终排序可以进一步结合：

```text
Provider Score
+
使用频率
+
最近使用
+
精确匹配奖励
+
前缀匹配奖励
```

---

#### Data

存储对应 Kind 的结构化数据。

第一阶段允许使用：

```go
map[string]any
```

但字段必须由 Core 统一约定，避免 Provider 随意定义。

App 示例：

```json
{
  "path": "/Applications/Visual Studio Code.app",
  "bundleId": "com.microsoft.VSCode"
}
```

Folder 示例：

```json
{
  "path": "/Users/user/Code/kyvro"
}
```

---

## 6. Action 模型

搜索结果不应该让 UI 自己判断“怎么打开”。

所有行为统一通过 Action 描述。

```go
type Action struct {
    Kind string
    Args map[string]any
}
```

例如：

```go
Action{
    Kind: "open-path",
    Args: map[string]any{
        "path": "/Users/user/Code/kyvro",
    },
}
```

---

## 7. ActionItem

搜索结果可以有多个附加操作。

```go
type ActionItem struct {
    ID       string
    Title    string
    Shortcut string
    Action   Action
}
```

例如 Folder：

```text
Enter
→ Open

⌘ Enter
→ Reveal in Finder

⌘ C
→ Copy Path
```

对应：

```go
SearchResult{
    Kind: KindFolder,

    PrimaryAction: Action{
        Kind: "open-path",
    },

    Actions: []ActionItem{
        {
            ID:       "reveal",
            Title:    "Reveal in Finder",
            Shortcut: "cmd+enter",
            Action: Action{
                Kind: "reveal-path",
            },
        },
        {
            ID:       "copy-path",
            Title:    "Copy Path",
            Shortcut: "cmd+c",
            Action: Action{
                Kind: "copy",
            },
        },
    },
}
```

---

## 8. 第一阶段 Action 类型

建议 Core 第一阶段内置：

```text
open-path
reveal-path
launch-app
copy
```

未来可增加：

```text
open-url
callback
open-with-app
```

但第一阶段可以不实现。

---

## 9. UI 与 Action 解耦

UI 不应该存在：

```go
switch result.Kind {
case KindFolder:
    ...
case KindApp:
    ...
}
```

UI 只处理：

```text
Enter
→ PrimaryAction

Shortcut
→ 找到对应 ActionItem
→ Execute

Action Menu
→ 展示 result.Actions
```

统一调用：

```go
ActionExecutor.Execute(action)
```

这样未来增加新的 ResultKind 或插件 Action 时，不需要修改搜索 UI。

---

# 10. App 搜索

## 10.1 目标

自动扫描操作系统中的本地应用程序，并建立快速搜索索引。

用户无需手动配置 App 路径。

---

## 10.2 macOS 第一阶段扫描范围

建议至少扫描：

```text
/Applications
/System/Applications
~/Applications
```

识别：

```text
*.app
```

读取基础信息：

```text
应用名称
.app 路径
Bundle Identifier
Icon
```

例如：

```text
Visual Studio Code
/Applications/Visual Studio Code.app
com.microsoft.VSCode
```

---

## 10.3 AppIndex

启动阶段扫描应用后建立 AppIndex。

示例：

```go
type AppIndexItem struct {
    ID       string
    Name     string
    Path     string
    BundleID string
}
```

建议主要在内存搜索。

必要时可将扫描结果持久化，启动后快速恢复，再异步刷新。

---

## 10.4 AppProvider

用户输入：

```text
code
```

返回：

```text
Visual Studio Code
/Applications/Visual Studio Code.app
```

Result：

```go
SearchResult{
    ID:       "app:com.microsoft.VSCode",
    Kind:     KindApp,
    Title:    "Visual Studio Code",
    Subtitle: "/Applications/Visual Studio Code.app",

    Data: map[string]any{
        "path": "/Applications/Visual Studio Code.app",
        "bundleId": "com.microsoft.VSCode",
    },

    PrimaryAction: Action{
        Kind: "launch-app",
    },
}
```

---

## 10.5 App 默认行为

第一阶段：

```text
Enter
→ Launch App
```

可选附加：

```text
Reveal in Finder
Copy Path
```

后续插件可增加：

```text
Quit
Force Quit
Open New Window
Open with Arguments
Move Window
```

但这些不属于第一阶段。

---

# 11. Folder 搜索

## 11.1 定位

Folder 搜索不是全盘文件搜索。

它主要用于：

```text
项目
Workspace
工作目录
常用目录
```

用户主动配置一个或多个 Root：

```text
~/Code
~/Projects
~/Work
```

Kyvro 对这些 Root 下的目录进行扫描，然后将目标目录加入索引。

---

## 11.2 Folder Source 配置

建议配置模型：

```go
type FolderSource struct {
    ID       string
    Path     string
    MaxDepth int
    Enabled  bool
}
```

例如：

```json
{
  "path": "~/Code",
  "maxDepth": 2,
  "enabled": true
}
```

第一阶段默认：

```text
MaxDepth = 1
```

即：

```text
~/Code/kyvro
~/Code/wallet
~/Code/backend
```

都会成为索引项。

但：

```text
~/Code/kyvro/internal
~/Code/kyvro/src
```

不会继续进入索引。

这样避免把目录搜索变成文件搜索。

---

## 11.3 FolderIndex

建议：

```go
type FolderIndexItem struct {
    ID   string
    Name string
    Path string
}
```

例如：

```text
ID:
folder:/Users/user/Code/kyvro

Name:
kyvro

Path:
/Users/user/Code/kyvro
```

---

## 11.4 是否识别“项目”

第一阶段不需要。

例如：

```text
~/Code/kyvro
```

无论内部有没有：

```text
.git
package.json
go.mod
Cargo.toml
```

都可以先作为：

```text
KindFolder
```

进入搜索。

后续 Projects Plugin 可以负责：

```text
Git 检测
项目类型检测
IDE 打开
GitHub 地址
Git 状态
项目标签
项目收藏
```

避免第一阶段 Core 过度复杂。

---

## 11.5 FolderProvider

用户输入：

```text
kyv
```

搜索：

```text
FolderIndex
```

返回：

```text
Kyvro
~/Code/kyvro
```

Result：

```go
SearchResult{
    ID:       "folder:/Users/user/Code/kyvro",
    Kind:     KindFolder,
    Title:    "kyvro",
    Subtitle: "~/Code/kyvro",

    Data: map[string]any{
        "path": "/Users/user/Code/kyvro",
    },

    PrimaryAction: Action{
        Kind: "open-path",
    },
}
```

---

## 11.6 Folder 默认行为

建议第一阶段：

```text
Enter
→ Open Folder

⌘ Enter
→ Reveal in Finder

⌘ C
→ Copy Path
```

这里的 Open Folder 可以暂时定义为：

```text
使用系统默认行为打开目录
```

macOS 通常进入 Finder。

未来可以允许用户设置默认项目打开工具：

```text
Finder
VS Code
Cursor
Terminal
```

但不建议第一阶段把这些 IDE 逻辑硬编码在 Core。

---

# 12. 索引持久化

Folder 必须写入本地索引表，避免每次输入搜索时实时扫描磁盘。

建议数据库：

```text
data.db
```

增加 bucket / table：

```text
search_index
```

也可以根据现有存储方案拆分：

```text
app-index
folder-index
```

第一阶段建议逻辑上拆开。

---

## 12.1 Folder 持久化字段

最低需要：

```text
id
name
path
source_id
updated_at
```

例如：

```json
{
  "id": "folder:/Users/user/Code/kyvro",
  "name": "kyvro",
  "path": "/Users/user/Code/kyvro",
  "source_id": "folder-source:code",
  "updated_at": 1787731200
}
```

---

## 12.2 App 是否持久化

App 可以：

```text
启动扫描
→ 建立内存索引
```

如果应用数量较少，第一阶段不一定必须持久化。

后续为了优化启动速度，可以：

```text
读取缓存
→ 立即可搜索
→ 后台重新扫描
→ 更新缓存
```

---

# 13. Folder 索引刷新

第一阶段可以简单实现：

```text
添加 Root
    ↓
立即扫描
    ↓
写入 FolderIndex

删除 Root
    ↓
删除该 source 对应索引

应用启动
    ↓
加载本地 FolderIndex
```

后续可以增加：

```text
fs.watch
定时刷新
手动 Refresh
增量扫描
```

第一阶段无需做复杂文件监听。

---

# 14. Search Engine

Search Engine 不关注具体对象。

统一接口：

```go
type Provider interface {
    Search(ctx context.Context, query string) ([]SearchResult, error)
}
```

第一阶段：

```text
AppProvider
FolderProvider
```

调用方式：

```text
SearchService
    ↓
并行执行 Provider
    ↓
合并结果
```

---

## 14.1 Provider 顺序

建议第一阶段：

```text
apps
folders
plugins
```

实际排序不应完全由 Provider 顺序决定。

最终应该基于 Score 合并。

---

# 15. 搜索匹配

第一阶段推荐支持：

```text
exact match
prefix match
fuzzy match
```

例如：

```text
Visual Studio Code
```

搜索：

```text
code
visual
vsc
```

可以匹配。

Folder：

```text
charityx-service
```

搜索：

```text
char
service
cxs
```

具体 fuzzy 算法可以后续调整，但 Provider 返回统一 Score。

---

# 16. 使用历史

用户打开结果后记录：

```text
result_id
usage_count
last_used_at
```

例如：

```text
folder:/Users/user/Code/kyvro
```

之后搜索：

```text
ky
```

可以根据历史提高排名。

排序大致：

```text
Search Match Score
+
Usage Boost
+
Recency Boost
```

这部分适用于所有 ResultKind。

---

# 17. 默认操作与插件扩展

当前 Core 产生基础 Result。

例如：

```text
Kyvro
~/Code/kyvro
```

Core Actions：

```text
Open
Reveal in Finder
Copy Path
```

未来插件可以对已有搜索结果追加 Action。

例如 Projects Plugin：

```text
Open in VS Code
Open in Cursor
Open Terminal
```

Git Plugin：

```text
Git Status
Git Log
Branches
```

AI Plugin：

```text
Explain Project
Review Changes
```

最终：

```text
Core Result
    │
    ▼
Action Extensions
    │
    ├── Projects Plugin
    ├── Git Plugin
    └── AI Plugin
    │
    ▼
Final SearchResult
```

---

# 18. 插件 Action Extension

未来插件 API 可以支持：

```javascript
ctx.actions.register({
  kind: "folder",

  actions: (result) => [
    {
      id: "projects.open-vscode",
      title: "Open in VS Code",
      action: {
        kind: "open-with-app",
        args: {
          app: "com.microsoft.VSCode",
          path: result.data.path
        }
      }
    }
  ]
});
```

这里只按：

```text
ResultKind
```

匹配。

第一阶段不引入：

```text
Capability
Trait
Entity Type
```

等后续需求明确后再扩展。

---

# 19. 插件扩展 App

未来 App 插件也可以增加行为：

```javascript
ctx.actions.register({
  kind: "app",

  actions: (result) => [
    {
      id: "app.reveal",
      title: "Reveal in Finder"
    },
    {
      id: "app.quit",
      title: "Quit App"
    }
  ]
});
```

---

# 20. URL / Command / Text 预留设计

当前只定义 Kind，不实现 Provider。

---

## 20.1 KindURL

未来可能来源：

```text
Bookmark Plugin
GitHub Plugin
Web Search
Browser History
Documentation Search
```

默认行为：

```text
Enter
→ Open URL
```

未来 Action：

```text
Copy URL
Open in Chrome
Open in Safari
Open Private Window
Generate QR Code
```

---

## 20.2 KindCommand

未来可能来源：

```text
Plugin Commands
System Commands
Developer Tools
Workflow Commands
```

示例：

```text
Generate UUID
Lock Screen
Clear Clipboard
Restart Docker
```

默认行为：

```text
Enter
→ Execute PrimaryAction
```

---

## 20.3 KindText

未来可能来源：

```text
Calculator
Text Transform
Base64
UUID
Timestamp
AI
Snippet
```

示例：

```text
Input:
uuid

Result:
550e8400-e29b-41d4-a716-446655440000
```

默认行为可考虑：

```text
Enter
→ Copy Text
```

具体行为由 Result 自己的 PrimaryAction 决定，不要求所有 `KindText` 完全相同。

---

# 21. 第一阶段范围

## 必须实现

### Search Core

- `SearchService`
- `Search Engine`
- Provider 接口
- Result 合并
- 基础排序
- 使用历史 Boost

### Result

- `ResultKind`
- `SearchResult`
- `Action`
- `ActionItem`
- `ActionExecutor`

### App

- macOS App 扫描
- AppIndex
- AppProvider
- Launch App

### Folder

- Folder Root 配置
- Folder Scanner
- FolderIndex 持久化
- FolderProvider
- Open Folder
- Reveal in Finder
- Copy Path

---

## 暂不实现

- Capability
- Project 类型
- 文件全文索引
- 全盘文件搜索
- Git 状态
- VS Code / Cursor 集成
- URL Provider
- Command Provider
- Text Provider
- Background Index Watcher
- 插件动态追加 Action
- `open-with-app`
- 自定义快捷键

但数据模型应避免阻碍这些能力未来加入。

---

# 22. 推荐目录结构

```text
internal/
├── search/
│   ├── engine.go
│   ├── result.go
│   ├── provider.go
│   ├── ranking.go
│   └── history.go
│
├── action/
│   ├── action.go
│   └── executor.go
│
├── appindex/
│   ├── scanner.go
│   ├── index.go
│   └── provider.go
│
├── folderindex/
│   ├── source.go
│   ├── scanner.go
│   ├── index.go
│   └── provider.go
│
└── plugin/
    └── ...
```

如果现有项目已经有：

```text
internal/core/Engine
```

也可以保持：

```text
service/SearchService
    ↓
internal/core/Engine
    ├── AppProvider
    ├── FolderProvider
    └── PluginProvider
```

不要求为了本 Spec 大规模调整现有目录。

---

# 23. 第一阶段完整数据流

```text
                    Kyvro Startup
                         │
             ┌───────────┴───────────┐
             ▼                       ▼
        Scan Applications       Load FolderIndex
             │                       │
             ▼                       ▼
          AppIndex              FolderIndex
             │                       │
             └───────────┬───────────┘
                         │
                         ▼

User Input
    │
    ▼
SearchService
    │
    ▼
Search Engine
    │
    ├──────────────┐
    ▼              ▼
AppProvider   FolderProvider
    │              │
    └──────┬───────┘
           ▼
      Merge + Rank
           │
           ▼
      SearchResult[]
           │
           ▼
            UI
     ┌─────┼────────┐
     ▼     ▼        ▼
   Enter Shortcut  Menu
     │     │        │
     └─────┴────────┘
           ▼
     ActionExecutor
           │
     ┌─────┴──────────┐
     ▼                ▼
Launch App        Open Folder
```

---

# 24. 未来演进

Phase 1：

```text
App
Folder
Search
Action
```

Phase 2：

```text
Plugin Action Extension
URL
Command
Text
open-with-app
```

Phase 3：

```text
Projects Plugin
Git Plugin
Clipboard Plugin
Browser Plugin
```

Phase 4：

根据实际插件需求再决定是否引入：

```text
Capability
Result Metadata Schema
Event Bus
Background Tasks
UI DSL
```

---

# 25. 核心边界总结

Kyvro Search Core 的核心职责：

> 找到对象，并描述用户当前可以对这个对象执行什么操作。

第一阶段：

```text
App
→ 自动扫描
→ 搜索
→ 启动

Folder
→ 用户配置 Root
→ 扫描
→ 写入索引
→ 搜索
→ 打开
```

未来：

```text
Core SearchResult
        │
        ▼
Plugin Action Extension
        │
        ├── Open in VS Code
        ├── Git Status
        ├── Open GitHub
        ├── Run AI Review
        └── ...
```

因此当前不需要把“项目管理”做进 Search Core。

Search Core 只需要把项目目录当作：

```text
KindFolder
```

快速找到并打开。

更丰富的项目语义和操作由未来插件继续扩展。
