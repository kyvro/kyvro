/**
 * @kyvro/plugin-sdk — type declarations for Kyvro launcher plugins.
 *
 * This package is types-only: it exists for JSDoc/TypeScript resolution,
 * IDE autocomplete and static validation. Kyvro Core executes plugins from
 * an embedded goja VM using the CommonJS protocol (`module.exports`; there
 * is no `require()` or ESM support), and sync-or-Promise returns are run
 * under per-call wall-clock timeouts.
 *
 * Runtime facts documented here mirror the host implementation in
 * `internal/plugin/*.go` (manifest.go, runtime.go, convert.go, storage.go,
 * permission.go). When host behavior changes, keep this file in sync.
 *
 * Annotating a plugin entry file for full IntelliSense:
 *
 *   // @ts-check
 *   // JSDoc annotation placed directly above `module.exports`:
 *   //   @type {import("@kyvro/plugin-sdk").Plugin}
 *
 * See README.md for complete examples.
 */

export type MaybePromise<T> = T | Promise<T>;

/* ------------------------------------------------------------------ */
/* Manifest (plugin.json, schemaVersion 1)                             */
/* ------------------------------------------------------------------ */

/**
 * plugin.json — the one schema the host understands is `1`; any other value
 * rejects the plugin (INCOMPATIBLE_VERSION).
 */
export interface PluginManifest {
  /** Exactly `1`. */
  schemaVersion: 1;
  /**
   * Reverse-domain identifier matching the install directory name,
   * e.g. `"com.example.demo"` (lowercase labels/digits/hyphens, ≥2 labels).
   */
  id: string;
  /** Display name; defaults to `id`. */
  name?: string;
  /** Plugin SemVer; also the version directory being loaded. */
  version: string;
  description?: string;
  author?: { name: string; url?: string };
  /** Entry script relative to the version directory (no abs paths, no `..`). */
  main: string;
  /** Icon relative to the version directory (svg/png/jpg…). */
  icon?: string;
  /** Must not exceed the running host version (`0.1.0` in V1). */
  minHostVersion: string;
  /** When present, must contain the host GOOS (`darwin` | `windows` | `linux`). */
  platforms?: Platform[];
  permissions?: Permission[];
  commands?: ManifestCommand[];
}

export type Platform = "darwin" | "windows" | "linux";

/**
 * Permission `<capability>` or `<capability>:<scope>`. V1 implements only
 * `"storage"`; other parsed capabilities are denied and their APIs never
 * appear on the {@link PluginContext} (CAPABILITY_UNAVAILABLE).
 */
export type Permission =
  | "storage"
  | `${
      | "network"
      | "filesystem"
      | "shell"
      | "clipboard"
      | "secrets"
      | "system"}:${string}`
  | "background";

/**
 * Statically declared command — the only extension type. `prefix` joins the
 * host command index (lowercased, duplicates within one manifest rejected);
 * while the query starts with the prefix at a word boundary ("gh" matches
 * "gh x" and bare "gh", never "ghost"), the host live-calls `onAction` with
 * this command's `id` as the actionId and the FULL query as the single
 * element of `args`.
 */
export interface ManifestCommand {
  /** Unique within the manifest; used as the `onAction` actionId. */
  id: string;
  /** Display name (settings UI); defaults to `id`. */
  title?: string;
  /** Trigger prefix; required. Matched case-insensitively. */
  prefix: string;
}

/* ------------------------------------------------------------------ */
/* Result rows                                                         */
/* ------------------------------------------------------------------ */

/**
 * One row returned by `onAction`. Rendered with the manifest icon;
 * effective ID becomes `plugin:<pluginId>:<rowId>`.
 *
 * Entries missing `id`, `title`, or valid `actions` are silently dropped
 * (host logs the count) — they are never shown and never crash the app.
 */
export interface ResultRow {
  id: string;
  title: string;
  subtitle?: string;
  /** Soft ranking hint clamped to `0..50`; relevance still dominates ordering. */
  scoreHint?: number;
  /** Non-empty; the FIRST action runs on Enter and it must be valid or the whole row is dropped. */
  actions: [PluginAction, ...PluginAction[]];
}

/** Host-executed side effects; unknown action types invalidate the whole row. */
export type PluginAction =
  | {
      /** Opens `url` in the user's configured browser (Settings › General). */
      type: "open-url";
      url: string;
    }
  | {
      /** Copies `value` to the clipboard. */
      type: "copy";
      value: string;
    }
  | {
      /** Re-enters the plugin via `onAction(id, args ?? [])`. */
      type: "callback";
      id: string;
      args?: string[];
    };

/* ------------------------------------------------------------------ */
/* Module exports                                                      */
/* ------------------------------------------------------------------ */

/**
 * The shape of `module.exports` expected by the host loader; all members
 * optional. `onAction` is the single business entry for both call sources
 * (the host distinguishes them by timing and budget, not by id).
 */
export interface Plugin {
  /**
   * Only business entry. Two call sources:
   *
   * - **prefix hit (live)**: called on EVERY keystroke while the query
   *   matches a declared command prefix; actionId is that command's `id`
   *   and `args` is `[fullQuery]` (e.g. `"gh vim"` arrives as-is — gate and
   *   slice the prefix yourself like the official GitHub plugin). Budget
   *   ~150ms; late results are dropped, and three consecutive timeout
   *   strikes auto-disable the plugin until it is re-enabled.
   * - **callback action**: called once when the user runs a `"callback"`
   *   action; actionId is the action's `id` and `args` is the action's
   *   `args ?? []`. Budget ~5s; rows open in the secondary list.
   *
   * Returning non-array values is treated as an error by the host.
   */
  onAction?(
    actionId: string,
    args: string[]
  ): MaybePromise<ResultRow[]>;
  /**
   * Optional init hook run once at load (~2s budget, awaited Promise
   * included). Typical use: `ctx.storage` warm-up. This is the only
   * lifecycle callback — there is no unload/deactivate hook; cleanup on
   * disable/uninstall/reload is host-side.
   */
  activate?(ctx: PluginContext): void | Promise<void>;
}

/* ------------------------------------------------------------------ */
/* Plugin context                                                      */
/* ------------------------------------------------------------------ */

/**
 * Capability surface handed to `activate()`. Unimplemented capabilities are
 * absent — feature-detect (`if (ctx.storage)`) instead of assuming.
 */
export interface PluginContext {
  /** Present ONLY when the `"storage"` permission was granted. */
  storage?: PluginStorage;
  /** Host logger; entries land in the application log as `plugin <id> [<level>]: …`. */
  log: LogAPI;
}

/**
 * Persisted per-plugin string→string KV store (bucket `plugin:<id>`);
 * survives reloads/upgrades. Keys and values pass through JS ToString before
 * being persisted.
 */
export interface PluginStorage {
  /** Returns the stored string, or `undefined` when missing. */
  get(key: string): string | undefined;
  set(key: string, value: string): void;
  delete(key: string): void;
}

/** Host logger; messages are space-joined into the application log. */
export interface LogAPI {
  info(...parts: unknown[]): void;
  warn(...parts: unknown[]): void;
  error(...parts: unknown[]): void;
}
