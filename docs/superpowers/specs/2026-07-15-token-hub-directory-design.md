# Token Hub Directory — Design Spec

**Date**: 2026-07-15
**Template**: `cmingxu/golang-react-router-template` (Go + Gin + React + shadcn/ui)

## Overview

A public directory website that collects, monitors, and displays token hubs — services running the [new-api](https://github.com/Calcium-Ion/new-api) open-source API gateway. Hub operators or anyone can submit hubs; admins approve them; the system probes each approved hub every 5 minutes to track availability and latency.

## Architecture

Extend the template's single-binary model. Go server serves both the API and embedded React SPA. A background goroutine scheduler handles health probes. PostgreSQL for persistence (SQLite supported for dev).

```
┌─────────────────────────────────────────────┐
│  Go Binary                                   │
│  ┌──────────┐  ┌──────────┐  ┌────────────┐ │
│  │ Gin API   │  │ Embedded │  │ Background  │ │
│  │ (public + │  │ React    │  │ Health      │ │
│  │  admin)   │  │ SPA      │  │ Prober      │ │
│  └─────┬─────┘  └──────────┘  └──────┬──────┘ │
│        │                             │        │
└────────┼─────────────────────────────┼────────┘
         │                             │
         ▼                             ▼
┌─────────────────────────────────────────────┐
│  PostgreSQL / SQLite                         │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐ │
│  │ TokenHub │  │HealthProbe│  │ User/     │ │
│  │          │  │           │  │ Config    │ │
│  └──────────┘  └───────────┘  └───────────┘ │
└─────────────────────────────────────────────┘
```

Single process handles everything. If scale demands, the health prober can later be extracted into its own binary reading from the same DB.

## Data Model

### TokenHub

| Field | Type | Notes |
|-------|------|-------|
| `id` | int64 (PK) | |
| `name` | string, not null | Display name |
| `url` | string, not null | Base URL of the new-api instance |
| `description` | text | Optional |
| `tags` | jsonb | Array of tag strings for filtering |
| `status` | string, default "pending" | `pending`, `approved`, `rejected` |
| `health_status` | string, default "unknown" | `unknown`, `online`, `offline` |
| `health_latency_ms` | int | Last probe latency in milliseconds |
| `last_probed_at` | timestamp, nullable | |
| `models_info` | jsonb, nullable | Snapshot of available models + pricing from new-api `/api/status` |
| `created_at` | timestamp, auto | |
| `updated_at` | timestamp, auto | |

### HealthProbe

| Field | Type | Notes |
|-------|------|-------|
| `id` | int64 (PK) | |
| `hub_id` | int64 (FK → token_hubs) | |
| `online` | bool | |
| `latency_ms` | int | |
| `error_msg` | text, nullable | |
| `probed_at` | timestamp | |

Index on `(hub_id, probed_at)` for efficient per-hub health queries. Probes older than 7 days are pruned by a daily cleanup job.

### Existing models retained

`User`, `SystemConfig` from the template remain as-is.

## API Design

### Public endpoints (no auth required)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/hubs` | List approved hubs with pagination, search, tag filter, status filter |
| `GET` | `/api/hubs/:id` | Single hub detail (includes models_info) |
| `GET` | `/api/hubs/:id/health` | Recent health history (last 24h, for sparkline/detail) |
| `POST` | `/api/hubs/submit` | Submit a new hub for admin approval |

**`GET /api/hubs`** query params: `page` (default 1), `per_page` (default 50, max 100), `search` (matches name, description, url), `tag` (exact match), `status` (filter by health_status: online, offline, unknown).

Response: `{ hubs: [...], total: 1700, page: 1, per_page: 50 }`

### Admin endpoints (auth required, behind existing session middleware)

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/api/admin/submissions` | List pending hubs |
| `POST` | `/api/admin/submissions/:id/approve` | Approve a submission |
| `POST` | `/api/admin/submissions/:id/reject` | Reject a submission |
| `PUT` | `/api/admin/hubs/:id` | Edit hub details |
| `DELETE` | `/api/admin/hubs/:id` | Remove a hub |
| `GET` | `/api/admin/dashboard` | Stats: total hubs, online/offline count, pending submissions |

### Existing endpoints retained

All template endpoints (`/api/login`, `/api/logout`, `/api/me`, `/api/users`, `/api/system-config`, `/api/health`) remain unchanged.

## Health Checker

Background goroutine started at server boot:

1. Every 5 minutes, fetch all approved hubs
2. For each hub (batched in groups of 100), spawn a goroutine from a bounded pool (max 200 concurrent)
3. `HTTP GET {hub.url}/api/status` with a 5-second timeout
4. Validate the response shape to confirm it's a legitimate new-api instance
5. Insert a `HealthProbe` record with online/latency/error
6. Update the `TokenHub` row: `health_status`, `health_latency_ms`, `last_probed_at`
7. Pause for 5 minutes, repeat

**Pruning**: A daily goroutine deletes `HealthProbe` rows where `probed_at < now() - 7 days`.

**Scale check**: 5,000 hubs ÷ 300 seconds = ~17 probes/second. With 5-second timeouts, a 200-goroutine pool handles this comfortably.

## Frontend

### Public layout

Simple top navigation bar: logo/site name on the left, "Submit a Hub" link, no login required. No sidebar.

### Public pages

- **`/` — Hub Directory**: Searchable, filterable table of approved hubs. Columns: name, description, tags (chips), online/offline badge, latency, last probed. Search bar at top, tag filter chips, health status filter dropdown, pagination controls at bottom.

- **`/hubs/:id` — Hub Detail**: Hub name, URL (linked), description, tags, health status badge. Uptime % for last 7 days. Health timeline chart (last 24 hours of probe results). Models table: model name, pricing per input/output token.

- **`/submit` — Submit a Hub**: Form with name, URL, description, tags multi-select. Success message on submit. Goes into `pending` queue, invisible until approved.

### Admin layout

Same sidebar layout from the template (`AppLayout`), updated with new nav items.

### Admin pages

- **`/admin/submissions`** — Pending submissions table, approve/reject buttons per row
- **`/admin/hubs`** — All hubs table (edit, delete), filter by status
- Admin dashboard updated with hub stats
- Existing pages (users, settings) kept as-is

### Tech stack

Same as template: React 19, TypeScript, Vite, Tailwind CSS, shadcn/ui (Radix primitives), React Router 7, Lucide icons, Recharts for the health timeline chart.

## Testing

- **Go unit tests**: health checker logic, API handler responses, DB operations
- **Playwright E2E**: public directory browsing flow, submit hub flow, admin approval flow, login flow
- Template already includes Playwright — extend the existing setup

## Non-Goals (explicitly excluded)

- Community ratings, reviews, or commenting
- Real-time alerts or notifications for hub outages
- Automated hub discovery / crawling
- OAuth or third-party login (session-based auth from template is sufficient)
