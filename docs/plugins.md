# 插件系统

本文档是 Kyvro 插件扩展模块的**协议文档**：定义插件的声明方式、统一接口、结果与动作、上下文 API、权限与错误契约。宿主实现（`internal/plugin/`）与 SDK 类型定义（`plugin-sdk/index.d.ts`，`@kyvro/plugin-sdk`）以本协议为准；协议统一 Command 模型，当前代码已按本协议落地（迁移记录见 §11）。

插件市场见 [plugin-marketplace.md](./plugin-marketplace.md)；权限系统详见 [permissions.md](./permissions.md)。

## 协议总览

| 协议部分 | 载体 | 说明 |
|-----|-----|-----|
| Manifest | `plugin.json`（schemaVersion 1） | 静态声明：身份、入口、命令（触发）、权限 |
| 命令 | manifest `commands[]` | 唯一扩展类型；`prefix` 自描述触发，prefix 建入命令索引（建立/移除时机见 [index.md](./index.md)） |
| 统一接口 | `module.exports.onAction` | 列表业务入口；详情走 `renderView` / `onViewEvent` |
| 生命周期钩子 | `module.exports.activate` | 加载时执行一次，非扩展类型接口 |
| 结果行 | `ResultRow[]` | 搜索 / callback 调用返回的结果条目 |
| 动作 | `PluginAction` | `open-url` / `copy` / `callback` / `open-view`，由宿主执行 |
| 上下文 | `activate(ctx)` 参数 | `storage` / `log` / `background` / `network` / `ui` 能力面 |
| 权限 | manifest `permissions` | 决定 ctx 上注入哪些能力 |
| 错误码 | `PluginError` | 所有失败的统一错误契约 |
| 生命周期 | 超时与禁用规则 | 按调用来源分配时间预算 |

每个插件运行在独立沙箱（独立 JS VM，无宿主 API 直通）。一个插件可包含多个命令和详情视图。

## 1. 扩展类型：Command

一个扩展类型对应一份接口契约。当前唯一类型是 **Command**：宿主在查询命中命令时调用插件，插件返回结果行。

命令经 **prefix 索引**触发，调用时机只有两种，接口同为 `onAction`：

| 调用来源 | 声明 | 宿主行为 | 调用时机 |
|-----|-----|-----|-----|
| prefix 命中（live） | `commands[].prefix` | 查询前缀命中命令索引 → live 调用 | **每次键入调用**（150ms 预算），结果进主列表实时刷新 |
| callback 动作 | ResultRow 的 `callback` action | 用户执行行动作 | **调用一次**（5s 预算），返回下一级列表 |

- 两种调用来源共用同一接口、同一结果行 schema；差别只在宿主调用时机与结果去向（主列表 / 下一级列表）
- 当前协议没有显式的类型注册字段，命令由 manifest `commands[]` 自描述

**命令索引**：加载 / 安装插件时把 `commands[].prefix` 建入宿主命令索引，查询在索引上做前缀精确检索，命中才 live 调用插件。匹配路径零 JS、检索 O(1)；禁用 / 卸载时 prefix 同步移出索引。索引的建立与移除时机、存储布局见 [index.md](./index.md)。

### 1.1 计划中的扩展类型（接口待定义）

每个新类型引入时需同时定义其接口契约、激活方式与权限要求，并保持同类型同接口：

| 扩展类型 | 目标 |
|-----|-----|
| Match Handler（`match_handler`） | 消费 Typed Match（如 `math.expression`），按类型分发 |
| Action Provider（`action_provider`） | 为已有 Result 追加可执行操作，优先消费结构化 Handle |
| Background Service（`background_service`） | 常驻服务，需 `background` 权限与资源管理 |
| Agent Tool（`agent_tool`） | 向 Kyvro Agent 暴露结构化工具，权限继承插件自身 |

## 2. Manifest（plugin.json）

### 2.1 安装目录

```
~/Library/Application Support/Kyvro/plugins/<plugin-id>/
├── <version>/           # 版本目录（SemVer）
│   ├── plugin.json      # 必需
│   ├── main.js          # 入口（manifest.main）
│   └── icon.png         # 可选（manifest.icon）
└── current.json         # 可选：{"version": "1.0.0"} 固定版本
```

- 版本选择：`current.json` 优先，否则取最高 SemVer 版本目录，垃圾目录忽略
- manifest `id` 必须与安装目录名一致

