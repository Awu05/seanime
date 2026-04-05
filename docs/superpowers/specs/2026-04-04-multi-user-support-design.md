# Multi-User Support Design Spec

## Overview

Refactor seanime from a single-user application to support multiple household profiles (2-5 users) on a single instance. Each profile gets its own AniList account, watch progress, playback preferences, and concurrent streaming sessions. The app must work identically on Electron desktop and Docker/browser deployments.

## Auth Model

### Two-Layer Authentication

**Layer 1 — Instance Access:**

- **Admin:** Authenticates with username + password. Has full access to admin settings, profile management, and global configuration. The admin password is personal — not shared.
- **Household members:** Enter a shared instance access code (set by the admin) to reach the profile picker. The access code is a short passphrase — not a full account.
- JWT sessions: Admin gets a JWT on login. Household members get a limited JWT on access code entry that only grants profile selection.

**Layer 2 — Profile Selection:**

- After instance auth, users see a profile picker (Netflix-style).
- Each profile can optionally set a 4-6 digit PIN to prevent casual switching.
- Selected profile is encoded into the session JWT (`profileId`).
- All subsequent API requests carry the profile context.

**Platform Modes:**

The multi-user system must be compatible with all installation methods. The backend detects its platform via the `--desktop-sidecar` flag and `SEA_PUBLIC_PLATFORM` build env. Three modes exist:

| Mode | Platform Detection | Auth Behavior |
|------|-------------------|---------------|
| Electron Desktop | `--desktop-sidecar true` | Single-user by default. No login/profile picker unless user explicitly enables multi-user in Denshi settings. Implicit admin + default profile. Server auto-exits on client disconnect (existing behavior preserved). |
| Docker / Self-hosted | No sidecar flag | Multi-user. Admin login + instance access code + profile picker. First-run setup or env var bootstrap. |
| Tauri Desktop | `SEA_PUBLIC_PLATFORM="desktop"` | Same as Electron — single-user default, opt-in multi-user. |

**Electron Single-User Mode (default):**

- When running as desktop sidecar, the app operates exactly as today — no login, no profile picker, implicit admin with a default profile.
- The server auto-exit on client disconnect (`ExitIfNoConnsAsDesktopSidecar`) is preserved.
- Multi-user can be opted into via Denshi settings, which disables auto-exit and enables the profile system.
- The frontend uses `127.0.0.1:43211` hardcoded — multi-user in Electron is for local household use (multiple profiles on the same machine), not remote access.

**Docker / Self-hosted Mode (default):**

- Multi-user is the default behavior.
- `SEANIME_ADMIN_USERNAME` and `SEANIME_ADMIN_PASSWORD` env vars create the admin account on first startup.
- Optionally `SEANIME_INSTANCE_ACCESS_CODE` sets the household access code.
- If the admin already exists, env vars are ignored.
- First visit without env vars shows admin setup screen.

**Auth Token Strategy (cross-platform):**

- JWTs stored in httpOnly cookies for browser/Docker (secure, automatic on requests).
- JWTs stored in memory (not localStorage) for Electron — cleared on app close, re-authenticated automatically since Electron controls the server lifecycle.
- Existing `X-Seanime-Token` header and HMAC query param mechanisms for streaming URLs remain unchanged.
- The current instance password system (`server_auth_middleware.go`) is replaced by the new auth system. Migration: existing instance password becomes the instance access code.

---

## Database Schema Changes

All IDs are UUID v4 strings.

### New Tables

```sql
-- Admin account (username/password login)
-- The admin has exactly one associated profile (linked via admin.profile_id).
-- On login, the admin skips the profile picker and goes directly to their profile.
CREATE TABLE admins (
    id            TEXT PRIMARY KEY,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    profile_id    TEXT NOT NULL REFERENCES profiles(id),
    created_at    TIMESTAMP,
    updated_at    TIMESTAMP
);

-- Instance access code (shared household key)
CREATE TABLE instance_config (
    id          TEXT PRIMARY KEY DEFAULT '1',
    access_code TEXT  -- bcrypt hashed, nullable (no code = open access after admin auth)
);

-- User profiles (Netflix-style)
CREATE TABLE profiles (
    id         TEXT PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    pin_hash   TEXT,          -- optional, bcrypt hashed 4-6 digit PIN
    is_admin   BOOLEAN DEFAULT FALSE,
    avatar     TEXT,          -- color/icon identifier
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

-- Per-profile settings overrides
CREATE TABLE profile_settings (
    id         TEXT PRIMARY KEY,
    profile_id TEXT UNIQUE NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    overrides  TEXT  -- JSON blob of overridden setting fields
);

-- Per-profile library path assignments
CREATE TABLE library_paths (
    id         TEXT PRIMARY KEY,
    path       TEXT NOT NULL,
    owner_id   TEXT REFERENCES profiles(id) ON DELETE SET NULL,  -- null = global
    shared     BOOLEAN DEFAULT TRUE
);
```

