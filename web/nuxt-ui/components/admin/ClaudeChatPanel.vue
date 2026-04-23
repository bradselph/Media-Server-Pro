<script setup lang="ts">
import type { ClaudeConversation, ClaudeMessage, ClaudeEvent, ClaudeToolCall, ClaudeChatRequest } from '~/composables/useApiEndpoints'

const adminApi = useAdminApi()
const toast = useToast()

// ── Conversations sidebar ──────────────────────────────────────────────────
const conversations = ref<ClaudeConversation[]>([])
const activeConvId = ref<string | null>(null)
const convsLoading = ref(false)

async function loadConversations() {
  convsLoading.value = true
  try {
    conversations.value = await adminApi.listClaudeConversations(50)
  } catch {
    // Non-fatal; sidebar just stays empty
  } finally {
    convsLoading.value = false
  }
}

async function openConversation(id: string) {
  if (streaming.value) return
  activeConvId.value = id
  chatMessages.value = []
  pendingToolIds.value = []
  try {
    const data = await adminApi.getClaudeConversation(id)
    // Reconstruct display messages from stored history
    chatMessages.value = data.messages.map(m => storedMessageToDisplay(m))
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load conversation', color: 'error', icon: 'i-lucide-x' })
  }
}

async function deleteConversation(id: string) {
  try {
    await adminApi.deleteClaudeConversation(id)
    conversations.value = conversations.value.filter(c => c.id !== id)
    if (activeConvId.value === id) {
      activeConvId.value = null
      chatMessages.value = []
      pendingToolIds.value = []
    }
    toast.add({ title: 'Conversation deleted', color: 'success', icon: 'i-lucide-check' })
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to delete', color: 'error', icon: 'i-lucide-x' })
  }
}

function newConversation() {
  if (streaming.value) return
  activeConvId.value = null
  chatMessages.value = []
  pendingToolIds.value = []
}

// ── Display message model ──────────────────────────────────────────────────
type DisplayRole = 'user' | 'assistant' | 'tool_call' | 'tool_result' | 'tool_pending' | 'error' | 'info'

interface DisplayMessage {
  id: string
  role: DisplayRole
  text: string
  toolCall?: ClaudeToolCall
  timestamp: Date
}

const chatMessages = ref<DisplayMessage[]>([])
let msgSeq = 0
function newMsg(role: DisplayRole, text = '', toolCall?: ClaudeToolCall): DisplayMessage {
  return { id: String(++msgSeq), role, text, toolCall, timestamp: new Date() }
}

function storedMessageToDisplay(m: ClaudeMessage): DisplayMessage {
  if (m.role === 'tool') {
    const tc = m.tool_result
    if (tc?.requires_confirm) return newMsg('tool_pending', '', tc)
    if (tc?.error) return newMsg('tool_result', '', tc)
    return newMsg('tool_result', '', tc)
  }
  return newMsg(m.role as DisplayRole, m.content)
}

// ── Streaming chat ─────────────────────────────────────────────────────────
const inputText = ref('')
const streaming = ref(false)
const modeOverride = ref('')
const pendingToolIds = ref<string[]>([])

const MODES = [
  { label: 'Default mode', value: '' },
  { label: 'Advisory', value: 'advisory' },
  { label: 'Interactive', value: 'interactive' },
  { label: 'Autonomous', value: 'autonomous' },
]

const chatAbortCtrl = new AbortController()
onBeforeUnmount(() => chatAbortCtrl.abort())

const messagesEl = ref<HTMLElement | null>(null)
function scrollBottom() {
  nextTick(() => {
    if (messagesEl.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight
    }
  })
}

