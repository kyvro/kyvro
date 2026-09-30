# 插件市场

本文档介绍 Kyvro 插件市场架构和规范。插件开发见 [plugins.md](./plugins.md)；插件声明的 `permissions` 字段语义见 [permissions.md](./permissions.md)。

## 增量更新机制

为减少网络流量和提高启动速度，插件市场采用增量更新机制：

1. **lastUpdated 检查**：应用启动时首先拉取 `lastUpdated` 文件（仅包含时间戳）
2. **时间戳对比**：与本地缓存 `~/Library/Application Support/Kyvro/lastUpdated` 对比
3. **条件更新**：只有时间戳不一致时才拉取完整的 `list.json`
4. **本地缓存**：最新数据缓存到本地，下次启动优先使用缓存

**数据流：**
```
启动 → 拉取 lastUpdated (小文件) → 对比本地时间戳
    ↓
    一致 → 使用本地缓存 (无网络请求)
    不一致 → 拉取 list.json → 更新本地缓存
```

## 插件仓库

官方插件托管在 GitHub：**https://github.com/kyvro/plugins**

### 目录结构

```
plugins/
├── list.json                    # 插件索引文件（必需）
├── lastUpdated                  # 时间戳文件（必需，用于增量更新）
├── com.kyvro.textsnippets/
│   └── 1.0.0.zip               # 插件压缩包
├── com.kyvro.github/
│   ├── 1.0.0.zip
│   └── 2.1.0.zip               # 多版本支持
└── com.example.encode/
    └── 1.0.0.zip
```

**本地缓存结构：**
```
~/Library/Application Support/Kyvro/
├── list.json                    # 缓存的插件列表
├── lastUpdated                  # 缓存的时间戳
├── data.db                      # 应用数据
└── plugins/                     # 插件目录
    ├── com.kyvro.textsnippets/
    └── com.kyvro.github/
```

### list.json 格式

```json
{
  "version": 1,
  "lastUpdated": "2026-08-26T00:00:00Z",
  "plugins": [
    {
      "id": "com.kyvro.textsnippets",
      "name": "Text Snippets",
      "description": "文本片段扩展，支持日期/时间模板和 UUID 生成",
      "author": {
        "name": "Kyvro Team"
      },
      "permissions": [],
      "platforms": ["darwin", "linux", "windows"],
      "versions": ["0.9.0", "1.0.0"],
      "minVersions": ["0.1.0", "0.1.0"]
    }
  ]
}
```

`version: 1` 为唯一线上格式（精简元数据 + `versions` / `minVersions` 平行数组，见下节）；客户端仅接受该版本，其他版本号一律拒绝。

### lastUpdated 文件格式

`lastUpdated` 文件只包含一个时间戳，格式为 ISO 8601：

```
2026-08-26T00:00:00Z
```

当 `list.json` 更新时，需要同步更新 `lastUpdated` 文件。

### list.json 字段说明

| 字段 | 类型 | 必需 | 说明 |
|-----|------|------|------|
| `id` | string | ✅ | 插件 ID（反向域名格式） |
| `name` | string | ✅ | 显示名称 |
| `description` | string | ✅ | 插件描述 |
| `author` | object | ✅ | 作者信息（`name` 必需，`url` 可选） |
| `permissions` | array | ✅ | 最新版本声明的权限（语义见 [permissions.md](./permissions.md)；用于市场列表权限预览） |
| `platforms` | array | ⭕ | 支持平台（默认全平台） |
| `versions` | array | ✅ | 可用版本列表（升序 SemVer）；最新版本 = 末位 |
| `minVersions` | array | ✅ | 与 `versions` 等长的平行数组：`minVersions[i]` 为 `versions[i]` 支持的最低 Kyvro 版本（SemVer），见下节 |

已移除字段：`repository` / `homepage` / `keywords` / `category` / `stats` 不再收录（源码与主页信息属于插件仓库本身，统计字段暂无消费方，registry 保持精简）；单值 `minHostVersion` 与顶层 `version` 分别被 `minVersions` 与 `versions` 末位取代。

### 版本适配（versions / minVersions）

registry 不再声明单一 `minHostVersion`，而是为每个插件版本单独声明适配线，让旧 App 也能安装到可用的旧版本插件，而不是对最新版本直接报不兼容：

- **选版规则**（安装与自动升级共用）：从 `versions` 最高版本向下找第一个满足 `当前App版本 >= minVersions[i]` 的版本；全部不满足 → 显示为不兼容（INCOMPATIBLE_VERSION）
- **权威校验**：registry 数组仅用于选版预检；zip 包内 manifest 的 `minHostVersion` 在加载时由 `ParseManifest` 权威校验，registry 数据应与其保持一致
- 下载 URL 规则不变，按选定版本拼接

### 下载 URL 拼接规则

插件下载 URL 根据插件 ID 和版本号自动拼接：

```
https://raw.githubusercontent.com/kyvro/plugins/main/{plugin-id}/{plugin-id}-{version}.zip
```

**示例：**
- 插件 ID: `com.kyvro.textsnippets`
- 版本: `1.0.0`
- 下载 URL: `https://raw.githubusercontent.com/kyvro/plugins/main/com.kyvro.textsnippets/com.kyvro.textsnippets-1.0.0.zip`

## 安装插件

### 应用内安装（推荐）

设置窗口 → Plugins 栏内置插件市场：浏览 registry 列表（本地缓存 + `lastUpdated` 增量检查），一键 Install / Uninstall。

