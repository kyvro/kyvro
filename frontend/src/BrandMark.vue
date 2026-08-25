<script setup lang="ts">
import { computed } from "vue";
import brandMarkRaw from "../../build/darwin/appicon.svg?raw";

// UI brand mark uses the full-color app icon. The macOS menu bar keeps its
// own monochrome template asset at build/darwin/tray-template.svg.
const props = withDefaults(defineProps<{ size?: number }>(), { size: 20 });

const svg = computed(() =>
  brandMarkRaw.replace("<svg ", '<svg class="block h-full w-full" '),
);
</script>

<template>
  <span
    class="inline-block shrink-0"
    :style="{
      width: `${size}px`,
      height: `${size}px`,
    }"
    aria-hidden="true"
    v-html="svg"
  />
</template>
