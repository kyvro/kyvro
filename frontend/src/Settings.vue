<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  Browsers,
  ExternalBrowser,
  InstallPlugin,
  Plugins,
  RevealPluginsFolder,
  SetExternalBrowser,
  SetPluginEnabled,
  UninstallPlugin,
  Version,
  AvailablePlugins,
  Snippets,
  AddSnippet,
  RemoveSnippet,
  SetSnippetEnabled,
  SnippetsEnabled,
  SetSnippetsEnabled,
  SnippetAccessibilityGranted,
  RequestSnippetAccessibility,
} from "../bindings/kyvro/service/searchservice";
import type { PluginInfo } from "../bindings/kyvro/internal/plugin/models";
import { PluginStatus } from "../bindings/kyvro/internal/plugin/models";
import type { Snippet as SnippetModel } from "../bindings/kyvro/internal/core/models";
import BrandMark from "./BrandMark.vue";

type Tab = "general" | "plugins" | "snippets" | "about";
type PluginTab = "installed" | "market";

const tab = ref<Tab>("general");
const pluginTab = ref<PluginTab>("installed");

const version = ref("");
const installedPlugins = ref<PluginInfo[]>([]);
const marketPlugins = ref<PluginInfo[]>([]);
const marketLoaded = ref(false);
const error = ref("");
const busyId = ref("");

// Text Snippets state
const snippets = ref<SnippetModel[]>([]);
const snippetsLoaded = ref(false);
const snippetsEnabled = ref(true);
const snippetAccessibilityGranted = ref(false);
const newTrigger = ref("");
const newReplacement = ref("");
const snippetsBusy = ref(false);
const openSnippetMenu = ref("");

// General pane
const browsers = ref<string[]>([]);
const browser = ref("");
const browserBusy = ref(false);

// Plugin icons
const brokenIcons = ref(new Set<string>());

function hasPluginIcon(p: PluginInfo) {
  return (!!p.IconPath && !brokenIcons.value.has(p.IconPath)) || (!!p.IconURL && !brokenIcons.value.has(p.IconURL));
}

function pluginIconSrc(p: PluginInfo) {
  if (p.IconPath && !brokenIcons.value.has(p.IconPath)) {
    return `/appicon?path=${encodeURIComponent(p.IconPath)}`;
  }
  if (p.IconURL && !brokenIcons.value.has(p.IconURL)) {
    return p.IconURL;
  }
  return "";
}

function onPluginIconError(p: PluginInfo) {
  if (p.IconPath) {
    brokenIcons.value.add(p.IconPath);
  }
  if (p.IconURL) {
    brokenIcons.value.add(p.IconURL);
  }
}

async function loadBrowser() {
  try {
    browsers.value = (await Browsers()) ?? [];
    browser.value = (await ExternalBrowser()) ?? "";
  } catch (e) {
    error.value = String(e);
  }
}

async function pickBrowser(v: string) {
  browserBusy.value = true;
  try {
    await SetExternalBrowser(v);
    browser.value = (await ExternalBrowser()) ?? "";
  } catch (e) {
    error.value = String(e);
  } finally {
    browserBusy.value = false;
  }
}

async function loadInstalledPlugins() {
  error.value = "";
  try {
    const plugins = await Plugins();
    installedPlugins.value = plugins ?? [];
  } catch (e) {
    error.value = String(e);
  }
}

async function loadMarketPlugins() {
  if (marketLoaded.value) return;

  error.value = "";
  try {
    const available = await AvailablePlugins();
    const installedIds = new Set(installedPlugins.value.map(p => p.ID));
    marketPlugins.value = (available ?? [])
      .filter(p => !installedIds.has(p.ID))
      .map(p => ({...p, Status: PluginStatus.StatusNotInstalled} as PluginInfo));
    marketLoaded.value = true;
  } catch (e) {
    error.value = String(e);
  }
}

async function toggle(p: PluginInfo) {
  busyId.value = p.ID;
  try {
    await SetPluginEnabled(p.ID, p.Disabled);
    await loadInstalledPlugins();
  } catch (e) {
    error.value = String(e);
  } finally {
    busyId.value = "";
  }
}

