<script setup lang="ts">
import type { UploadResult, UploadProgress } from '~/types/api'
import { formatBytes } from '~/utils/format'

definePageMeta({ layout: 'default', title: 'Upload Media', middleware: 'auth' })

const authStore = useAuthStore()
const router = useRouter()
const toast = useToast()
const uploadApi = useUploadApi()

// Redirect if user cannot upload
watchEffect(() => {
  if (!authStore.isLoading && authStore.isLoggedIn && !authStore.user?.permissions?.can_upload) {
    router.replace('/')
  }
})

const dragOver = ref(false)
const uploading = ref(false)
const category = ref('')
const selectedFiles = ref<File[]>([])
const result = ref<UploadResult | null>(null)
const progressMap = ref<Record<string, UploadProgress>>({})

// Track active poll controllers so they can be aborted on unmount
const activePolls = new Map<string, AbortController>()

async function pollProgress(uploadId: string) {
  const controller = new AbortController()
  activePolls.set(uploadId, controller)
  const maxAttempts = 20
  let consecutiveErrors = 0
  for (let i = 0; i < maxAttempts; i++) {
    if (controller.signal.aborted) return
    await new Promise(r => setTimeout(r, 1500))
    if (controller.signal.aborted) return
    try {
      const p = await uploadApi.getProgress(uploadId)
      consecutiveErrors = 0
      progressMap.value = { ...progressMap.value, [uploadId]: p }
      if (p.status === 'completed' || p.status === 'error') {
        activePolls.delete(uploadId)
        return
      }
    } catch {
      consecutiveErrors++
      if (consecutiveErrors >= 3) {
        toast.add({ title: `Unable to check status for upload — it may still be processing in the background`, color: 'warning', icon: 'i-lucide-alert-triangle' })
        activePolls.delete(uploadId)
        return
      }
    }
  }
  activePolls.delete(uploadId)
}

onUnmounted(() => {
  activePolls.forEach(c => c.abort())
  activePolls.clear()
})

const dropZoneRef = ref<HTMLElement | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)

function openFilePicker() {
  fileInputRef.value?.click()
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
  dragOver.value = true
}

function onDragLeave() {
  dragOver.value = false
}

function onDrop(e: DragEvent) {
  e.preventDefault()
  dragOver.value = false
  const files = Array.from(e.dataTransfer?.files ?? [])
  addFiles(files)
}

function onFileInput(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  addFiles(files)
  input.value = ''
}

const EXTENSION_ALLOWLIST = new Set(['.mp4', '.mkv', '.avi', '.flac', '.ogg', '.webm', '.m4a', '.aac', '.wav', '.wmv', '.mov', '.ts', '.m4v', '.mpg', '.mpeg', '.3gp', '.opus', '.wma', '.vob', '.ogv'])

function getExtension(name: string): string {
  const idx = name.lastIndexOf('.')
  return idx >= 0 ? name.slice(idx).toLowerCase() : ''
}

function addFiles(files: File[]) {
  const allowed = files.filter(f => {
    if (f.type.startsWith('video/') || f.type.startsWith('audio/') || f.type.startsWith('image/')) return true
    if (f.type === '' && EXTENSION_ALLOWLIST.has(getExtension(f.name))) return true
    return false
  })
  const rejected = files.length - allowed.length
  if (rejected > 0) {
    toast.add({ title: `${rejected} file(s) skipped — only video, audio, and image files are accepted`, color: 'warning', icon: 'i-lucide-alert-triangle' })
  }
  const existingNames = new Set(selectedFiles.value.map(f => f.name))
  const deduped = allowed.filter(f => !existingNames.has(f.name))
  const dupeCount = allowed.length - deduped.length
  if (dupeCount > 0) {
    toast.add({ title: `${dupeCount} duplicate file(s) skipped`, color: 'warning', icon: 'i-lucide-alert-triangle' })
  }
  selectedFiles.value = [...selectedFiles.value, ...deduped]
}

function removeFile(index: number) {
  selectedFiles.value = selectedFiles.value.filter((_, i) => i !== index)
}

// formatBytes imported from ~/utils/format

async function handleUpload() {
  if (selectedFiles.value.length === 0) return
  uploading.value = true
  result.value = null
  try {
    const res = await uploadApi.upload(selectedFiles.value, category.value || undefined)
    result.value = res
    progressMap.value = {}
    res.uploaded?.forEach(u => pollProgress(u.upload_id))
    const successCount = res.uploaded?.length ?? 0
    const errorCount = res.errors?.length ?? 0
    if (successCount > 0) {
      toast.add({
        title: `${successCount} file${successCount === 1 ? '' : 's'} uploaded successfully`,
        color: 'success',
        icon: 'i-lucide-check',
      })
    }
    if (errorCount > 0) {
      toast.add({
        title: `${errorCount} file${errorCount === 1 ? '' : 's'} failed to upload`,
        color: 'error',
        icon: 'i-lucide-x',
      })
    }
    if (successCount > 0) {
      selectedFiles.value = []
      category.value = ''
    }
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Upload failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    uploading.value = false
  }
}
</script>