### Modified Tables

```sql
-- accounts: add profile_id
ALTER TABLE accounts ADD COLUMN profile_id TEXT REFERENCES profiles(id);

-- local_files: add profile_id for scan ownership
ALTER TABLE local_files ADD COLUMN profile_id TEXT REFERENCES profiles(id);
```

All existing data migrates to the admin's default profile during the schema migration.

---

## Phase Breakdown

### Phase 1: Profiles & Instance Auth

**Goal:** Add admin account, instance access code, profile model, JWT auth, and request context threading.

**Backend:**
- `internal/database/models/admin.go` — Admin model
- `internal/database/models/profile.go` — Profile model
- `internal/database/models/instance_config.go` — Instance config model
- `internal/handlers/auth.go` — Login, access code validation, profile selection endpoints:
  - `POST /api/v1/auth/admin-login` — admin username/password → JWT
  - `POST /api/v1/auth/access-code` — household access code → limited JWT
  - `POST /api/v1/auth/select-profile` — profile selection (+ optional PIN) → full JWT with profileId
  - `GET /api/v1/auth/me` — current session info
- `internal/core/auth_middleware.go` — Echo middleware that:
  - Extracts JWT from httpOnly cookie or Authorization header
  - Attaches `profileId` and `isAdmin` to echo.Context
  - Rejects unauthenticated requests (except login/access-code/status endpoints)
  - Skips auth entirely when `App.IsDesktopSidecar` is true and multi-user is not opted into
- Helper: `GetProfile(c echo.Context) *models.Profile` available to all handlers
- Docker env var bootstrap in `internal/core/app.go` startup
- Respect `--desktop-sidecar` flag: when true and no admin exists, create implicit admin + default profile silently (no setup screen)

**Frontend:**
- `/login` route — admin login form (Docker/self-hosted only)
- `/access` route — instance access code form for household members (Docker/self-hosted only)
- `/profiles` route — profile picker grid (all platforms when multi-user enabled)
- Auth context provider wrapping the app — checks `serverStatus.isDesktopSidecar` to determine auth flow
- Protected route wrapper:
  - Docker/self-hosted: redirect to `/login` or `/access` if unauthenticated
  - Electron single-user: skip auth, auto-select default profile
  - Electron multi-user (opted in): show profile picker only (admin auth implicit since Electron controls the server)
- Profile indicator in top nav with logout/switch-profile option

**Migration:**
- Create new tables
- Create a default admin profile linked to the existing account (ID=1)
- Existing instance password migrates to the instance access code
- If `--desktop-sidecar` mode: create implicit admin + default profile, no instance access code

**Backward Compatibility:**
- Electron: implicit admin + default profile, zero UX change from current behavior
- Electron with multi-user opted in: profile picker after app launch, no login screen (admin is implicit)
- Docker without env vars: first visit shows admin setup screen
- Docker with env vars: admin bootstrapped, household members enter access code

---

### Phase 2: Per-Profile AniList & Settings

**Goal:** Each profile links their own AniList account. Settings inherit from admin-set global defaults with per-profile overrides.

**Backend:**
- `accounts` table gets `profile_id` FK
- AniList OAuth flow scoped to the requesting profile
- `App.AnilistClientRef` becomes a per-profile client pool:
  - `AnilistClientPool` struct holding `map[profileId]*anilist.Client`
  - Clients created lazily on first request, cached for the session
  - Pool cleans up inactive clients periodically
- Settings resolution: `GetSettingsForProfile(profileId)` merges global defaults with `profile_settings.overrides`
- Admin settings page writes to global `settings` table
- Profile settings page writes to `profile_settings` table (overrides only)

**Frontend:**
- AniList link/unlink scoped to current profile
- Settings page split: admin sees global defaults section + personal overrides, members see only their overrides
- Override UI: toggle per-field "Use custom" vs "Use default"

---

### Phase 3: Per-Profile Libraries

**Goal:** Shared and per-profile library paths. Scan results tagged by profile ownership.

**Backend:**
- `library_paths` table replaces `settings.Library.LibraryPath`
- Migration: existing library path → global shared entry in `library_paths`
- `local_files` table gets `profile_id` column
- Scan logic:
  - Shared library scans produce files visible to all profiles
  - Per-profile library scans tag files with `profile_id`
  - Each profile's library view: their own files + all shared files
- AutoScanner watches all paths, tags results appropriately
- Admin can manage all library paths; members manage only their own

