# Changelog

## [0.4.0] - 2026-02-28 (minor)

- Merge pull request #7 from bradselph/development
- Remove deprecated frontend types and consolidate AdminUser → User
- Fix authStore 401 detection to match ApiError class shape
- Add nil guards for optional admin/playlist modules to prevent panics
- Fix media-not-found on deploy, improve startup readiness, enrich watch history


## [0.3.0] - 2026-02-28 (minor)

- Merge pull request #6 from bradselph/development
- Use PlaylistItem type in addItem instead of removing the import
- Fix frontend build errors: mediaPath→mediaId, discovery type, unused import
- Wire module constructors to accept *database.Module for lazy repo init
- Merge pull request #5 from bradselph/development
- Merge branch 'main' into development
- Switch modules to DB-backed persistence
- Merge branch 'main' into development
- Add MySQL repositories, migrations and auth fixes
- Switch media APIs and UI to use IDs (not paths)
- Use media ID in APIs and hide internal paths
- Hide filesystem paths in APIs; SPA route refactor
- Security and robustness hardening across codebase
- Split auto-version into dev/release/sync workflows


## [0.2.0] - 2026-02-28 (minor)

- Merge pull request #5 from bradselph/development
- Merge branch 'main' into development
- Switch modules to DB-backed persistence
- Merge branch 'main' into development
- Add MySQL repositories, migrations and auth fixes
- Switch media APIs and UI to use IDs (not paths)
- Use media ID in APIs and hide internal paths
- Hide filesystem paths in APIs; SPA route refactor
- Security and robustness hardening across codebase
- Split auto-version into dev/release/sync workflows


## [0.2.0] - 2026-02-28 (minor)

- Generate dev build label instead of bumping patch version


## [0.1.0] - 2026-02-27 (minor)

- Split auto-version into dev/release/sync workflows