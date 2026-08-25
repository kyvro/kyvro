# 插件系统详细说明

本文档详细描述 Kyvro 的插件系统架构与实现。概述见 [features.md](./features.md)。

## 设计目标

插件系统允许用户和开发者扩展 Kyvro 的功能，无需修改核心代码。插件可以：
- 提供自定义搜索功能（通过前缀触发）
- 声明静态命令（通过模糊匹配触发）
- 持久化数据（独立存储空间）
- 执行多种操作（打开链接、复制文本、二级交互）
- 注册模板函数供 Text Snippets 使用

### 动态模板

插件系统提供模板函数注册能力，用于 Text Snippets 动态内容：

#### 模板语法

```
${func("arg1","arg2")}
```

#### 注册模板函数

插件可以在 `activate` 钩子中注册模板函数，供 Text Snippets 使用：

```javascript
module.exports.activate = (ctx) => {
  // 注册日期函数
  ctx.template.registerFunc("date", (args) => {
    const format = args[0] || "YYYY-MM-DD";
    const now = new Date();
    // 格式化日期...
    return formattedDate;
  });

  // 注册 UUID 生成函数
  ctx.template.registerFunc("uuid", (args) => {
    return crypto.randomUUID();
  });

  ctx.log.info("Template functions registered");
};
```

#### 官方 Text Snippets Plugin

官方插件 `com.kyvro.textsnippets` 提供内置模板函数：

- `${date("format")}` - 当前日期/时间
- `${now("format")}` - 当前时间（别名）
- `${today("format")}` - 今天日期
- `${timestamp()}` - Unix 时间戳
- `${uuid()}` - 随机 UUID
- `${uuid()}` - 随机 UUID

**格式占位符：**
- `YYYY` - 4位年份，`YY` - 2位年份
- `MM` - 月份，`DD` - 日期
- `HH` - 24小时制，`mm` - 分钟，`ss` - 秒

#### Text Snippets 集成

Text Snippets 可使用任何插件注册的模板函数：

```json
{
  "trigger": "dd",
  "replacement": "${date(\"YYMMDD\")}"
}
```

输入 `dd` → 扩展为 `260825`（当前日期）

## 架构概览

```
main.go
  └─ service/SearchService
      └─ internal/core/Engine
          └─ internal/plugin/Manager
              └─ PluginProvider (聚合所有插件)
                  ├─ 命令浮出（模糊匹配，无 JS 调用）
                  └─ 实时搜索（前缀门控，并行调用 JS）
```

**Provider 优先级顺序**：`[calc, apps, plugins, web]`

## 插件清单（Manifest）

### 文件结构

```
~/Library/Application Support/Kyvro/plugins/
├── com.example.encode/
│   ├── 1.0.0/
│   │   ├── plugin.json        # 必需的清单文件
│   │   ├── main.js            # 入口文件（manifest.main 指定）
│   │   └── icon.png           # 可选图标（manifest.icon 指定）
│   └── current.json           # 可选：固定版本（"version": "1.0.0"）
└── com.kyvro.github/
    └── 2.1.0/
        └── plugin.json
```

### Manifest 字段

```json
{
  "schemaVersion": 1,                    // 必需，当前固定为 1
  "id": "com.example.encode",            // 必需，反向域名格式，须与目录名一致
  "name": "Base64 Encoder",              // 可选，显示名称
  "version": "1.0.0",                     // 必需，SemVer 格式
  "minHostVersion": "0.1.0",              // 必需，最低 Kyvro 版本
  "description": "Encode and decode...", // 可选，描述
  "author": {                             // 可选，作者信息
    "name": "Your Name",
    "url": "https://example.com"
  },
  "main": "main.js",                      // 必需，入口文件相对路径
  "icon": "icon.png",                     // 可选，图标文件相对路径
  "platforms": ["darwin"],               // 可选，平台过滤
  "permissions": ["storage"],             // 可选，权限声明
  "commands": [                           // 可选，静态命令声明
    {
      "id": "encode.url",
      "title": "URL Encode",
      "subtitle": "Encode text for URLs",
      "keywords": ["urlencode", "percent"]
    }
  ],
  "activationEvents": [                  // 必需，激活事件
    "onSearchPrefix:b64 ",
    "onCommand:encode.url"
  ]
}
```

### 激活事件类型

| 事件类型 | 说明 | 示例 |
|---------|------|------|
| `onSearchPrefix:<prefix>` | 搜索前缀，查询以此开头时触发 | `"onSearchPrefix:gh "` |
| `onCommand:<id>` | 命令触发，通过模糊匹配浮出 | `"onCommand:encode.url"` |

## 插件开发

### JavaScript 入口（CommonJS）

```javascript
// main.js
const { storage } = ctx;

// 声明命令（可选，也可在 manifest 中静态声明）
module.exports.commands = [
  {
    id: "my.cmd",
    title: "My Command",
    keywords: ["alias"]
  }
];

// 提供实时搜索（可选）
module.exports.provider = {
  search: async (query, signal) => {
    // query: 完整查询（包含前缀）
    // signal: AbortSignal，用于响应超时
    const results = [];
    // ... 处理逻辑
    return [
      {
        id: "unique-id",
        title: "Result Title",
        subtitle: "Optional subtitle",
        scoreHint: 10,  // 可选，0-50，影响排序
        action: {
          kind: "open-url",  // 或 "copy" 或 "callback"
          arg: "https://example.com"
        }
      }
    ];
  }
};

// 处理命令回调（可选）
module.exports.onCommand = async (commandId, args) => {
  // commandId: 命令 ID
  // args: 参数数组（来自查询）
  return [
    {
      id: "result-1",
      title: "Secondary Result",
      action: { kind: "copy", arg: "text to copy" }
    }
  ];
};

// 激活钩子（可选，返回 Promise）
module.exports.activate = async (ctx) => {
  // 初始化逻辑
  console.log("Plugin activated");
};
```

