import { defineStore } from 'pinia'

export const usePlaybackStore = defineStore('playback', () => {
  const currentMediaId = ref<string | null>(null)
  const position = ref(0)
  const duration = ref(0)
  const isPlaying = ref(false)

  let saveInterval: ReturnType<typeof setInterval> | null = null

  function setMedia(id: string) {
    currentMediaId.value = id
    position.value = 0
    duration.value = 0
    isPlaying.value = false
  }

  function updatePosition(pos: number, dur?: number) {
    position.value = pos
    if (dur !== undefined) duration.value = dur
  }

  async function savePosition() {
    if (!currentMediaId.value || position.value <= 0) return
    try {
      const { savePosition: apiSave } = usePlaybackApi()
      await apiSave(currentMediaId.value, position.value, duration.value)
    } catch {}
  }

  async function loadPosition(mediaId: string): Promise<number> {
    try {
      const { getPosition } = usePlaybackApi()
      const res = await getPosition(mediaId)
      return res.position ?? 0
    } catch {
      return 0
    }
  }

  function startAutoSave() {
    stopAutoSave()
    saveInterval = setInterval(savePosition, 15_000)
  }

  function stopAutoSave() {
    if (saveInterval !== null) {
      clearInterval(saveInterval)
      saveInterval = null
    }
  }

  return {
    currentMediaId, position, duration, isPlaying,
    setMedia, updatePosition, savePosition, loadPosition,
    startAutoSave, stopAutoSave,
  }
})
