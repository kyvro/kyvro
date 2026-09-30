# Kyvro 官方插件

官方维护的插件源码。与 `plugins-example/`（教学示例）不同，这里的插件保持长期维护并跟随 Kyvro 版本验证。

## 插件列表

| 插件 | 前缀/命令 | 说明 |
| --- | --- | --- |
| [com.kyvro.github](./com.kyvro.github/) | `gh <query>` | 打开 GitHub 搜索对应项目；`gh owner/repo` 直接打开仓库（GitHub 官方 mark 图标） |

## 安装

插件目录布局与 spec §11 一致（`<id>/<版本>/`），拷贝后重启 Kyvro 生效：

```sh
mkdir -p "$HOME/Library/Application Support/Kyvro/plugins/com.kyvro.github/0.1.0"
cp plugins-official/com.kyvro.github/* \
   "$HOME/Library/Application Support/Kyvro/plugins/com.kyvro.github/0.1.0/"
```

也可在 设置 → Plugins → Open Plugins Folder 打开目录后手动拷入。卸载即删除对应插件目录。

## 打包发布

```sh
build/package-plugins.sh
```

脚本遍历本目录下每个插件，按 manifest 生成 `plugins/<id>/<id>-<version>.zip`（包内文件位于根目录），把版本累积合并进 `plugins/list.json`（升序 SemVer，`minVersions` 取各自 manifest 的 `minHostVersion`），并以同一 ISO 8601 时间戳刷新 `plugins/lastUpdated`。不在本目录的已发布插件（如 textsnippets）保持原样。生成结果即 registry 布局（见 plugin-marketplace.md），拷入 kyvro/plugins 仓库提交发布。
