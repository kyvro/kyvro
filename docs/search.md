# Kyvro Core Search 升级规格

## 1. 文档目标

本文档定义 Kyvro 核心搜索模块的下一阶段升级规格。内容基于 `docs/original/search.md`，并对照当前代码库实现校准。

文档分工：本文负责搜索架构、Result / Action 模型、Folder 搜索语义、Engine 语义、Service API、平台与 UI 要求；索引分类、存储布局、扫描时机与建立/更新流程由 [index.md](./index.md) 负责，两文以引用互链。

本次升级目标是把现有的应用 / 插件 / Web 启动器搜索，扩展为更完整的本地启动器 Core：支持用户配置目录根路径并建立 Folder 索引，同时提供更丰富的结果与 Action 契约。平台细节与插件业务逻辑仍不能进入 `internal/core`。

## 2. 当前基线

当前搜索链路：

```text
frontend/App.vue
    -> service.SearchService.Search(query)
    -> internal/core.Engine
       providers 按优先级顺序：
         1. calc
         2. apps
         3. plugins
         4. web
    -> []core.SearchResult
```

当前 provider 顺序具有业务语义。`Engine.Search` 会按顺序消费 provider，并在结果数量达到 limit 后停止访问后续 provider。早出现的 provider 永远排在后出现的 provider 前面，不受分数跨 provider 比较影响。这个规则保证计算器结果在最前，本地 App 高于插件，插件高于 Web，Web 始终作为尾部兜底。

当前 Core 模型：

```go
type SearchResult struct {
    ID       string
    Title    string
    Subtitle string
    Action   Action
    Score    float64
    IconPath string
}

type Action struct {
    Kind     ActionKind
    Arg      string
    PluginID string
    ActionID string
    Args     []string
}
```

当前内置 Action 类型：

- `ActionLaunchApp`
- `ActionOpenURL`
- `ActionCopyText`
- `ActionPlugin`

当前 frecency 公式：

```text
score = fuzzyScore + 8 * log2(count + 1) + 12 * 2^(-ageHours / 72)
```

使用历史存储在 bbolt 的 `usage` bucket 中，数据库路径为 `~/Library/Application Support/Kyvro/data.db`，key 为结果 ID。Store 必须在 `ServiceStartup` 内、单实例保护生效后再打开，避免重复进程争抢 bbolt 锁。

## 3. 升级范围

### 必须交付

- 基于用户配置的根目录增加一等 Folder 搜索。
- 将 Folder source 配置和扫描得到的 Folder index entries 持久化到现有 bbolt store。
- 保持 App 搜索、计算器搜索、插件搜索和 Web 兜底行为不退化。
- 引入可描述结果类型、结构化元数据、主操作和附加操作的结果模型。
- 在 service 层引入统一 Action 执行机制，让 UI 不需要按结果类型决定如何启动。
- 保持 `internal/core` 为纯 Go 层，不引入 Wails 或具体平台实现。
- 为 core model、rank、store、engine、provider 的行为变化补充单元测试。

### 本次不交付

- 全盘文件搜索。
- 文件内容索引。
- Core 内置 Git 项目识别。
- Core 内置 VS Code / Cursor / IDEA 等 IDE 打开逻辑。
- 插件对现有 Core 结果动态追加 Action。
- Capability / Trait / Entity Type 系统。
- 除当前已实现的 calculator、web、plugin 行为外，不新增 URL / Command / Text Provider。

## 4. 目标架构

```text
Search Input
    -> SearchService
    -> Engine
       -> CalcProvider
       -> AppProvider
       -> FolderProvider
       -> PluginProvider
       -> WebProvider
    -> merged SearchResult[]
    -> UI
       -> Enter: Execute PrimaryAction
       -> shortcut/menu: Execute ActionItem.Action
    -> SearchService.ExecuteAction / Launch compatibility wrapper
```

目标 provider 顺序：

```text
calc -> apps -> folders -> plugins -> web
```

设计理由：

- Calculator 继续作为即时答案 provider。
- Apps 继续作为启动器的主要本地入口。
- Folders 是一等本地结果，应排在第三方插件结果前。
- Plugins 继续排在 Web 之前。
- Web 继续固定在最后，不能挤掉本地结果。

## 5. Result 模型

升级后的模型应增加结果类型和附加操作，同时控制 Wails 绑定迁移成本。

