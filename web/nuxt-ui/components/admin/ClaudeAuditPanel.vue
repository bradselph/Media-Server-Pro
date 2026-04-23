<script setup lang="ts">
import type { AuditLogEntry } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const entries = ref<AuditLogEntry[]>([])
const loading = ref(false)
const offset = ref(0)
const PAGE = 100

let mounted = true
onUnmounted(() => { mounted = false })

const filtered = computed(() =>
  entries.value.filter(e => e.action.startsWith('claude.'))
)

async function load(reset = false) {
  if (reset) offset.value = 0
  loading.value = true
  try {
    const data = await adminApi.getAuditLog({ offset: offset.value, limit: PAGE })
    if (!mounted) return
    if (reset) {
      entries.value = data ?? []
    } else {
      entries.value = [...entries.value, ...(data ?? [])]
    }
  } catch (e: unknown) {
    if (!mounted) return
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load audit log', color: 'error', icon: 'i-lucide-x' })
  } finally {
    if (mounted) loading.value = false
  }
}

function loadMore() {
  offset.value += PAGE
  load()
}

function actionColor(action: string): 'info' | 'success' | 'error' | 'warning' {
  if (action === 'claude.kill_switch') return 'warning'
  if (action.startsWith('claude.tool.')) return 'info'
  if (action.startsWith('claude.conversation.')) return 'neutral' as 'info'
  return 'info'
}

function shortAction(action: string): string {
  return action.replace('claude.', '')
}

function fmtTime(ts: string): string {
  return new Date(ts).toLocaleString()
}

function formatDetails(details?: Record<string, unknown>): string {
  if (!details) return ''
  const parts: string[] = []
  if (details.tool) parts.push(`tool: ${details.tool}`)
  if (details.input) parts.push(`input: ${String(details.input).slice(0, 120)}`)
  if (details.output_preview) parts.push(`output: ${String(details.output_preview).slice(0, 200)}`)
  if (details.error) parts.push(`error: ${details.error}`)
  if (details.output_size !== undefined) parts.push(`output_size: ${details.output_size}`)
  return parts.join('\n')
}

onMounted(() => load(true))
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-2">
        <UIcon name="i-lucide-scroll-text" class="size-4 text-primary" />
        <span class="font-semibold">Claude Audit Log</span>
        <UBadge v-if="filtered.length" color="neutral" variant="subtle">{{ filtered.length }}</UBadge>
      </div>
      <UButton size="sm" variant="outline" icon="i-lucide-refresh-cw" label="Refresh" :loading="loading" @click="load(true)" />
    </div>

    <div v-if="loading && !entries.length" class="flex justify-center py-12">
      <UIcon name="i-lucide-loader-2" class="animate-spin size-8 text-primary" />
    </div>

    <div v-else-if="!filtered.length" class="text-center py-12 text-muted">
      <UIcon name="i-lucide-scroll-text" class="size-10 mb-3 mx-auto opacity-40" />
      <p>No Claude activity recorded yet.</p>
    </div>

    <div v-else class="space-y-2">
      <div
        v-for="entry in filtered"
        :key="entry.id"
        class="rounded-lg border border-default p-3 space-y-1.5"
        :class="!entry.success ? 'border-l-4 border-l-red-400' : ''"
      >
        <div class="flex items-center flex-wrap gap-2 text-xs">
          <UBadge :color="actionColor(entry.action)" variant="subtle" size="xs">
            {{ shortAction(entry.action) }}
          </UBadge>
          <UBadge v-if="!entry.success" color="error" variant="subtle" size="xs" icon="i-lucide-x">
            failed
          </UBadge>
          <span class="text-muted">{{ fmtTime(entry.timestamp) }}</span>
          <span v-if="entry.username" class="text-muted">by {{ entry.username }}</span>
          <span v-if="entry.ip_address" class="text-muted">from {{ entry.ip_address }}</span>
          <span v-if="entry.resource && entry.resource !== entry.action" class="font-mono text-muted">
            {{ entry.resource }}
          </span>
        </div>

        <pre
          v-if="formatDetails(entry.details)"
          class="text-xs bg-elevated rounded p-2 overflow-x-auto whitespace-pre-wrap max-h-40"
        >{{ formatDetails(entry.details) }}</pre>
      </div>

      <div class="flex justify-center pt-2">
        <UButton
          size="sm"
          variant="ghost"
          color="neutral"
          :loading="loading"
          label="Load more"
          @click="loadMore"
        />
      </div>
    </div>
  </div>
</template>