### 2.2 字段

| 字段 | 必需 | 约束 |
|-----|-----|-----|
| `schemaVersion` | ✅ | 仅 `1`，其他值拒绝 |
| `id` | ✅ | 反向域名（≥2 个小写 label），与目录名一致 |
| `version` | ✅ | SemVer |
| `minHostVersion` | ✅ | SemVer，不得超过运行中的宿主版本 |
| `main` | ✅ | 版本目录内相对路径，禁止绝对路径与 `..` 逃逸 |
| `name` | — | 显示名，缺省用 id |
| `description` / `author` | — | 描述 / `{name, url?}` |
| `icon` | — | 版本目录内相对路径 |
| `platforms` | — | 声明时必须包含宿主平台（`darwin` 等） |
| `permissions` | — | 权限声明，见 §7 |
| `commands` | — | 命令声明（触发自描述），见 §2.3 |

校验失败返回 `INVALID_ARGUMENT`；schema / 宿主版本 / 平台不匹配返回 `INCOMPATIBLE_VERSION`。

### 2.3 命令（commands）

```json
{
  "id": "github.search",
  "title": "GitHub Search",
  "prefix": "gh"
}
```

| 字段 | 必需 | 约束 |
|-----|-----|-----|
| `id` | ✅ | manifest 内唯一；作为 `onAction` 的 `actionId` |
| `title` | — | 显示名（设置页等），缺省用 id |
| `prefix` | ✅ | 触发前缀，建入命令索引；匹配大小写不敏感 |

## 3. 调用语义

统一接口 `onAction(actionId, args)` 的两种调用来源：

| 调用来源 | `actionId` | `args` | 预算 | 结果去向 |
|-----|-----|-----|-----|-----|
| prefix 命中（live） | 命令 `id` | `[完整查询]` | 150ms | 主列表 |
| callback 动作 | callback 的 `id` | 自定义 `args` | 5s | 下一级列表 |

- 完整查询包含前缀（如 `"gh kyvro"`），插件自行截取业务部分
- live 调用迟到结果丢弃；连续超时见 §9
- 插件返回非数组值按 `INVALID_ARGUMENT` 处理

`actionId` 对宿主是不透明字符串：取值为插件自有的命令 id 或 callback id（插件自己的命名空间），宿主不解释具体值、不按值分派——宿主逻辑只区分上表的调用来源（时机 / 预算 / 结果去向），id 由插件自行分派。

## 4. 导出协议（module.exports）

CommonJS（无 `require` / ESM）；返回值支持同步或 Promise。

| 导出 | 签名 | 说明 |
|-----|-----|-----|
| `onAction` | `(actionId, args: string[]) => ResultRow[] \| Promise` | **列表业务入口**。调用来源见 §3 |
| `activate` | `(ctx) => void \| Promise` | 生命周期钩子，加载时执行一次（唯一生命周期回调，无卸载回调，见 §9）；非扩展类型接口 |

## 5. 结果行与动作

### 5.1 ResultRow

| 字段 | 必需 | 说明 |
|-----|-----|-----|
| `id` | ✅ | 行内唯一；宿主命名空间为 `plugin:<pluginId>:<id>` |
| `title` | ✅ | 标题 |
| `actions` | ✅ | 非空数组；第一个 action 为回车主操作 |
| `subtitle` | — | 副标题 |
| `scoreHint` | — | 0–50 软排序提示；模糊相关性仍主导排序 |

规则：

- 缺少 `id`、`title` 或合法 `actions` 的行被静默丢弃（计数写日志），不影响宿主
- 未知 action 类型使整行失效
- 结果行使用 manifest 图标渲染

### 5.2 PluginAction

| type | 字段 | 宿主行为 |
|-----|-----|-----|
| `open-url` | `url` | 用用户设置的浏览器打开 URL |
| `copy` | `value` | 复制文本到剪贴板 |
| `open-view` | `id`、`args?` | 当前窗口切换为插件布局详情页，见 §13 |
| `callback` | `id`、`args?` | 调用 `onAction(id, args ?? [])`，返回下一级列表；`open-url` / `copy` 执行后隐藏窗口 |

## 6. 上下文（PluginContext）

