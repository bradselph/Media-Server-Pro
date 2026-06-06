<script setup lang="ts">
import {useHoverFrames} from '~/composables/useHoverFrames'

const props = defineProps<{
  mediaId: string
  src: string
  alt: string
  width?: number | string
  height?: number | string
  imgClass?: string
}>()

const emit = defineEmits<{
  error: []
}>()

const {previewSrc, onEnter, onLeave} = useHoverFrames(() => props.mediaId)
</script>

<template>
  <img
      :src="previewSrc ?? src"
      :alt="alt"
      :width="width"
      :height="height"
      :class="imgClass"
      loading="lazy"
      @mouseenter="onEnter"
      @mouseleave="onLeave"
      @error="emit('error')"
  >
</template>
