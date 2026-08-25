# Launcher Plugin Specification

插件平台规范 v0.1

适用于：Wails v3 + Go Core + TypeScript Plugin SDK

目标：低内存、低启动延迟、跨平台、可审计、可扩展

核心原则：Core 负责安全边界与运行机制；Plugin 负责具体“搜什么、做什么”。

# 目录

- 1. 目标与非目标

- 2. 产品分层与能力边界

- 3. 插件生命周期

- 4. Manifest 规范

- 5. 插件 Runtime 与隔离

- 6. Plugin SDK

- 7. Extension Points

- 8. UI DSL

- 9. 权限与安全模型

- 10. Host API

- 11. 数据与存储

- 12. Marketplace / Registry

- 13. 安装、升级与回滚

- 14. 版本兼容与能力协商

- 15. 性能与资源预算

- 16. 开发者工具链

- 17. 官方插件与社区插件

- 18. 错误模型与可观测性

- 19. MVP 实施顺序

- 20. 示例插件

# 1. 目标与非目标

本规范定义一个与 Wails 解耦的 Launcher Plugin Platform。Wails v3 只作为桌面 Host 和 UI 容器；插件运行协议、权限、市场、SDK 均由 Launcher 自身定义。

目标是让 macOS、Windows、Linux 共用同一套插件生态，同时保留每个平台的原生系统能力。插件默认不获得文件系统、网络、Shell、剪贴板、密码库等敏感能力。

| 目标 | 说明 |
| --- | --- |
| 稳定 API | 插件 SDK 与宿主实现解耦，宿主重构不要求插件重写 |
| 安全 | 权限最小化、可审计、可撤销、敏感操作需显式授权 |
| 性能 | 插件不可阻塞 Launcher 主搜索路径；超时可取消 |
| 跨平台 | 同一插件可声明平台能力差异 |
| 生态 | 官方插件与社区插件使用同一公开 API |

## 1.1 非目标

- 不允许第三方插件直接注入 Go Service 或调用任意 Wails Binding。

- 不把 Go .so/.dll/.dylib 动态库作为默认插件格式。

- 不允许插件无授权读取 Vault 全部秘密。

- 不保证所有 OS 能力跨平台一致；通过 capability 协商处理。

# 2. 产品分层与能力边界

```
Launcher Core
├── Global Hotkey / Window Lifecycle
├── Search Dispatcher / Ranking
├── Action Router
├── Plugin Runtime / Permission Engine
├── Platform Adapter
├── App & File Provider
└── Secure Boundary

Official Plugins
├── Clipboard
├── Calculator
├── Snippets
├── Window Manager
├── SSH / Git
└── Developer Tools

Community Plugins
├── GitHub / Jira / Notion
├── Docker / Kubernetes
├── AI Providers
└── Other integrations
```

| 能力 | 归属 | 原因 |
| --- | --- | --- |
| 全局快捷键 | Core | 系统生命周期基础设施 |
| App 搜索 | Core | Launcher 基础体验 |
| 文件搜索 | Core | 性能敏感，直接对接 Spotlight/Windows Search |
| Ranking | Core | 统一排序，不能被插件绕过 |
| Plugin Runtime | Core | 安全边界 |
| Clipboard History | Official Plugin | 可独立演进但需官方维护 |
| SSH/Git | Official Plugin | 开发者增强能力 |
| GitHub/Jira/AI | Community/Official Plugin | 第三方服务、变化快 |

# 3. 插件生命周期

```
Discover → Download → Verify → Install → Enable → Load
   → Activate → Invoke/Search → Suspend → Update → Disable/Uninstall
```

| 阶段 | Host 行为 |
| --- | --- |
| Discover | 读取 Marketplace/Registry 元数据 |
| Verify | 校验 SHA-256、发布签名、manifest schema、兼容版本 |
| Install | 解压到版本目录，写入安装记录 |
| Enable | 用户确认权限后进入可加载状态 |
| Load | 创建独立 runtime context，注册 extension points |
| Activate | 首次命令/搜索或声明的 activation event 触发 |
| Suspend | 空闲时释放 runtime，保留插件存储 |
| Update | 新版本旁路安装，通过后切换 current |
| Rollback | 更新失败恢复旧版本 |
| Uninstall | 删除代码；数据是否保留由用户选择 |

# 4. Manifest 规范

推荐文件名：plugin.json。Manifest 必须可静态审查，禁止通过执行代码才能确定权限或命令。

```json
{
  "schemaVersion": 1,
  "id": "com.example.github",
  "name": "GitHub",
  "version": "1.2.0",
  "description": "Search GitHub repositories",
  "author": { "name": "Example", "url": "https://example.com" },
  "main": "dist/index.js",
  "minHostVersion": "0.1.0",
  "platforms": ["darwin", "windows", "linux"],
  "activationEvents": ["onCommand:github.search", "onSearchPrefix:gh"],
  "permissions": ["network:api.github.com", "storage"],
  "commands": [
    { "id": "github.search", "title": "Search GitHub", "keywords": ["github", "gh"] }
  ]
}
```

