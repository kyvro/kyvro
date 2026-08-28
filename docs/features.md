# Kyvro 当前支持的功能（v0.1，macOS 首发）

本文档描述当前代码库已实现并验证的功能。核心搜索升级规格见 [search.md](./search.md)。

## 核心功能

### 全局呼出

- ⌥Space（Alt+Space）全局热键呼出/隐藏启动器窗口，任何应用前台时均生效
- 热键冲突处理：⌥Space 被其他应用占用时自动回退注册 ⌥⌘Space（Alt+Cmd+Space），两个都失败时输出日志提示
- 窗口失焦自动隐藏（点击别处即消失）
- Esc 隐藏窗口
- 菜单栏托盘（Kyvro 模板图标，自动适配浅色/深色菜单栏）：左键/右键点击均打开托盘菜单（原生高亮），菜单项 Show Kyvro / Settings… / Quit；搜索窗口不再由托盘点击直接呼出，统一走热键或菜单项

### 应用搜索与启动

- 应用扫描范围：`/Applications`、`/System/Applications`、`~/Applications`（含一层子目录，如 Utilities）
- 解析每个 `.app` 的 `Contents/Info.plist`，读取 `CFBundleDisplayName` / `CFBundleName` / `CFBundleIdentifier` / `CFBundleIconFile`；跳过 `LSUIItem` 后台代理类应用；按 bundle ID 去重
- 本地化显示名：按用户偏好语言（`.GlobalPreferences.plist` 的 `AppleLanguages` / `AppleLocale`，含 `zh-Hans-CN` → `zh_CN.lproj` 等回退链）读取 `<lang>.lproj/InfoPlist.strings` 里的 `CFBundleDisplayName` / `CFBundleName`；无 `.strings` 文件、只带编译版 `Resources/InfoPlist.loctable` 的现代系统应用走该二进制表回退（键为 `zh_CN` 等老式 locale 名），中文系统显示中文名（钉钉、百度网盘、系统设置、计算器）；无本地化时回退 Info.plist 原始值
- 应用图标：优先解析 bundle 图标文件路径（`CFBundleIconFile`，缺省回退 `AppIcon.icns`）；两类不可解码情形都在**扫描时**经 AppKit 落盘为 `~/Library/Application Support/Kyvro/cache/app-icons/<bundleID>.png` 并让 `IconPath` 直接指向缓存文件——① 无可解析 `.icns` 的资源目录型应用（系统设置等，图标只在编译版 `Assets.car` 内）由 NSWorkspace 渲染；② `.icns` 轻量嗅探（只读各元素 8 字节头部判 PNG 载荷）发现纯 Go 解码器读不了（如 JPEG2000 载荷的 ic09——OpenVPN Connect）时经 NSImage 按 64pt 绘制转码（与 NSWorkspace 渲染同管线同尺寸，@2x ≈ 128px），bundle 元数据或 .icns 比缓存新即自动重转；写入均为临时文件+rename 原子替换，转换/渲染失败回退原 .icns 路径或留空（运行期 `/appicon` 服务端仍有 AppKit 兜底）。图标经 asset middleware 的 `GET /appicon?path=<icon>` 提供：`.icns` 在服务端解码为 PNG（WebKit 的 `<img>` 不支持 icns；取最大内嵌表示，进程内缓存，上限 64MB）；纯 Go 解码失败时（如个别 Electron 应用的 icns 只含 JPEG2000 载荷的 ic09，纯 Go 解码器会跳过 JP2）经 AppKit `NSImage` 兜底转 PNG 同样进缓存；带 `Cache-Control: private, max-age=86400`；解码失败或无图标时回退首字母 monogram
- 启动时后台全量扫描；之后每次搜索时若距上次扫描超过 60 秒，后台节流重扫（不阻塞当前搜索）
- 模糊搜索（基于 `sahilm/fuzzy`，大小写不敏感，支持跨单词字母匹配，如 "gc" → Google Chrome）
- 多 key 匹配：每个应用的匹配目标 = 本地化显示名 + 未本地化原始名（plist `CFBundleDisplayName`/`CFBundleName`）+ bundle 文件名基名（如 `BaiduNetdisk_mac`），取多 key 中的最高模糊分
- 拼音搜索：含汉字的名称自动生成全拼与首字母两份拼音 key（钉钉 → `dingding` / `dd`，微信Mac → `weixinMac` / `wxMac`），中文应用可用拼音或英文原始名命中；`go-pinyin` 纯 Go 实现
- 通过 `open -a <path>` 启动，默认复用已运行实例

