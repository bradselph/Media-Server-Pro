<script setup lang="ts">
import type { User, UserSession } from '~/types/api'

const adminApi = useAdminApi()
const toast = useToast()

const users = ref<User[]>([])
const loading = ref(true)
const search = ref('')
const createOpen = ref(false)
const editUser = ref<User | null>(null)
const deleteUser = ref<User | null>(null)
const deleting = ref(false)

// Sessions viewer
const sessionsUser = ref<User | null>(null)
const sessions = ref<UserSession[]>([])
const sessionsLoading = ref(false)

async function openSessions(user: User) {
  sessionsUser.value = user
  sessions.value = []
  sessionsLoading.value = true
  try {
    sessions.value = (await adminApi.getUserSessions(user.username)) ?? []
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed to load sessions', color: 'error', icon: 'i-lucide-x' })
  } finally {
    sessionsLoading.value = false
  }
}

// Create form
const createForm = reactive({ username: '', password: '', email: '', role: 'viewer' as 'admin' | 'viewer' })
const createLoading = ref(false)
const createError = ref('')

// Edit form
const editForm = reactive({
  role: 'viewer' as 'admin' | 'viewer',
  enabled: true,
  email: '',
  type: 'standard',
  newPassword: '',
  permissions: {
    can_stream: true,
    can_download: false,
    can_upload: false,
    can_delete: false,
    can_manage: false,
    can_view_mature: false,
    can_create_playlists: true,
  },
})

const USER_TYPE_OPTIONS = [
  { label: 'Standard', value: 'standard' },
  { label: 'Premium', value: 'premium' },
  { label: 'Trial', value: 'trial' },
  { label: 'Guest', value: 'guest' },
]
const editLoading = ref(false)
const editError = ref('')

const filtered = computed(() =>
  search.value
    ? users.value.filter(u => u.username.toLowerCase().includes(search.value.toLowerCase()) || u.email?.toLowerCase().includes(search.value.toLowerCase()))
    : users.value,
)

// Bulk actions
const selectedUsernames = ref<string[]>([])
const bulkLoading = ref(false)

function toggleSelectAll() {
  if (selectedUsernames.value.length === filtered.value.length) {
    selectedUsernames.value = []
  } else {
    selectedUsernames.value = filtered.value.map(u => u.username)
  }
}

function toggleSelectUser(username: string) {
  const idx = selectedUsernames.value.indexOf(username)
  if (idx === -1) selectedUsernames.value.push(username)
  else selectedUsernames.value.splice(idx, 1)
}

async function bulkAction(action: 'delete' | 'enable' | 'disable') {
  if (selectedUsernames.value.length === 0) return
  bulkLoading.value = true
  try {
    const res = await adminApi.bulkUsers(selectedUsernames.value, action)
    toast.add({ title: `${action}: ${res.success} succeeded, ${res.failed} failed`, color: res.failed > 0 ? 'warning' : 'success', icon: 'i-lucide-check' })
    selectedUsernames.value = []
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Bulk action failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    bulkLoading.value = false
  }
}