async function sendMessage(approved?: string[]) {
  const text = approved ? '(continuing with approved tool calls)' : inputText.value.trim()
  if (!text && !approved?.length) return
  if (streaming.value) return

  if (!approved) {
    chatMessages.value.push(newMsg('user', inputText.value.trim()))
    inputText.value = ''
    scrollBottom()
  }

  streaming.value = true
  pendingToolIds.value = []

  // Track the current assistant message being built
  let assistantMsgId: string | null = null

  try {
    const req: ClaudeChatRequest = {
      message: approved ? '' : text,
      conversation_id: activeConvId.value ?? undefined,
      mode_override: modeOverride.value || undefined,
      approved_tool_calls: approved,
    }

    // For approval re-submissions, message can't be empty — send a placeholder
    if (approved) req.message = '(approving pending tool calls)'

    const response = await fetch('/api/admin/claude/chat', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(req),
      signal: chatAbortCtrl.signal,
    })

    if (!response.ok) {
      const errText = await response.text()
      chatMessages.value.push(newMsg('error', `Request failed: ${response.status} ${errText}`))
      return
    }

    if (!response.body) {
      chatMessages.value.push(newMsg('error', 'No response body'))
      return
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts[parts.length - 1]
        for (const part of parts.slice(0, -1)) {
          if (!part.startsWith('data: ')) continue
          try {
            const ev: ClaudeEvent = JSON.parse(part.slice(6))
            handleEvent(ev)
          } catch { /* malformed event — skip */ }
        }
      }
    } finally {
      reader.releaseLock()
    }
  } catch (e: unknown) {
    chatMessages.value.push(newMsg('error', e instanceof Error ? e.message : 'Stream error'))
  } finally {
    streaming.value = false
    assistantMsgId = null
    await loadConversations()
    scrollBottom()
  }

  function handleEvent(ev: ClaudeEvent) {
    switch (ev.type) {
      case 'info':
        if (ev.conversation_id && !activeConvId.value) {
          activeConvId.value = ev.conversation_id
        }
        break

      case 'delta':
        if (ev.text) {
          if (!assistantMsgId) {
            const m = newMsg('assistant', ev.text)
            assistantMsgId = m.id
            chatMessages.value.push(m)
          } else {
            const idx = chatMessages.value.findIndex(m => m.id === assistantMsgId)
            if (idx !== -1) chatMessages.value[idx].text += ev.text
          }
          scrollBottom()
        }
        break

      case 'tool_call':
        if (ev.tool_call) {
          chatMessages.value.push(newMsg('tool_call', '', ev.tool_call))
          scrollBottom()
        }
        break

      case 'tool_result':
        if (ev.tool_call) {
          chatMessages.value.push(newMsg('tool_result', '', ev.tool_call))
          scrollBottom()
        }
        break

      case 'tool_pending':
        if (ev.tool_call) {
          chatMessages.value.push(newMsg('tool_pending', '', ev.tool_call))
          pendingToolIds.value.push(ev.tool_call.id)
          scrollBottom()
        }
        break

      case 'error':
        chatMessages.value.push(newMsg('error', ev.error ?? 'Unknown error'))
        scrollBottom()
        break

      case 'final':
        // Turn complete — nothing special needed; streaming=false in finally
        break
    }
  }
}

async function approveAll() {
  const ids = [...pendingToolIds.value]
  await sendMessage(ids)
  // Clear state only after sendMessage resolves (success or failure handled inside sendMessage)
  pendingToolIds.value = []
  chatMessages.value = chatMessages.value.filter(m => m.role !== 'tool_pending')
}

function rejectAll() {
  pendingToolIds.value = []
  chatMessages.value = chatMessages.value.filter(m => m.role !== 'tool_pending')
  chatMessages.value.push(newMsg('info', 'Pending tool calls rejected.'))
}

function handleKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    sendMessage()
  }
}

onMounted(loadConversations)
</script>

