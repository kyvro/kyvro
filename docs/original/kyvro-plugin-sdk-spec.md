# @kyvro/plugin-sdk Specification

## 1. Overview

`@kyvro/plugin-sdk` is the official **type definition SDK** for Kyvro plugins.

The package is published to npm and is intended for plugin development only.

Its primary responsibilities are:

- Provide TypeScript declaration files (`index.d.ts`)
- Provide JSDoc type inference for JavaScript plugins
- Provide IDE autocomplete and API documentation
- Define the public contract between Kyvro Core and plugins
- Provide compile-time / editor-time validation
- Track plugin API compatibility across SDK versions

The SDK does **not** provide runtime implementations.

Kyvro plugins are executed by the Kyvro runtime and receive platform capabilities through the `PluginContext` object.

---

## 2. Design Goals

### 2.1 JavaScript First

Kyvro plugins use JavaScript and CommonJS.

A plugin should not require TypeScript compilation.

Example:

```js
/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  id: "date",
  name: "Date",

  activate(ctx) {
    // plugin logic
  }
};
```

The plugin remains plain JavaScript while receiving TypeScript-level editor support.

---

### 2.2 Types Only

`@kyvro/plugin-sdk` should contain no required runtime implementation.

The package exists only for:

- JSDoc type resolution
- TypeScript type resolution
- IDE autocomplete
- API documentation
- Static validation

Plugin code must not require the SDK at runtime.

Do not use:

```js
const sdk = require("@kyvro/plugin-sdk");
```

Instead use JSDoc:

```js
/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  // ...
};
```

---

### 2.3 Runtime Independence

The following two concepts must remain separate:

```text
Development Time

plugin/index.js
      |
      | JSDoc import()
      v
@kyvro/plugin-sdk
      |
      v
index.d.ts
      |
      v
VS Code / WebStorm / TypeScript Language Service
```

```text
Runtime

plugin/index.js
      |
      v
Kyvro CommonJS Loader
      |
      v
module.exports
      |
      v
plugin.activate(ctx)
```

Kyvro Core does not need to load `@kyvro/plugin-sdk`.

---

## 3. Package Name

Official npm package:

```text
@kyvro/plugin-sdk
```

Recommended installation:

```bash
npm install -D @kyvro/plugin-sdk
```

or:

```bash
pnpm add -D @kyvro/plugin-sdk
```

or:

```bash
yarn add -D @kyvro/plugin-sdk
```

The package should normally be declared under `devDependencies`.

Example:

```json
{
  "devDependencies": {
    "@kyvro/plugin-sdk": "^1.0.0"
  }
}
```

---

## 4. Project Structure

Recommended repository structure:

```text
plugin-sdk/
├── package.json
├── index.d.ts
├── README.md
├── LICENSE
├── CHANGELOG.md
├── tsconfig.json
└── test/
    ├── basic-plugin.js
    └── type-tests.ts
```

Minimal npm package contents:

```text
@kyvro/plugin-sdk/
├── package.json
├── index.d.ts
├── README.md
└── LICENSE
```

No `index.js` is required.

---

## 5. package.json

Recommended `package.json`:

```json
{
  "name": "@kyvro/plugin-sdk",
  "version": "1.0.0",
  "description": "Type definitions and development SDK for Kyvro plugins",
  "license": "MIT",
  "types": "./index.d.ts",
  "files": [
    "index.d.ts",
    "README.md",
    "LICENSE",
    "CHANGELOG.md"
  ],
  "keywords": [
    "kyvro",
    "plugin",
    "plugin-sdk",
    "types",
    "jsdoc"
  ],
  "engines": {
    "node": ">=18"
  },
  "publishConfig": {
    "access": "public"
  }
}
```

Because the package is types-only, `main` is intentionally omitted.

---

## 6. Core Type Model

The SDK should define the public Kyvro plugin API.

Initial top-level model:

```text
Plugin
├── metadata
├── activate()
└── deactivate()

PluginContext
├── commands
├── actions
├── index
├── storage
├── clipboard
├── shell
├── http
├── template
├── project
├── ui
└── logger
```

Not every API must be implemented in v1.

The declarations should represent only APIs that are officially supported by Kyvro Core.

---

## 7. index.d.ts

Initial recommended definition:

