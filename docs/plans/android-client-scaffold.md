# Android Client Scaffold — Plan

Status: **in-progress**

## Goal

Build the Android reader client for Raven: a Compose app that syncs articles from the
Raven backend, caches them locally in Room, and presents a scrollable article river
with detail view. Works offline after initial sync.

## Stack

| Layer | Choice |
|---|---|
| UI | Jetpack Compose + Material 3 |
| DB | Room (SQLite) |
| HTTP | Ktor client + kotlinx.serialization |
| Sync | WorkManager (periodic background) |
| DI | Manual (Hilt adds build complexity for now) |
| Config | EncryptedSharedPreferences for API token + server URL |

## Tasks

### 1. Project scaffold
- Gradle wrapper, root + app build files
- AndroidManifest with INTERNET permission
- Application class, MainActivity
- Compose theme (dark, minimal)

### 2. Data layer — Room
- `FeedEntity` — mirrors `feeds` table (id, url, title)
- `ArticleEntity` — mirrors `articles` table + embedded content
- `ArticleStateEntity` — read/starred state
- `ArticleDao` — list (paginated), get by id, upsert
- `RavenDatabase` — Room DB with migrations

### 3. Data layer — Remote
- `RavenApi` — Ktor client wrapping:
  - `GET /v1/articles` (cursor pagination)
  - `GET /v1/articles/{id}`
- `ApiConfig` — reads token + server URL from EncryptedSharedPreferences
- `ArticleRepository` — coordinates remote ↔ local

### 4. Sync worker
- `SyncWorker` — periodic WorkManager job
- Fetches latest articles from API, upserts into Room
- Uses cursor from last sync stored in SharedPreferences

### 5. UI — Article river
- `RiverScreen` — LazyColumn with article cards
- `RiverViewModel` — loads from Room, triggers refresh
- Card: title, author, date, excerpt, lead image, read indicator

### 6. UI — Article detail
- `DetailScreen` — full article text with formatting
- `DetailViewModel` — loads article + extracted text from Room
