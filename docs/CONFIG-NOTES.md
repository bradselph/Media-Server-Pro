# Configuration notes

## config.json vs .env
- **Server port**: Default in config.json is 8080 to match Go defaults, vite dev proxy, and deploy scripts.
- **Timeouts**: In config.json, duration fields (e.g. `read_timeout`) are in **nanoseconds** (Go `time.Duration`). Env overrides (e.g. `SERVER_READ_TIMEOUT`) are in **seconds** and are multiplied by `time.Second` when applied.
- **Admin auth**: `password_hash` in config.json may be empty. Set `ADMIN_PASSWORD` or `ADMIN_PASSWORD_HASH` in `.env` for admin login to work.
- **Database password**: Not stored in config.json. Use `DATABASE_PASSWORD` in `.env`.
- **Features**: Receiver, extractor, crawler, duplicate detection, and HuggingFace are controlled via `.env` / defaults; config.json does not list every feature flag.
- **Updater**: Set `UPDATER_GITHUB_TOKEN` (and optionally `UPDATER_APP_DIR`, etc.) in `.env` for the updater to fetch releases.
