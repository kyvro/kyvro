如果你的目标是：

**macOS + Windows + Linux 跨端，同时要求接近 Alfred 的启动速度、低内存和深度系统集成**，我会优先考虑：

**Wails v3 + Go Core + 各平台 Native Bridge + Web UI**。

而不是 Electron，也不建议纯 Flutter。

### 我会这样排

| 方案                                |  启动速度 |    内存 |  系统集成 |  UI开发 |    跨端 |            推荐 |
| --------------------------------- | ----: | ----: | ----: | ----: | ----: | ------------: |
| **Wails v3 + Go + Native Bridge** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |  ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |        **首选** |
| Tauri 2 + Rust                    | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |  ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |            很强 |
| Slint + Rust                      | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |  ⭐⭐⭐⭐ |   ⭐⭐⭐ |  ⭐⭐⭐⭐ |          极致性能 |
| Flutter                           |  ⭐⭐⭐⭐ |   ⭐⭐⭐ |   ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 不太适合 Launcher |
| Electron                          |   ⭐⭐⭐ |    ⭐⭐ |   ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |           不推荐 |

Wails v3 的架构本身就很适合这种项目：**Go 做业务逻辑，HTML/CSS/JS 做 UI，但使用系统自带 WebView，不捆绑 Chromium**。官方目前称典型应用二进制大约 10MB，并强调低内存和快速启动。macOS 使用系统 WebKit，Windows 使用 WebView2。([Wails][1])

### 不要把所有东西都放进 Wails

关键是采用这种架构：

```text
              Launcher Core
                  Go
                   │
       ┌───────────┼────────────┐
       │           │            │
     macOS       Windows       Linux
       │           │            │
 Swift/ObjC      Win32/C#     GTK/dbus
 AppKit          WinRT          native
       │           │            │
       └───────────┼────────────┘
                   │
               Wails v3
                   │
             Vue / React
                   │
             Launcher UI
```

也就是说：

**跨平台的是 Core。**

**系统能力不是强行跨平台。**

例如：

```text
core/
├── search/
├── ranking/
├── history/
├── clipboard/
├── plugin/
├── workflow/
├── command/
└── indexer/

platform/
├── darwin/
│   ├── spotlight
│   ├── accessibility
│   ├── keychain
│   ├── global_hotkey
│   ├── app_launcher
│   └── window
│
├── windows/
│   ├── windows_search
│   ├── ui_automation
│   ├── credential_manager
│   ├── global_hotkey
│   └── shell
│
└── linux/
    ├── xdg
    ├── dbus
    ├── secret_service
    └── global_hotkey
```

这样才可能同时做到跨平台和原生体验。

---

### Alfred 类型的软件，最重要的其实不是 UI

真正决定体验的是这几个东西：

```text
用户按 ⌥ Space
       ↓
< 30ms
       ↓
窗口出现
       ↓
立即响应键盘输入
       ↓
搜索结果 < 10~30ms
```

所以最好让程序：

```text
开机
 ↓
Go Core 常驻
 ↓
索引已经加载到内存
 ↓
WebView/UI 可以隐藏
 ↓
快捷键触发
 ↓
直接显示 Window
```

**不要每次快捷键都重新启动程序。**

这和 Alfred / Raycast 的思路类似：launcher 是一个轻量后台进程。

---

### Wails 的一个优势

你前端甚至可以继续使用：

```text
Vue 3
+
TypeScript
+
Tailwind
```

但 UI 不要做成普通网站。

Launcher UI 非常简单：

```text
┌────────────────────────────────────────────┐
│ 🔍  docker                                 │
├────────────────────────────────────────────┤
│ 🐳 Docker Desktop                 ↵ Open   │
│ > docker ps                      Command   │
│ > docker images                  Command   │
│ 🌐 Search Google for docker      Web       │
└────────────────────────────────────────────┘
```

WebView 只是承担：

**绘制这一小块 UI。**

搜索、排序、插件、系统 API 全部放 Go。

这样 WebView 的性能问题其实非常有限。

---

### 系统集成建议单独做 Native Adapter

例如 macOS：

```text
Go
 ↓
CGO
 ↓
Objective-C / Swift
 ↓
AppKit
Accessibility
Spotlight
Keychain
NSWorkspace
NSPasteboard
```

比如搜索应用不要：