async function installPlugin(p: PluginInfo) {
  busyId.value = p.ID;
  try {
    await InstallPlugin(p.ID);
    await loadInstalledPlugins();
    marketPlugins.value = marketPlugins.value.filter(mp => mp.ID !== p.ID);
  } catch (e) {
    error.value = String(e);
  } finally {
    busyId.value = "";
  }
}

async function uninstallPlugin(p: PluginInfo) {
  if (!confirm(`Uninstall "${p.Name}"?`)) return;
  busyId.value = p.ID;
  try {
    await UninstallPlugin(p.ID);
    await loadInstalledPlugins();
    marketLoaded.value = false;
    if (pluginTab.value === "market") {
      await loadMarketPlugins();
    }
  } catch (e) {
    error.value = String(e);
  } finally {
    busyId.value = "";
  }
}

async function openFolder() {
  try {
    await RevealPluginsFolder();
  } catch (e) {
    error.value = String(e);
  }
}

async function switchPluginTab(newTab: PluginTab) {
  pluginTab.value = newTab;
  if (newTab === "market" && !marketLoaded.value) {
    await loadMarketPlugins();
  }
}

onMounted(async () => {
  await Promise.all([
    loadInstalledPlugins(),
    loadBrowser(),
    loadSnippets(),
    Version().then((v) => { version.value = v; }).catch(() => { version.value = ""; })
  ]);
});

// Text Snippets functions
async function loadSnippets() {
  error.value = "";
  try {
    const [snipList, enabled] = await Promise.all([
      Snippets(),
      SnippetsEnabled()
    ]);
    snippets.value = snipList ?? [];
    snippetsEnabled.value = enabled ?? true;
    snippetAccessibilityGranted.value = await SnippetAccessibilityGranted().catch(() => false);
    snippetsLoaded.value = true;
  } catch (e) {
    error.value = String(e);
  }
}

async function requestSnippetAccessibility() {
  snippetsBusy.value = true;
  try {
    await RequestSnippetAccessibility();
    snippetAccessibilityGranted.value = await SnippetAccessibilityGranted().catch(() => false);
  } catch (e) {
    error.value = String(e);
  } finally {
    snippetsBusy.value = false;
  }
}

async function toggleSnippetsEnabled() {
  snippetsBusy.value = true;
  try {
    await SetSnippetsEnabled(!snippetsEnabled.value);
    snippetsEnabled.value = !snippetsEnabled.value;
  } catch (e) {
    error.value = String(e);
  } finally {
    snippetsBusy.value = false;
  }
}

async function addSnippet() {
  if (!newTrigger.value || !newReplacement.value) return;

  snippetsBusy.value = true;
  try {
    await AddSnippet(newTrigger.value, newReplacement.value);
    newTrigger.value = "";
    newReplacement.value = "";
    await loadSnippets();
  } catch (e) {
    error.value = String(e);
  } finally {
    snippetsBusy.value = false;
  }
}

async function removeSnippet(snippet: SnippetModel) {
  if (!confirm(`Remove snippet "${snippet.trigger}"?`)) return;

  snippetsBusy.value = true;
  try {
    await RemoveSnippet(snippet.trigger);
    await loadSnippets();
  } catch (e) {
    error.value = String(e);
  } finally {
    snippetsBusy.value = false;
  }
}

async function setSnippetEnabled(snippet: SnippetModel, enabled: boolean) {
  snippetsBusy.value = true;
  try {
    await SetSnippetEnabled(snippet.trigger, enabled);
    await loadSnippets();
  } catch (e) {
    error.value = String(e);
  } finally {
    snippetsBusy.value = false;
  }
}

function toggleSnippetMenu(snippet: SnippetModel) {
  if (snippetsBusy.value) return;
  openSnippetMenu.value = openSnippetMenu.value === snippet.trigger ? "" : snippet.trigger;
}

async function runSnippetAction(snippet: SnippetModel, action: "enable" | "disable" | "delete") {
  openSnippetMenu.value = "";

  if (action === "enable") {
    await setSnippetEnabled(snippet, true);
  } else if (action === "disable") {
    await setSnippetEnabled(snippet, false);
  } else if (action === "delete") {
    await removeSnippet(snippet);
  }
}
</script>