`activate(ctx)` 收到的能力面。未授权 / 未实现的命名空间**不出现在 ctx 上**，插件应 feature-detect（`if (ctx.storage)`）。

| API | 权限 | 契约 |
|-----|-----|-----|
| `ctx.storage.get/set/delete` | `storage` | 同步 string→string KV；键值经 JS ToString 后持久化到插件专属 bucket `plugin:<id>`，跨升级 / 重载保留 |
| `ctx.log.info/warn/error(...)` | 无 | 写宿主日志：`plugin <id> [<level>]: …` |

## 7. 权限

manifest `permissions` 声明 `<capability>` 或 `<capability>:<scope>` 形式。模型为 default-deny：

| capability | 状态 | 行为 |
|-----|-----|-----|
| `storage` | ✅ 实现 | 声明即授权，注入 `ctx.storage` |
| `network:<hostname>` / `background` | ✅ 实现 | 声明即授权，见 §12 |
| `filesystem:read:<path>` | ✅ 实现 | 精确文件只读，见 §14 |
| 其他 `filesystem` / `shell` / `clipboard` / `secrets` / `system` | ⏳ 保留 | API 不注入；调用返回 `CAPABILITY_UNAVAILABLE` |
| 未知能力 | — | `PERMISSION_DENIED` |

三态校验语义、授权策略、Trust Level 与 Capability 演进方向见 [permissions.md](./permissions.md)。

## 8. 错误码

所有 manifest 加载、调用失败以 `PluginError{pluginId, code, message}` 交换；单插件失败不影响宿主与其他插件。

| 错误码 | 触发 |
|-----|-----|
| `INVALID_ARGUMENT` | manifest / 参数 / 返回值非法 |
| `INCOMPATIBLE_VERSION` | schema / 宿主版本 / 平台不匹配 |
| `PERMISSION_DENIED` | 权限未授予 |
| `CAPABILITY_UNAVAILABLE` | 宿主版本未提供该能力 |
| `TIMEOUT` | 超出调用预算（见 §3 / §9） |
| `PLUGIN_EXCEPTION` | JS 异常 / panic |
| `NETWORK_BLOCKED` | URL 或重定向域名不在授权范围 |
| `HOST_API_ERROR` | 网络传输、响应读取失败或超限 |

## 9. 生命周期与超时

```text
加载：读取 manifest → 校验 → 解析权限 → 创建沙箱 → 注入 ctx
  ↓
activate(ctx)                  2s 预算
  ↓
onAction（prefix live）        每次 150ms 软预算，迟到结果丢弃
onAction（callback）           5s 预算
renderView（打开详情）          5s 预算
onViewEvent → renderView       合计 5s 预算
```

- live 调用连续 3 次超时 → 自动禁用该插件；用户重新启用即恢复并清零计数；自动禁用不持久化，重启重新加载
- 用户启用 / 禁用状态持久化，跨升级 / 重载保留
- **协议不设卸载回调**：禁用 / 卸载 / 重载 / 退出时的清理由宿主负责，不依赖插件主动注销——命令索引、storage 等注册项均由宿主按插件 ID 回收或重建，沙箱 VM 直接销毁
- 若未来扩展类型持有需释放的宿主资源（如常驻任务，已实现的后台能力见 §12），停止语义由该类型自身接口契约定义，不是通用卸载回调（见 §1.1）

## 10. 最小示例

```json
// plugin.json
{
  "schemaVersion": 1,
  "id": "com.example.github",
  "version": "1.0.0",
  "minHostVersion": "0.1.0",
  "main": "main.js",
  "commands": [
    {
      "id": "github.search",
      "title": "GitHub Search",
      "prefix": "gh"
    }
  ]
}
```

```javascript
// main.js
module.exports.onAction = (actionId, args) => {
  const query = args[0] ?? "";          // 完整查询，如 "gh kyvro"
  if (!query.startsWith("gh ")) return [];
  const input = query.slice(3).trim();
  if (!input) return [];

  const rows = [];
  if (/^[\w.-]+\/[\w.-]+$/.test(input)) {
    rows.push({
      id: "repo",
      title: "Open " + input + " on GitHub",
      actions: [{ type: "open-url", url: "https://github.com/" + input }]
    });
  }
  rows.push({
    id: "search",
    title: 'Search GitHub for "' + input + '"',
    actions: [{ type: "open-url", url: "https://github.com/search?q=" + encodeURIComponent(input) }]
  });
  return rows;
};
```

