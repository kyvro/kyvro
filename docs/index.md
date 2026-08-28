# Kyvro 索引系统（index）

本文档描述搜索索引的分类、存储布局、扫描（scan）时机与建立/更新流程。规格背景见 [search.md](./search.md)，已实现功能总览见 [features.md](./features.md)。

文档分工：本文负责索引相关内容（分类、存储、扫描时机、建立与更新、降级规则）；搜索架构、Result / Action 模型、Engine 语义、Service API 与 UI 要求由 [search.md](./search.md) 负责，两文以引用互链。

## 1. 索引分类

| 索引 | 数据性质 | 存储位置 | 是否可重建 | 生命周期 |
|---|---|---|---|---|
| App index | 可重建缓存 | `cache/app-index.json` | ✅ 后台重扫 | 进程内 + 磁盘缓存 |
| Folder index | 可重建缓存 | `cache/folder-index.json` | ✅ 重扫 enabled sources | 进程内 + 磁盘缓存 |
| folder-sources | 用户状态 | bbolt `folder-sources` bucket | ❌ 不可丢弃 | 永久（除非用户删除） |
| usage (frecency) | 用户状态 | bbolt `usage` bucket | ❌ 不可丢弃 | 永久 |
| 内存 SearchIndex | 运行时结构 | 进程内存 | ✅ 由缓存条目重建 | 随进程 |

核心原则：**用户状态与可重建索引分离**。bbolt 只存不能随意丢弃的数据（usage、source 配置、settings、snippets、插件状态/存储）；App / Folder 索引是纯派生数据，放独立 JSON 缓存文件，损坏或丢失时后台重扫即可恢复，也让 `data.db` 不被大批量目录条目撑大。

### 1.1 内存 SearchIndex（泛型）

App 与 Folder 共用同一套内存索引结构（`internal/core/searchindex.go`）：

```go
type SearchIndex[T any] struct {
    Items  []T      // 索引条目（稳定顺序）
    Keys   []string // 扁平化搜索 key
    ItemOf []int    // Keys[i] -> Items 下标
}
```

- 构建时机（`BuildSearchIndex`）：启动读缓存后、App rescan 完成后、Folder source 增/删/改/刷新后。
- **搜索路径禁止重建 keys**：`Search` 只读预构建 `index.Keys`，每条目取多 key 最优分。
- 泛型类型不出现在任何 Wails 绑定签名中。

### 1.2 缓存文件格式

两个缓存文件共用同一 JSON 信封（`internal/core/folder.go`）：

```go
type AppIndexFile / FolderIndexFile struct {
    Version   int       // 当前 core.IndexVersion = 1
    UpdatedAt time.Time
    Entries   []AppIndexEntry / FolderIndexEntry
}
```

- App entry：`ID`（`app:<bundleID>`，缺省 `app:path:<绝对路径>`）、`Name`、`Path`、`BundleID`、`IconPath`、`AltNames`、`SearchKeys`（显示名 / 未本地化名 / 文件名基名 / 拼音全拼 / 拼音首字母）。
- Folder entry：`ID`（`folder:<绝对路径>`）、`Name`（basename）、`Path`、`SourceID`、`SearchKeys`（仅 basename）、`UpdatedAt`。

`SearchKeys` 是 Kyvro 自己的预计算字段，不是 fuzzy 库内部索引；启动时从缓存读 entries 直接恢复内存索引，搜索时对内存 keys 调 `fuzzy.Find`。**不要把 `sahilm/fuzzy` 的内部数据 dump 到磁盘**——库暴露的 `Find` / `FindFrom` / `FindNoSort` 都是按查询即时遍历字符串集合的函数，没有可持久化、可反序列化的索引对象；持久化 Kyvro 自己的原始 entry 与可稳定重建的 search keys。后续若需要真正的持久化倒排索引，应单独设计 Kyvro-owned 格式，不绑定 fuzzy 实现细节。

## 2. 存储布局

```text
~/Library/Application Support/Kyvro/
  data.db                    bbolt：usage / folder-sources / settings / snippets /
                             plugins-state / 插件 storage 命名空间
  plugins/                   插件安装目录
  cache/
    app-index.json           App 索引缓存（Version=1）
    folder-index.json        Folder 索引缓存（所有 source 的条目合并存储，
                             条目按 SourceID 归属；per-source 替换见 §4.3）
    app-icons/               NSWorkspace 渲染的 App 图标 PNG
                             （<bundleID>.png，仅无 .icns 的 bundle；
                             bundle 元数据更新后自动重渲染）
```