```go
type ResultKind string

const (
    KindApp     ResultKind = "app"
    KindFolder  ResultKind = "folder"
    KindURL     ResultKind = "url"
    KindCommand ResultKind = "command"
    KindText    ResultKind = "text"
)

type SearchResult struct {
    ID       string
    Kind     ResultKind
    Title    string
    Subtitle string
    Score    float64
    IconPath string

    Data          map[string]any
    PrimaryAction Action
    Actions       []ActionItem
}

type ActionItem struct {
    ID       string
    Title    string
    Shortcut string
    Action   Action
}
```

`ActionItem.Shortcut` 第一版使用字符串，例如 `cmd+enter`、`cmd+c`。这样绑定和 UI 实现最简单。未来如果要支持跨平台快捷键差异、冲突检测或用户自定义快捷键，再迁移到结构化表示。

兼容规则：迁移期间可以保留现有 `Action` 字段作为 `PrimaryAction` 的废弃别名；也可以一次性更新前端绑定和所有调用点。只要修改了 Wails 绑定可见的 service/model 字段，就必须执行：

```sh
~/Go.proj/bin/wails3 generate bindings -d frontend/bindings -clean -names
pnpm --dir frontend build
```

### 稳定 ID

Frecency 以结果 ID 为 key，因此 ID 必须稳定。

必需前缀：

- Apps：直接使用 `app:<bundleID>`；如果 bundle ID 缺失，使用 `app:path:<absolute-app-path>` 作为稳定 fallback。
- Folders：`folder:<absolute-path>`
- Calculator：`calc:<normalized-expression>`
- Web：`web:<raw-query>`
- Plugins：沿用当前 `plugin:<pluginID>:<resultID>`

App result ID 不需要兼容当前内存中的 bundle ID / path 形式。本次升级可以直接切到 `app:<bundleID>`，并接受现有 App frecency 重置。

## 6. Action 模型

现有 `ActionKind` enum 可以扩展，不需要整体替换。

必需 Action 类型：

- `ActionLaunchApp`：按路径启动 `.app`。
- `ActionOpenURL`：通过用户配置的外部浏览器打开 URL。
- `ActionCopyText`：在 service 层通过 Wails clipboard 复制文本。
- `ActionPlugin`：通过 `RunAction` 分发插件 command/callback。
- `ActionOpenPath`：使用系统默认处理器打开文件系统路径。
- `ActionRevealPath`：在 macOS Finder 中 reveal 文件系统路径。

`internal/core` 只定义 Action payload。具体执行由 `service` 和 `internal/platform` 负责。

目标 Action payload：

```go
type Action struct {
    Kind     ActionKind
    Arg      string
    PluginID string
    ActionID string
    Args     []string
}
```

Folder 行为：

```text
PrimaryAction: ActionOpenPath(path)
Actions:
  - reveal: ActionRevealPath(path), shortcut cmd+enter
  - copy-path: ActionCopyText(path), shortcut cmd+c
```

UI 不应通过 `ResultKind` 决定启动逻辑。UI 可以为了图标、样式、展示方式读取 kind 或 ID prefix，但执行行为必须由 Action 描述。

## 7. Folder 搜索

Folder 搜索索引用户配置根目录下的目录。它不是文件系统搜索引擎。

### Folder Source