```ts
export type MaybePromise<T> = T | Promise<T>;

export interface Disposable {
  dispose(): void;
}

export interface Plugin {
  /**
   * Stable plugin identifier.
   *
   * Example:
   * "com.kyvro.date"
   */
  id: string;

  /**
   * Human-readable plugin name.
   */
  name: string;

  /**
   * Optional plugin version.
   *
   * The package.json version remains the authoritative package version.
   */
  version?: string;

  /**
   * Called when the plugin is activated.
   */
  activate(context: PluginContext): MaybePromise<void>;

  /**
   * Called before the plugin is unloaded.
   */
  deactivate?(): MaybePromise<void>;
}

export interface PluginContext {
  commands: CommandAPI;
  actions: ActionAPI;
  index: IndexAPI;
  storage: StorageAPI;
  clipboard: ClipboardAPI;
  shell: ShellAPI;
  http: HttpAPI;
  template: TemplateAPI;
  project: ProjectAPI;
  ui: UIAPI;
  logger: Logger;
}

/* -------------------------------------------------------------------------- */
/* Commands                                                                   */
/* -------------------------------------------------------------------------- */

export interface CommandAPI {
  /**
   * Register a command.
   */
  register(command: Command): Disposable;
}

export interface Command {
  /**
   * Unique command id inside the plugin.
   */
  id: string;

  /**
   * Display title.
   */
  title: string;

  /**
   * Optional description.
   */
  description?: string;

  /**
   * Optional keywords used by search.
   */
  keywords?: string[];

  /**
   * Execute the command.
   */
  execute(
    args?: unknown,
    context?: CommandExecutionContext
  ): MaybePromise<unknown>;
}

export interface CommandExecutionContext {
  query?: string;
}

/* -------------------------------------------------------------------------- */
/* Actions                                                                    */
/* -------------------------------------------------------------------------- */

export interface ActionAPI {
  register(action: Action): Disposable;
}

export interface Action {
  id: string;
  title: string;
  description?: string;

  execute(
    input?: unknown,
    context?: ActionExecutionContext
  ): MaybePromise<unknown>;
}

export interface ActionExecutionContext {
  query?: string;
}

/* -------------------------------------------------------------------------- */
/* Index                                                                      */
/* -------------------------------------------------------------------------- */

export interface IndexAPI {
  add(item: IndexItem): MaybePromise<void>;

  addMany(items: IndexItem[]): MaybePromise<void>;

  remove(id: string): MaybePromise<void>;

  clear(): MaybePromise<void>;
}

export interface IndexItem {
  id: string;
  title: string;
  subtitle?: string;
  keywords?: string[];
  icon?: string;
  kind?: IndexItemKind;
  data?: Record<string, unknown>;
}

export type IndexItemKind =
  | "app"
  | "folder"
  | "file"
  | "url"
  | "command"
  | "text"
  | "project"
  | "custom";

/* -------------------------------------------------------------------------- */
/* Storage                                                                    */
/* -------------------------------------------------------------------------- */

export interface StorageAPI {
  get<T = unknown>(key: string): MaybePromise<T | undefined>;

  set<T = unknown>(key: string, value: T): MaybePromise<void>;

  delete(key: string): MaybePromise<void>;

  has(key: string): MaybePromise<boolean>;

  clear(): MaybePromise<void>;
}

/* -------------------------------------------------------------------------- */
/* Clipboard                                                                  */
/* -------------------------------------------------------------------------- */

export interface ClipboardAPI {
  readText(): MaybePromise<string>;

  writeText(text: string): MaybePromise<void>;
}

/* -------------------------------------------------------------------------- */
/* Shell                                                                      */
/* -------------------------------------------------------------------------- */

export interface ShellAPI {
  execute(
    command: string,
    options?: ShellExecuteOptions
  ): MaybePromise<ShellResult>;

  open(target: string): MaybePromise<void>;
}

export interface ShellExecuteOptions {
  cwd?: string;
  env?: Record<string, string>;
  timeoutMs?: number;
}

export interface ShellResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

/* -------------------------------------------------------------------------- */
/* HTTP                                                                       */
/* -------------------------------------------------------------------------- */

export interface HttpAPI {
  request<T = unknown>(
    options: HttpRequestOptions
  ): MaybePromise<HttpResponse<T>>;

  get<T = unknown>(
    url: string,
    options?: Omit<HttpRequestOptions, "url" | "method">
  ): MaybePromise<HttpResponse<T>>;

  post<T = unknown>(
    url: string,
    body?: unknown,
    options?: Omit<HttpRequestOptions, "url" | "method" | "body">
  ): MaybePromise<HttpResponse<T>>;
}

export interface HttpRequestOptions {
  url: string;
  method?: string;
  headers?: Record<string, string>;
  body?: unknown;
  timeoutMs?: number;
}

export interface HttpResponse<T = unknown> {
  status: number;
  headers: Record<string, string>;
  data: T;
}

/* -------------------------------------------------------------------------- */
/* Template                                                                   */
/* -------------------------------------------------------------------------- */

export interface TemplateAPI {
  registerFunction(
    name: string,
    handler: TemplateFunction
  ): Disposable;
}

export type TemplateFunction = (
  ...args: string[]
) => MaybePromise<string>;

/* -------------------------------------------------------------------------- */
/* Project                                                                    */
/* -------------------------------------------------------------------------- */

export interface ProjectAPI {
  detect(path?: string): MaybePromise<ProjectInfo | null>;

  current(): MaybePromise<ProjectInfo | null>;
}

export interface ProjectInfo {
  path: string;
  name?: string;
  type?: ProjectType;
  metadata?: Record<string, unknown>;
}

export type ProjectType =
  | "node"
  | "next"
  | "react"
  | "vue"
  | "go"
  | "rust"
  | "android"
  | "ios"
  | "python"
  | "unknown";

/* -------------------------------------------------------------------------- */
/* UI                                                                         */
/* -------------------------------------------------------------------------- */

export interface UIAPI {
  notify(options: NotificationOptions | string): MaybePromise<void>;

  confirm(options: ConfirmOptions | string): MaybePromise<boolean>;
}

export interface NotificationOptions {
  title?: string;
  message: string;
}

export interface ConfirmOptions {
  title?: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
}

/* -------------------------------------------------------------------------- */
/* Logger                                                                     */
/* -------------------------------------------------------------------------- */

export interface Logger {
  debug(message: string, data?: unknown): void;

  info(message: string, data?: unknown): void;

  warn(message: string, data?: unknown): void;

  error(message: string, data?: unknown): void;
}
```