### 文件夹搜索（一等结果）

- 用户在设置窗口 Folders 栏配置根目录（支持 `~/Code` 输入与原生目录选择器），每个 source 可配扫描深度（默认 1 = 仅直接子目录）与启用状态
- 扫描规则：只索引目录；跳过隐藏目录与 `.git` / `node_modules` / `vendor` / `dist` / `build` / `.next` / `.turbo` / `.cache`；不跟随 symlink；支持 `context.Context` 取消
- 结果 ID `folder:<绝对路径>`；匹配 key 仅 basename（不匹配路径片段，避免弱相关目录浮前）；与 App 搜索共用预构建泛型 `core.SearchIndex`（搜索路径不重建 key 列表）
- 文件夹行：文件夹图标、`~` 缩写显示路径副标题；Enter 打开（Finder）、`⌘↵` 在 Finder 中 Reveal、`⌘C` 复制绝对路径（均为声明式 ActionItem，UI 不按类型分派执行）
- Provider 顺序：`calc → apps → folders → plugins → web`，folders 在插件结果前、web 恒最后；engine 按 ID 跨/内 provider 去重，先出现者胜
- 状态存储分离：source 配置存 bbolt `folder-sources` bucket（用户状态）；扫描索引存 `cache/folder-index.json`（可重建缓存，临时文件 + rename 原子写，损坏/未来版本降级为空）
- 启动从缓存播种 provider（无需扫描即可搜到上次结果），后台刷新 enabled sources；禁用 source 保留缓存条目（快速 re-enable），刷新失败不清空已有缓存/provider 条目，每个 source 独立失败、行内报错
- App 索引缓存：`cache/app-index.json`（rescan 成功后锁外原子写回）；App 结果 ID 切换为 `app:<bundleID>`（缺省 `app:path:<绝对路径>`），旧 usage key 成为孤儿数据（无害），App frecency 因此重置

### 结果与 Action 模型

- `SearchResult`：`Kind`（app/folder/url/command/text，仅用于展示）、`Data map[string]any` 结构化元数据、`PrimaryAction`（Enter 执行）、`Actions []ActionItem`（按 ID/Shortcut 寻址的次级操作）
- `ActionKind`：`ActionLaunchApp` / `ActionOpenURL` / `ActionCopyText` / `ActionPlugin` / `ActionOpenPath` / `ActionRevealPath`（int，只追加不重排）
- 统一执行入口 `Execute(id, actionID)`：空 actionID 走 PrimaryAction，非空按 `Actions[].ID` 查找；插件 callback 返回二级结果列表（推送二级视图），其余终态动作返回空（UI 隐藏窗口）；成功后按结果 ID 记录 usage
- 原 `Launch` / `RunAction` 已删除，前端一次性迁移到 `Execute`

### 排序（frecency）

综合评分公式：

```
score = 模糊匹配分 + 8 × log₂(启动次数 + 1) + 12 × 2^(−距上次使用小时数 / 72)
```

- 模糊相关性始终优先，使用频率/近期度只在相近结果间重排
- 空查询时列出全部应用，纯按 frecency 排序（未使用过的按名称字母序）——此为引擎层行为，当前 UI 对空查询不请求、不展示列表

### 内置计算器

- 输入算式即时求值：支持 `+ - * / % ^`（幂右结合）、括号、一元正负、小数；`×` `÷` 空格与千分位逗号自动归一化（含中文逗号）
- 纯数字（如 `123`、`-5`）不触发，需至少一个二元运算符；语法错误 / 除零 / 溢出（±Inf）静默不显示
- 浮点噪声清理：`0.1+0.2 → 0.3`（超过 12 位有效数字时按 12 位重格式化）
- 结果固定显示在列表首位（Spotlight 风格）：标题 `= <值>`，副标题显示算式；回车通过 Wails 剪贴板复制结果（`wails://` 下 `navigator.clipboard` 不可用，故走 Go 侧）
- 实现：`internal/providers/calc` 递归下降解析器（float64，无第三方依赖），provider 优先级最高（apps 之前、web 之前）

### 兜底搜索

- 无匹配或查询非空时，结果尾部固定追加一条 `Search Google for "<查询词>"`，回车后用默认浏览器打开 Google 搜索

### 键盘交互