```go
type FolderSource struct {
    ID       string
    Path     string
    MaxDepth int
    Enabled  bool
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

规则：

- `Path` 存储为绝对路径，并经过 clean。
- `~` 必须在校验前展开。
- 默认 `MaxDepth` 为 `1`。
- `MaxDepth` 表示配置根目录下的目录层级。root 为 `~/Code` 且 depth 为 `1` 时，`~/Code/kyvro` 会进入索引，`~/Code/kyvro/internal` 不会进入索引。
- disabled source 仍保留配置，但不贡献搜索结果。

### Folder Index Entry

```go
type FolderIndexEntry struct {
    ID        string
    Name      string
    Path      string
    SourceID  string
    UpdatedAt time.Time
}
```

规则：

- `ID = "folder:" + absolutePath`。
- `Name = filepath.Base(path)`。
- `Subtitle` 优先展示用户友好的路径；但 `Data["path"]` 和 action arg 必须使用绝对路径。
- 默认排除隐藏目录和包 / 构建缓存目录：`.git`、`node_modules`、`vendor`、`dist`、`build`、`.next`、`.turbo`、`.cache`。
- 默认不递归跟随 symlink 目录，除非后续显式引入该能力。

### Folder Provider

`FolderProvider` 搜索已加载到内存的持久化 Folder index。

搜索行为：

- 空查询返回所有 enabled folder entries，score 为 `0`；随后由 engine frecency 排序。
- 非空查询使用与 App 搜索相同的 fuzzy 库。
- 匹配 key 只包含 folder basename。第一版不匹配完整路径，避免路径片段让弱相关目录浮到前面。
- Folder 搜索必须使用预构建的内存 search index，不允许每次搜索临时从 entries 生成 names/search keys。
- 结果使用 `KindFolder`、`ActionOpenPath`、`ActionRevealPath` 和 `ActionCopyText`。

Folder 结果示例：

```go
SearchResult{
    ID:       "folder:/Users/alice/Code/kyvro",
    Kind:     KindFolder,
    Title:    "kyvro",
    Subtitle: "~/Code/kyvro",
    Data: map[string]any{
        "path": "/Users/alice/Code/kyvro",
    },
    PrimaryAction: Action{Kind: ActionOpenPath, Arg: "/Users/alice/Code/kyvro"},
    Actions: []ActionItem{
        {ID: "reveal", Title: "Reveal in Finder", Shortcut: "cmd+enter", Action: Action{Kind: ActionRevealPath, Arg: "/Users/alice/Code/kyvro"}},
        {ID: "copy-path", Title: "Copy Path", Shortcut: "cmd+c", Action: Action{Kind: ActionCopyText, Arg: "/Users/alice/Code/kyvro"}},
    },
}
```

## 8. 状态与缓存存储

本次升级将“用户状态”和“可重建索引缓存”分开存储：

- bbolt 只保存不能随意丢弃的用户状态：`usage`（现有启动历史，格式不变）、`folder-sources`（Folder source 配置，source ID 为 key），以及 settings、snippets、plugins-state、plugin storage 等现有命名空间。
- App / Folder index 不放入 bbolt，改为独立 cache 文件（`~/Library/Application Support/Kyvro/cache/app-index.json`、`folder-index.json`），由 `internal/indexcache` 统一管理；该组件只读写可重建缓存，不参与 usage / settings / plugin storage。

分离原因、目录布局、缓存文件格式（`AppIndexFile` / `FolderIndexFile` / entry 字段 / `SearchKeys` 语义）、原子写入与损坏 / 未来版本降级、per-source 替换与删除、启动 enabled 过滤等细节，统一由 [index.md](./index.md) 负责（§1 索引分类、§2 存储布局、§4 建立与更新流程）。

契约级要求（实现细节见 index.md）：

- Store 或 cache 读取错误不能导致搜索崩溃：Engine / provider 降级为空缓存或已有内存缓存；配置或刷新操作中的错误通过 settings / refresh API 暴露。
- Cache 文件写入必须使用临时文件 + rename 的原子替换，避免崩溃留下半截 JSON。
- 不要持久化 `sahilm/fuzzy` 的内部数据——库没有可序列化的索引对象；持久化 Kyvro 自己的 index entry 与可稳定重建的 `SearchKeys`（见 index.md §1.2）。
- App / Folder 共用同一套泛型内存索引 `SearchIndex[T]`（见 index.md §1.1）；搜索路径禁止每次 keypress 重建 names / search keys，必须使用预构建 `SearchIndex.Keys`。

## 9. 刷新流程

首版保持显式和简单：Add 后立即扫描并替换该 source 的缓存条目；Remove 删除配置与缓存条目；启动时加载缓存文件并后台扫描 enabled sources；手动刷新支持单 source 或全部 enabled。各触发时机总表与完整更新链见 [index.md](./index.md) §3（Scan 时机）、§4.3（Folder 更新链）。

本次升级不要求文件监听。扫描必须支持 `context.Context` 取消，并受 `MaxDepth` 限制。测试应使用临时目录；涉及时间的行为使用 fake clock。

## 10. 启动与索引构建流程

本节明确当前启动建索引行为和改造后的目标行为。核心约束不变：bbolt store 必须在 Wails 单实例保护之后打开；启动器窗口和热键初始化不应被磁盘扫描阻塞。

### 10.1 当前启动流程

当前入口在 `main.go`：

```text
main
    -> core.DefaultDataPath()
       -> resolve ~/Library/Application Support/Kyvro/data.db
       -> migrate Lumo config dir to Kyvro when needed
    -> service.New(dataPath, platform.NewAppSource(), platform.NewAppLauncher())
    -> application.New(...)
       -> SingleInstance guard configured
       -> SearchService registered as Wails service
    -> create summon window / settings opener / hotkey / tray
    -> app.Run()
       -> SearchService.ServiceStartup(...)
