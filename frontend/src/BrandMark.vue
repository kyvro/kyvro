<script setup lang="ts">
import { computed } from "vue";
import brandMarkRaw from "../../build/darwin/tray-template.svg?raw";

// Single source for the app's small brand mark: edit
// build/darwin/tray-template.svg, then regenerate tray-template.png.
const props = withDefaults(defineProps<{ size?: number }>(), { size: 20 });

const svg = computed(() =>
  brandMarkRaw
    .replace("<svg ", '<svg class="block h-full w-full" ')
    .replaceAll("#000000", "currentColor"),
);
</script>

<template>
  <span
    class="inline-block shrink-0 text-white"
    :style="{
      width: `${size}px`,
      height: `${size}px`,
    }"
    aria-hidden="true"
    v-html="svg"
  />
</template>
