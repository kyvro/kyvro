# 插件系统

本文档是 Kyvro 插件扩展模块的**协议文档**：定义插件的声明方式、统一接口、结果与动作、上下文 API、权限与错误契约。宿主实现（`internal/plugin/`）与 SDK 类型定义（`plugin-sdk/index.d.ts`，`@kyvro/plugin-sdk`）以本协议为准；协议统一 Command 模型，当前代码已按本协议落地（迁移记录见 §11）。

插件市场见 [plugin-marketplace.md](./plugin-marketplace.md)；权限系统详见 [permissions.md](./permissions.md)。

## 协议总览

| 协议部分 | 载体 | 说明 |
|-----|-----|-----|
| Manifest | `plugin.json`（schemaVersion 1） | 静态声明：身份、入口、命令（触发）、权限 |
| 命令 | manifest `commands[]` | 唯一扩展类型；`prefix` 自描述触发，prefix 建入命令索引（建立/移除时机见 [index.md](./index.md)） |
| 统一接口 | `module.exports.onAction` | 唯一业务入口，所有调用来源共用 |
| 生命周期钩子 | `module.exports.activate` | 加载时执行一次，非扩展类型接口 |
| 结果行 | `ResultRow[]` | 所有调用返回的结果条目 |
| 动作 | `PluginAction` | `open-url` / `copy` / `callback`，由宿主执行 |
| 上下文 | `activate(ctx)` 参数 | `storage` / `log` 能力面 |
| 权限 | manifest `permissions` | 决定 ctx 上注入哪些能力 |
| 错误码 | `PluginError` | 所有失败的统一错误契约 |
| 生命周期 | 超时与禁用规则 | 按调用来源分配时间预算 |

每个插件运行在独立沙箱（独立 JS VM，无宿主 API 直通）。一个插件 = N 个命令 + 1 个业务入口。

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
| `onAction` | `(actionId, args: string[]) => ResultRow[] \| Promise` | **唯一业务入口**。三种调用来源见 §3 |
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
| `callback` | `id`、`args?` | 调用 `onAction(id, args ?? [])`，返回下一级列表；其余动作执行后隐藏窗口 |

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
| `network` / `filesystem` / `shell` / `clipboard` / `secrets` / `background` / `system` | ⏳ 保留 | API 不注入；调用返回 `CAPABILITY_UNAVAILABLE` |
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
| `NETWORK_BLOCKED` | 预留（网络域名策略） |
| `HOST_API_ERROR` | 预留（宿主 API 内部错误） |

## 9. 生命周期与超时

```text
加载：读取 manifest → 校验 → 解析权限 → 创建沙箱 → 注入 ctx
  ↓
activate(ctx)                  2s 预算
  ↓
onAction（prefix live）        每次 150ms 软预算，迟到结果丢弃
onAction（callback）           5s 预算
```

- live 调用连续 3 次超时 → 自动禁用该插件；用户重新启用即恢复并清零计数；自动禁用不持久化，重启重新加载
- 用户启用 / 禁用状态持久化，跨升级 / 重载保留
- **协议不设卸载回调**：禁用 / 卸载 / 重载 / 退出时的清理由宿主负责，不依赖插件主动注销——命令索引、storage 等注册项均由宿主按插件 ID 回收或重建，沙箱 VM 直接销毁
- 若未来扩展类型持有需释放的宿主资源（如常驻任务），停止语义由该类型自身接口契约定义，不是通用卸载回调（见 §1.1）

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
- Match Handler / Action Provider / Background Service / Agent Tool 四类扩展——见 §1.1
- 权限实现扩展与 per-plugin 授权 UI——见 [permissions.md](./permissions.md#路线图)
- 插件结果多 action 与自定义 UI
- Secrets 管理