读写由 `internal/indexcache` 统一管理：

- **原子写**：序列化到 `<file>.tmp` 后 `os.Rename` 替换，崩溃不留半截 JSON。
- **损坏降级**：JSON 解析失败只记日志，返回空索引（不报错），后台重扫重建。
- **未来版本降级**：文件 `Version` 大于当前 `core.IndexVersion` 时同样视为空。
- **内存镜像**：`Open` 时预载 folder-index.json，per-source 查询（如快速 re-enable）无磁盘 I/O。
- bbolt 仅在 `ServiceStartup` 内（单实例保护之后）打开。

## 3. Scan 时机

### 3.1 App 扫描（`internal/providers/apps`）

| 时机 | 行为 |
|---|---|
| 启动 | `ServiceStartup` 末尾 `go appsProvider.Warmup()`：后台全量扫描，不阻塞 service ready；启动首查由 `cache/app-index.json` 播种，不等扫描 |
| 每次搜索 | `maybeRescan()`：距上次扫描 ≥ 60s（`rescanInterval`）时触发**后台**节流重扫，绝不阻塞当前搜索；`scanning` CAS 保证同一时刻至多一个在途扫描 |
| 扫描失败 | 保留旧快照（不 swap、不写缓存），仅记日志 |
| 扫描成功 | ① 换内存 entries+index 快照 → ② 锁外触发 cacheHook 原子重写 `app-index.json`（hook 在 `Warmup` 前注册） |

扫描范围：`/Applications`、`/System/Applications`、`~/Applications`（深度 ≤ 2，跳过 `LSUIItem`）。

### 3.2 Folder 扫描（`internal/folders`）

| 时机 | 行为 |
|---|---|
| 启动 | `ServiceStartup` 末尾 `go folderCtl.BackgroundRefresh(ctx)`：后台重扫全部 enabled sources；首查由 `folder-index.json` 播种 |
| 添加 source | `AddSource` **同步**扫描（校验 → 扫描 → 持久化 → 写缓存 → 换 provider），返回即可搜 |
| 手动刷新 | `RefreshSource(id)`（单个）/ `RefreshAll()`（全部 enabled）；disabled source 返回显式错误 |
| 重新启用 | `SetEnabled(true)`：先从缓存恢复 provider 条目，再**后台**刷新 |
| 刷新失败 | 记 `LastScanError`，**不清空**已有缓存与 provider 条目；`RefreshAll` 中每个 source 独立失败 |

扫描规则（`scanner.go`）：

- `filepath.WalkDir`，只收目录；root 自身不入索引。
- 深度：`MaxDepth=1` = 仅直接子目录；到上限 `SkipDir`。
- 排除表（目录名完全匹配，进入前跳过）：`.git` `node_modules` `vendor` `dist` `build` `.next` `.turbo` `.cache`；隐藏目录（`.` 开头）一律跳过。
- 不跟随 symlink（WalkDir 天然 lstat 语义）。
- 支持 `context.Context` 取消，逐条目检查。

## 4. 索引建立 / 更新流程

### 4.1 启动（`ServiceStartup`，顺序固定）

```text
core.OpenStore(data.db)            确保 usage + folder-sources bucket（单实例保护后）
indexcache.Open(<configDir>/cache) MkdirAll + 预载 folder-index 镜像
plugin manager LoadAll / installer
cache.LoadAppIndex() ────────────► apps.NewWithCache(source, entries)   // 失败降级为空
folders.NewController + LoadAtStartup
    ListFolderSources (bbolt)
    cache.LoadFolderIndex()
    过滤 enabled sources ────────► folderProvider.Replace(entries)
engine = [calc, apps, folders, plugins, web]
appsProvider.SetCacheHook(SaveAppIndex)   // 必须在 Warmup 前
go appsProvider.Warmup()                  // 后台：rescan → 换快照 → 写缓存
go folderCtl.BackgroundRefresh(ctx)       // 后台：逐 enabled source 刷新
```

