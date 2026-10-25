<script setup lang="ts">
import type { ViewNode } from "../bindings/kyvro/internal/plugin/models";

const props = defineProps<{ node: ViewNode; busy: boolean }>();
const emit = defineEmits<{ (event: "action", id: string): void }>();
const tones: Record<string, string> = {
  default: "text-white/90", muted: "text-white/45", success: "text-emerald-300",
  warning: "text-amber-300", danger: "text-red-300"
};
const gaps: Record<string, string> = { small: "gap-1.5", medium: "gap-3", large: "gap-4" };
</script>

<template>
  <div v-if="node.type === 'column' || node.type === 'row' || node.type === 'card'"
    class="min-w-0" :class="[
      node.type === 'row' ? 'flex flex-wrap items-center' : 'flex flex-col',
      node.type === 'card' ? 'rounded-xl border border-white/10 bg-white/5 p-3' : '',
      gaps[node.gap] || gaps.medium
    ]">
    <h2 v-if="node.title" class="text-[15px] leading-tight text-white/90">{{ node.title }}</h2>
    <PluginViewNode v-for="(child, i) in node.children || []" :key="i"
      :node="child" :busy="busy" @action="emit('action', $event)" />
  </div>
  <p v-else-if="node.type === 'text'" class="whitespace-pre-wrap break-words text-xs leading-tight"
    :class="tones[node.tone] || tones.default">{{ node.text }}</p>
  <div v-else-if="node.type === 'progress'" class="min-w-[120px] space-y-1">
    <div class="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1 text-white/60">
      <span :class="node.text ? 'text-[15px] leading-tight text-white/90' : 'text-xs'">{{ node.title }}</span>
      <span class="text-xs" :class="node.tone === 'warning' ? tones.warning : ''">{{ node.text || (Math.round(node.value / node.max * 100) + '%') }}</span>
    </div>
    <progress :value="node.value" :max="node.max" :aria-label="node.title || 'Progress'"
      class="block h-1.5 w-full overflow-hidden rounded-full"
      :class="node.tone === 'warning' ? 'accent-amber-300' : node.text ? 'accent-cyan-300' : 'accent-sky-400'" />
  </div>
  <button v-else-if="node.type === 'button'" type="button" :disabled="busy"
    class="self-start rounded-lg border border-white/15 bg-white/5 px-2.5 py-1.5 text-xs text-white/85 hover:bg-white/10 focus-visible:outline-2 focus-visible:outline-sky-400 disabled:opacity-40"
    @click="emit('action', props.node.event)">{{ node.text }}</button>
</template>