<template>
  <div class="flex gap-4 h-[70vh] min-h-96">
    <!-- Conversations sidebar -->
    <div class="hidden lg:flex flex-col w-48 shrink-0 gap-1">
      <div class="flex items-center justify-between mb-1">
        <span class="text-xs font-semibold text-muted uppercase tracking-wide">Conversations</span>
        <UButton size="xs" icon="i-lucide-plus" variant="ghost" color="neutral" title="New conversation" @click="newConversation" />
      </div>

      <div v-if="convsLoading" class="flex justify-center py-4">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-4 text-muted" />
      </div>

      <div v-else-if="!conversations.length" class="text-xs text-muted text-center py-4">
        No conversations yet
      </div>

      <div
        v-for="conv in conversations"
        :key="conv.id"
        class="group flex items-center gap-1 px-2 py-1.5 rounded-lg cursor-pointer text-xs transition-colors"
        :class="activeConvId === conv.id
          ? 'bg-primary text-white'
          : 'text-muted hover:bg-elevated hover:text-highlighted'"
        @click="openConversation(conv.id)"
      >
        <span class="truncate flex-1">{{ conv.title || 'Untitled' }}</span>
        <UButton
          size="xs"
          icon="i-lucide-trash-2"
          variant="ghost"
          :color="activeConvId === conv.id ? 'neutral' : 'neutral'"
          class="opacity-0 group-hover:opacity-100 shrink-0"
          @click.stop="deleteConversation(conv.id)"
        />
      </div>

      <UButton
        size="xs"
        icon="i-lucide-plus"
        label="New Chat"
        variant="outline"
        color="neutral"
        class="mt-auto"
        @click="newConversation"
      />
    </div>

    <!-- Chat area -->
    <div class="flex flex-col flex-1 min-w-0">
      <!-- Messages -->
      <div
        ref="messagesEl"
        class="flex-1 overflow-y-auto space-y-3 p-3 rounded-lg bg-elevated border border-default"
      >
        <div v-if="!chatMessages.length" class="flex flex-col items-center justify-center h-full text-muted gap-3">
          <UIcon name="i-lucide-brain" class="size-10 opacity-30" />
          <p class="text-sm">Start a conversation with Claude.</p>
          <p class="text-xs opacity-60">Claude has direct access to your server's logs, config, and tools.</p>
        </div>

        <template v-for="msg in chatMessages" :key="msg.id">
          <!-- User message -->
          <div v-if="msg.role === 'user'" class="flex justify-end">
            <div class="max-w-[80%] bg-primary text-white px-3 py-2 rounded-xl rounded-br-sm text-sm whitespace-pre-wrap">
              {{ msg.text }}
            </div>
          </div>

          <!-- Assistant message -->
          <div v-else-if="msg.role === 'assistant'" class="flex justify-start">
            <div class="max-w-[85%]">
              <div class="flex items-center gap-1.5 mb-1">
                <UIcon name="i-lucide-brain" class="size-3 text-primary" />
                <span class="text-xs text-muted font-medium">Claude</span>
              </div>
              <div class="bg-neutral-100 dark:bg-neutral-800 px-3 py-2 rounded-xl rounded-bl-sm text-sm whitespace-pre-wrap">
                {{ msg.text }}
                <span v-if="streaming && msg === chatMessages[chatMessages.length - 1]" class="inline-block w-1.5 h-3.5 bg-primary animate-pulse ml-0.5 align-text-bottom" />
              </div>
            </div>
          </div>

          <!-- Tool call card -->
          <div v-else-if="msg.role === 'tool_call'" class="mx-auto max-w-[90%]">
            <UCard :ui="{ body: 'p-3' }" class="border-l-4 border-l-blue-400">
              <div class="flex items-center gap-2 text-xs">
                <UIcon name="i-lucide-wrench" class="size-3.5 text-blue-400 shrink-0" />
                <span class="font-mono font-semibold text-blue-600 dark:text-blue-400">{{ msg.toolCall?.name }}</span>
                <UBadge size="xs" color="info" variant="subtle">calling</UBadge>
              </div>
              <details v-if="msg.toolCall?.input" class="mt-1">
                <summary class="text-xs text-muted cursor-pointer select-none">Input</summary>
                <pre class="mt-1 text-xs bg-elevated rounded p-2 overflow-x-auto">{{ JSON.stringify(msg.toolCall.input, null, 2) }}</pre>
              </details>
            </UCard>
          </div>

          <!-- Tool result card -->
          <div v-else-if="msg.role === 'tool_result'" class="mx-auto max-w-[90%]">
            <UCard :ui="{ body: 'p-3' }" :class="msg.toolCall?.error ? 'border-l-4 border-l-red-400' : 'border-l-4 border-l-green-400'">
              <div class="flex items-center gap-2 text-xs">
                <UIcon
                  :name="msg.toolCall?.error ? 'i-lucide-x-circle' : 'i-lucide-check-circle'"
                  :class="msg.toolCall?.error ? 'text-red-400' : 'text-green-400'"
                  class="size-3.5 shrink-0"
                />
                <span class="font-mono font-semibold">{{ msg.toolCall?.name }}</span>
                <UBadge size="xs" :color="msg.toolCall?.error ? 'error' : 'success'" variant="subtle">
                  {{ msg.toolCall?.error ? 'error' : 'ok' }}
                </UBadge>
              </div>
              <details v-if="msg.toolCall?.output || msg.toolCall?.error" class="mt-1">
                <summary class="text-xs text-muted cursor-pointer select-none">Output</summary>
                <pre class="mt-1 text-xs bg-elevated rounded p-2 overflow-x-auto max-h-40">{{ msg.toolCall?.error || msg.toolCall?.output }}</pre>
              </details>
            </UCard>
          </div>

          <!-- Tool pending (approval required) -->
          <div v-else-if="msg.role === 'tool_pending'" class="mx-auto max-w-[90%]">
            <UCard :ui="{ body: 'p-3' }" class="border-l-4 border-l-yellow-400 border border-yellow-200 dark:border-yellow-800">
              <div class="flex items-center gap-2 text-xs mb-1">
                <UIcon name="i-lucide-shield-alert" class="size-3.5 text-yellow-500 shrink-0" />
                <span class="font-mono font-semibold text-yellow-600 dark:text-yellow-400">{{ msg.toolCall?.name }}</span>
                <UBadge size="xs" color="warning" variant="subtle">awaiting approval</UBadge>
              </div>
              <details v-if="msg.toolCall?.input">
                <summary class="text-xs text-muted cursor-pointer select-none">Input to review</summary>
                <pre class="mt-1 text-xs bg-elevated rounded p-2 overflow-x-auto">{{ JSON.stringify(msg.toolCall.input, null, 2) }}</pre>
              </details>
            </UCard>
          </div>

          <!-- Error message -->
          <div v-else-if="msg.role === 'error'" class="mx-auto max-w-[90%]">
            <UAlert color="error" :description="msg.text" icon="i-lucide-alert-circle" />
          </div>

          <!-- Info message -->
          <div v-else-if="msg.role === 'info'" class="flex justify-center">
            <span class="text-xs text-muted italic">{{ msg.text }}</span>
          </div>
        </template>
      </div>

      <!-- Approval bar -->
      <div v-if="pendingToolIds.length && !streaming" class="mt-2 p-3 rounded-lg bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 flex items-center justify-between gap-3">
        <div class="flex items-center gap-2 text-sm">
          <UIcon name="i-lucide-shield-alert" class="size-4 text-yellow-500 shrink-0" />
          <span>{{ pendingToolIds.length }} tool call{{ pendingToolIds.length === 1 ? '' : 's' }} need{{ pendingToolIds.length === 1 ? 's' : '' }} approval</span>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <UButton size="xs" color="neutral" variant="outline" icon="i-lucide-x" label="Reject" @click="rejectAll" />
          <UButton size="xs" color="success" icon="i-lucide-check" label="Approve All" @click="approveAll" />
        </div>
      </div>

      <!-- Input bar -->
      <div class="mt-2 flex gap-2 items-end">
        <USelect
          v-model="modeOverride"
          :items="MODES"
          value-key="value"
          label-key="label"
          size="sm"
          class="w-36 shrink-0"
        />
        <UTextarea
          v-model="inputText"
          :disabled="streaming"
          placeholder="Ask Claude anything about your server..."
          :rows="2"
          class="flex-1 resize-none"
          @keydown="handleKeydown"
        />
        <UButton
          icon="i-lucide-send"
          :loading="streaming"
          :disabled="!inputText.trim() && !streaming"
          @click="sendMessage()"
        />
      </div>
      <p class="text-xs text-muted mt-1">Enter to send · Shift+Enter for newline</p>
    </div>
  </div>
</template>