This definition is an initial contract and should evolve together with Kyvro Core.

---

## 8. Plugin JSDoc Usage

Typical plugin:

```js
/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  id: "com.kyvro.date",
  name: "Date",

  activate(ctx) {
    ctx.commands.register({
      id: "date.now",
      title: "Current Date",

      execute() {
        return new Date().toISOString();
      }
    });
  }
};
```

The IDE should infer:

```text
ctx
├── commands
├── actions
├── index
├── storage
├── clipboard
├── shell
├── http
├── template
├── project
├── ui
└── logger
```

No runtime SDK import is required.

---

## 9. Alternative JSDoc Patterns

For individual types:

```js
/**
 * @param {import("@kyvro/plugin-sdk").PluginContext} ctx
 */
function activate(ctx) {
}
```

For commands:

```js
/**
 * @type {import("@kyvro/plugin-sdk").Command}
 */
const command = {
  id: "hello",
  title: "Hello",

  execute() {
    return "Hello";
  }
};
```

For indexed items:

```js
/**
 * @type {import("@kyvro/plugin-sdk").IndexItem}
 */
const item = {
  id: "github",
  title: "GitHub",
  kind: "url"
};
```

---

## 10. JavaScript Type Checking

Plugin authors may enable stronger checking without converting plugins to TypeScript.

Recommended `jsconfig.json`:

```json
{
  "compilerOptions": {
    "checkJs": true,
    "strict": true,
    "module": "commonjs",
    "target": "ES2020"
  },
  "include": [
    "**/*.js"
  ]
}
```

Alternatively:

```js
// @ts-check
```

can be placed at the top of individual plugin files.

Example:

```js
// @ts-check

/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  // ...
};
```

---

## 11. SDK vs Plugin Manifest

The SDK describes JavaScript APIs.

Plugin metadata, permissions and compatibility should remain in the plugin's `package.json`.

Example plugin:

```json
{
  "name": "@kyvro-plugins/date",
  "version": "1.0.0",
  "main": "index.js",
  "devDependencies": {
    "@kyvro/plugin-sdk": "^1.0.0"
  },
  "kyvro": {
    "apiVersion": 1,
    "permissions": []
  }
}
```

The SDK version and runtime API version are separate concepts.

---

## 12. API Versioning

Kyvro should distinguish:

```text
SDK Package Version
@kyvro/plugin-sdk@1.5.2

Runtime API Version
kyvro.apiVersion = 1
```

### SDK Version

Uses Semantic Versioning:

```text
MAJOR.MINOR.PATCH
```

Examples:

```text
1.0.0
1.1.0
1.1.1
2.0.0
```

Use:

- PATCH for documentation/type corrections without API behavior changes
- MINOR for backward-compatible API additions
- MAJOR for breaking SDK declaration changes

---

### Runtime API Version

Runtime compatibility should be declared separately:

```json
{
  "kyvro": {
    "apiVersion": 1
  }
}
```

A new SDK version does not automatically imply a new runtime API version.

Example:

```text
plugin-sdk 1.0.0 -> apiVersion 1
plugin-sdk 1.1.0 -> apiVersion 1
plugin-sdk 1.5.0 -> apiVersion 1
plugin-sdk 2.0.0 -> apiVersion 2
```

This allows SDK documentation and type definitions to evolve without forcing runtime incompatibility.

---

## 13. Compatibility Policy

The type definitions must match behavior actually implemented by Kyvro Core.

Do not expose experimental Core APIs as stable declarations unless explicitly marked experimental.

Possible convention:

```ts
/**
 * @experimental
 */
export interface ExperimentalAPI {
}
```

Deprecated APIs should remain available for at least one compatibility cycle when practical.

Example:

```ts
export interface ClipboardAPI {
  /**
   * @deprecated Use readText() instead.
   */
  read(): MaybePromise<string>;

  readText(): MaybePromise<string>;
}
```

---

## 14. npm Publishing

### 14.1 npm Organization

The npm scope should be:

```text
@kyvro
```

Package:

```text
@kyvro/plugin-sdk
```

---

### 14.2 Initial npm Login

```bash
npm login
```

Verify:

```bash
npm whoami
```

---

### 14.3 Build Validation

Before publishing:

```bash
npm install
npm test
```

Optional type validation:

```bash
npx tsc --noEmit
```

---

### 14.4 Inspect Package

Before every release:

```bash
npm pack --dry-run
```

Expected published files should be limited to:

```text
package.json
index.d.ts
README.md
LICENSE
CHANGELOG.md
```

No test files or local project files should be included unless intentionally published.

---

### 14.5 Publish

For the first scoped public package:

```bash
npm publish --access public
```

Subsequent releases:

```bash
npm publish
```

---

## 15. Release Workflow

Recommended release sequence:

```text
1. Update index.d.ts
2. Update README.md
3. Update CHANGELOG.md
4. Run type tests
5. Run npm pack --dry-run
6. Update package version
7. Commit
8. Create git tag
9. Push
10. npm publish
```

Example:

```bash
npm version patch
npm publish
```

or:

```bash
npm version minor
npm publish
```

or:

```bash
npm version major
npm publish
```

---

## 16. Git Repository

Recommended repository:

```text
github.com/kyvro/plugin-sdk
```

Suggested branches:

```text
main
```

Tags:

```text
v1.0.0
v1.1.0
v2.0.0
```

Recommended repository layout:

```text
plugin-sdk/
├── .github/
│   └── workflows/
│       └── publish.yml
├── test/
├── .gitignore
├── CHANGELOG.md
├── LICENSE
├── README.md
├── index.d.ts
├── package.json
├── spec.md
└── tsconfig.json
```

---

## 17. Automated npm Publishing

Publishing can later be automated through GitHub Actions.

Recommended model:

```text
Git Tag
   |
   v
GitHub Actions
   |
   +-- install
   +-- test
   +-- npm pack --dry-run
   +-- npm publish
```

Example trigger:

```yaml
on:
  push:
    tags:
      - "v*"
```

Prefer npm trusted publishing / OIDC when available instead of storing long-lived npm tokens.

---

## 18. Type Tests

The SDK repository should contain type-level tests.

Example:

```ts
import type {
  Plugin,
  PluginContext,
  Command,
  IndexItem
} from "../index";

const plugin: Plugin = {
  id: "test",
  name: "Test",

  activate(ctx: PluginContext) {
    ctx.commands.register({
      id: "hello",
      title: "Hello",
      execute() {
        return "hello";
      }
    });
  }
};
```

A JavaScript/JSDoc test should also exist:

```js
// @ts-check

/**
 * @type {import("../index").Plugin}
 */
module.exports = {
  id: "test",
  name: "Test",

  activate(ctx) {
    ctx.clipboard.writeText("hello");
  }
};
```

This is important because JavaScript + JSDoc is the primary Kyvro plugin development mode.

---

## 19. API Documentation

Every public interface and important field should use TSDoc-compatible comments.

Example:

```ts
export interface StorageAPI {
  /**
   * Reads a value from plugin-scoped persistent storage.
   *
   * @param key Storage key.
   * @returns Stored value, or undefined when the key does not exist.
   */
  get<T = unknown>(key: string): MaybePromise<T | undefined>;
}
```

This ensures IDE hover documentation remains useful.

---

## 20. Runtime Boundary