```go
filepath.Walk("/Applications")
```

而应该调用：

```text
NSMetadataQuery
Spotlight Metadata
NSWorkspace
```

这样才能接近 Alfred。

Windows 同理：

```text
Go
 ↓
Win32
COM
WinRT
Windows Search Index
UI Automation
Credential Manager
```

Linux：

```text
Go
 ↓
D-Bus
XDG
Secret Service
Wayland/X11
```

---

### Wails v3 目前已经比较适合这个方向

它现在支持 Windows AMD64/ARM64、macOS Intel/Apple Silicon、Linux AMD64/ARM64。macOS 使用系统 WebKit，Windows 使用 WebView2，Linux 默认 GTK4 + WebKitGTK 6。([Wails][1])

而且 Wails v3 已经增加不少 native window 能力，例如 Windows Snap Assist、macOS 透明 title bar 等。([Wails][2])

---

### Tauri 也非常合适

如果你愿意大量写 Rust：

```text
Tauri 2
+
Rust Core
+
React/Vue
+
Swift Native Plugin
+
Windows Native Plugin
```

其实架构上和我上面说的 Wails 完全类似。

差别主要是：

```text
Wails
Go ecosystem
开发简单
并发简单
后台服务简单
CGO/native bridge

Tauri
Rust ecosystem
内存控制更强
FFI 更漂亮
系统级开发能力更强
学习/开发成本更高
```

对于一个 Launcher，我认为两者性能差异最终可能只有很小一部分。

因为真正耗时的是：

```text
文件索引
插件
WebView 初始化
系统 API
搜索算法
```

而不是：

```text
Go vs Rust
```

---

### Flutter 我反而不会首选

Flutter Desktop 官方确实完整支持 macOS / Windows / Linux，也允许写 platform plugin 调用系统 API。([Flutter 文档][3])

但 Launcher 有一个特殊需求：

> UI 极少，但系统集成极深。

Flutter 最擅长的是：

```text
复杂跨平台 UI
动画
业务 App
```

而你的需求更像：

```text
10% UI
90% OS integration
```

所以 Flutter 的优势发挥不出来。

---

## 我最推荐的最终技术栈

如果让我现在从零设计一个 **Open Source Alfred / Raycast**，我会这样做：

```text
                OpenLauncher

┌──────────────────────────────────────┐
│             UI Layer                 │
│                                      │
│       Vue 3 + TypeScript             │
│       Tailwind / CSS                 │
└─────────────────┬────────────────────┘
                  │
              Wails v3
                  │
┌─────────────────▼────────────────────┐
│              Go Core                 │
│                                      │
│ Search Engine                        │
│ Ranking Engine                       │
│ Command Engine                       │
│ Plugin Runtime                       │
│ Clipboard Manager                    │
│ History                              │
│ Index Cache                          │
│ Workflow Engine                      │
└─────────────────┬────────────────────┘
                  │
       Platform Adapter Interface
                  │
       ┌──────────┼──────────┐
       ▼          ▼          ▼

     macOS      Windows     Linux

 Swift/AppKit    Win32     DBus/GTK
 Spotlight       COM       XDG
 Keychain        WinRT     SecretSvc
 Accessibility   UIA       Wayland
```

再加一个非常关键的设计：

```text
launcher-core
```

**不要依赖 Wails。**

也就是：

```text
launcher-core      ← Go pure library
launcher-native    ← platform implementation
launcher-plugin    ← plugin engine
launcher-ui        ← Wails
launcher-app       ← executable
```

未来你甚至可以做：

```text
CLI
Desktop
MenuBar
Tray
Daemon
Remote Control
```

都复用同一个 Core。

如果核心目标就是“**跨平台 Alfred，但性能和原生能力不能因为跨平台而妥协**”，我会选 **Wails v3 + Go Core + Native Adapter**。它比“全部跨平台化”更重要：**UI 跨平台，核心跨平台，OS 能力坚持原生实现。**

[1]: https://v3.wails.io/faq/?utm_source=chatgpt.com "Frequently Asked Questions"
[2]: https://v3.wails.io/features/windows/basics/?utm_source=chatgpt.com "Window Basics"
[3]: https://docs.flutter.dev/platform-integration/desktop?utm_source=chatgpt.com "Desktop support for Flutter"
