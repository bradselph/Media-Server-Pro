# CI Failures Log

## 2026-03-28

**Phase:** 0b — Frontend typecheck (nuxi typecheck)
**Error:**
```
pages/player.vue(560,14): error TS2322: Type 'boolean | undefined' is not assignable to type 'boolean'.
  Type 'undefined' is not assignable to type 'boolean'.
```
**Root cause:** `import.meta.client` is typed as `boolean | undefined` in Nuxt, causing the expression `import.meta.client && 'pictureInPictureEnabled' in document` to resolve to `boolean | undefined`. The `PlayerControls` prop `pipSupported` expects `boolean`.
**Status:** Fixed in same cycle (frontend scope, player.vue line 239).