- 安装：下载 `<plugin-id>-<version>.zip` 并解压到插件目录，随后热加载（无需重启）；下载版本由选版规则决定（见「版本适配」）
- 卸载：移除插件目录并即时移出搜索轮换
- 已安装与 registry 插件在设置页合并展示（`AllPlugins`），按启用状态排序；已安装行带来源标记（Marketplace / Offline，见下文「安装来源与自动升级」）

### 离线导入（本地 zip 包）

无网络环境或安装未上架插件时，设置窗口 → Plugins 栏 → **Import zip…** 选择本地 `.zip` 包即可安装，无需手动解压或重启：

- **导入即校验**：manifest 必须通过完整协议校验（schemaVersion / id / SemVer / main 防逃逸 / prefix 声明），声明的 `main` 文件必须存在于包内；任一不满足则整个导入失败、不落盘
- **包布局**：`plugin.json` 在 zip 根目录或单一顶层目录内均可（GitHub "Source code" 下载的 zip 直接可用）；包内子目录结构原样保留
- **同版本覆盖**：重复导入同一 `<id> <version>` 时替换旧目录，并写入 `current.json` 固定到该版本
- **与在线安装混用**：两种来源共用 `<plugins>/<id>/<version>/` 布局，多版本目录并存；离线导入把激活版本切到导入版本（允许比线上版本旧，即降级），在市场重新 Install 则切回线上版本；卸载按插件 ID 移除全部版本与 `current.json`，同时清除持久化的禁用状态，重装后以启用态加载
- **热加载**：导入成功后立即重建命令索引并进入搜索轮换，无需重启；安装 / 导入 / 卸载触发的重载为全量对账——被替换或已移除的插件即时退出管理器并停掉其运行时（不留幽灵条目、不泄漏资源）
- **安全**：拒绝路径逃逸条目（zip-slip）、`__MACOSX` / `.DS_Store` 元数据、含多个 `plugin.json` 的歧义包

### 安装来源与自动升级

安装来源记录在插件目录的 `current.json`：

```json
{ "version": "1.0.0", "source": "market" }
```

- 在线安装写 `source: "market"`；离线导入（Import zip…）写 `source: "local"`；手动拷入目录（无 `current.json`）视同 `local`
- 设置页已安装列表按 source 显示来源标记（Marketplace / Offline），区分哪些插件由市场管理
- source 只记录最近一次安装/导入；多版本并存时以 `current.json` 当前指向的来源为准

**自动升级**仅作用于 `source: "market"` 的已安装插件（`local` 插件由用户自行管理，永不自动改动）：

1. **时机**：启动后台执行——市场列表增量检查发现新 `list.json` 后触发，不阻塞启动与搜索
2. **选版**：对每个 market 插件，用选版规则（见「版本适配」）计算 App 兼容的最新版本；SemVer 高于当前激活版本才升级（不跨过适配线降级）
3. **安装**：走与在线安装相同的管道——多版本目录并存、`current.json` 切到新版本（source 保持 `market`）、热加载；启用 / 禁用状态不变（禁用的插件只更新文件，保持禁用）
4. **失败处理**：保留旧版本目录与 pin，仅记日志——激活版本未切换即天然回滚；旧版本目录的清理由后续「版本管理」处理

> 来源标记与自动升级已实现：pin 读写见 `internal/plugin/manifest.go`（`ReadCurrentPin` / `SourceMarket` / `SourceLocal`），安装管道见 `internal/plugin/installer.go`（`InstallRemote`），后台升级见 `service/search_service.go`（`autoUpgradePlugins`）。

### 手动安装

设置窗口的 **Import zip…**（见上节）覆盖绝大多数手动安装场景；若需要直接操作目录：

1. 访问 https://github.com/kyvro/plugins
2. 浏览插件目录，选择需要的插件
3. 下载对应版本的 `.zip` 文件
4. 解压到插件目录：
   ```
   ~/Library/Application Support/Kyvro/plugins/<plugin-id>/<version>/
   ```
5. 重启 Kyvro（或在设置窗口 Plugins 栏通过 "Open Plugins Folder" 打开目录后操作）

## 插件开发

如果你是开发者，想要创建自己的插件，请参考 [插件开发指南](./plugins.md)。

## 插件市场计划

### 短期（M2）
- ✅ **UI 市场**：设置窗口内置市场浏览与一键安装/卸载（已交付）
- ✅ **离线导入**：设置窗口 Import zip… 选择本地包安装，校验 + 热加载（已交付）
- ✅ **list.json**：精简字段 + `versions` / `minVersions` 版本适配（客户端 `RemotePlugin` / `SelectVersion` 已落地；registry 仓库按此格式首发）
- ✅ **来源标记**：`current.json` 记录 `source`（market / local），已安装列表显示来源徽章
- ✅ **自动升级**：market 来源插件启动后台热升级（见「安装来源与自动升级」）
- **版本管理**：多版本目录与 `current.json` 已支持；旧版本目录清理由升级后自动处理待实现
- **依赖处理**：处理插件间依赖关系

### 中期（M3）
- **社区插件**：支持第三方插件提交到 GitHub
- **评分和评论**：插件评分系统（届时需重新引入 registry `stats` 字段）
- **分类浏览**：按分类筛选插件（需恢复 registry `category` / `keywords` 字段）

### 长期（M4+）
- **自动发布**：GitHub Actions 自动构建和发布
- **插件模板**：脚手架工具快速创建插件
- **API 生态**：扩展插件 API（network, filesystem, shell）
- **UI DSL**：自定义插件界面
- **后台任务**：支持后台运行的插件