| 字段 | 必需 | 规则 |
| --- | --- | --- |
| schemaVersion | 是 | 整数；用于 Manifest 迁移 |
| id | 是 | 反向域名，全局唯一，不可在升级中改变 |
| version | 是 | SemVer |
| main | 是 | 相对路径，禁止逃逸插件目录 |
| minHostVersion | 是 | 宿主最低兼容版本 |
| platforms | 否 | 默认全部支持 |
| activationEvents | 否 | 未声明时仅显式调用激活 |
| permissions | 否 | 默认空权限 |
| commands | 否 | 静态命令声明 |

# 5. 插件 Runtime 与隔离

推荐 V1 使用 JavaScript/TypeScript 插件，运行时优先考虑 Goja；后续可增加独立 Node/Deno Worker 作为增强运行时，但不能改变 Host API 的安全语义。

| 方案 | 优点 | 缺点 | 建议 |
| --- | --- | --- | --- |
| Goja | 单二进制、启动快、Go 集成简单 | Node/npm 兼容有限 | V1 默认 |
| QuickJS | 轻量、隔离好 | Go 生态集成成本稍高 | 可选 |
| Node sidecar | 生态最强 | 内存高、进程管理复杂 | 高级插件 |
| Go native plugin | 性能高 | ABI/平台/崩溃/签名问题 | 不作为市场格式 |

## 5.1 Runtime 约束

- 每插件独立上下文；禁止共享全局可变对象。

- 所有 Host API 调用必须经过 Permission Engine。

- Search/Command 支持 context cancellation。

- 单次搜索默认软超时 150ms；网络型 provider 默认不进入即时搜索主路径，除非显式触发。

- 异常不得使 Host 崩溃；runtime panic/exception 统一转换为 PluginError。

# 6. Plugin SDK

```ts
export interface PluginContext {
  command: CommandAPI;
  ui: UIAPI;
  storage: StorageAPI;
  clipboard: ClipboardAPI;
  filesystem: FileSystemAPI;
  network: NetworkAPI;
  shell: ShellAPI;
  secrets: SecretsAPI;
  browser: BrowserAPI;
  system: SystemAPI;
}

export interface PluginModule {
  activate?(ctx: PluginContext): Promise<void> | void;
  deactivate?(): Promise<void> | void;
}
```

SDK 只暴露稳定能力接口，不暴露 Wails、Go 内部结构、数据库连接或 OS 原始句柄。

# 7. Extension Points

| Extension Point | 用途 | 是否进入 Universal Search |
| --- | --- | --- |
| Command | 显式命令，如 GitHub Search | 是 |
| SearchProvider | 返回搜索候选 | 是 |
| ActionProvider | 给 Result 增加动作 | 间接 |
| View | List/Grid/Detail/Form | 否 |
| ContextAction | 基于当前文件/文本/URL 提供动作 | 是 |
| BackgroundJob | 低频后台同步/缓存 | 否 |
| Settings | 插件配置项 | 否 |
| QuicklinkProvider | 动态链接/快捷入口 | 是 |

```ts
export interface SearchProvider {
  id: string;
  search(query: string, ctx: SearchContext): Promise<SearchResult[]>;
}

export interface SearchResult {
  id: string;
  title: string;
  subtitle?: string;
  icon?: IconRef;
  scoreHint?: number;
  actions: Action[];
  metadata?: Record<string, string>;
}
```

插件可以提供 scoreHint，但最终排序权属于 Core Ranking Engine，避免插件通过任意高分霸占结果。

# 8. UI DSL

第三方插件默认禁止直接访问宿主 DOM。插件输出声明式 UI，由 Host 统一渲染，以保证主题、键盘导航、Accessibility 与安全。

| 组件 | 用途 |
| --- | --- |
| List / List.Item | 搜索结果、命令列表 |
| Grid / Grid.Item | 图片、卡片集合 |
| Detail | Markdown/结构化详情 |
| Form | 输入与配置 |
| ActionPanel | 动作菜单 |
| EmptyState | 空状态 |
| Progress | 长任务状态 |

```json
{
  "type": "List",
  "items": [
    {
      "id": "repo:wailsapp/wails",
      "title": "wailsapp/wails",
      "subtitle": "Create desktop apps using Go",
      "actions": [
        { "type": "open-url", "url": "https://github.com/wailsapp/wails" }
      ]
    }
  ]
}
```

# 9. 权限与安全模型

