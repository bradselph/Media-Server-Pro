/**
 * API endpoint functions organized by domain.
 * Mirrors the React endpoints.ts — each function calls the typed API client
 * and returns strongly-typed data.
 *
 * This file starts with auth endpoints; additional domains will be added
 * as the migration progresses.
 */

import type {
  User,
  UserPermissions,
  UserPreferences,
  LoginResponse,
  SessionCheckResponse,
} from '~/types/api'

/**
 * Composable providing typed API endpoint functions.
 * Usage: const { login, logout, register, getSession } = useApiEndpoints()
 */
export function useApiEndpoints() {
  // ── Auth ──

  function login(username: string, password: string) {
    return api.post<LoginResponse>('/api/auth/login', { username, password })
  }

  function logout() {
    return api.post<void>('/api/auth/logout')
  }

  function register(username: string, password: string, email?: string) {
    return api.post<User>('/api/auth/register', { username, password, email })
  }

  function getSession() {
    return api.get<SessionCheckResponse>('/api/auth/session')
  }

  function changePassword(currentPassword: string, newPassword: string) {
    return api.post<void>('/api/auth/change-password', {
      current_password: currentPassword,
      new_password: newPassword,
    })
  }

  function deleteAccount(password: string) {
    return api.post<void>('/api/auth/delete-account', { password })
  }

  // ── Preferences ──

  function getPreferences() {
    return api.get<UserPreferences>('/api/preferences')
  }

  function updatePreferences(prefs: Partial<UserPreferences>) {
    return api.put<UserPreferences>('/api/preferences', prefs)
  }

  // ── Permissions ──

  function getPermissions() {
    return api.get<UserPermissions>('/api/permissions')
  }

  return {
    // Auth
    login,
    logout,
    register,
    getSession,
    changePassword,
    deleteAccount,
    // Preferences
    getPreferences,
    updatePreferences,
    // Permissions
    getPermissions,
  }
}
