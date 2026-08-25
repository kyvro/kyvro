# com.kyvro.textsnippets

Kyvro 官方插件：为 Text Snippets 提供日期/时间/UUID 等动态函数。

## 用法

在 Text Snippets 配置中使用模板语法 `${func("args")}`：

```json
{
  "trigger": "dd",
  "replacement": "${date(\"YYMMDD\")}"
}
```

### 支持的函数

- `${date("format")}` - 当前日期/时间
- `${now("format")}` - 当前时间（别名）
- `${today("format")}` - 今天日期
- `${timestamp()}` - Unix 时间戳（毫秒）
- `${uuid()}` - 随机 UUID v4

### 格式占位符

- `YYYY` - 4位年份，`YY` - 2位年份
- `MM` - 月份，`DD` - 日期
- `HH` - 24小时制，`mm` - 分钟，`ss` - 秒

### 示例

| Trigger | Replacement | 扩展结果 |
|---------|-------------|----------|
| `dd` | `${date("YYMMDD")}` | `260825` |
| `today` | `${date("YYYY-MM-DD")}` | `2026-08-25` |
| `now` | `${now("HH:mm:ss")}` | `18:46:30` |
| `ts` | `${timestamp()}` | `1724587590000` |
| `uuid` | `${uuid()}` | `550e8400-e29b-41d4-a716-446655440000` |

## 安装

见[上级 README](../README.md)，或手动：

```sh
mkdir -p "$HOME/Library/Application Support/Kyvro/plugins/com.kyvro.textsnippets/1.0.0"
cp plugins-official/com.kyvro.textsnippets/{plugin.json,index.js,icon.svg} \
   "$HOME/Library/Application Support/Kyvro/plugins/com.kyvro.textsnippets/1.0.0/"
```

无需任何权限。插件在启动时自动注册模板函数，Text Snippets 核心解析模板并调用这些函数。