权限模型采用“默认拒绝 + 最小权限 + 作用域限定 + 运行时可撤销”。Manifest 权限仅代表申请，不代表自动获批。

| 权限示例 | 说明 | 敏感级别 |
| --- | --- | --- |
| network:api.github.com | 仅访问指定域名 | 中 |
| clipboard:read | 读取剪贴板 | 高 |
| clipboard:write | 写剪贴板 | 中 |
| filesystem:~/Projects/** | 限定目录访问 | 高 |
| shell:git | 仅执行 git | 高 |
| system:open-url | 打开 URL | 低 |
| secrets:github.com | 申请 GitHub 相关凭据 | 极高 |
| background | 允许后台运行 | 中 |

## 9.1 密码库 / Secrets 特别规则

- 禁止 vault.getAll()、listAllSecrets() 等批量读取 API。

- Secrets 请求必须绑定 resource/domain/secret-id。

- 高敏请求可要求 Touch ID / Windows Hello / 用户确认。

- Secret 默认不返回给插件 JS；优先提供“代执行”能力，例如由 Host 代填 Header 或进程环境变量。

- 插件卸载后立即撤销所有 Secret grants。

```
await ctx.secrets.withSecret({
  resource: "api.github.com",
  secretId: "github-token"
}, async (secretHandle) => {
  return ctx.network.fetch("https://api.github.com/user", {
    auth: secretHandle
  });
});
```

# 10. Host API

| API | 典型能力 | 备注 |
| --- | --- | --- |
| clipboard | readText/writeText/readImage | 受权限控制 |
| filesystem | read/write/list/stat | 路径必须经过 scope 校验 |
| network | fetch/websocket | 域名白名单 |
| shell | execute/spawn | command 与 cwd 双重校验 |
| browser | open/searchTabs | 浏览器能力按平台/插件支持 |
| storage | get/set/delete | 插件私有 namespace |
| system | notify/openPath/openURL | 不可绕过权限 |
| secrets | request/use handle | 极高敏感 |
| ui | showList/showForm/toast | 声明式 |
| context | selectedText/selectedFiles/frontmostApp | 按需授权 |

# 11. 数据与存储

```
~/.launcher/
├── plugins/
│   └── com.example.github/
│       ├── 1.1.0/
│       ├── 1.2.0/
│       └── current.json
├── plugin-data/
│   └── com.example.github/
├── cache/
└── registry/
```

- 插件代码目录只读；更新通过新版本目录切换。

- plugin-data 与插件版本解耦，升级不自动删除。

- 缓存可由 Host 随时清理，插件不能把关键状态只放 cache。

- 敏感数据不得放普通 storage，必须使用 Secrets API。

# 12. Marketplace / Registry

V1 推荐 GitHub Registry + GitHub Releases，减少基础设施成本；V2 再迁移到独立 Marketplace API + 对象存储/CDN。

```
plugin-registry/
├── registry.json
└── plugins/
    ├── github.json
    ├── docker.json
    └── ssh.json
```

| Registry 元数据 | 说明 |
| --- | --- |
| id/name/description | 基础展示 |
| latestVersion | 最新稳定版 |
| downloadURL | 压缩包地址 |
| sha256 | 完整性校验 |
| signature | 发布签名 |
| publisher | 发布者身份 |
| permissions | 展示权限变更 |
| minHostVersion | 兼容性过滤 |
| verified | 官方审核标记 |

## 12.1 发布流程

```
Developer → Build → GitHub Release → Submit Registry PR
→ CI Schema Check → Static Scan → Signature Verify
→ Review → Merge → Marketplace Visible
```

# 13. 安装、升级与回滚

1. 下载到临时目录。

1. 验证 SHA-256、签名和 Manifest。

1. 检查 Host/平台兼容性。

1. 比较新增权限；有新增敏感权限必须重新提示用户。

1. 解压到新的版本目录。

1. 执行非代码型数据迁移声明；禁止安装脚本任意执行。

1. 健康检查通过后原子切换 current。

1. 若加载失败，自动回滚上一版本并记录错误。

# 14. 版本兼容与能力协商

```
host.capabilities = {
  "ui.list": 2,
  "ui.form": 1,
  "filesystem.scoped": 1,
  "secrets.handle": 1,
  "context.selectedText": 1
}
```

插件不得只靠 hostVersion 猜能力。Host 应提供 capability/version 查询；插件可声明 requiredCapabilities 与 optionalCapabilities。

# 15. 性能与资源预算

| 指标 | 建议预算 |
| --- | --- |
| Launcher 热启动窗口显示 | 目标 < 50ms（平台相关） |
| 本地 SearchProvider 首批结果 | 目标 < 30ms |
| 单插件即时搜索软超时 | 150ms |
| Plugin activate | 目标 < 100ms |
| 单轻量插件 idle 内存 | 尽量 < 5–10MB；Goja 可共享引擎实现降低开销 |
| 后台任务 | 默认禁止常驻；需 background 权限 |
| Marketplace 请求 | 不应阻塞 Launcher 主 UI |

即时搜索必须允许先返回 Core 结果，再增量追加插件结果；网络搜索建议显式命令触发，避免输入每个字符都请求公网。

# 16. 开发者工具链

```bash
npm create launcher-plugin
launcher plugin dev
launcher plugin validate
launcher plugin test
launcher plugin build
launcher plugin pack
launcher plugin publish
```

| 工具 | 能力 |
| --- | --- |
| create-plugin | 生成 TypeScript 模板 |
| dev | 热加载/调试插件 |
| validate | Schema、权限、路径、兼容性检查 |
| test | SDK mock + 单元测试 |
| build | TS → JS，生成产物 |
| pack | 生成 .zip + sha256 |
| publish | 创建 Release / Registry PR |

# 17. 官方插件与社区插件

| 类型 | 示例 | 策略 |
| --- | --- | --- |
| Core | App/File Search、Ranking、Runtime | 随主程序发布 |
| Official Plugin | Clipboard、Calculator、Snippets、Window、SSH、Git | 官方签名，默认可推荐 |
| Community Plugin | GitHub、Jira、Notion、AI、Docker | 独立发布，权限审查 |
| Experimental | 新 API 验证 | 显式开启实验通道 |

官方插件必须使用和社区插件一致的公开 SDK。若官方功能必须调用私有 API，应先评估是否需要把该能力正式纳入 SDK。

# 18. 错误模型与可观测性

```
type PluginErrorCode =
  | "PERMISSION_DENIED"
  | "TIMEOUT"
  | "CAPABILITY_UNAVAILABLE"
  | "INVALID_ARGUMENT"
  | "NETWORK_BLOCKED"
  | "HOST_API_ERROR"
  | "PLUGIN_EXCEPTION"
  | "INCOMPATIBLE_VERSION";
```

- Host 记录插件启动时间、Search 延迟、错误率、超时次数。

- 日志默认按插件 namespace 分离。

- 禁止插件读取其他插件日志。

- 敏感值必须在日志层做 redaction。

- 用户可在设置中查看“该插件最近访问了哪些权限”。

# 19. MVP 实施顺序

| 阶段 | 交付 |
| --- | --- |
| M0 | Manifest、PluginManager、Goja Runtime、Command、Storage |
| M1 | SearchProvider、Action、UI List/Detail、权限系统 |
| M2 | Clipboard/Filesystem/Network/Shell scoped API、官方 Calculator/Clipboard 插件 |
| M3 | GitHub Registry、安装/升级/回滚、插件设置页 |
| M4 | ContextAction、Secrets handle、官方 SSH/Git 插件 |
| M5 | Form/Grid、后台任务、独立 Marketplace、签名体系增强 |

# 20. 示例插件：SSH

SSH 插件读取用户允许的 ~/.ssh/config，建立本地 SearchProvider；不保存私钥，不实现 SSH 协议，只调用系统 ssh/终端。

```json
{
  "schemaVersion": 1,
  "id": "org.launcher.ssh",
  "name": "SSH",
  "version": "0.1.0",
  "main": "dist/index.js",
  "permissions": [
    "filesystem:~/.ssh/config:read",
    "shell:ssh",
    "system:terminal"
  ],
  "commands": [
    { "id": "ssh.connect", "title": "Connect SSH Host" }
  ]
}
```

```
export const provider: SearchProvider = {
  id: "ssh.hosts",
  async search(query, ctx) {
    const hosts = await loadHosts();
    return hosts
      .filter(h => fuzzyMatch(query, h.alias))
      .map(h => ({
        id: h.alias,
        title: h.alias,
        subtitle: `${h.user ?? ""}@${h.hostname}`,
        actions: [
          { id: "connect", title: "Connect", command: "ssh.connect", args: [h.alias] },
          { id: "copy", title: "Copy Host", type: "copy", value: h.hostname }
        ]
      }));
  }
};
```

# 附录 A：核心设计决策

- 插件平台独立于 Wails：Wails 可替换，插件生态不随宿主 UI 重写。

- 第三方插件不直接注入 DOM，不直接绑定 Go Service。

- 官方插件吃同一套公开 SDK，以持续验证 SDK 能力。

- 文件/App 搜索、Ranking、权限、Runtime 属于 Core。

- 密码管理属于 Secure Core/Official Module；社区插件只能通过受限 Secrets API 使用。

- V1 先把插件“安全、稳定、快”做对，再追求 npm 全生态兼容。

Spec 状态：Draft v0.1。建议实现 M0/M1 后，根据真实插件开发体验再冻结 Plugin API v1。