- 输入框自动聚焦，输入 30ms 防抖后触发搜索
- 空查询不展示列表：每次呼出均为空白初始状态，输入后才出结果（核心引擎仍支持空查询返回全量 frecency 列表，仅 UI 层拦截）
- 隐藏时清空搜索词与结果列表：Esc / 启动应用后先隐藏再清空，失焦自动隐藏通过 visibilitychange 清空——清空动作全部发生在窗口不可见期间，呼出时不会闪旧内容
- ↑↓ 选择结果（列表自动滚动到选中项），回车执行主操作，Esc 隐藏（二级视图中 Esc 先返回上一级）
- `⌘↵` 执行选中行 `Shortcut == "cmd+enter"` 的次级操作（如文件夹 Reveal in Finder），无则回退主操作；`⌘C` 执行 `cmd+c` 次级操作（如复制文件夹路径），无则放行浏览器原生复制
- 鼠标悬停高亮、单击执行主操作

### 使用历史持久化

- bbolt 单文件数据库：`~/Library/Application Support/Kyvro/data.db`（buckets：`usage`、`folder-sources` 及 settings/snippets/plugins 等命名空间）
- 每次执行结果后按结果 ID 记录 `count` 与 `lastUsed`，重启后 frecency 排序依然生效
- 索引缓存目录 `~/Library/Application Support/Kyvro/cache/`：`app-index.json`、`folder-index.json`（可重建数据，损坏自动降级重建）；索引分类、扫描时机与建立流程详见 [index.md](./index.md)

### Text Snippets（全局文本扩展）—— 已临时下线，待后续重新开放

> 该功能代码全部保留（`internal/core/snippets.go` 与 `template.go`、`internal/platform/darwin/expander.go`、设置界面 Text Snippets 页），但当前版本停用：
> 启动不再初始化 `SnippetsService` / `TextExpander`（CGEventTap 全局键盘钩子不挂载，也不再需要辅助功能权限），设置窗口隐藏 Text Snippets 标签页。
> 恢复入口见 `service/search_service.go` 与 `frontend/src/Settings.vue` 中的 `TODO(snippets)` 标注；bbolt `snippets` namespace 中用户已保存的片段数据不受影响。
> 以下为下线前的完整功能描述：

- 全局键盘监听：任何软件中输入触发词后自动扩展为替换文本
- 静态文本：直接替换为固定内容（如 `dd` → `Dear Team`）
- 动态模板：支持 `${func("args")}` 语法，由插件提供动态能力
  - 官方 Text Snippets Plugin 提供 `${date("format")}` 等函数
  - `${date("YYMMDD")}` → `260825`（当前日期）
  - `${date("YYYY-MM-DD")}` → `2026-08-25`
  - `${now("HH:mm:ss")}` → `14:30:00`（当前时间）
  - `${timestamp()}` → `1724587800000`（Unix 时间戳）
  - `${uuid()}` → `550e8400-e29b-41d4-a716-446655440000`（随机 UUID）
  - 支持日期格式：`YYYY`（4位年份）、`YY`（2位年份）、`MM`（月份）、`DD`（日期）、`HH`（24小时）、`mm`（分钟）、`ss`（秒）
- 可扩展：插件可注册自定义模板函数（详见 plugins.md）
- 管理界面：设置窗口 Text Snippets 栏支持添加/删除/启用/禁用片段、全局开关
- 权限要求：需要 macOS 辅助功能权限（Accessibility），设置界面可引导授权
- 存储位置：`~/Library/Application Support/Kyvro/data.db`（`snippets` namespace）

### 插件系统

可扩展的插件架构，支持用户和开发者自定义搜索功能与命令。详见 [plugins.md](./plugins.md)；插件接口（manifest 字段、导出函数、结果行/动作 schema、PluginContext 能力面）的权威参考在仓库 `plugin-sdk/index.d.ts`（模块化声明，与 `internal/plugin` 实现保持对齐；插件入口以 JSDoc 标注 `@type {import("@kyvro/plugin-sdk").Plugin}` 即可获得编辑器全量补全）。

- **扩展能力**：前缀触发实时搜索、静态命令（模糊匹配浮出）、数据持久化、多种操作类型（打开链接/复制/二级交互）
- **安装位置**：`~/Library/Application Support/Kyvro/plugins/`
- **官方插件**：`com.kyvro.github`（GitHub 仓库搜索，`gh <query>` 或 `gh owner/repo`）
- **示例插件**：`com.example.encode`（Base64 编码/URL 编码，包含 storage 演示）
- **管理界面**：设置窗口 Plugins 栏支持启用/禁用、查看权限、打开插件目录

