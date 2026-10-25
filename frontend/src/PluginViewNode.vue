<script setup lang="ts">
import type { ViewNode } from "../bindings/kyvro/internal/plugin/models";

const props = defineProps<{ node: ViewNode; busy: boolean }>();
const emit = defineEmits<{ (event: "action", id: string): void }>();
const tones: Record<string, string> = {
  default: "text-white/90", muted: "text-white/45", success: "text-emerald-300",
  warning: "text-amber-300", danger: "text-red-300"
};
const gaps: Record<string, string> = { small: "gap-2", medium: "gap-4", large: "gap-6" };
</script>

<template>
  <div v-if="node.type === 'column' || node.type === 'row' || node.type === 'card'"
    class="min-w-0" :class="[
      node.type === 'row' ? 'flex flex-wrap items-center' : 'flex flex-col',
      node.type === 'card' ? 'rounded-xl border border-white/10 bg-white/5 p-4' : '',
      gaps[node.gap] || gaps.medium
    ]">
    <h2 v-if="node.title" class="text-base font-medium text-white/90">{{ node.title }}</h2>
    <PluginViewNode v-for="(child, i) in node.children || []" :key="i"
      :node="child" :busy="busy" @action="emit('action', $event)" />
  </div>
  <p v-else-if="node.type === 'text'" class="whitespace-pre-wrap break-words text-sm"
    :class="tones[node.tone] || tones.default">{{ node.text }}</p>
  <div v-else-if="node.type === 'progress'" class="min-w-[120px] flex-1 space-y-2">
    <div class="flex justify-between gap-4 text-xs text-white/60">
      <span>{{ node.title }}</span><span>{{ Math.round(node.value / node.max * 100) }}%</span>
    </div>
    <progress :value="node.value" :max="node.max" :aria-label="node.title || 'Progress'"
      class="h-2 w-full overflow-hidden rounded-full accent-sky-400" />
  </div>
  <button v-else-if="node.type === 'button'" type="button" :disabled="busy"
    class="self-start rounded-lg border border-white/15 bg-white/5 px-3 py-2 text-sm text-white/85 hover:bg-white/10 focus-visible:outline-2 focus-visible:outline-sky-400 disabled:opacity-40"
    @click="emit('action', props.node.event)">{{ node.text }}</button>
</template>