### 可用 API

**ctx.storage**（需要 `storage` 权限）
```javascript
await ctx.storage.set("key", "value");
const value = await ctx.storage.get("key");
await ctx.storage.delete("key");
```

**ctx.log**
```javascript
ctx.log.info("info message");
ctx.log.warn("warning message");
ctx.log.error("error message");
```

### Action 类型

**open-url**：用默认浏览器（或用户设置的浏览器）打开 URL
```javascript
{ kind: "open-url", arg: "https://github.com" }
```

**copy**：复制文本到剪贴板
```javascript
{ kind: "copy", arg: "text to copy" }
```

**callback**：返回二级结果列表（用户按 Enter 时调用 `onCommand`）
```javascript
{ kind: "callback", arg: ["command-id", "arg1", "arg2"] }
```

## 运行时实现

### goja VM 隔离

- 每个插件一个独立的 goja VM
- 每个插件一个专用的 worker goroutine
- 外部调用通过 channel 派发到 worker
- 超时通过 `vm.Interrupt()` 实现，VM 复用不重建

### 并行搜索

```go
// 伪代码
for plugin in plugins {
    if plugin.HasProvider() && prefixMatches(plugin, query) {
        go func(p) {
            ctx, cancel := context.WithTimeout(context.Background(), 150ms)
            results := p.rt.Search(ctx, query)
            // 合并结果
        }(plugin)
    }
}
```

### 超时处理

- 搜索超时：150ms 软超时，迟到结果丢弃
- 连续 3 次超时：自动禁用插件
- 命令回调：5s 超时

### 错误处理

- JS 异常/panic → `PluginError`（带错误码）
- 单插件错误不影响宿主
- 错误码详见 `internal/plugin/errors.go`

## 权限系统

当前版本（V1）权限状态：

| 权限 | 状态 | 说明 |
|-----|------|------|
| storage | ✅ 支持 | 声明即授权，独立 bucket |
| network | ⏳ 保留 | 调用返回 `CAPABILITY_UNAVAILABLE` |
| filesystem | ⏳ 保留 | 同上 |
| shell | ⏳ 保留 | 同上 |
| clipboard | ⏳ 保留 | 同上 |
| secrets | ⏳ 保留 | 同上 |
| background | ⏳ 保留 | 同上 |
| system | ⏳ 保留 | 同上 |

## 数据持久化

### 使用历史

- 宿主：`data.db` 的 `usages` bucket
- 插件：`plugin:<pluginID>` bucket（string→string）

### 插件状态

- 启用/禁用状态：`plugins-state` bucket
- 跨版本升级/重载持久化

## 示例插件

### Base64 编码器（`plugins-example/com.example.encode`）

```json
{
  "id": "com.example.encode",
  "name": "Base64 Encoder",
  "main": "main.js",
  "permissions": ["storage"],
  "commands": [
    { "id": "encode.url", "title": "URL Encode" }
  ],
  "activationEvents": [
    "onSearchPrefix:b64 ",
    "onCommand:encode.url"
  ]
}
```

```javascript
// main.js
module.exports.provider = {
  search: async (query, signal) => {
    const text = query.slice(3); // 去掉 "b64 " 前缀
    try {
      const encoded = btoa(text);
      return [{
        id: "b64-result",
        title: encoded,
        subtitle: `Base64: ${text}`,
        action: { kind: "copy", arg: encoded }
      }];
    } catch {
      return [];
    }
  }
};

module.exports.onCommand = async (cmdId, args) => {
  const text = args[0] || "";
  const encoded = encodeURIComponent(text);
  return [{
    id: "url-result",
    title: encoded,
    action: { kind: "copy", arg: encoded }
  }];
};
```

### GitHub 搜索（`plugins-official/com.kyvro.github`）

- 前缀：`gh <query>` 打开 GitHub 仓库搜索
- 直接访问：`gh owner/repo` 直接打开仓库
- 无需权限，使用宿主 `open-url`

## 测试

插件系统测试覆盖（`internal/plugin/*_test.go`）：

- Manifest 校验全矩阵
- 版本目录选择（含 current.json 覆盖）
- 权限解析/拒绝/不可用
- 存储隔离与持久
- 结果转换与非法条目丢弃
- 超时中断与迟到结果
- 异常转错误
- 命令回调往返
- activate Promise
- LoadAll 容错
- 前缀门控
- 命令浮出
- 并行合并
- 3 次超时禁用
- 引擎顺序断言
- 端到端示例插件测试

## 限制与未来计划

### 当前限制

- 仅支持 CommonJS（不支持 ESM）
- 不支持 TypeScript（需手动编译）
- 无插件市场（需手动安装）
- 无版本升级/回滚机制

### 未来计划（M2+）

- ESM 支持 + esbuild 工具链
- 插件市场 + 一键安装
- Secrets 管理
- network/filesystem/shell/clipboard API
- UI DSL（自定义界面）
- 后台任务