<template>
  <div class="flex h-screen bg-[#1b1b20] text-white">
    <!-- Sidebar -->
    <aside class="flex w-[184px] shrink-0 flex-col border-r border-white/10 bg-white/[0.03] p-2.5">
      <div class="flex items-center gap-2.5 px-2 pb-4 pt-2 text-white/90">
        <BrandMark :size="20" />
        <span class="text-[13px] font-semibold tracking-wide">Kyvro</span>
      </div>

      <button
        v-for="t in (['general', 'plugins', 'snippets', 'about'] as Tab[])"
        :key="t"
        class="flex items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[13px] transition-colors"
        :class="tab === t ? 'bg-white/12 text-white/95' : 'text-white/50 hover:bg-white/[0.06] hover:text-white/80'"
        @click="tab = t"
      >
        <svg class="h-4 w-4 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <template v-if="t === 'general'">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h0a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h0a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v0a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </template>
          <template v-else-if="t === 'plugins'">
            <path d="M10 3v4M14 3v4M10 17v4M14 17v4M3 10h4M3 14h4M17 10h4M17 14h4" />
            <rect x="7" y="7" width="10" height="10" rx="2" />
          </template>
          <template v-else-if="t === 'snippets'">
            <path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" />
          </template>
          <template v-else>
            <circle cx="12" cy="12" r="9" />
            <path d="M12 8h.01M12 11v5" />
          </template>
        </svg>
        {{ t === "general" ? "General" : t === "plugins" ? "Plugins" : t === "snippets" ? "Text Snippets" : "About" }}
      </button>

      <div class="mt-auto px-2.5 py-2 text-[10px] text-white/25">
        {{ version ? `v${version}` : "Kyvro" }}
      </div>
    </aside>

    <!-- General pane -->
    <main v-if="tab === 'general'" class="flex min-w-0 flex-1 flex-col">
      <header class="shrink-0 border-b border-white/10 px-5 py-3.5">
        <h1 class="text-[15px] font-medium leading-tight">General</h1>
        <p class="mt-0.5 text-xs leading-tight text-white/40">Host preferences</p>
      </header>

      <div class="flex-1 overflow-y-auto p-5">
        <section class="max-w-[520px]">
          <h2 class="text-[13px] font-medium text-white/85">External Browser</h2>
          <p class="mt-1 text-xs leading-relaxed text-white/40">
            Browser used for web results and plugin links (open-url actions).
          </p>
          <select
            :disabled="browserBusy"
            :value="browser"
            class="mt-3 w-full appearance-none rounded-lg border border-white/15 bg-[#242429] px-3 py-2 text-[13px] text-white/90 outline-none transition-colors hover:border-white/25 focus:border-white/35 disabled:opacity-50"
            @change="pickBrowser(($event.target as HTMLSelectElement).value)"
          >
            <option value="">System Default</option>
            <option v-for="b in browsers" :key="b" :value="b">{{ b }}</option>
          </select>
        </section>
      </div>
    </main>

    <!-- Plugins pane -->
    <main v-else-if="tab === 'plugins'" class="flex min-w-0 flex-1 flex-col">
      <header class="shrink-0 border-b border-white/10 px-5 py-3.5">
        <div class="flex items-center justify-between">
          <div>
            <h1 class="text-[15px] font-medium leading-tight">Plugins</h1>
            <p class="mt-0.5 text-xs leading-tight text-white/40">
              Manage your installed plugins or discover new ones from the marketplace
            </p>
          </div>
          <button
            class="rounded-lg border border-white/15 px-3 py-1.5 text-xs text-white/80 transition-colors hover:bg-white/10"
            @click="openFolder"
          >
            Open Plugins Folder
          </button>
        </div>

        <div class="mt-4 flex gap-2">
          <button
            class="rounded-lg px-3 py-1.5 text-xs transition-colors"
            :class="pluginTab === 'installed' ? 'bg-white/12 text-white/95' : 'text-white/50 hover:bg-white/[0.06] hover:text-white/80'"
            @click="switchPluginTab('installed')"
          >
            Installed
          </button>
          <button
            class="rounded-lg px-3 py-1.5 text-xs transition-colors"
            :class="pluginTab === 'market' ? 'bg-white/12 text-white/95' : 'text-white/50 hover:bg-white/[0.06] hover:text-white/80'"
            @click="switchPluginTab('market')"
          >
            Marketplace
          </button>
        </div>
      </header>

      <div class="min-h-0 flex-1 overflow-y-auto p-4">
        <div v-if="error" class="px-2 py-4 text-sm text-red-300/90">{{ error }}</div>

        <!-- Installed plugins -->
        <div v-if="pluginTab === 'installed'">
          <div v-if="installedPlugins.length === 0" class="px-2 py-10 text-center text-sm text-white/35">
            No plugins installed
          </div>

          <div v-for="p in installedPlugins" v-else :key="p.ID" class="mb-2 flex items-center gap-3.5 rounded-xl px-3 py-2.5" :class="p.Disabled ? 'opacity-50' : ''">
            <img v-if="hasPluginIcon(p)" :src="pluginIconSrc(p)" alt="" loading="lazy" decoding="async" draggable="false" class="h-10 w-10 shrink-0 rounded-[10px] bg-white/[0.06] object-contain p-1.5" @error="onPluginIconError(p)" />
            <div v-else class="flex h-10 w-10 shrink-0 items-center justify-center rounded-[10px] bg-white/[0.08] text-white/55">
              <svg class="h-[18px] w-[18px]" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M10 3v4M14 3v4M10 17v4M14 17v4M3 10h4M3 14h4M17 10h4M17 14h4" />
                <rect x="7" y="7" width="10" height="10" rx="2" />
              </svg>
            </div>

            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="truncate text-[14px] leading-tight text-white/90">{{ p.Name }}</span>
                <span class="shrink-0 rounded bg-white/10 px-1.5 py-0.5 text-[10px] leading-none text-white/50">{{ p.Version }}</span>
              </div>
              <div class="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] leading-none text-white/40">
                <span class="truncate">{{ p.ID }}</span>
                <span v-if="p.AutoDisabled" class="rounded bg-amber-400/15 px-1.5 py-0.5 text-amber-300/80">disabled after repeated timeouts</span>
                <span v-for="perm in p.Permissions" :key="perm" class="rounded bg-white/10 px-1.5 py-0.5 text-white/55">{{ perm }}</span>
              </div>
            </div>

            <div class="flex items-center gap-2">
              <button :disabled="busyId === p.ID" class="rounded-lg border border-red-400/30 px-2.5 py-1 text-xs text-red-300/80 transition-colors hover:bg-red-400/10 disabled:opacity-40" @click="uninstallPlugin(p)">Uninstall</button>
              <button :disabled="busyId === p.ID" class="relative h-6 w-10 shrink-0 rounded-full transition-colors disabled:opacity-40" :class="p.Disabled ? 'bg-white/15' : 'bg-emerald-500/80'" :title="p.Disabled ? 'Enable' : 'Disable'" @click="toggle(p)">
                <span class="absolute top-[3px] h-[18px] w-[18px] rounded-full bg-white transition-all" :class="p.Disabled ? 'left-[3px]' : 'left-[19px]'"></span>
              </button>
            </div>
          </div>
        </div>

        <!-- Marketplace plugins -->
        <div v-if="pluginTab === 'market'">
          <div v-if="marketPlugins.length === 0 && !marketLoaded" class="px-2 py-10 text-center text-sm text-white/35">Loading marketplace...</div>
          <div v-else-if="marketPlugins.length === 0" class="px-2 py-10 text-center text-sm text-white/35">No plugins available in marketplace</div>

          <div v-for="p in marketPlugins" v-else :key="p.ID" class="mb-2 flex items-center gap-3.5 rounded-xl px-3 py-2.5">
            <img v-if="hasPluginIcon(p)" :src="pluginIconSrc(p)" alt="" loading="lazy" decoding="async" draggable="false" class="h-10 w-10 shrink-0 rounded-[10px] bg-white/[0.06] object-contain p-1.5" @error="onPluginIconError(p)" />
            <div v-else class="flex h-10 w-10 shrink-0 items-center justify-center rounded-[10px] bg-white/[0.08] text-white/55">
              <svg class="h-[18px] w-[18px]" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M10 3v4M14 3v4M10 17v4M14 17v4M3 10h4M3 14h4M17 10h4M17 14h4" />
                <rect x="7" y="7" width="10" height="10" rx="2" />
              </svg>
            </div>

            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span class="truncate text-[14px] leading-tight text-white/90">{{ p.Name }}</span>
                <span class="shrink-0 rounded bg-white/10 px-1.5 py-0.5 text-[10px] leading-none text-white/50">{{ p.Version }}</span>
              </div>
              <div class="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] leading-none text-white/40">
                <span class="truncate">{{ p.ID }}</span>
                <span class="truncate">{{ p.Description }}</span>
              </div>
            </div>

            <button :disabled="busyId === p.ID" class="rounded-lg bg-emerald-500/20 px-3 py-1.5 text-xs text-emerald-300/90 transition-colors hover:bg-emerald-500/30 disabled:opacity-40" @click="installPlugin(p)">
              {{ busyId === p.ID ? 'Installing...' : 'Install' }}
            </button>
          </div>
        </div>
      </div>
    </main>

    <!-- Text Snippets pane -->
    <main v-else-if="tab === 'snippets'" class="flex min-w-0 flex-1 flex-col">
      <header class="shrink-0 border-b border-white/10 px-5 py-3.5">
        <h1 class="text-[15px] font-medium leading-tight">Text Snippets</h1>
        <p class="mt-0.5 text-xs leading-tight text-white/40">
          Global text expansion for static text and date templates
        </p>
      </header>

      <div class="flex-1 overflow-y-auto p-5">
        <div v-if="error" class="mb-4 px-2 py-3 text-sm text-red-300/90">{{ error }}</div>

        <section v-if="!snippetAccessibilityGranted" class="mb-6 flex items-center justify-between rounded-xl border border-amber-300/20 bg-amber-300/[0.06] px-4 py-3">
          <div>
            <h2 class="text-[13px] font-medium text-amber-100/90">Accessibility Permission Required</h2>
            <p class="mt-0.5 text-xs text-amber-100/55">
              Grant permission to Kyvro itself, not VS Code, for installed app expansion.
            </p>
          </div>
          <button
            :disabled="snippetsBusy"
            class="rounded-lg bg-amber-300/15 px-3 py-1.5 text-xs text-amber-100/90 transition-colors hover:bg-amber-300/25 disabled:opacity-40"
            @click="requestSnippetAccessibility"
          >
            Allow
          </button>
        </section>

        <!-- Enable/Disable toggle -->
        <section class="mb-6 flex items-center justify-between rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
          <div>
            <h2 class="text-[13px] font-medium text-white/85">Enable Text Expansion</h2>
            <p class="mt-0.5 text-xs text-white/40">
              Automatically expand triggers as soon as they are typed
            </p>
          </div>
          <button
            :disabled="snippetsBusy"
            class="relative h-6 w-10 shrink-0 rounded-full transition-colors disabled:opacity-40"
            :class="snippetsEnabled ? 'bg-emerald-500/80' : 'bg-white/15'"
            :title="snippetsEnabled ? 'Disable' : 'Enable'"
            @click="toggleSnippetsEnabled"
          >
            <span class="absolute top-[3px] h-[18px] w-[18px] rounded-full bg-white transition-all" :class="snippetsEnabled ? 'left-[19px]' : 'left-[3px]'"></span>
          </button>
        </section>

        <!-- Add new snippet -->
        <section class="mb-6 rounded-xl border border-white/10 bg-white/[0.03] p-4">
          <h2 class="mb-3 text-[13px] font-medium text-white/85">Add New Snippet</h2>
          <div class="flex gap-3">
            <input
              v-model="newTrigger"
              :disabled="snippetsBusy"
              type="text"
              placeholder="Trigger (e.g., dd)"
              class="flex-1 appearance-none rounded-lg border border-white/15 bg-[#242429] px-3 py-2 text-[13px] text-white/90 outline-none transition-colors placeholder:text-white/30 hover:border-white/25 focus:border-white/35 disabled:opacity-50"
            />
            <input
              v-model="newReplacement"
              :disabled="snippetsBusy"
              type="text"
              placeholder='Replacement (e.g., xxxxx or ${date("YYMMDD")})'
              class="flex-2 w-64 appearance-none rounded-lg border border-white/15 bg-[#242429] px-3 py-2 text-[13px] text-white/90 outline-none transition-colors placeholder:text-white/30 hover:border-white/25 focus:border-white/35 disabled:opacity-50"
            />
            <button
              :disabled="snippetsBusy || !newTrigger || !newReplacement"
              class="rounded-lg bg-emerald-500/20 px-4 py-2 text-xs text-emerald-300/90 transition-colors hover:bg-emerald-500/30 disabled:opacity-40 disabled:hover:bg-emerald-500/20"
              @click="addSnippet"
            >
              Add
            </button>
          </div>
        </section>

        <!-- Snippets list -->
        <section>
          <div v-if="snippets.length === 0" class="px-2 py-10 text-center text-sm text-white/35">
            No snippets configured yet
          </div>

          <div v-else class="divide-y divide-white/[0.06]">
            <div
              v-for="sn in snippets"
              :key="sn.trigger"
              class="flex min-h-9 items-center gap-3 px-2 py-1.5 transition-colors hover:bg-white/[0.035]"
              :class="sn.enabled ? '' : 'opacity-60'"
            >
              <div class="grid min-w-0 flex-1 grid-cols-[minmax(80px,140px)_minmax(0,1fr)] items-center gap-3">
                <div class="truncate font-mono text-[13px] leading-tight text-white/90">{{ sn.trigger }}</div>
                <div class="truncate text-[12px] leading-tight text-white/50">{{ sn.replacement }}</div>
              </div>

              <div class="relative shrink-0">
                <button
                  type="button"
                  :disabled="snippetsBusy"
                  class="flex h-7 w-[92px] items-center justify-between rounded-md border border-white/10 bg-[#242429] px-2 text-left text-[11px] outline-none transition-colors hover:border-white/20 focus:border-white/30 disabled:opacity-40"
                  :class="sn.enabled ? 'text-emerald-300/90' : 'text-white/45'"
                  :title="`Actions for '${sn.trigger}'`"
                  @click="toggleSnippetMenu(sn)"
                >
                  <span>{{ sn.enabled ? "Enabled" : "Disabled" }}</span>
                  <span class="text-white/35">v</span>
                </button>

                <div
                  v-if="openSnippetMenu === sn.trigger"
                  class="absolute right-0 top-full z-10 mt-1 w-[104px] overflow-hidden rounded-md border border-white/10 bg-[#242429] py-1 shadow-xl"
                >
                  <button
                    type="button"
                    class="block w-full px-2.5 py-1.5 text-left text-[11px] text-white/70 transition-colors hover:bg-white/[0.08]"
                    @click="runSnippetAction(sn, sn.enabled ? 'disable' : 'enable')"
                  >
                    {{ sn.enabled ? "Disable" : "Enable" }}
                  </button>
                  <button
                    type="button"
                    class="block w-full px-2.5 py-1.5 text-left text-[11px] text-red-300/85 transition-colors hover:bg-red-400/10"
                    @click="runSnippetAction(sn, 'delete')"
                  >
                    Delete
                  </button>
                </div>
              </div>
            </div>
          </div>
        </section>
      </div>
    </main>

    <!-- About pane -->
    <main v-else class="flex min-w-0 flex-1 flex-col items-center justify-center px-8">
      <div class="flex h-20 w-20 items-center justify-center rounded-[22px] bg-white/[0.08] text-white/90 shadow-lg ring-1 ring-white/10">
        <BrandMark :size="44" />
      </div>
      <h1 class="mt-5 text-[22px] font-semibold tracking-wide">Kyvro</h1>
      <p class="mt-1 text-[13px] text-white/40">{{ version ? `Version ${version}` : "Kyvro" }}</p>
      <p class="mt-4 max-w-[320px] text-center text-[13px] leading-relaxed text-white/55">
        Fast, keyboard-first launcher. Summon with the global hotkey, launch apps, compute, and extend with plugins.
      </p>
      <div class="mt-6 flex items-center gap-2 text-xs text-white/35">
        <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <rect x="2" y="6" width="20" height="12" rx="2" />
          <path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10" />
        </svg>
        <span>Global hotkey ⌥Space (fallback ⌥⌘Space)</span>
      </div>
    </main>
  </div>
</template>