### 设置窗口（通用 / 插件管理 / Folders / 文本片段(隐藏) / 关于）

- Text Snippets 导航项当前临时下线（功能代码保留，见上文标注），侧边栏仅显示 General / Plugins / Folders / About

- 托盘菜单 Settings… 打开独立常规窗口（760×640、居中、深色），与 summon 窗口共用同一 SPA（`/#settings` hash 路由）；窗口点击时惰性创建（不预加载，零启动开销），已打开时点击仅聚焦，关闭后再点菜单重建；创建/聚焦时激活应用（`activateIgnoringOtherApps`），确保盖在前台应用（如终端）之上
- 左右结构：左侧导航栏（Kyvro 品牌标识 + General / Plugins / Folders / Text Snippets / About + 底部版本号）；品牌标识与菜单栏模板图标同源（tray-template 几何，currentColor 渲染）
- 通用栏（General）：外部浏览器选择——open-url 动作（web 兜底搜索、插件链接如 gh）默认走系统默认浏览器，可指定已安装浏览器（自动探测 /Applications 与 ~/Applications 下的 Safari/Chrome/Edge/Arc/Firefox/Brave/Chromium/Opera/Vivaldi/DuckDuckGo，darwin 经 `open -a` 打开）；偏好持久化于 bbolt `settings` 命名空间
- 插件栏：插件 manifest `icon` 字段声明的图标（相对路径、防逃逸校验，经现有 `/appicon` 路由服务，svg/png 等格式；缺失或损坏回退统一拼图占位图标）/版本/id/声明权限 chips，每行启用开关；搜索结果行与 Execute 二级列表同样携带插件图标（搜索结果行的插件图标在统一 36px 灰底容器内按 18px 图形尺寸渲染，与文件夹/计算器/网页行一致；应用图标仍满幅显示）
- 禁用：立即移出搜索轮换并持久化（bbolt `plugins-state` 命名空间，重启仍生效）；启用：恢复轮换并清空连续超时计数；自动禁用（3 次超时）的插件显示 "disabled after repeated timeouts" 标记，同样可一键恢复
- "Open Plugins Folder" 在 Finder 中打开插件目录（不存在则创建）——手动安装/卸载入口
- Folders 栏：添加 source（路径文本框支持粘贴 `~/Code` + "Choose Folder…" 原生目录选择器 + MaxDepth 数字输入默认 1 最小 1，前端阻断非法值、后端复校验）；source 行展示路径 / enabled 开关 / MaxDepth / 已索引数量 / 最近扫描时间（零值显示 "—"）/ 行内扫描错误；每行 Refresh / Remove，头部 Refresh All；单 source 失败仅行内报错，不影响页面与其他 source
- 关于栏：应用图标、版本（`Version` 绑定，`AppVersion` 常量）、简介、全局热键提示
- 绑定方法：`Search` / `Execute` / `Plugins` / `SetPluginEnabled` / `RevealPluginsFolder` / `FolderSources` / `AddFolderSource` / `RemoveFolderSource` / `SetFolderSourceEnabled` / `RefreshFolderSource` / `RefreshAllFolderSources` / `PickFolderSourcePath` / `Version`

## 应用行为

- 单实例：重复启动时第二个进程立即退出（exit 0）并唤起已运行实例
- 无 Dock 图标、无主菜单栏（macOS Accessory 激活策略），通过热键或托盘交互
- 窗口：无边框、透明背景 + macOS 毛玻璃（Translucent Backdrop）、置顶、不可缩放，680×440，水平居中、距主屏顶部固定 140px（避开菜单栏/刘海）
- 深色 UI：Vue 3 + TypeScript + Tailwind CSS v4，应用结果显示真实 .icns 图标（36px 圆角），加载失败回退首字母 monogram（按名称哈希取色）；图标懒加载，Web 搜索结果仍显示地球图标

## 架构分层（为后续功能预留）

