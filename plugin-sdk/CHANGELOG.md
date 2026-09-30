# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-09-29

### Changed

- Unified Command protocol (plugins.md): `onAction` is the single business
  entry for both call sources — prefix-hit live calls (command `id` as
  actionId, full query as `args[0]`, ~150ms budget) and callback actions
  (~5s budget). `provider.search` is removed.

### Removed

- `ActivationEvent` type and the manifest `activationEvents` field;
  `activate` runs unconditionally at load
- `ManifestCommand.subtitle` / `keywords`; `prefix` is now required
- `TemplateAPI` and `ctx.template` (Text Snippets is offline)

## [0.0.1] - 2026-08-27

### Added

- Initial declaration surface, aligned 1:1 with the Kyvro plugin host
  (`internal/plugin/*.go`):
  - Manifest schema v1 types: `PluginManifest`, `ManifestCommand`,
    `ActivationEvent`, `Permission`, `Platform`
  - Result-row contract: `ResultRow`, `PluginAction`
  - Module-exports contract: `Plugin` (`provider.search`, `onAction`,
    `activate`)
  - Context capability surfaces: `PluginStorage`, `LogAPI`, `TemplateAPI`,
    plus `MaybePromise<T>` helper
- Type tests (`test/type-tests.ts`) and JavaScript/JSDoc tests
  (`test/basic-plugin.js`)