**Frontend:**
- Library settings: admin sees global + personal paths, members see shared (read-only) + personal
- Library view filters by current profile's accessible files

---

### Phase 4: Per-Profile Streaming Sessions

**Goal:** Refactor singletons into per-profile session pools for concurrent streaming.

**Backend:**

New `StreamSessionManager`:
```go
type StreamSessionManager struct {
    sessions map[string]*ProfileStreamSession  // keyed by profile_id
    mu       sync.RWMutex
}

type ProfileStreamSession struct {
    ProfileID       string
    NativePlayer    *nativeplayer.NativePlayer
    PlaybackManager *playbackmanager.PlaybackManager
    TorrentClient   *torrentstream.Client
    DirectStream    *directstream.Manager
    LastActive      time.Time
}
```

- Sessions created lazily when a profile starts streaming
- Cleaned up after configurable inactivity timeout (default 30 min)
- `App.StreamSessionManager` replaces individual singleton fields
- Handlers call `GetSession(profileId)` to get the correct session
- Shared anacrolix torrent engine (single process) — each session gets its own torrent/file selection within it
- No hard concurrency limit — best effort with available resources

**Affected singletons:**
- `App.NativePlayer` → per-session
- `App.PlaybackManager` → per-session
- `App.TorrentRepository` client → per-session
- `App.DirectStreamManager` → per-session
- `App.VideoCore` → per-session

---

### Phase 5: WebSocket Multi-Profile Isolation

**Goal:** Route WebSocket events to the correct profile's connected clients.

**Backend:**

Extended connection model:
```go
type Connection struct {
    ClientId  string
    ProfileId string
    Conn      *websocket.Conn
}
```

- WebSocket handshake authenticates via JWT (query param or first message)
- Connection tagged with `profileId`

Three event routing modes:
- `SendToProfile(profileId, event)` — all clients of a profile (e.g. phone + laptop)
- `SendToClient(clientId, event)` — specific client (e.g. native player events)
- `SendToAll(event)` — broadcast (e.g. server shutdown, shared library scan progress)

Update all existing `SendEvent()` call sites to use the appropriate mode:
- Playback/stream state → `SendToProfile`
- Native player events → `SendToClient`
- Scan progress (shared) → `SendToAll`
- Scan progress (private) → `SendToProfile`

**Electron single-user mode:** All connections implicitly belong to the default profile.

---

### Phase 6: Frontend Multi-User UX

**Goal:** Profile picker, admin panel, and profile-scoped UI.

**Screens (platform-aware):**

| Route | Docker / Self-hosted | Electron (single-user) | Electron (multi-user opted in) |
|-------|---------------------|----------------------|-------------------------------|
| `/login` | Admin username/password form | Not shown | Not shown (admin implicit) |
| `/access` | Instance access code input | Not shown | Not shown (local access) |
| `/profiles` | Profile picker grid | Not shown | Shown after app launch |
| `/profiles/manage` | Admin: manage profiles | Not shown | Shown in Denshi settings |
| `/setup` | First-run admin creation | Not shown | Not shown |

**Admin Panel (under Settings):**

- Create/delete profiles (with name + avatar)
- Set/change instance access code (Docker/self-hosted only — not relevant for Electron)
- Manage global library paths
- View active streaming sessions across profiles
- Electron: "Enable multi-user" toggle in Denshi settings (creates additional profiles, disables auto-exit)

**Profile Area (under Settings):**

- Link/unlink AniList account
- Set/change/remove profile PIN
- Override settings (torrent provider, playback preferences)
- Manage personal library paths
- Change profile name/avatar

**Nav Bar:**

- Profile avatar/name indicator (shown when multi-user is active)
- Click → dropdown with "Switch Profile" and "Logout"
- "Switch Profile" returns to profile picker (re-enter PIN if set)
- "Logout" behavior:
  - Docker: returns to login/access code screen
  - Electron multi-user: returns to profile picker
  - Electron single-user: no logout option shown

**Component Changes:**

- Auth context provider at app root — exposes `{ profile, isAdmin, isMultiUser, isDesktopSidecar }`
- Protected route HOC — behavior varies by platform mode (see table above)
- Admin-only UI gating (`isAdmin` from context)
- Settings page restructured for global vs personal
- All platform checks use existing `__isElectronDesktop__` / `__isDesktop__` constants plus `serverStatus.isDesktopSidecar` from the backend

---

### Phase 7: Docker Integration

**Goal:** Bring the Dockerfile, docker-compose, and entrypoint into the main seanime repo with qBittorrent integrated. Extend env vars and entrypoint for multi-user bootstrapping. Maintain all existing image variants (default, rootless, hwaccel, cuda).

