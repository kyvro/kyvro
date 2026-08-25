<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { Events, Window } from "@wailsio/runtime";
import { Search, Launch, RunAction } from "../bindings/kyvro/service/searchservice";
import { ActionKind, type SearchResult } from "../bindings/kyvro/internal/core/models";
import Settings from "./Settings.vue";
import { monogramStyle } from "./monogram";

// The summon window serves "/" and the settings window "/#settings" from the
// same SPA; hash routing avoids server-side fallback concerns.
const isSettings = ref(window.location.hash === "#settings");
function onHashChange() {
  isSettings.value = window.location.hash === "#settings";
}

const query = ref("");
const results = ref<SearchResult[]>([]);
// Secondary (plugin) views: each entry is the list to restore on "esc back".
// Empty stack = primary mode.
const viewStack = ref<SearchResult[][]>([]);
const selected = ref(0);
const inputEl = ref<HTMLInputElement | null>(null);
const error = ref("");

let debounceTimer: ReturnType<typeof setTimeout> | undefined;
let seq = 0; // guards out-of-order search responses
let offShown: (() => void) | undefined;

async function runSearch(q: string) {
  const mine = ++seq;
  if (q === "") {
    // Blank input shows no list — each summon starts from a clean slate.
    results.value = [];
    selected.value = 0;
    error.value = "";
    return;
  }
  try {
    const res = await Search(q);
    if (mine !== seq) return; // a newer keystroke already resolved
    results.value = res ?? [];
    selected.value = 0;
    error.value = "";
  } catch (e) {
    if (mine !== seq) return;
    error.value = String(e);
  }
}

function onInput() {
  if (viewStack.value.length > 0) {
    // Typing in a secondary view pops back to the primary list first.
    results.value = viewStack.value[viewStack.value.length - 1];
    viewStack.value = [];
    selected.value = 0;
  }
  clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => runSearch(query.value), 30);
}

// reset blanks the UI. It must only run while the window is invisible
// (on hide, or defensively on summon) so the clear never paints.
function reset() {
  seq++; // invalidate in-flight responses
  query.value = "";
  results.value = [];
  viewStack.value = [];
  selected.value = 0;
  error.value = "";
}

async function activate(index: number) {
  const r = results.value[index];
  if (!r) return;
  if (r.Action?.Kind === ActionKind.ActionPlugin) {
    // Plugin row: run it; results push a secondary view (window stays
    // visible), an empty list ends the interaction like a normal launch.
    try {
      const res = await RunAction(r.ID);
      if (res && res.length > 0) {
        seq++; // drop any in-flight search so it cannot overwrite the view
        viewStack.value.push(results.value);
        results.value = res;
        selected.value = 0;
        return;
      }
      await Window.Hide();
    } catch (e) {
      error.value = String(e);
    }
    return;
  }
  try {
    await Launch(r.ID);
    await Window.Hide();
  } catch (e) {
    error.value = String(e);
  }
}

function scrollSelectedIntoView() {
  document
    .getElementById(`kyvro-row-${selected.value}`)
    ?.scrollIntoView({ block: "nearest" });
}

function onKeydown(e: KeyboardEvent) {
  switch (e.key) {
    case "ArrowDown":
      if (selected.value < results.value.length - 1) selected.value++;
      e.preventDefault();
      scrollSelectedIntoView();
      break;
    case "ArrowUp":
      if (selected.value > 0) selected.value--;
      e.preventDefault();
      scrollSelectedIntoView();
      break;
    case "Enter":
      void activate(selected.value);
      break;
    case "Escape":
      if (viewStack.value.length > 0) {
        results.value = viewStack.value.pop()!;
        selected.value = 0;
      } else {
        void Window.Hide().then(() => reset());
      }
      break;
  }
}

// Deterministic monogram colour comes from ./monogram (shared with Settings).

// Icon paths that failed to load fall back to the monogram.
const brokenIcons = ref(new Set<string>());

function hasIcon(r: SearchResult) {
  return !!r.IconPath && !brokenIcons.value.has(r.IconPath);
}

function iconSrc(r: SearchResult) {
  return `/appicon?path=${encodeURIComponent(r.IconPath)}`;
}

function onIconError(r: SearchResult) {
  brokenIcons.value.add(r.IconPath);
}

function isCalcResult(r: SearchResult) {
  return r.ID.startsWith("calc:");
}

function isWebResult(r: SearchResult) {
  return r.ID.startsWith("web:");
}

function isPluginResult(r: SearchResult) {
  return r.ID.startsWith("plugin:");
}

