# com.kyvro.github

Kyvro 官方插件：从启动器直达 GitHub 搜索 / 仓库。

## 用法

- `gh wails` → 回车打开 GitHub 仓库搜索 `wails`（`github.com/search?q=wails&type=repositories`）
- `gh wailsapp/wails` → 识别 `owner/repo` 形态，直接打开仓库页
- 仅输入 `gh` 或 `gh ` 无结果；`ghost` 等 gh 开头的普通单词不拦截（留给应用搜索）

## 安装

见[上级 README](../README.md)，或手动：

```sh
mkdir -p "$HOME/Library/Application Support/Kyvro/plugins/com.kyvro.github/0.1.0"
cp plugins-official/com.kyvro.github/{plugin.json,index.js} \
   "$HOME/Library/Application Support/Kyvro/plugins/com.kyvro.github/0.1.0/"
```

无需任何权限（结果动作为宿主侧 open-url）。manifest 声明 `icon: icon.svg`（GitHub 官方 mark，白色适配深色 UI），搜索结果行与插件管理页都会显示。