`@kyvro/plugin-sdk` must never become the source of runtime capabilities.

Incorrect architecture:

```text
Plugin
   |
   v
@kyvro/plugin-sdk runtime JS
   |
   v
OS / Core
```

Correct architecture:

```text
Plugin
   |
   | activate(ctx)
   v
Kyvro Core
   |
   +-- commands
   +-- storage
   +-- clipboard
   +-- shell
   +-- http
   +-- index
   +-- actions
   +-- project
   +-- UI
```

The SDK only describes this contract:

```text
@kyvro/plugin-sdk
        |
        v
     index.d.ts
        |
        v
Description of PluginContext
```

---

## 21. Security Boundary

Types are not permissions.

For example, the SDK may declare:

```ts
ctx.shell.execute(...)
```

but Core must still validate that the plugin has the appropriate permission.

Example manifest:

```json
{
  "kyvro": {
    "permissions": [
      "shell.execute"
    ]
  }
}
```

Runtime enforcement remains the responsibility of Kyvro Core.

The SDK only describes the callable API.

---

## 22. Recommended Permission Types

The SDK may export permission names so plugin tooling can reuse them:

```ts
export type Permission =
  | "clipboard.read"
  | "clipboard.write"
  | "storage"
  | "shell.execute"
  | "http.request"
  | "project.read"
  | "index.write";
```

However, the authoritative permission validation is still performed by Core.

---

## 23. Future SDK Layout

When `index.d.ts` becomes too large, the package may be split internally:

```text
@kyvro/plugin-sdk/
├── index.d.ts
├── plugin.d.ts
├── command.d.ts
├── action.d.ts
├── index-api.d.ts
├── storage.d.ts
├── clipboard.d.ts
├── shell.d.ts
├── http.d.ts
├── project.d.ts
└── ui.d.ts
```

`index.d.ts` remains the public entry:

```ts
export * from "./plugin";
export * from "./command";
export * from "./action";
export * from "./index-api";
export * from "./storage";
export * from "./clipboard";
export * from "./shell";
export * from "./http";
export * from "./project";
export * from "./ui";
```

Plugin authors continue using:

```js
/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
```

without knowing the internal SDK file structure.

---

## 24. Plugin Developer Experience

Target developer workflow:

```bash
mkdir kyvro-date-plugin
cd kyvro-date-plugin

npm init -y

npm install -D @kyvro/plugin-sdk
```

Create:

```text
index.js
```

Then:

```js
// @ts-check

/**
 * @type {import("@kyvro/plugin-sdk").Plugin}
 */
module.exports = {
  id: "date",
  name: "Date",

  activate(ctx) {
    ctx.commands.register({
      id: "date.now",
      title: "Current Date",

      execute() {
        return new Date().toISOString();
      }
    });
  }
};
```

The developer immediately receives IDE autocomplete without:

- TypeScript source files
- compilation
- bundling
- runtime SDK imports
- Node.js runtime dependency inside Kyvro

---

## 25. Non-Goals

The initial `@kyvro/plugin-sdk` does not:

- Execute plugins
- Load CommonJS modules
- Implement `require()`
- Access the filesystem
- Access the clipboard
- Execute shell commands
- Perform HTTP requests
- Manage plugin permissions
- Install plugins
- Package plugins
- Provide Node.js polyfills
- Guarantee Node.js compatibility

These responsibilities belong to Kyvro Core and related tooling.

---

## 26. Final Architecture

```text
                        npm
                         |
                         v
              @kyvro/plugin-sdk
                         |
                         v
                    index.d.ts
                         |
                JSDoc / TypeScript
                         |
                         v
                   Plugin Author
                         |
                         v
                     index.js
                         |
                    CommonJS
                         |
                         v
                  Kyvro Runtime
                         |
                 plugin.activate(ctx)
                         |
                         v
                  PluginContext
               /       |        \
              /        |         \
      commands       storage      shell
      actions        clipboard    http
      index          project      ui
```

---

## 27. Final Decision

The official Kyvro plugin development model is:

```text
Language
└── JavaScript

Module Format
└── CommonJS

Type System
├── JSDoc
└── @kyvro/plugin-sdk/index.d.ts

SDK Distribution
└── npm

SDK Dependency
└── devDependency

Runtime SDK Dependency
└── None

Runtime
└── Kyvro Core / goja

Runtime Capabilities
└── PluginContext
```

`@kyvro/plugin-sdk` is therefore a **types-only development contract** between Kyvro Core and plugin developers.

The plugin SDK should stay lightweight, stable, well-documented and independent from the actual plugin runtime implementation.