onMounted(() => {
  window.addEventListener("hashchange", onHashChange);
  offShown = Events.On("kyvro:shown", () => {
    reset();
    void nextTick(() => inputEl.value?.focus());
  });
  // Covers the focus-loss hide too: clear while the window is already
  // invisible so the next summon starts empty without a flash.
  document.addEventListener("visibilitychange", onVisibility);
  inputEl.value?.focus();
});

function onVisibility() {
  if (document.hidden) reset();
}

onBeforeUnmount(() => {
  window.removeEventListener("hashchange", onHashChange);
  offShown?.();
  document.removeEventListener("visibilitychange", onVisibility);
  clearTimeout(debounceTimer);
});
</script>

<template>
  <Settings v-if="isSettings" />
  <div
    v-else
    class="flex h-screen flex-col overflow-hidden rounded-2xl border border-white/10 bg-[#1b1b20]/75 shadow-2xl backdrop-blur-xl"
  >
    <!-- Search input -->
    <div class="flex h-[68px] shrink-0 items-center gap-3.5 border-b border-white/10 px-5">
      <svg
        class="h-5 w-5 shrink-0 text-white/35"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        stroke-linecap="round"
      >
        <circle cx="11" cy="11" r="7" />
        <path d="m20 20-3.5-3.5" />
      </svg>
      <input
        ref="inputEl"
        v-model="query"
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder="Search apps…"
        class="h-full flex-1 bg-transparent text-[19px] font-light text-white placeholder-white/25 outline-none"
        @input="onInput"
        @keydown="onKeydown"
      />
    </div>

    <!-- Results -->
    <div class="min-h-0 flex-1 overflow-y-auto py-2">
      <div
        v-for="(r, i) in results"
        :id="`kyvro-row-${i}`"
        :key="r.ID"
        class="mx-2 flex cursor-default items-center gap-3.5 rounded-lg px-3 py-2"
        :class="i === selected ? 'bg-white/12 ring-1 ring-white/10' : ''"
        @mouseenter="selected = i"
        @click="activate(i)"
      >
        <!-- Real app icon served by the Go side; monogram fallback -->
        <img
          v-if="hasIcon(r)"
          :src="iconSrc(r)"
          alt=""
          loading="lazy"
          decoding="async"
          draggable="false"
          class="h-9 w-9 shrink-0 rounded-[10px]"
          @error="onIconError(r)"
        />
        <div
          v-else-if="isCalcResult(r)"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-[10px] bg-white/10"
        >
          <svg
            class="h-4.5 w-4.5 text-white/70"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
          >
            <rect x="5" y="3" width="14" height="18" rx="2" />
            <path d="M8 7h8M8 12h.01M12 12h.01M16 12h.01M8 16h.01M12 16h.01M16 16h.01" />
          </svg>
        </div>
        <!-- Plugin rows share the uniform puzzle glyph (per-plugin logos are M2) -->
        <div
          v-else-if="isPluginResult(r)"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-[10px] bg-white/10 text-white/60"
        >
          <svg
            class="h-4.5 w-4.5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="M10 3v4M14 3v4M10 17v4M14 17v4M3 10h4M3 14h4M17 10h4M17 14h4" />
            <rect x="7" y="7" width="10" height="10" rx="2" />
          </svg>
        </div>
        <div
          v-else-if="!isWebResult(r)"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-[10px] text-[15px] font-semibold text-white/95"
          :style="monogramStyle(r.Title)"
        >
          {{ r.Title.trim().charAt(0).toUpperCase() || "?" }}
        </div>
        <div
          v-else
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-[10px] bg-white/10"
        >
          <svg
            class="h-4.5 w-4.5 text-white/70"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
          >
            <circle cx="12" cy="12" r="9" />
            <path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18" />
          </svg>
        </div>

        <div class="min-w-0 flex-1">
          <div class="truncate text-[15px] leading-tight" :class="isWebResult(r) ? 'text-white/75 italic' : 'text-white/90'">
            {{ r.Title }}
          </div>
          <div class="truncate text-xs leading-tight text-white/35">{{ r.Subtitle }}</div>
        </div>
      </div>

      <!-- Empty / error states -->
      <div v-if="error" class="px-6 py-6 text-sm text-red-300/90">{{ error }}</div>
      <div v-else-if="query && results.length === 0" class="px-6 py-6 text-sm text-white/35">
        No results for “{{ query }}”
      </div>
    </div>

    <!-- Footer -->
    <div
      class="flex h-9 shrink-0 items-center justify-between border-t border-white/10 px-5 text-[11px] text-white/30"
    >
      <span>Kyvro</span>
      <span class="tracking-wide">↑↓ navigate&nbsp;&nbsp;↵ open&nbsp;&nbsp;<template v-if="viewStack.length">esc back&nbsp;&nbsp;</template>esc hide</span>
    </div>
  </div>
</template>