```
main.go                  窗口/热键/托盘/单实例（Wails 接线层）
service/                 Wails 绑定薄桥：SearchService.Search / Execute / 文件夹与插件管理方法
internal/core/           纯 Go 核心：模型（Kind/PrimaryAction/Actions）、泛型 SearchIndex、引擎、frecency、bbolt 存储
internal/folders/        文件夹搜索：scanner（WalkDir 深度/排除）、provider、controller（source 生命周期）
internal/indexcache/     app/folder JSON 索引缓存（原子写、损坏降级）
internal/plugin/         插件平台：manifest、权限、goja 运行时、存储、聚合 Provider
internal/providers/      apps（预构建索引 + 缓存钩子）+ calc + web（Google 兜底）
internal/platform/       AppSource / AppLauncher / PathOpener 接口；darwin 实现 + 非 darwin stub
frontend/                Vue 3 UI，bindings 由 wails3 生成到 frontend/bindings
plugins-example/         示例插件（拷入 plugins 目录即装）
```

- core 层禁止依赖 Wails；平台能力全部经 `internal/platform` 接口注入
- internal/plugin 为纯 Go，仅依赖 internal/core；goja 不泄漏到 core/service 之外
- Windows / Linux 已有接口与空实现（可编译，返回"平台未支持"），待后续填充

## 开发与验证

```sh
# 开发（热重载，vite 端口 9245）
wails3 dev

# 生产构建（前端 + Go 二进制 → bin/kyvro）
wails3 build          # 或：pnpm --dir frontend build && go build -o bin/kyvro .

# 测试与静态检查
go vet ./...
go test ./internal/... ./service/...

# 修改 service 方法后重新生成前端绑定
wails3 generate bindings -d frontend/bindings -clean -names
```

测试覆盖：模糊命中、排序稳定性、frecency 提升可复现（含跨实例持久化）、兜底固定尾部、结果截断、engine ID 去重（先者胜/内折/limit 边界/provider 顺序 web 恒尾）、扫描节流、LSUIItem 跳过、嵌套目录发现、图标路径解析（含无扩展名/回退/不可解析）、图标 HTTP 端点（icns→PNG 解码、损坏文件 404、缓存命中、扩展名白名单、缓存头、非图标路由透传）、本地化名称（候选链展开、本地化命中与回退）、计算器求值（优先级/结合性/归一化/浮点清理/拒绝用例、结果可复制）、拼音与备选名匹配（全拼/首字母/原始名/文件名、key 去重、混合名透传）、SearchIndex（key 扁平化去重、每 item 最优 key、降序稳定）、folder source CRUD（往返/排序/删除幂等/bucket 自动创建）、索引缓存（app/folder 往返、损坏降级、未来版本降级、per-source 替换隔离、删除隔离、无 tmp 残留、目录自动创建、镜像预载）、文件夹 scanner（深度 1/2、排除表、隐藏目录、symlink 不跟随、ctx 取消、根校验、entry 形状）、文件夹 provider（空查询全量、仅 basename 匹配、spec 结果形状断言、source 增删改后搜索反映）、folders controller（add 三层接线、重复路径、remove 三处齐清、disable 留 cache/re-enable 恢复、刷新失败保留旧 entries 且记 LastError、refresh-all 隔离失败、启动缓存播种、~ 展开）、apps provider（缓存播种不扫盘、rescan 原子换快照 + hook 锁外触发、失败保留旧快照、app: ID 形态）、service（New 不开 bbolt、Execute 六种 ActionKind 分派与 usage 记录、ActionItem 寻址与未知 ID 报错）、插件（manifest 校验全矩阵、版本目录选择含 current.json 覆盖与垃圾目录忽略、权限解析/拒绝/不可用、存储隔离与持久、结果转换与非法条目丢弃、超时中断与迟到结果丢弃与恢复、异常转错误、storage 缺失/往返、命令回调往返、activate Promise、LoadAll 容错、Shutdown 无泄漏、前缀门控、命令浮出、并行合并、3 次超时禁用、引擎顺序断言、示例插件端到端）。

## 尚未支持（后续版本）

- Text Snippets / 全局文本扩展：功能已开发完成并验证，当前临时下线（代码保留，恢复方式见上文标注），计划后续版本重新开放
- 剪贴板历史
- 插件市场 / 安装升级回滚 / Secrets / network・filesystem・shell・clipboard・background API / UI DSL（插件 M2+）
- ESM 插件与 TypeScript 工具链（当前仅 CommonJS）
- 文件索引 / Spotlight 集成
- 设置界面扩展：热键自定义、插件权限粒度授权（当前仅插件启用/禁用）
- Windows / Linux 平台实现