Currently Docker lives in a separate repo (`Awu05/seanime-docker`). This phase moves it into the main repo and extends it for multi-user support.

**New files in seanime repo:**
```
docker/
  Dockerfile           # Multi-stage: node build → go build → 4 variants
  Dockerfile.cuda      # NVIDIA CUDA variant
  config/
    entrypoint.sh      # Supervisord + qBittorrent + seanime + admin bootstrap
  examples/
    01-default/docker-compose.yml
    02-rootless/docker-compose.yml
    03-hwaccel/docker-compose.yml
    04-hwaccel-cuda/docker-compose.yml
```

**Dockerfile (multi-stage, matches current seanime-docker structure):**
- Stage 1: Node.js builder — builds `seanime-web` frontend
- Stage 2: Go builder — compiles Go binary with embedded web assets, cross-platform (amd64/arm64/armv7)
- Stage 3: Common base — Alpine with `ca-certificates tzdata curl qbittorrent-nox supervisor python3`
- Stage 4: Default (root) — standard ffmpeg
- Stage 5: Rootless — non-root `seanime` user (UID 1000)
- Stage 6: Hardware Acceleration — Jellyfin FFmpeg + Intel drivers (amd64)
- Dockerfile.cuda — separate file, NVIDIA CUDA Ubuntu base

No source cloning stage needed since Dockerfile lives in the repo now — `COPY . .` replaces the git clone.

**Environment Variables:**

| Variable | Default | Purpose |
|----------|---------|---------|
| `QBIT_WEBUI_PORT` | `8081` | qBittorrent WebUI port |
| `QBIT_USERNAME` | `admin` | qBittorrent login username |
| `QBIT_PASSWORD` | `adminadmin` | qBittorrent login password |
| `SEANIME_ADMIN_USERNAME` | *(none)* | Auto-create admin account on first start |
| `SEANIME_ADMIN_PASSWORD` | *(none)* | Admin account password |
| `SEANIME_INSTANCE_ACCESS_CODE` | *(none)* | Household access code for non-admin members |

**Entrypoint changes:**
- Existing qBittorrent config generation stays as-is
- New: passes `SEANIME_ADMIN_USERNAME`, `SEANIME_ADMIN_PASSWORD`, and `SEANIME_INSTANCE_ACCESS_CODE` as env vars that the Go binary reads on startup to bootstrap the admin account and access code (Phase 1 backend handles this)
- Supervisord config unchanged — manages both `qbittorrent-nox` and `seanime`

**Ports:**
- `43211` — Seanime API/Web UI
- `8081` — qBittorrent WebUI (configurable)

**Volumes:**

| Variant | Config | Anime | Downloads |
|---------|--------|-------|-----------|
| Default | `/root/.config` | `/anime` | `/downloads` |
| Rootless | `/home/seanime/.config` | `/anime` | `/downloads` |
| HwAccel | `/home/seanime/.config` | `/anime` | `/downloads` |
| CUDA | `/home/seanime/.config` | `/anime` | `/downloads` |

**Example docker-compose.yml (rootless with multi-user):**
```yaml
services:
  seanime:
    build:
      context: .
      dockerfile: docker/Dockerfile
      target: rootless
    container_name: seanime
    environment:
      - QBIT_WEBUI_PORT=8081
      - QBIT_USERNAME=admin
      - QBIT_PASSWORD=adminadmin
      - SEANIME_ADMIN_USERNAME=admin
      - SEANIME_ADMIN_PASSWORD=changeme
      - SEANIME_INSTANCE_ACCESS_CODE=family123
    ports:
      - "3211:43211"
      - "8081:8081"
    volumes:
      - ./config:/home/seanime/.config
      - ./anime:/anime
      - ./downloads:/downloads
    restart: unless-stopped
```

**Health check:** Same as current — `curl -f http://localhost:43211 || exit 1`

---

## Key Architectural Decisions

1. **SQLite retained** — sufficient for 2-5 household users, avoids infrastructure complexity
2. **UUIDs for all new IDs** — avoid collision issues, cleaner for future federation
3. **Profile model separate from admin auth** — admin is an account, profiles are personas within the instance
4. **Lazy session creation** — streaming sessions spin up on demand, not on profile selection
5. **Shared torrent engine** — single anacrolix instance, per-profile torrent/file selection within it
6. **Settings inheritance** — admin sets global defaults, profiles override specific fields via JSON merge
7. **Electron backward compatible** — no profiles in DB = current single-user behavior, zero friction

## Non-Goals

- User registration / self-service account creation
- Remote access auth (OAuth, SSO)
- Per-profile bandwidth limits or quotas
- Activity logging / watch history visible to admin
- Multi-instance federation