输入 `gh kyvro`：prefix 命中命令索引 → 每次键入调用 `onAction` → 主列表实时出现两行 → 回车打开。

## 11. 迁移与计划

协议迁移已落地（当前代码 = 本协议）：

- `internal/plugin/runtime.go` / `provider.go`：`provider.search` 已删除，live 调用统一走 `onAction`
- `internal/plugin/provider.go` / `manager.go`：关键词模糊匹配路径已删除；触发为 prefix 命令索引——加载 / 启用 / 自动禁用时全量重建，查询在索引上做最长前缀 + 词边界精确检索（索引建立/移除时机见 [index.md](./index.md) §4.4）
- manifest：`commands[].prefix`（必需，小写归一）已生效，`keywords` / `subtitle` / `activationEvents` 已移除——`activate` 在加载时无条件执行，命令触发由 `commands[]` 派生
- runtime：`ctx.template` 注入已移除——Text Snippets 当前下线，模板注册无消费方；功能恢复时随协议一并恢复（Text Snippets 功能描述见 [features.md](./features.md)）
- 插件迁移完成：`com.kyvro.github`、`com.example.encode`、`com.example.datesnippet`；`com.kyvro.textsnippets` 经 feature-detect（`ctx.template` 缺席即不注册）保持可安装
- `plugin-sdk/index.d.ts` 已同步类型定义

后续计划：

