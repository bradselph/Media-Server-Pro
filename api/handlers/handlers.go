// Package handlers provides HTTP request handlers for the API.
// This file is intentionally empty. All handlers have been split into
// domain-specific files:
//   - handler.go      - Handler struct, HandlerDeps, NewHandler, helper functions
//   - media.go        - Media handlers
//   - auth.go         - Authentication handlers
//   - hls.go          - HLS streaming handlers
//   - playlists.go    - Playlist handlers
//   - analytics.go    - Analytics handlers
//   - suggestions.go  - Suggestions handlers
//   - upload.go       - Upload handlers
//   - thumbnails.go   - Thumbnail handlers
//   - admin.go        - Admin handlers
//   - admin_media.go  - Admin media handlers
//   - admin_hls.go    - Admin HLS handlers (thin, delegates to hls.go)
//   - admin_scanner.go       - Admin scanner handlers
//   - admin_security.go      - Admin security handlers
//   - admin_remote.go        - Admin remote media handlers
//   - admin_backups.go       - Admin backup handlers
//   - admin_discovery.go     - Admin discovery handlers
//   - admin_categorizer.go   - Admin categorizer handlers
//   - admin_playlists.go     - Admin playlist handlers
//   - admin_thumbnails.go    - Admin thumbnail handlers (delegates to thumbnails.go)
//   - admin_validator.go     - Admin validator handlers
//   - system.go              - System handlers (health, metrics, storage, database)
package handlers
