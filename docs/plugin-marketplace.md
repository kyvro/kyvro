# 插件市场

本文档介绍 Kyvro 插件市场架构和规范。

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
      "version": "1.0.0",
      "author": {
        "name": "Kyrovo Team",
        "url": "https://github.com/kyvro"
      },
      "repository": "https://github.com/kyvro/plugins",
      "homepage": "https://github.com/kyvro/plugins/tree/main/com.kyvro.textsnippets",
      "minHostVersion": "0.1.0",
      "permissions": [],
      "platforms": ["darwin", "linux", "windows"],
      "versions": ["1.0.0", "0.9.0"],
      "category": "productivity",
      "keywords": ["snippets", "text", "templates", "date", "uuid"],
      "stats": {
        "downloads": 1234,
        "rating": 4.8
      }
    }
  ]
}
```

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
| `version` | string | ✅ | 最新版本号（SemVer） |
| `author` | object | ✅ | 作者信息（name, url） |
| `repository` | string | ⭕ | 源码仓库 |
| `homepage` | string | ⭕ | 项目主页 |
| `minHostVersion` | string | ✅ | 最低 Kyvro 版本 |
| `permissions` | array | ✅ | 权限声明 |
| `platforms` | array | ⭕ | 支持平台（默认全平台） |
| `versions` | array | ✅ | 可用版本列表 |
| `category` | string | ⭕ | 分类 |
| `keywords` | array | ⭕ | 搜索关键词 |
| `stats` | object | ⭕ | 统计信息（下载量、评分） |

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

1. 访问 https://github.com/kyvro/plugins
2. 浏览插件目录，选择需要的插件
3. 下载对应版本的 `.zip` 文件
4. 解压到插件目录：
   ```
   ~/Library/Application Support/Kyvro/plugins/<plugin-id>/<version>/
   ```
5. 重启 Kyvro

## 插件开发

如果你是开发者，想要创建自己的插件，请参考 [插件开发指南](./plugins.md)。

## 插件市场计划

### 短期（M2）
- **自动更新**：启动时检查插件更新
- **版本管理**：支持多版本并存和切换
- **依赖处理**：处理插件间依赖关系

### 中期（M3）
- **UI 市场**：内置插件浏览界面
- **社区插件**：支持第三方插件提交到 GitHub
- **评分和评论**：插件评分系统
- **分类浏览**：按分类筛选插件

### 长期（M4+）
- **自动发布**：GitHub Actions 自动构建和发布
- **插件模板**：脚手架工具快速创建插件
- **API 生态**：扩展插件 API（network, filesystem, shell）
- **UI DSL**：自定义插件界面
- **后台任务**：支持后台运行的插件