- ESM / TypeScript 编译工具链（当前仅 CommonJS）
- Match Handler / Action Provider / Agent Tool 等扩展（后台定时能力已实现，见 §12）——见 §1.1
- 权限实现扩展与 per-plugin 授权 UI——见 [permissions.md](./permissions.md#路线图)
- 自定义 HTML / 组件 UI（当前已提供列表、多 action、左右导航）
- Secrets 管理

## 12. 后台、网络与列表 UI

SDK 类型：`plugin-sdk/index.d.ts`；示例：`plugins-example/com.example.capabilities/`。
官方 Codex 用量插件见 §14。

当前 JS API 不提供搜索索引更新能力；后台任务、网络刷新、storage 写入与列表导航均不触发索引更新。索引更新边界及后续变更约定见 [index.md §4.5](./index.md#45-插件-api-的索引边界与变更约定)。业务缓存可按需使用，搜索入口无需读取缓存。

### 后台定时任务

声明 `background` 权限，在 `activate(ctx)` 中调用：

```js
const task = ctx.background.every(60000, () => {
  // 获取数据并写入 ctx.storage；搜索只读缓存。
});
// ctx.background.cancel(task);
```

间隔 1000–86400000ms，每插件最多 16 个任务，首次执行在一个间隔之后。
回调可返回 Promise，每次最多 5s。同一 VM 串行执行，不重入，繁忙时不积压 tick。
禁用暂停后续执行（已开始调用可完成），重新启用继续计时；卸载、重载、退出自动取消并回收。
窗口隐藏时仍运行，仅应用进程运行期间有效。异常写宿主日志，不纳入搜索三次超时规则。

### 网络请求

声明 `network:api.github.com` 后注入 `ctx.network`；裸 `network` 默认不授权。
精确匹配 hostname，不含端口、不支持通配符/隐式子域名；HTTP/HTTPS 均支持，每次重定向复检，最多 10 次。
URL 内嵌用户名/密码被拒绝，无浏览器 cookie、凭证读取、自动重试或 cookie jar。

```js
const response = ctx.network.request("https://api.github.com/zen", {
  method: "GET", headers: { "User-Agent": "Kyvro-Demo" }
});
if (response.ok) ctx.storage.set("cached", response.body);
```

同步 API，支持 `method`、`headers`、string `body`、`followRedirects`（默认 true；false 返回原始 3xx，Codex 查询使用 false）。跨源重定向不转发插件自定义请求头，禁止 HTTPS 降级至 HTTP。网络错误不包含原始 URL 或底层错误文本，避免查询参数或凭据进入日志。
返回 `{status, ok, headers, body}`，响应头键为小写；非 2xx 正常返回，JSON 使用 `JSON.parse(response.body)`。
请求受当前调用剩余预算与最大 5s 限制；响应最多 2MiB。
网络会阻塞本插件 VM，应在后台刷新或 Enter 回调中调用，避免搜索时发请求。
域名未授权 / 重定向越界返回 `NETWORK_BLOCKED`，超时返回 `TIMEOUT`，传输或超限返回 `HOST_API_ERROR`。
域名授权不包含私网地址或 DNS 解析过滤。

### 列表 UI 与左右导航

`ctx.ui.list(rows)` 无需权限，是标准列表的便利函数；插件布局详情使用 §13 的 `open-view` / `renderView`，不提供 HTML / DOM。
`actions[0]` 是 Enter 主动作，其余合法动作转换为次级动作。
次级动作可声明 `actionId`（行内唯一，缺省用 callback 的 `id`）、`title` 和 `shortcut`。
快捷键支持 `left` / `right` / `cmd+enter` / `cmd+c`。
左右键只在 Enter 后的详情列表生效，替换当前列表，不新增返回层级；Esc 返回入口。
导航动作取自当前选中行，多行详情应为每行附上导航动作。

```js
return ctx.ui.list([{
  id: "page", title: "Model A", subtitle: "示例数据",
  actions: [
    {type: "copy", value: "示例数据"},
    {type: "callback", id: "page", actionId: "next", shortcut: "right", args: ["1"]}
  ]
}]);
```

### 运行 demo

复制示例文件到 `~/Library/Application Support/Kyvro/plugins/com.example.capabilities/0.1.0/` 后重启，
或把 `plugin.json` 和 `main.js` 打包 zip，通过设置的导入插件功能安装。
输入 `plugdemo`，Enter 进入插件定义的详情页，左右键或按钮切换 Background / Network / Storage，Enter 或 Refresh now 手动刷新，Esc 返回搜索结果。
每 60s 后台请求公开 GitHub Zen 接口，展示缓存和更新时间，无需 API Key。

## 13. 插件布局详情页（open-view）

### 打开与返回

`onAction` 返回搜索行时使用新 action；它不调用 `onAction`，而是调用该插件的 `renderView`：

```js
module.exports.onAction = () => [{
  id: "entry", title: "Usage Demo",
  actions: [{ type: "open-view", id: "usage-detail", args: [] }]
}];
```

Enter / 点击后在当前窗口以详情页替换搜索输入和结果列表。详情具有宿主 Back 按钮，Esc 返回之前的列表、查询和选中位置。
仅支持当前窗口，不创建新窗口。插件不能覆盖 Esc，也不能修改宿主搜索索引。
普通 `callback` 仍返回下一级标准列表，`open-url` / `copy` 保持原有行为。

### 布局协议

```js
let selectedModel = 0;
module.exports.renderView = (viewId, args) => ({
  title: "Usage Demo",
  shortcuts: { left: "previous", right: "next" },
  root: {
    type: "column", gap: "large", children: [
      { type: "card", title: "Model " + (selectedModel + 1), children: [
        { type: "text", text: "示例数据，并非真实用量", tone: "muted" },
        { type: "progress", title: "已使用", value: 65, max: 100 }
      ] },
      { type: "row", children: [
        { type: "button", text: "上一个", event: "previous" },
        { type: "button", text: "下一个", event: "next" }
      ] }
    ]
  }
});
module.exports.onViewEvent = (viewId, eventId, args) => {
  if (eventId === "next") selectedModel = (selectedModel + 1) % 3;
  if (eventId === "previous") selectedModel = (selectedModel + 2) % 3;
};
```

布局由插件定义，宿主渲染固定组件：

| type | 字段 | 行为 |
|---|---|---|
| `column` | `children?`、`gap?`、`title?` | 纵向排列 |
| `row` | `children?`、`gap?`、`title?` | 横向排列，空间不足换行 |
| `card` | `children?`、`gap?`、`title?` | 带背景和边框的卡片 |
| `text` | `text`、`tone?` | 转义文本，可换行 |
| `progress` | `value`、`max`、`title?` | 进度条；`max > 0`、`0 ≤ value ≤ max`，显示百分比 |
| `button` | `text`、`event` | 点击发送声明的事件 ID |

`gap`：`small` / `medium` / `large`，默认 medium。
`tone`：`default` / `muted` / `success` / `warning` / `danger`。
`shortcuts` 支持 `left` / `right` / `enter`，值是插件事件 ID；Tab 可聚焦按钮，按钮上的 Enter 优先点击按钮。
不接收 HTML / DOM / 自定义 CSS / 可执行脚本；所有文本按普通文本渲染。

### 事件、预算与会话

- `renderView(viewId, args)` 返回 `ViewDefinition` 或 Promise；首次打开最多 5s。
- `onViewEvent(viewId, eventId, args)` 修改插件状态，可返回 void / Promise；完成后宿主重新调用 `renderView`，两者共用一次 5s 预算。无需在事件处理器返回布局。
- `args` 来自 `open-view` 的 action，事件期间保持不变；局部页面状态由插件管理。全局 `pluginContext` 是实例能力句柄，不是页面会话数据。
- 左右键 / 按钮替换当前详情，不叠加导航层级；请求期间按钮禁用，重复事件忽略。出错保留当前页面并显示错误，仍可 Back / Esc 返回。
- 后台刷新仅更新业务数据，不自动推送页面；下一次页面打开或事件后渲染读取最新数据。
- 宿主仅允许从当前结果会话中的 `open-view` 动作打开详情，后续事件使用随机会话 ID；只接受当前页面声明的按钮 / 快捷键事件。新页面替换旧会话；关闭后会话失效；插件禁用期间拒绝事件，重载/卸载后旧 VM 会话失效。
- 页面最多 256 个节点、嵌套深度 16、JSON 256KiB；单文本最多 16384 字节，标题最多 1024 字节，事件 ID 最多 128 字节。非法组件树返回 `INVALID_ARGUMENT`。

详情 API 和会话均为内存状态，不增加搜索索引更新接口、不写索引文件，边界见 [index.md §4.5](./index.md#45-插件-api-的索引边界与变更约定)。


## 14. 通用文件读取与 Codex 用量插件

声明 `filesystem:read:<path>` 后注入 `ctx.filesystem.readText(path)`。path 必须是绝对路径或 `~/` 开头；只授权一个精确文件，不支持目录、通配符、环境变量或写入。路径归一化后逐项检查授权，拒绝末级符号链接和非普通文件，以 OpenRoot 限制父目录内解析并核对打开前后文件身份。仅返回 UTF-8 文本，最大 2MiB；文件内容和底层读取错误不写入宿主日志。未声明 / 未授权时不注入 API。沿用当前 manifest 声明即授权策略，`GrantDecision` 可拒绝授权。

官方插件：`plugins-official/com.kyvro.codex/`。通过设置导入打包 zip，输入 `codex`，Enter 查看。插件自己读取 `~/.codex/auth.json`、解析 OAuth 登录态、调用 `https://chatgpt.com/backend-api/wham/usage`、处理错误和缓存。宿主没有任何 Codex 专用能力或数据类型。插件声明 `filesystem:read:~/.codex/auth.json`、`network:chatgpt.com`、`storage` 和 `background`；token 在插件内短暂使用，不写入缓存、日志或错误文本，缓存只保存展示字段。

参考本地 CodexBar 的 `docs/codex-oauth.md` 和 `CodexOAuthUsageFetcher.swift` 协议。展示主 / 次额度窗口的已用、剩余百分比、重置时间、订阅计划与额外 Credits；不能换算成剩余请求数或 token 数。首次打开查询，后续读取缓存，按钮或 Enter 刷新，应用运行时每 5 分钟后台刷新。后台更新不主动重绘已打开页面，点击刷新更新显示。刷新失败保留上次数据并提示错误，超过 10 分钟的缓存标为过期。

当前支持默认目录的单账号 OAuth 文件登录；不支持钥匙串、仅 API Key、多账号、自定义 CODEX_HOME 或代理端点。自定义路径可修改插件源码及 manifest 的精确文件授权。插件不修改登录文件、不刷新共享 refresh token；401/403 提示通过 Codex 重新登录。内部用量 API 变化只需更新插件。

后续通用能力可增加：逐项权限授权 UI、详情页更新通知、后台立即执行与失败退避、按名称授权的环境变量读取和系统密钥存储。后续服务接入复用文件 / 网络 / 后台 / 缓存接口，在插件内完成业务。

进度条节点可通过 `text` 指定右侧摘要（替代默认百分比），`tone: "warning"` 高亮低额度。Codex 插件使用此通用展示能力显示剩余比例与相对重置时间；倒计时按本次渲染时间计算，点击刷新后更新。
