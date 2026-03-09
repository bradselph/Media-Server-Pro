# Database Table Usage Audit

This document maps each database table to its repository and usage. All tables are created in `internal/database/migrations.go` and are used by at least one module.

## Table → Repository → Module

| Table(s) | Repository | Module | Usage |
|----------|------------|--------|--------|
| `users` | UserRepository | auth | Create, GetByID, GetByUsername, Update, Delete, List |
| `user_permissions` | UserPermissionsRepository (via UserRepository) | auth | Loaded in GetByID, GetByUsername, List; updated in Create/Update |
| `user_preferences` | UserPreferencesRepository (via UserRepository) | auth | Loaded in GetByID, GetByUsername, List; updated in Create/Update; API: GetPreferences, UpdatePreferences |
| `sessions` | SessionRepository | auth | Create, Get, Delete, DeleteExpired, List, ListByUser |
| `media_metadata` | MediaMetadataRepository | media | Upsert, Get, Delete, List, ListFiltered, IncrementViews, UpdatePlaybackPosition, GetPlaybackPosition |
| `media_tags` | (part of MediaMetadataRepository) | media | Upsert/delete in Upsert; loaded in Get, List, ListFiltered |
| `playback_positions` | (part of MediaMetadataRepository) | media | UpdatePlaybackPosition, GetPlaybackPosition |
| `playlists` | PlaylistRepository | playlist | Create, Get, Update, Delete, ListByUser, ListAll |
| `playlist_items` | (part of PlaylistRepository) | playlist | AddItem, RemoveItem, UpdateItem, GetItems |
| `analytics_events` | AnalyticsRepository | analytics | Create, List, GetByMediaID, GetByUserID, DeleteOlderThan, Count, CountByType |
| `audit_log` | AuditLogRepository | admin | Create, List, GetByUser, DeleteOlderThan; API: AdminGetAuditLog, AdminExportAuditLog |
| `scan_results` | ScanResultRepository | scanner | Save, Get, GetPendingReview, MarkReviewed |
| `scan_reasons` | (part of ScanResultRepository) | scanner | Save/delete in Save; loaded in Get, GetPendingReview |
| `categorized_items` | CategorizedItemRepository | categorizer | Upsert, Get, Delete, List; API: CategorizeFile, CategorizeDirectory, GetByCategory, SetMediaCategory, CleanStale |
| `hls_jobs` | HLSJobRepository | hls | Save, Get, Delete, List |
| `validation_results` | ValidationResultRepository | validator | Upsert, Get, Delete, List |
| `suggestion_profiles` | SuggestionProfileRepository | suggestions | SaveProfile, GetProfile, DeleteProfile, ListProfiles |
| `suggestion_view_history` | (part of SuggestionProfileRepository) | suggestions | SaveViewHistory, GetViewHistory, DeleteViewHistory |
| `autodiscovery_suggestions` | AutoDiscoverySuggestionRepository | autodiscovery | Save, Get, List, Delete, DeleteAll; API: discovery scan, apply, clear |
| `ip_list_config` | IPListRepository | security | SaveListConfig, GetListConfig, SaveEntries, GetEntries, SetEnabled |
| `ip_list_entries` | (part of IPListRepository) | security | AddEntry, RemoveEntry, GetEntries |
| `remote_cache_entries` | RemoteCacheRepository | remote | Save, Get, Delete, List |
| `backup_manifests` | BackupManifestRepository | backup | Save, Get, Delete, List |
| `receiver_slaves` | ReceiverSlaveRepository | receiver | Upsert, Get, Delete, List |
| `receiver_media` | ReceiverMediaRepository | receiver, duplicates | UpsertBatch, Get, ListBySlave, ListAll, DeleteBySlave, DeleteByID, Search |
| `receiver_duplicates` | ReceiverDuplicateRepository | duplicates | Create, Get, List, ListPending, ExistsByPair, UpdateStatus, DeleteForItem, DeleteBySlave, etc. |
| `extractor_items` | ExtractorItemRepository | extractor | Upsert, Get, Delete, List, ListActive, UpdateStatus |
| `crawler_targets` | CrawlerTargetRepository | crawler | Upsert, Get, Delete, List, UpdateLastCrawled |
| `crawler_discoveries` | CrawlerDiscoveryRepository | crawler | Create, Get, Delete, List, ListByTarget, ListPending, UpdateStatus, ExistsByStreamURL |

## Gaps / Notes

1. **MediaMetadataRepository.ListFiltered** — Used by the media module’s `ListMediaPaginated`. The admin media list (`AdminListMedia`) calls `ListMediaPaginated` when `limit > 0`, so DB-level filtering and pagination are used for the admin catalog. Type and Tags are still applied in-memory on the returned page. Public list continues to use `ListMedia` (full in-memory) because it merges receiver and extractor items in the handler.

2. **UserRepository** uses sub-repositories for `user_permissions` and `user_preferences`; those tables are written via the same transaction in Create/Update (using raw GORM on the transaction) and read via `permsRepo.Get` / `prefsRepo.Get` in GetByID, GetByUsername, and List. So all three tables (users, user_permissions, user_preferences) are consistently referenced.

3. **PlaylistRepository** uses `playlists` and `playlist_items`; both are referenced for create/read/update/delete flows.

## Conclusion

- **All tables are used** by a repository and by at least one module or API.
- **One underused capability**: `ListFiltered` on media metadata is implemented but never called; consider using it for admin (or public) media list when pagination and DB-level filter (category, is_mature, search) are sufficient.
