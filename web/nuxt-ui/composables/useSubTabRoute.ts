/**
 * useSubTabRoute — seeds a sub-tab ref from the `?tab=` query at mount, mapping
 * known aliases to their canonical sub-tab and falling back to defaultTab.
 * Read-once (never writes back), shared by the admin shell-tab components.
 */
export function useSubTabRoute(aliasMap: Record<string, string>, defaultTab: string) {
    const route = useRoute()
    return ref(aliasMap[route.query.tab as string] ?? defaultTab)
}
