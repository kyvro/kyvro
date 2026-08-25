# com.example.encode

Kyvro 插件系统示例（M0+M1）：base64 即时搜索 + URL 编码命令 + 插件存储演示。

## 功能

- 输入 `b64 <text>` → 实时给出 base64 编码结果（若输入本身是合法 base64，同时给出解码结果），回车复制
- 输入 `url` → 模糊浮出 `URL Encode` 命令 → 回车 → 二级列表（URL 编码/解码）→ 回车复制 → `Esc` 返回一级
- 每次激活通过 `ctx.storage` 记录次数（`plugin:com.example.encode` bucket，重启后保留）

## 手动安装

插件目录布局（spec §11，版本目录名即 SemVer 版本）：

```
~/Library/Application Support/Kyvro/plugins/
└── com.example.encode/
    └── 0.1.0/            # plugin.json + index.js
```

安装即拷贝，重启 Kyvro 生效：

```sh
mkdir -p "$HOME/Library/Application Support/Kyvro/plugins/com.example.encode/0.1.0"
cp plugins-example/com.example.encode/{plugin.json,index.js} \
   "$HOME/Library/Application Support/Kyvro/plugins/com.example.encode/0.1.0/"
```

删除整个 `com.example.encode/` 目录即卸载。

## 注意

- V1 运行时为 goja：仅支持 CommonJS（`module.exports`），不支持 ESM 的
  `export const`（规范示例为 ESM，M2 工具链引入 esbuild 后解决）
- `b64` 前缀在 manifest 的 `activationEvents`（`onSearchPrefix:b64`）中声明，
  只有命中前缀的查询才会调用插件的 `provider.search`（软超时 150ms）
- `storage` 权限声明即授权（V1 默认策略）；未授予时 JS 侧看不到 `ctx.storage`