async function load() {
  loading.value = true
  try { users.value = (await adminApi.listUsers()) ?? [] }
  catch (e: unknown) { toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' }) }
  finally { loading.value = false }
}

async function handleCreate() {
  createLoading.value = true
  createError.value = ''
  try {
    await adminApi.createUser(createForm)
    toast.add({ title: `User ${createForm.username} created`, color: 'success', icon: 'i-lucide-check' })
    createOpen.value = false
    Object.assign(createForm, { username: '', password: '', email: '', role: 'viewer' })
    await load()
  } catch (e: unknown) {
    createError.value = e instanceof Error ? e.message : 'Failed to create user'
  } finally {
    createLoading.value = false
  }
}

async function openEdit(user: User) {
  // Fetch fresh user data before populating the form so edits are based on
  // current server state, not a potentially stale list entry.
  const fresh = await adminApi.getUser(user.username).catch(() => user)
  editUser.value = fresh
  Object.assign(editForm, {
    role: fresh.role,
    enabled: fresh.enabled,
    email: fresh.email ?? '',
    type: fresh.type || 'standard',
    newPassword: '',
    permissions: {
      can_stream: fresh.permissions?.can_stream ?? true,
      can_download: fresh.permissions?.can_download ?? false,
      can_upload: fresh.permissions?.can_upload ?? false,
      can_delete: fresh.permissions?.can_delete ?? false,
      can_manage: fresh.permissions?.can_manage ?? false,
      can_view_mature: fresh.permissions?.can_view_mature ?? false,
      can_create_playlists: fresh.permissions?.can_create_playlists ?? true,
    },
  })
  editError.value = ''
}

async function handleSave() {
  if (!editUser.value) return
  editLoading.value = true
  editError.value = ''
  try {
    await adminApi.updateUser(editUser.value.username, {
      role: editForm.role,
      enabled: editForm.enabled,
      email: editForm.email || undefined,
      type: editForm.type,
      permissions: { ...editForm.permissions },
    })
  } catch (e: unknown) {
    editError.value = e instanceof Error ? e.message : 'Failed to update user profile'
    editLoading.value = false
    return
  }
  // Profile saved — attempt password change separately so a failure here doesn't
  // give a misleading "Failed to update user" message when the profile was saved.
  if (editForm.newPassword) {
    try {
      await adminApi.changeUserPassword(editUser.value.username, editForm.newPassword)
    } catch (e: unknown) {
      editError.value = (e instanceof Error ? e.message : 'Password change failed') +
        ' (profile changes were saved)'
      editLoading.value = false
      return
    }
  }
  toast.add({ title: 'User updated', color: 'success', icon: 'i-lucide-check' })
  editUser.value = null
  editLoading.value = false
  await load()
}

async function handleDelete() {
  if (!deleteUser.value) return
  deleting.value = true
  try {
    await adminApi.deleteUser(deleteUser.value.username)
    toast.add({ title: `Deleted ${deleteUser.value.username}`, color: 'success', icon: 'i-lucide-check' })
    deleteUser.value = null
    await load()
  } catch (e: unknown) {
    toast.add({ title: e instanceof Error ? e.message : 'Failed', color: 'error', icon: 'i-lucide-x' })
  } finally {
    deleting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Header -->
    <div class="flex items-center justify-between gap-3 flex-wrap">
      <div class="flex items-center gap-2 flex-wrap">
        <UInput v-model="search" icon="i-lucide-search" placeholder="Search users…" class="w-64" />
        <template v-if="selectedUsernames.length > 0">
          <span class="text-sm text-muted">{{ selectedUsernames.length }} selected</span>
          <UButton :loading="bulkLoading" icon="i-lucide-toggle-right" label="Enable" size="sm" variant="outline" color="success" @click="bulkAction('enable')" />
          <UButton :loading="bulkLoading" icon="i-lucide-toggle-left" label="Disable" size="sm" variant="outline" color="neutral" @click="bulkAction('disable')" />
          <UButton :loading="bulkLoading" icon="i-lucide-trash-2" label="Delete" size="sm" variant="outline" color="error" @click="bulkAction('delete')" />
        </template>
      </div>
      <UButton icon="i-lucide-user-plus" label="Create User" @click="createOpen = true" />
    </div>

    <!-- Table -->
    <UCard>
      <div v-if="loading" class="flex justify-center py-8">
        <UIcon name="i-lucide-loader-2" class="animate-spin size-6 text-primary" />
      </div>
      <UTable
        v-else
        :data="filtered"
        :columns="[
          { accessorKey: 'select', header: '' },
          { accessorKey: 'username', header: 'Username' },
          { accessorKey: 'email', header: 'Email' },
          { accessorKey: 'role', header: 'Role' },
          { accessorKey: 'enabled', header: 'Status' },
          { accessorKey: 'created_at', header: 'Created' },
          { accessorKey: 'actions', header: '' },
        ]"
      >
        <template #select-header>
          <UCheckbox
            :model-value="filtered.length > 0 && selectedUsernames.length === filtered.length"
            :indeterminate="selectedUsernames.length > 0 && selectedUsernames.length < filtered.length"
            @update:model-value="toggleSelectAll"
          />
        </template>
        <template #select-cell="{ row }">
          <UCheckbox
            :model-value="selectedUsernames.includes(row.original.username)"
            @update:model-value="toggleSelectUser(row.original.username)"
          />
        </template>
        <template #username-cell="{ row }">
          <span class="font-medium">{{ row.original.username }}</span>
        </template>
        <template #email-cell="{ row }">
          <span class="text-muted text-sm">{{ row.original.email || '—' }}</span>
        </template>
        <template #role-cell="{ row }">
          <UBadge
            :label="row.original.role"
            :color="row.original.role === 'admin' ? 'warning' : 'neutral'"
            variant="subtle"
            size="xs"
          />
        </template>
        <template #enabled-cell="{ row }">
          <UBadge
            :label="row.original.enabled ? 'Active' : 'Disabled'"
            :color="row.original.enabled ? 'success' : 'error'"
            variant="subtle"
            size="xs"
          />
        </template>
        <template #created_at-cell="{ row }">
          <span class="text-sm text-muted">
            {{ row.original.created_at ? new Date(row.original.created_at).toLocaleDateString() : '—' }}
          </span>
        </template>
        <template #actions-cell="{ row }">
          <div class="flex items-center gap-1 justify-end">
            <UButton icon="i-lucide-monitor" aria-label="View sessions" size="xs" variant="ghost" color="neutral" @click="openSessions(row.original)" />
            <UButton icon="i-lucide-pencil" aria-label="Edit user" size="xs" variant="ghost" color="neutral" @click="openEdit(row.original)" />
            <UButton icon="i-lucide-trash-2" aria-label="Delete user" size="xs" variant="ghost" color="error" @click="deleteUser = row.original" />
          </div>
        </template>
      </UTable>
      <p v-if="!loading && filtered.length === 0" class="text-center py-6 text-muted text-sm">
        No users found.
      </p>
    </UCard>

    <!-- Create user modal -->
    <UModal v-model:open="createOpen" title="Create User" description="Add a new user account">
      <template #body>
        <div v-if="createError" class="mb-3">
          <UAlert :title="createError" color="error" variant="soft" icon="i-lucide-x-circle" />
        </div>
        <form class="space-y-4" @submit.prevent="handleCreate">
          <UFormField label="Username" required>
            <UInput v-model="createForm.username" placeholder="username" required />
          </UFormField>
          <UFormField label="Password" required>
            <UInput v-model="createForm.password" type="password" placeholder="••••••••" required minlength="8" />
          </UFormField>
          <UFormField label="Email">
            <UInput v-model="createForm.email" type="email" placeholder="user@example.com" />
          </UFormField>
          <UFormField label="Role">
            <USelect
              v-model="createForm.role"
              :items="[{ label: 'Viewer', value: 'viewer' }, { label: 'Admin', value: 'admin' }]"
            />
          </UFormField>
        </form>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="createOpen = false" />
        <UButton :loading="createLoading" label="Create User" @click="handleCreate" />
      </template>
    </UModal>

    <!-- Edit user modal -->
    <UModal v-if="editUser" :open="!!editUser" title="Edit User" @update:open="val => { if (!val) editUser = null }">
      <template #body>
        <div v-if="editError" class="mb-3">
          <UAlert :title="editError" color="error" variant="soft" icon="i-lucide-x-circle" />
        </div>
        <div class="space-y-4">
          <p class="text-muted text-sm">Editing: <strong class="text-default">{{ editUser.username }}</strong></p>
          <UFormField label="Email">
            <UInput v-model="editForm.email" type="email" />
          </UFormField>
          <UFormField label="Role">
            <USelect
              v-model="editForm.role"
              :items="[{ label: 'Viewer', value: 'viewer' }, { label: 'Admin', value: 'admin' }]"
            />
          </UFormField>
          <UFormField label="User Type" description="Affects feature access and display grouping">
            <USelect v-model="editForm.type" :items="USER_TYPE_OPTIONS" />
          </UFormField>
          <UFormField label="Status">
            <div class="flex items-center gap-2">
              <USwitch v-model="editForm.enabled" />
              <span class="text-sm">{{ editForm.enabled ? 'Active' : 'Disabled' }}</span>
            </div>
          </UFormField>
          <UFormField label="Permissions">
            <div class="grid grid-cols-2 gap-2 mt-1">
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_stream" />
                Stream media
              </label>
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_download" />
                Download
              </label>
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_upload" />
                Upload
              </label>
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_delete" />
                Delete media
              </label>
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_manage" />
                Manage library
              </label>
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_view_mature" />
                View mature
              </label>
              <label class="flex items-center gap-2 cursor-pointer text-sm">
                <UCheckbox v-model="editForm.permissions.can_create_playlists" />
                Create playlists
              </label>
            </div>
          </UFormField>
          <UFormField label="New Password" description="Leave blank to keep current password">
            <UInput v-model="editForm.newPassword" type="password" placeholder="••••••••" minlength="8" />
          </UFormField>
        </div>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="editUser = null" />
        <UButton :loading="editLoading" label="Save Changes" @click="handleSave" />
      </template>
    </UModal>

    <!-- Sessions viewer -->
    <UModal v-if="sessionsUser" :open="!!sessionsUser" title="Active Sessions" @update:open="val => { if (!val) sessionsUser = null }">
      <template #description>
        Sessions for <strong>{{ sessionsUser?.username }}</strong>
      </template>
      <template #body>
        <div v-if="sessionsLoading" class="flex justify-center py-6">
          <UIcon name="i-lucide-loader-2" class="animate-spin size-5" />
        </div>
        <div v-else-if="sessions.length === 0" class="text-center py-4 text-muted text-sm">
          No active sessions.
        </div>
        <ul v-else class="divide-y divide-default">
          <li v-for="s in sessions" :key="s.id" class="py-3 space-y-0.5 text-sm">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="font-mono text-xs text-muted">{{ s.id.slice(0, 12) }}…</span>
              <span class="text-muted text-xs">{{ s.ip_address }}</span>
            </div>
            <div class="text-xs text-muted">{{ s.user_agent }}</div>
            <div class="text-xs text-muted flex gap-4 flex-wrap">
              <span>Created: {{ s.created_at ? new Date(s.created_at).toLocaleString() : '—' }}</span>
              <span>Expires: {{ s.expires_at ? new Date(s.expires_at).toLocaleString() : '—' }}</span>
              <span v-if="s.last_activity">Last active: {{ new Date(s.last_activity).toLocaleString() }}</span>
            </div>
          </li>
        </ul>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Close" @click="sessionsUser = null" />
      </template>
    </UModal>

    <!-- Delete confirmation -->
    <UModal v-if="deleteUser" :open="!!deleteUser" title="Delete User" @update:open="val => { if (!val) deleteUser = null }">
      <template #body>
        <p>Are you sure you want to delete <strong>{{ deleteUser?.username }}</strong>? This action cannot be undone.</p>
      </template>
      <template #footer>
        <UButton variant="ghost" color="neutral" label="Cancel" @click="deleteUser = null" />
        <UButton :loading="deleting" color="error" label="Delete" @click="handleDelete" />
      </template>
    </UModal>
  </div>
</template>