<template>
  <UContainer class="py-8 max-w-2xl space-y-6">
    <div>
      <h1 class="text-2xl font-bold text-highlighted">Upload Media</h1>
      <p class="text-sm text-muted mt-1">Upload video, audio, or image files to the media library.</p>
    </div>

    <!-- Access check -->
    <template v-if="!authStore.isLoggedIn || !authStore.user?.permissions?.can_upload">
      <UAlert
        icon="i-lucide-lock"
        color="error"
        title="Upload not permitted"
        description="Your account does not have upload permissions. Contact an administrator."
      />
    </template>

    <template v-else>
      <!-- Drop zone -->
      <div
        ref="dropZoneRef"
        class="border-2 border-dashed rounded-lg p-10 text-center transition-colors cursor-pointer"
        :class="dragOver ? 'border-primary bg-primary/5' : 'border-default hover:border-primary/50'"
        @dragover="onDragOver"
        @dragleave="onDragLeave"
        @drop="onDrop"
        @click="openFilePicker()"
      >
        <UIcon name="i-lucide-upload-cloud" class="size-12 mx-auto text-muted mb-3" />
        <p class="text-sm font-medium">Drag and drop files here, or <span class="text-primary underline">browse</span></p>
        <p class="text-xs text-muted mt-1">Video, audio, and image files accepted</p>
        <input ref="fileInputRef" type="file" multiple accept="video/*,audio/*,image/*" class="hidden" @change="onFileInput" />
      </div>

      <!-- Category -->
      <UFormField label="Category (optional)">
        <UInput v-model="category" placeholder="e.g. Entertainment, Music, Sports…" class="w-full" />
      </UFormField>

      <!-- Selected files -->
      <div v-if="selectedFiles.length > 0" class="space-y-2">
        <p class="text-sm font-medium">{{ selectedFiles.length }} file{{ selectedFiles.length !== 1 ? 's' : '' }} selected</p>
        <UCard>
          <ul class="divide-y divide-default">
            <li
              v-for="(file, i) in selectedFiles"
              :key="i"
              class="flex items-center justify-between py-2 px-1 gap-3"
            >
              <div class="flex items-center gap-2 min-w-0">
                <UIcon
                  :name="file.type.startsWith('video/') ? 'i-lucide-film' : file.type.startsWith('audio/') ? 'i-lucide-music' : 'i-lucide-image'"
                  class="size-4 text-muted shrink-0"
                />
                <span class="text-sm truncate">{{ file.name }}</span>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <span class="text-xs text-muted">{{ formatBytes(file.size) }}</span>
                <UButton
                  icon="i-lucide-x"
                  size="xs"
                  variant="ghost"
                  color="neutral"
                  aria-label="Remove"
                  @click.stop="removeFile(i)"
                />
              </div>
            </li>
          </ul>
        </UCard>
      </div>

      <!-- Upload button -->
      <div class="flex items-center justify-end gap-3">
        <p v-if="selectedFiles.length === 0" class="text-xs text-muted">Select files to upload</p>
        <UButton
          label="Upload"
          icon="i-lucide-upload"
          color="primary"
          :loading="uploading"
          :disabled="selectedFiles.length === 0"
          @click="handleUpload"
        />
      </div>

      <!-- Results -->
      <div v-if="result" class="space-y-3">
        <div v-if="result.uploaded?.length > 0">
          <p class="text-sm font-medium text-success mb-2">Uploaded successfully</p>
          <UCard>
            <ul class="divide-y divide-default">
              <li
                v-for="u in result.uploaded"
                :key="u.upload_id"
                class="flex items-center justify-between py-2 px-1 gap-3"
              >
                <div class="flex items-center gap-2 min-w-0">
                  <UIcon name="i-lucide-check-circle" class="size-4 text-success shrink-0" />
                  <span class="text-sm truncate">{{ u.filename }}</span>
                </div>
                <div class="flex items-center gap-2 shrink-0">
                  <UBadge
                    v-if="progressMap[u.upload_id]"
                    :label="progressMap[u.upload_id].status"
                    :color="progressMap[u.upload_id].status === 'completed' ? 'success' : progressMap[u.upload_id].status === 'error' ? 'error' : 'warning'"
                    variant="subtle"
                    size="xs"
                  />
                  <span class="text-xs text-muted">{{ formatBytes(u.size) }}</span>
                </div>
              </li>
            </ul>
          </UCard>
        </div>

        <div v-if="result.errors?.length > 0">
          <p class="text-sm font-medium text-error mb-2">Failed uploads</p>
          <UCard>
            <ul class="divide-y divide-default">
              <li
                v-for="(e, i) in result.errors"
                :key="i"
                class="flex items-center justify-between py-2 px-1 gap-3"
              >
                <div class="flex items-center gap-2 min-w-0">
                  <UIcon name="i-lucide-x-circle" class="size-4 text-error shrink-0" />
                  <span class="text-sm truncate">{{ e.filename }}</span>
                </div>
                <span class="text-xs text-error shrink-0">{{ e.error }}</span>
              </li>
            </ul>
          </UCard>
        </div>
      </div>
    </template>
  </UContainer>
</template>