目的：窗口/热键/首查不被磁盘扫描阻塞；后台刷新完成后，下一次搜索自然看到新索引。

### 4.2 App 更新链

```text
rescan 成功
  -> swap(entries, SearchIndex)          // idxMu 内原子成对替换
  -> cacheHook: SaveAppIndex(AppIndexEntries(list))  // 锁外，tmp+rename 原子写
```

### 4.3 Folder 更新链（顺序约定：先缓存文件，后 provider；锁不反向嵌套）

```text
Add    : 校验(~展开/绝对化/stat/查重/depth>=1)
         -> Scan -> PutFolderSource(bbolt)
         -> cache.ReplaceFolderEntriesForSource(id, entries)   // 重写 folder-index.json
         -> prov.ReplaceSourceEntries(id, entries)             // 重建内存索引
Remove : store.DeleteFolderSource -> cache.DeleteFolderEntriesForSource
         -> prov.DeleteSourceEntries          // 三层齐清
Enable : 从 cache.FolderEntriesForSource 免 I/O 恢复 provider -> 后台刷新
Disable: 仅删 provider 条目；缓存保留（快速 re-enable）
Refresh: Scan ->（失败：记 LastError，保留旧条目）
         （成功：cache 替换该 source 条目 -> provider 替换）
```

`folder-index.json` 合并存储所有 source 的条目（条目带 `SourceID`），per-source 替换只重写该 source 的部分、其余原样保留；删除同理隔离。

## 5. 搜索路径（运行时只读）

```text
Engine.Search(query)
  providers 固定顺序: calc -> apps -> folders -> plugins -> web
  每个 provider: 预构建 SearchIndex.Keys 上 fuzzy.Find（空查询返回全量 score 0）
  -> frecency 加分（usage 以结果 ID 为 key）
  -> provider 内排序（分数降序，tie-break 标题升序）
  -> 按 ID 去重（先出现者胜） -> 截断 limit(9) -> 追加
```

索引稳定性依赖**稳定 ID**（前缀规则详见 [search.md](./search.md) §5）：`app:<bundleID>`（缺省 `app:path:<abs>`）、`folder:<abs>`、`calc:<expr>`、`web:<query>`、`plugin:<pluginID>:<id>`。

## 6. 降级与一致性规则汇总

- 缓存文件缺失/损坏/未来版本 → 空索引 + 后台重扫，不阻断启动。
- 扫描失败 → 保留旧快照/旧条目，绝不因失败清空数据。
- 单个 folder source 失败 → 仅该 source 记 `LastScanError`，不影响其它 source 与搜索。
- 所有缓存写入原子（tmp + rename），不留 `.tmp` 残留。
- 设置页展示 per-source：`IndexedCount`、`LastScannedAt`（零值显示 "—"）、`LastScanError`、`Scanning`。

## 7. 未来索引更新策略

当前策略（每次启动后台扫描 + 搜索时节流）之外的中期方向：

- source 级别 `LastScannedAt` / `LastScanError` 展示（已实现，见 §6 与设置页 Folders 栏）。
- 将 App / Folder 扫描从启动流程抽离为独立 index updater。
- index updater 可由定时器触发，例如每 N 分钟或每天空闲时刷新。
- index updater 可由系统 / 文件变化回调触发，例如 App 目录变化、Folder source 下目录变化。
- 支持增量扫描，只更新发生变化的 source 或路径。
- 启动阶段最终只负责加载 cache；是否立即 refresh 交给 index updater 策略决定。

## 8. 代码位置

| 组件 | 路径 |
|---|---|
| 索引文件格式 / IndexVersion | `internal/core/folder.go` |
| 泛型内存索引 | `internal/core/searchindex.go` |
| 缓存读写（原子/降级/镜像） | `internal/indexcache/cache.go` |
| folder-sources CRUD | `internal/core/store.go` |
| App 扫描与缓存钩子 | `internal/providers/apps/apps.go` |
| key 展开（拼音等） | `internal/providers/apps/keys.go` |
| Folder scanner | `internal/folders/scanner.go` |
| Folder provider | `internal/folders/provider.go` |
| Folder 生命周期控制器 | `internal/folders/controller.go` |
| 启动编排 | `service/search_service.go` (`ServiceStartup`) |