```

`SearchService.ServiceStartup` 当前负责搜索核心初始化：

```text
ServiceStartup
    -> core.OpenStore(dataPath)
       -> create/open bbolt
       -> ensure usage bucket
    -> plugin.NewManager(pluginsDir, store, nil)
    -> mgr.LoadAll()
       -> load plugin manifests/runtime
       -> single plugin failures logged, not fatal
    -> apps.New(platform.AppSource)
    -> core.NewEngine([calc, apps, plugins, web], store, DefaultLimit)
    -> go appsProvider.Warmup()
       -> platform.AppSource.Rescan()
       -> scan /Applications, /System/Applications, ~/Applications
       -> parse .app Info.plist / localized names / icon path
       -> replace in-memory app cache
    -> initialize snippets service and text expander
```

当前 App index 是内存索引：

- `platform.AppSource.Rescan()` 重新扫描 macOS app roots。
- `platform.AppSource.List()` 返回缓存 app 列表，不能阻塞磁盘 I/O。
- `apps.Provider.Search()` 每次搜索先调用 `maybeRescan()`；如果上次扫描超过 60 秒，会后台触发一次 rescan。
- 首次 `Warmup()` 也是后台 goroutine，因此搜索服务可先就绪，App 结果会在扫描完成后出现。
- App 扫描结果不写入 bbolt；bbolt 当前只保存 usage、插件状态 / 存储、settings、snippets 等命名空间数据。

当前启动时没有 Folder index：

- 没有 Folder source 配置。
- 没有 Folder scanner。
- 没有 `folder-sources` / `folder-index` bucket。
- 没有启动时加载 Folder 缓存的流程。

### 10.2 改造后启动流程

改造后仍由 `ServiceStartup` 组装搜索核心，但需要把 Folder index 的加载放在 engine 构建前完成，让首次搜索即可返回已有 Folder 结果。

目标流程：

```text
ServiceStartup
    -> core.OpenStore(dataPath)
       -> ensure usage bucket
       -> ensure folder-sources bucket
    -> open index cache directory
    -> load plugin manager
    -> load app-index cache file
    -> load folder sources from bbolt
    -> load folder-index cache file
    -> apps.NewProviderWithCache(platform.AppSource, appIndexEntries)
    -> folders.NewProvider(folderIndexEntries)
    -> core.NewEngine([calc, apps, folders, plugins, web], store, DefaultLimit)
    -> go appsProvider.Warmup()
       -> rescan apps
       -> rewrite cache/app-index.json atomically
       -> replace apps provider cache
    -> go folderProvider.RefreshEnabledSources()
       -> optional startup refresh, non-blocking
    -> initialize snippets service and text expander
```

改造后的搜索 index 有三层数据：

- `app-index`：App 扫描缓存，决定启动后 App 结果能否立即出现。
- `folder-sources`：用户配置，决定扫描哪些 root、depth 和启用状态。
- `folder-index`：扫描结果，决定搜索时可命中的目录列表。

启动时必须先加载持久化 index，再进行后台刷新；App 路径与 Folder 路径的完整时序图见 [index.md](./index.md) §4.1。这个顺序的目的：应用启动后无需等待扫描即可搜索上次缓存的结果；大目录扫描不阻塞热键、窗口、插件加载；后台刷新完成后下一次搜索自然看到新索引；扫描失败只影响对应 source，不影响已有缓存结果和其它 provider。

### 10.3 新增 / 刷新 Source 的建索引流程

添加 source 时同步执行一次受控扫描，保证添加后立刻可搜索；启用时先从缓存恢复 provider 条目再后台刷新；禁用只移除 provider 条目、保留缓存（快速 re-enable）；刷新 disabled source 返回显式错误。Add / Refresh / Remove / Enable-Disable 四条更新链的完整步骤与顺序约定（先 cache 后 provider）见 [index.md](./index.md) §4.3。

### 10.4 启动刷新策略

首版策略：启动时同步打开 store、加载插件 manifest 与两个缓存文件；App 与 enabled folder sources 均在后台扫描替换缓存，不阻塞 service ready；每个 source 独立失败，错误记录在 source 状态或日志；搜索路径只读 provider 内存 cache，不在输入时扫描磁盘。触发时机总表见 [index.md](./index.md) §3。

未来索引更新策略（index updater、定时器 / 文件变化回调触发、增量扫描）见 [index.md](./index.md) §7。

## 11. Engine 语义

当前 engine 不做跨 provider 去重。本次升级应在每个 provider 内完成打分和排序后、追加到最终结果前增加去重。

去重规则：

- 主 key 为 `SearchResult.ID`。
- 相同 ID 下，先出现的 provider 获胜，因为 provider 顺序就是优先级顺序。
- 本次升级不把 plugin row 合并进一等 Core row。
- 除非产品要求变化，limit 继续使用 `DefaultLimit = 9`。

排序保持 provider 内排序：

```text
provider fuzzy score + frecency boost
tie-break by Title ascending
```

本次升级明确不做跨 provider 的全局分数合并，因为这会改变当前 Web tail fallback 语义。

## 12. Service API

当前绑定方法：

- `Search(query string) ([]core.SearchResult, error)`
- `Launch(id string) error`
- `RunAction(id string) ([]core.SearchResult, error)`

目标新增：

- 给 settings UI 使用的 Folder source 管理方法。
- 通用 Action 执行入口。

建议 service 方法：

```go
func (s *SearchService) FolderSources() ([]core.FolderSource, error)
func (s *SearchService) AddFolderSource(path string, maxDepth int) (core.FolderSource, error)
func (s *SearchService) RemoveFolderSource(id string) error
func (s *SearchService) SetFolderSourceEnabled(id string, enabled bool) error
func (s *SearchService) RefreshFolderSource(id string) error
func (s *SearchService) Execute(id string, actionID string) ([]core.SearchResult, error)
```

Settings 页如果需要原生目录选择器，可额外提供：

```go
func (s *SearchService) PickFolderSourcePath() (string, error)
```

该方法只负责打开系统目录选择器并返回路径，不写入配置、不触发扫描。实际添加仍通过 `AddFolderSource(path, maxDepth)` 完成，便于测试和手动输入路径复用同一条逻辑。

`Launch(id)` 可以继续作为执行 primary action 的兼容 wrapper。`RunAction(id)` 可以在前端迁移完成前继续承载插件二级视图。

插件结果在本次升级中暂时只接入 `PrimaryAction`。当前插件转换逻辑只保留第一个合法 action 作为 primary，本次不扩展插件 API 的多 action 语义。`Actions []ActionItem` 先服务 Core 一等结果，例如 Folder；后续插件 API 升级时，再允许插件结果填充同一套 `Actions`。

`Execute(id, actionID)` 行为：

- 空 `actionID` 执行 `PrimaryAction`。
- 非空 `actionID` 按 ID 查找 `Actions`。
- 插件 callback action 返回二级结果列表。
- 非插件终态 action 返回 nil / 空列表，UI 隐藏窗口。
- 成功执行后，对被激活的结果 ID 记录 usage。

## 13. Platform 要求

增加平台抽象，不要在 core 中导入具体 OS 包：

```go
type PathOpener interface {
    OpenPath(path string) error
    RevealPath(path string) error
}
```

macOS 实现：

- `OpenPath`：使用系统默认行为打开路径。
- `RevealPath`：在 Finder 中 reveal 路径。

未支持平台应返回 `platform.ErrUnsupported`，并保持可编译。

## 14. UI 要求

搜索 UI 要求：

- 继续保持 30ms 输入防抖。
- 除非产品行为另行调整，空查询继续不展示列表。
- Folder row 使用明确的文件夹图标或 monogram fallback。
- Enter 执行 primary action。
- 存在 `reveal` action 时，`cmd+enter` 执行 reveal。
- 存在 `copy-path` action 时，`cmd+c` 执行 copy path。
- 第一版 Folder release 可以不做 action menu，但结果模型必须支持。

Settings UI 要求：

- 左侧导航新增 `Folders` 栏，和现有 `General` / `Plugins` / `About` 同级。
- `Folders` 栏用于管理 Folder sources，不混入插件管理页。
- 顶部提供添加 source 的控件：`Choose Folder...` 使用原生目录选择器；也允许手动输入路径，便于粘贴 `~/Code` 这类路径。
- 添加表单包含 `MaxDepth` 数字输入，默认值为 `1`，最小值为 `1`。非法值在前端阻止提交，后端仍做校验。
- 点击 Add 后调用 `AddFolderSource(path, maxDepth)`；添加成功后立即显示该 source，并展示扫描中 / 已索引数量 / 错误状态。
- Source 列表展示：路径、enabled 状态、`MaxDepth`、索引目录数量、最近扫描时间、最近错误。
- 每行支持启用 / 禁用。禁用只从搜索结果中移除，不删除配置和 cache entries；重新启用后先恢复已有 cache，再按策略后台刷新。
- 每行支持 Refresh，调用 `RefreshFolderSource(id)`，只刷新该 source。
- 每行支持 Remove，删除 source 配置并从 `cache/folder-index.json` 移除该 source 的 entries。
- 页面提供 Refresh All，刷新所有 enabled sources。
- 扫描错误展示在对应 source 行内，不应导致 settings 页整体失败或全局搜索失败。
- 搜索主窗口不提供 Folder source 配置入口；配置只在 Settings 的 `Folders` 栏完成。

## 15. 测试要求

必需单元测试：

- 当前启动流程不在 `ServiceStartup` 前打开 bbolt store。
- App startup load 从 `cache/app-index.json` 填充 provider cache，不依赖即时扫描。
- Folder startup load 从 `cache/folder-index.json` 填充 provider cache，不依赖即时扫描。
- App background rescan 成功后原子替换 `cache/app-index.json` 和 provider cache。
- Folder startup background refresh 失败不清空已有 cache。
- Folder provider 只匹配 basename，不匹配完整路径。
- `core.Store` folder source CRUD。
- Index cache load/save、损坏 JSON 降级、原子替换。
- Folder source index replacement 会重写 `cache/folder-index.json` 并更新 provider cache。
- App / Folder provider 使用预构建 `SearchIndex.Keys`，搜索路径不重新构造 names/search keys。
- Folder scanner depth 行为。
- Folder scanner exclusion 行为。
- Folder provider fuzzy search 和 empty-query 行为。
- Folder provider result actions。
- Engine 按 ID 去重，且先出现的 provider 获胜。
- 插入 folders 后，Engine 仍保持 Web fallback 在最后。
- Service execution 正确分发 `ActionOpenPath`、`ActionRevealPath` 和 `ActionCopyText`。
- 未支持平台 stub 保持可编译。

Settings UI 验收：

- Settings 左侧出现 `Folders` 导航项。
- 可以通过目录选择器或手动路径添加 source。
- 添加 source 后列表立即更新，并触发扫描 / cache 更新。
- 启用、禁用、刷新、删除 source 后，搜索结果和 `cache/folder-index.json` 状态一致。
- 单个 source 扫描失败时，只在该行显示错误，不影响其它 source 和搜索。

必需命令检查：

```sh
go test ./internal/...
GOOS=linux go build ./...
```

如果 `frontend/dist` 尚不存在，Linux build 仍可能因为 embed 前置条件失败；但 platform layer 的编译错误不可接受。

## 16. 迁移计划

1. 在 `internal/core` 增加 model 字段和 action kinds。
2. 增加 folder sources 的 store 支持，以及 app/folder index cache 文件读写组件。
3. 增加 folder scanner / provider 及测试。
4. 将 provider 顺序接线为 `calc, apps, folders, plugins, web`。
5. 在 `ServiceStartup` 中加载 `cache/app-index.json` / `cache/folder-index.json`，并后台刷新 apps / enabled folder sources。
6. 增加 path open / reveal platform interface 和 service execution。
7. 绑定 model / service 变化后重新生成 Wails bindings。
8. 更新前端激活逻辑，使用 primary / secondary actions。
9. 在 settings UI 增加 Folder sources 管理。
10. 实现落地后更新 `docs/features.md`。

## 17. 已定设计决策

- App result ID 直接使用 `app:<bundleID>`；缺 bundle ID 时使用 `app:path:<absolute-app-path>`。
- Folder 第一版只匹配 basename，不匹配完整路径。
- App / Folder index 放入独立 cache JSON 文件，不进入 bbolt。
- 当前阶段 App / Folder 每次启动都后台扫描；未来抽成独立 index updater，由定时器或系统 / 文件变化回调触发。
- `ActionItem.Shortcut` 第一版使用 `cmd+enter` 这类字符串。
- 插件结果本次只接入 `PrimaryAction`；`Actions []ActionItem` 先用于 Core 一等结果，后续插件 API 再扩展多 action。
