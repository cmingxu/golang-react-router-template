# Token Hub Directory — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a public directory website for token hubs (new-api instances) with admin approval and automated health monitoring.

**Architecture:** Extend the template's Go monolith — Gin API + embedded React SPA + background health prober goroutine, all in one binary. PostgreSQL primary, SQLite for dev.

**Tech Stack:** Go 1.25, Gin, GORM, React 19, TypeScript, Vite, Tailwind CSS, shadcn/ui, React Router 7

## Global Constraints

- Module name: `tokenhub`
- Cmd path: `cmd/tokenhub/main.go`
- API prefix: `/api/` — public endpoints bypass auth, admin endpoints use existing session middleware
- Health probe interval: 5 minutes, HTTP GET `{hub.url}/api/status`, 5s timeout, 200 concurrent max
- Probe retention: 7 days, pruned daily
- Pagination: `?page=1&per_page=50`, max 100 per page
- All API responses are JSON. Error shape: `{ "error": "message" }`
- Frontend follows existing patterns: shadcn/ui components, `cn()` utility, named exports for pages

---

### Task 1: Bootstrap project from template

**Files:**
- Create: all project files (clone + rename)
- Modify: `go.mod`, `cmd/willing/main.go` → `cmd/tokenhub/main.go`, all `import` paths

**Interfaces:**
- Produces: working Go module `tokenhub` that compiles, all template functionality intact

- [ ] **Step 1: Clone template into project directory**

```bash
cd /Users/kx/Code/go/tokenminics/openrouter
git clone git@github.com:cmingxu/golang-react-router-template.git tmp-bootstrap
cp -r tmp-bootstrap/. .
rm -rf tmp-bootstrap
```

- [ ] **Step 2: Rename Go module and update all import paths**

Update `go.mod` line 1: replace `module willing` with `module tokenhub`.

Rename `cmd/willing/` to `cmd/tokenhub/`:

```bash
mv cmd/willing cmd/tokenhub
```

Update all Go import paths — replace `"willing/` with `"tokenhub/` in every `.go` file:

```bash
grep -rl '"willing/' --include='*.go' . | xargs sed -i '' 's|"willing/|"tokenhub/|g'
```

- [ ] **Step 3: Verify it builds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Verify dev mode runs**

```bash
go run ./cmd/tokenhub serve
```

Expected: server starts on `:8080`. Ctrl+C to stop.

- [ ] **Step 5: Initialize git and commit**

```bash
git init
git add -A
git commit -m "chore: bootstrap from golang-react-router-template

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 2: Add Go models — TokenHub and HealthProbe

**Files:**
- Create: `internal/models/token_hub.go`
- Create: `internal/models/health_probe.go`

**Interfaces:**
- Produces: `models.TokenHub` struct, `models.HealthProbe` struct

- [ ] **Step 1: Create TokenHub model**

Create `internal/models/token_hub.go`:

```go
package models

import "time"

type TokenHub struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	Name             string    `gorm:"not null" json:"name"`
	URL              string    `gorm:"not null" json:"url"`
	Description      string    `json:"description"`
	Tags             string    `gorm:"type:jsonb;default:'[]'" json:"tags"`
	Status           string    `gorm:"not null;default:pending" json:"status"`
	HealthStatus     string    `gorm:"not null;default:unknown" json:"healthStatus"`
	HealthLatencyMs  int       `json:"healthLatencyMs"`
	LastProbedAt     *time.Time `json:"lastProbedAt"`
	ModelsInfo       string    `gorm:"type:jsonb" json:"modelsInfo"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func (TokenHub) TableName() string {
	return "token_hubs"
}
```

- [ ] **Step 2: Create HealthProbe model**

Create `internal/models/health_probe.go`:

```go
package models

import "time"

type HealthProbe struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	HubID      int64     `gorm:"not null;index:idx_hub_probed" json:"hubId"`
	Online     bool      `gorm:"not null" json:"online"`
	LatencyMs  int       `json:"latencyMs"`
	ErrorMsg   string    `json:"errorMsg"`
	ProbedAt   time.Time `gorm:"not null;index:idx_hub_probed" json:"probedAt"`
}

func (HealthProbe) TableName() string {
	return "health_probes"
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/models/token_hub.go internal/models/health_probe.go
git commit -m "feat: add TokenHub and HealthProbe models

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 3: Add DB operations for TokenHub

**Files:**
- Create: `internal/db/hub.go`
- Modify: `internal/db/db.go` — add models to `Migrate()`

**Interfaces:**
- Consumes: `models.TokenHub`, `models.HealthProbe`
- Produces: `(*Store).CreateHub(ctx, hub) (TokenHub, error)`, `(*Store).ListApprovedHubs(ctx, opts ListHubsOptions) ([]TokenHub, int64, error)`, `(*Store).GetHubByID(ctx, id) (TokenHub, error)`, `(*Store).UpdateHub(ctx, id, upd HubUpdate) (TokenHub, error)`, `(*Store).DeleteHub(ctx, id) error`, `(*Store).ApproveHub(ctx, id) error`, `(*Store).RejectHub(ctx, id) error`, `(*Store).ListPendingHubs(ctx) ([]TokenHub, error)`, `(*Store).ListAllHubs(ctx) ([]TokenHub, error)`, `(*Store).GetHubStats(ctx) (HubStats, error)`

- [ ] **Step 1: Create `internal/db/hub.go` with all CRUD operations**

```go
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"tokenhub/internal/models"
)

type ListHubsOptions struct {
	Page       int
	PerPage    int
	Search     string
	Tag        string
	StatusFilter string
}

type HubUpdate struct {
	Name        *string
	URL         *string
	Description *string
	Tags        *string
}

type HubStats struct {
	TotalHubs    int64 `json:"totalHubs"`
	OnlineHubs   int64 `json:"onlineHubs"`
	OfflineHubs  int64 `json:"offlineHubs"`
	PendingHubs  int64 `json:"pendingHubs"`
}

func (s *Store) CreateHub(ctx context.Context, hub models.TokenHub) (models.TokenHub, error) {
	hub.Status = "pending"
	hub.HealthStatus = "unknown"
	if hub.Tags == "" {
		hub.Tags = "[]"
	}
	err := s.db.WithContext(ctx).Create(&hub).Error
	return hub, err
}

func (s *Store) ListApprovedHubs(ctx context.Context, opts ListHubsOptions) ([]models.TokenHub, int64, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 || opts.PerPage > 100 {
		opts.PerPage = 50
	}

	q := s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("status = ?", "approved")

	if strings.TrimSpace(opts.Search) != "" {
		like := "%" + strings.TrimSpace(opts.Search) + "%"
		q = q.Where("name ILIKE ? OR description ILIKE ? OR url ILIKE ?", like, like, like)
	}

	if strings.TrimSpace(opts.Tag) != "" {
		q = q.Where("tags::jsonb @> ?", fmt.Sprintf(`["%s"]`, strings.TrimSpace(opts.Tag)))
	}

	if opts.StatusFilter == "online" {
		q = q.Where("health_status = ?", "online")
	} else if opts.StatusFilter == "offline" {
		q = q.Where("health_status = ?", "offline")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var hubs []models.TokenHub
	offset := (opts.Page - 1) * opts.PerPage
	err := q.Order("name ASC").Offset(offset).Limit(opts.PerPage).Find(&hubs).Error
	return hubs, total, err
}

func (s *Store) GetHubByID(ctx context.Context, id int64) (models.TokenHub, error) {
	var hub models.TokenHub
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&hub).Error
	return hub, err
}

func (s *Store) UpdateHub(ctx context.Context, id int64, upd HubUpdate) (models.TokenHub, error) {
	hub, err := s.GetHubByID(ctx, id)
	if err != nil {
		return models.TokenHub{}, err
	}

	if upd.Name != nil {
		hub.Name = strings.TrimSpace(*upd.Name)
	}
	if upd.URL != nil {
		hub.URL = strings.TrimSpace(*upd.URL)
	}
	if upd.Description != nil {
		hub.Description = strings.TrimSpace(*upd.Description)
	}
	if upd.Tags != nil {
		hub.Tags = *upd.Tags
	}

	hub.UpdatedAt = time.Now().UTC()
	err = s.db.WithContext(ctx).Save(&hub).Error
	return hub, err
}

func (s *Store) DeleteHub(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&models.TokenHub{}, id).Error
}

func (s *Store) ApproveHub(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("id = ?", id).Updates(map[string]any{
		"status":     "approved",
		"updated_at": time.Now().UTC(),
	}).Error
}

func (s *Store) RejectHub(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("id = ?", id).Updates(map[string]any{
		"status":     "rejected",
		"updated_at": time.Now().UTC(),
	}).Error
}

func (s *Store) ListPendingHubs(ctx context.Context) ([]models.TokenHub, error) {
	var hubs []models.TokenHub
	err := s.db.WithContext(ctx).Where("status = ?", "pending").Order("created_at ASC").Find(&hubs).Error
	return hubs, err
}

func (s *Store) ListAllHubs(ctx context.Context) ([]models.TokenHub, error) {
	var hubs []models.TokenHub
	err := s.db.WithContext(ctx).Order("name ASC").Find(&hubs).Error
	return hubs, err
}

func (s *Store) ListApprovedHubsAll(ctx context.Context) ([]models.TokenHub, error) {
	var hubs []models.TokenHub
	err := s.db.WithContext(ctx).Where("status = ?", "approved").Find(&hubs).Error
	return hubs, err
}

func (s *Store) GetHubStats(ctx context.Context) (HubStats, error) {
	var stats HubStats
	s.db.WithContext(ctx).Model(&models.TokenHub{}).Count(&stats.TotalHubs)
	s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("health_status = ?", "online").Count(&stats.OnlineHubs)
	s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("health_status = ?", "offline").Count(&stats.OfflineHubs)
	s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("status = ?", "pending").Count(&stats.PendingHubs)
	return stats, nil
}

func (s *Store) UpdateHubHealth(ctx context.Context, id int64, online bool, latencyMs int, errorMsg string) error {
	healthStatus := "online"
	if !online {
		healthStatus = "offline"
	}
	return s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("id = ?", id).Updates(map[string]any{
		"health_status":      healthStatus,
		"health_latency_ms":  latencyMs,
		"last_probed_at":     time.Now().UTC(),
		"updated_at":         time.Now().UTC(),
	}).Error
}

func (s *Store) UpdateHubModelsInfo(ctx context.Context, id int64, modelsInfo string) error {
	return s.db.WithContext(ctx).Model(&models.TokenHub{}).Where("id = ?", id).Updates(map[string]any{
		"models_info": modelsInfo,
		"updated_at":  time.Now().UTC(),
	}).Error
}

func (s *Store) InsertHealthProbe(ctx context.Context, probe models.HealthProbe) error {
	return s.db.WithContext(ctx).Create(&probe).Error
}

func (s *Store) GetHealthProbes(ctx context.Context, hubID int64, since time.Time) ([]models.HealthProbe, error) {
	var probes []models.HealthProbe
	err := s.db.WithContext(ctx).
		Where("hub_id = ? AND probed_at >= ?", hubID, since).
		Order("probed_at ASC").
		Find(&probes).Error
	return probes, err
}

func (s *Store) PruneHealthProbes(ctx context.Context, before time.Time) error {
	return s.db.WithContext(ctx).Where("probed_at < ?", before).Delete(&models.HealthProbe{}).Error
}
```

- [ ] **Step 2: Update `Migrate()` in `internal/db/db.go` to include new models**

In `internal/db/db.go`, find the `Migrate` method (line 88). Change:

```go
if err := s.db.WithContext(ctx).AutoMigrate(&models.SystemConfig{}, &models.User{}); err != nil {
```

To:

```go
if err := s.db.WithContext(ctx).AutoMigrate(&models.SystemConfig{}, &models.User{}, &models.TokenHub{}, &models.HealthProbe{}); err != nil {
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/db/hub.go internal/db/db.go
git commit -m "feat: add DB operations for TokenHub and HealthProbe

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 4: Add public API handlers (hub listing, detail, health, submit) + fix auth middleware

**Files:**
- Create: `internal/admin/hub_public.go`
- Modify: `internal/admin/admin.go` — update auth middleware bypass + register public routes

**Interfaces:**
- Consumes: `db.Store` methods from Task 3
- Produces: `registerPublicHubRoutes(r *gin.Engine, store *db.Store)` — called from `admin.go`

- [ ] **Step 1: Create `internal/admin/hub_public.go`**

```go
package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"tokenhub/internal/db"
	"tokenhub/internal/models"
)

func registerPublicHubRoutes(r *gin.Engine, store *db.Store) {
	public := r.Group("/api")

	public.GET("/hubs", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
		search := c.Query("search")
		tag := c.Query("tag")
		statusFilter := c.Query("status")

		if store == nil {
			c.JSON(http.StatusOK, gin.H{"hubs": []models.TokenHub{}, "total": 0, "page": page, "per_page": perPage})
			return
		}

		hubs, total, err := store.ListApprovedHubs(c.Request.Context(), db.ListHubsOptions{
			Page:         page,
			PerPage:      perPage,
			Search:       search,
			Tag:          tag,
			StatusFilter: statusFilter,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if hubs == nil {
			hubs = []models.TokenHub{}
		}

		c.JSON(http.StatusOK, gin.H{
			"hubs":     hubs,
			"total":    total,
			"page":     page,
			"per_page": perPage,
		})
	})

	public.GET("/hubs/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		if store == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		hub, err := store.GetHubByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		c.JSON(http.StatusOK, hub)
	})

	public.GET("/hubs/:id/health", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		if store == nil {
			c.JSON(http.StatusOK, gin.H{"probes": []models.HealthProbe{}})
			return
		}

		since := time.Now().UTC().Add(-24 * time.Hour)
		probes, err := store.GetHealthProbes(c.Request.Context(), id, since)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if probes == nil {
			probes = []models.HealthProbe{}
		}

		c.JSON(http.StatusOK, gin.H{"probes": probes})
	})

	public.POST("/hubs/submit", func(c *gin.Context) {
		var req struct {
			Name        string `json:"name"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Tags        string `json:"tags"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if store == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db not available"})
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.URL = strings.TrimSpace(req.URL)
		if req.Name == "" || req.URL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name and url are required"})
			return
		}

		if req.Tags == "" {
			req.Tags = "[]"
		}

		hub, err := store.CreateHub(c.Request.Context(), models.TokenHub{
			Name:        req.Name,
			URL:         req.URL,
			Description: req.Description,
			Tags:        req.Tags,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, hub)
	})
}
```

- [ ] **Step 2: Update auth middleware AND register public routes in `internal/admin/admin.go`**

Two changes needed in `admin.go`:

**Change A — Update the auth middleware** to bypass public API paths and let all non-API SPA paths through. In the `authMiddleware` function, find the block starting around line 45:

```go
		if strings.HasPrefix(p, "/api/") {
			if p == "/api/login" {
				c.Next()
				return
			}
```

Change to:

```go
		if strings.HasPrefix(p, "/api/") {
			if p == "/api/login" || p == "/api/hubs" || strings.HasPrefix(p, "/api/hubs/") {
				c.Next()
				return
			}
```

This allows `/api/hubs` (list), `/api/hubs/123` (detail), `/api/hubs/123/health` (health history), and `/api/hubs/submit` (submit) without authentication.

**Change B — Simplify non-API auth to let SPA handle routing.** In the non-API section of `authMiddleware`, remove the session check and redirect to `/login`. Change from:

```go
		if p == "/login" || p == "/assets/" || strings.HasPrefix(p, "/assets/") {
			c.Next()
			return
		}

		session, _ := store.Get(c.Request, "session-name")
		if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
```

To just:

```go
		// Non-API paths: let the SPA handle routing and auth client-side
		c.Next()
```

(The SPA's `PrivateRoute` component checks auth via `/api/me` and redirects to `/login` client-side when needed.)

**Change C — Register public hub routes.** Inside `New()`, right before `webui.Register(r)`, add:

```go
	if cfg.DB != nil {
		registerPublicHubRoutes(r, cfg.DB)
	}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/admin/hub_public.go internal/admin/admin.go
git commit -m "feat: add public API handlers for hub directory

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 5: Add admin API handlers (submissions, approve/reject, edit, delete, dashboard)

**Files:**
- Create: `internal/admin/hub_admin.go`
- Modify: `internal/admin/admin.go` — register admin routes

**Interfaces:**
- Consumes: `db.Store` methods from Task 3
- Produces: `registerAdminHubRoutes(api *gin.RouterGroup, store *db.Store)` — called from `admin.go`

- [ ] **Step 1: Create `internal/admin/hub_admin.go`**

```go
package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"tokenhub/internal/db"
	"tokenhub/internal/models"
)

func registerAdminHubRoutes(api *gin.RouterGroup, store *db.Store) {
	api.GET("/submissions", func(c *gin.Context) {
		if store == nil {
			c.JSON(http.StatusOK, gin.H{"submissions": []models.TokenHub{}})
			return
		}

		hubs, err := store.ListPendingHubs(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if hubs == nil {
			hubs = []models.TokenHub{}
		}

		c.JSON(http.StatusOK, gin.H{"submissions": hubs})
	})

	api.POST("/submissions/:id/approve", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		if store == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db not available"})
			return
		}

		if err := store.ApproveHub(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	api.POST("/submissions/:id/reject", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		if store == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db not available"})
			return
		}

		if err := store.RejectHub(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	api.GET("/hubs", func(c *gin.Context) {
		if store == nil {
			c.JSON(http.StatusOK, gin.H{"hubs": []models.TokenHub{}})
			return
		}

		hubs, err := store.ListAllHubs(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if hubs == nil {
			hubs = []models.TokenHub{}
		}

		c.JSON(http.StatusOK, gin.H{"hubs": hubs})
	})

	api.PUT("/hubs/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		if store == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db not available"})
			return
		}

		var req struct {
			Name        *string `json:"name"`
			URL         *string `json:"url"`
			Description *string `json:"description"`
			Tags        *string `json:"tags"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		hub, err := store.UpdateHub(c.Request.Context(), id, db.HubUpdate{
			Name:        req.Name,
			URL:         req.URL,
			Description: req.Description,
			Tags:        req.Tags,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, hub)
	})

	api.DELETE("/hubs/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid hub id"})
			return
		}

		if store == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db not available"})
			return
		}

		if err := store.DeleteHub(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	api.GET("/dashboard", func(c *gin.Context) {
		if store == nil {
			c.JSON(http.StatusOK, gin.H{
				"user_count":    0,
				"total_hubs":    0,
				"online_hubs":   0,
				"offline_hubs":  0,
				"pending_hubs":  0,
				"time":          "now",
			})
			return
		}

		var userCount int64
		users, err := store.ListUsers(c.Request.Context())
		if err == nil {
			userCount = int64(len(users))
		}

		stats, err := store.GetHubStats(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"user_count":   userCount,
			"total_hubs":   stats.TotalHubs,
			"online_hubs":  stats.OnlineHubs,
			"offline_hubs": stats.OfflineHubs,
			"pending_hubs": stats.PendingHubs,
			"time":         time.Now().UTC().Format(time.RFC3339),
		})
	})
}
```

Wait — that `"time"` line is a silly placeholder. Fix it:

```go
		"time": time.Now().UTC().Format(time.RFC3339),
```

And add `"time"` to the import block.

- [ ] **Step 2: Register admin hub routes in `internal/admin/admin.go`**

In `admin.go`, inside the `New()` function, after the existing `api := r.Group("/api")` line and before the `api.GET("/me", ...)` block, find the `api` group definition around line 164. After the line `api := r.Group("/api")`, add:

```go
	if cfg.DB != nil {
		registerAdminHubRoutes(api, cfg.DB)
	}
```

- [ ] **Step 3: Replace the existing inline `/api/dashboard` handler with the new one**

In `admin.go`, find and remove the existing inline `api.GET("/dashboard", ...)` handler (approximately lines 262-275). The new admin hub routes file provides an updated version.

- [ ] **Step 4: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/admin/hub_admin.go internal/admin/admin.go
git commit -m "feat: add admin API handlers for hub management

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 6: Add health checker background worker

**Files:**
- Create: `internal/health/checker.go`
- Modify: `cmd/tokenhub/main.go` — start checker goroutine

**Interfaces:**
- Consumes: `db.Store` (ListApprovedHubsAll, UpdateHubHealth, InsertHealthProbe, PruneHealthProbes, UpdateHubModelsInfo)
- Produces: `health.Start(ctx, store)` — blocking-style function launched in a goroutine

- [ ] **Step 1: Create `internal/health/checker.go`**

```go
package health

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"tokenhub/internal/db"
	"tokenhub/internal/models"
)

const (
	probeInterval  = 5 * time.Minute
	probeTimeout   = 5 * time.Second
	maxConcurrency = 200
	pruneInterval  = 24 * time.Hour
	pruneRetention = 7 * 24 * time.Hour
)

func Start(ctx context.Context, store *db.Store) {
	if store == nil {
		return
	}

	probeTicker := time.NewTicker(probeInterval)
	pruneTicker := time.NewTicker(pruneInterval)
	defer probeTicker.Stop()
	defer pruneTicker.Stop()

	// Run immediately on start, then on tick
	runProbe(ctx, store)
	runPrune(ctx, store)

	for {
		select {
		case <-ctx.Done():
			return
		case <-probeTicker.C:
			runProbe(ctx, store)
		case <-pruneTicker.C:
			runPrune(ctx, store)
		}
	}
}

func runProbe(ctx context.Context, store *db.Store) {
	hubs, err := store.ListApprovedHubsAll(ctx)
	if err != nil {
		log.Printf("[Health] failed to list hubs: %v", err)
		return
	}

	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for i := range hubs {
		hub := hubs[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			probeHub(ctx, store, hub)
		}()
	}

	wg.Wait()
	log.Printf("[Health] probed %d hubs", len(hubs))
}

func probeHub(ctx context.Context, store *db.Store, hub models.TokenHub) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, hub.URL+"/api/status", nil)
	if err != nil {
		recordProbe(ctx, store, hub.ID, false, 0, err.Error())
		return
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		recordProbe(ctx, store, hub.ID, false, int(latency), err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		recordProbe(ctx, store, hub.ID, true, int(latency), "")
		// Try to parse models info
		tryUpdateModelsInfo(ctx, store, hub.ID, resp)
	} else {
		recordProbe(ctx, store, hub.ID, false, int(latency), "non-2xx status: "+resp.Status)
	}
}

func tryUpdateModelsInfo(ctx context.Context, store *db.Store, hubID int64, resp *http.Response) {
	var body struct {
		Data []struct {
			ID    string  `json:"id"`
			Name  string  `json:"name"`
			Price float64 `json:"price"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	if len(body.Data) > 0 {
		b, _ := json.Marshal(body.Data)
		_ = store.UpdateHubModelsInfo(ctx, hubID, string(b))
	}
}

func recordProbe(ctx context.Context, store *db.Store, hubID int64, online bool, latencyMs int, errorMsg string) {
	if err := store.UpdateHubHealth(ctx, hubID, online, latencyMs, errorMsg); err != nil {
		log.Printf("[Health] update hub health %d: %v", hubID, err)
	}

	probe := models.HealthProbe{
		HubID:     hubID,
		Online:    online,
		LatencyMs: latencyMs,
		ErrorMsg:  errorMsg,
		ProbedAt:  time.Now().UTC(),
	}
	if err := store.InsertHealthProbe(ctx, probe); err != nil {
		log.Printf("[Health] insert probe %d: %v", hubID, err)
	}
}

func runPrune(ctx context.Context, store *db.Store) {
	before := time.Now().UTC().Add(-pruneRetention)
	if err := store.PruneHealthProbes(ctx, before); err != nil {
		log.Printf("[Health] prune probes: %v", err)
	} else {
		log.Printf("[Health] pruned probes before %s", before.Format(time.RFC3339))
	}
}
```

- [ ] **Step 2: Start health checker in `cmd/tokenhub/main.go`**

In `main.go`, after the store is created and after `store.CreateDefaultUser(ctx)`, add:

```go
	go health.Start(ctx, store)
```

Add `"tokenhub/internal/health"` to the import block.

- [ ] **Step 3: Verify compilation**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/health/checker.go cmd/tokenhub/main.go
git commit -m "feat: add background health checker with 5-min probe cycle

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 7: Frontend — TypeScript types and API client

**Files:**
- Create: `web/src/types/hub.ts`
- Create: `web/src/lib/api.ts`

**Interfaces:**
- Produces: TypeScript types matching Go models, API client functions for all endpoints

- [ ] **Step 1: Create `web/src/types/hub.ts`**

```typescript
export interface TokenHub {
  id: number;
  name: string;
  url: string;
  description: string;
  tags: string;
  status: 'pending' | 'approved' | 'rejected';
  healthStatus: 'unknown' | 'online' | 'offline';
  healthLatencyMs: number;
  lastProbedAt: string | null;
  modelsInfo: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface HealthProbe {
  id: number;
  hubId: number;
  online: boolean;
  latencyMs: number;
  errorMsg: string;
  probedAt: string;
}

export interface HubListResponse {
  hubs: TokenHub[];
  total: number;
  page: number;
  per_page: number;
}

export interface HubStats {
  user_count: number;
  total_hubs: number;
  online_hubs: number;
  offline_hubs: number;
  pending_hubs: number;
  time: string;
}

export function parseTags(tagsJson: string): string[] {
  try {
    const parsed = JSON.parse(tagsJson);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function uptimePercent(probes: HealthProbe[]): number {
  if (probes.length === 0) return 0;
  const online = probes.filter(p => p.online).length;
  return Math.round((online / probes.length) * 100);
}
```

- [ ] **Step 2: Create `web/src/lib/api.ts`**

```typescript
import type { TokenHub, HubListResponse, HealthProbe, HubStats } from '../types/hub';

const BASE = '/api';

async function request<T>(url: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(url, opts);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error((body as { error?: string }).error || `HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export async function fetchHubs(params: {
  page?: number;
  per_page?: number;
  search?: string;
  tag?: string;
  status?: string;
} = {}): Promise<HubListResponse> {
  const qs = new URLSearchParams();
  if (params.page) qs.set('page', String(params.page));
  if (params.per_page) qs.set('per_page', String(params.per_page));
  if (params.search) qs.set('search', params.search);
  if (params.tag) qs.set('tag', params.tag);
  if (params.status) qs.set('status', params.status);
  return request<HubListResponse>(`${BASE}/hubs?${qs.toString()}`);
}

export async function fetchHub(id: number): Promise<TokenHub> {
  return request<TokenHub>(`${BASE}/hubs/${id}`);
}

export async function fetchHubHealth(id: number): Promise<{ probes: HealthProbe[] }> {
  return request<{ probes: HealthProbe[] }>(`${BASE}/hubs/${id}/health`);
}

export async function submitHub(data: {
  name: string;
  url: string;
  description?: string;
  tags?: string;
}): Promise<TokenHub> {
  return request<TokenHub>(`${BASE}/hubs/submit`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
}

export async function fetchSubmissions(): Promise<{ submissions: TokenHub[] }> {
  return request<{ submissions: TokenHub[] }>(`${BASE}/submissions`);
}

export async function approveSubmission(id: number): Promise<void> {
  await request(`${BASE}/submissions/${id}/approve`, { method: 'POST' });
}

export async function rejectSubmission(id: number): Promise<void> {
  await request(`${BASE}/submissions/${id}/reject`, { method: 'POST' });
}

export async function fetchAdminHubs(): Promise<{ hubs: TokenHub[] }> {
  return request<{ hubs: TokenHub[] }>(`${BASE}/hubs`);
}

export async function updateHub(id: number, data: {
  name?: string;
  url?: string;
  description?: string;
  tags?: string;
}): Promise<TokenHub> {
  return request<TokenHub>(`${BASE}/hubs/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
}

export async function deleteHub(id: number): Promise<void> {
  await request(`${BASE}/hubs/${id}`, { method: 'DELETE' });
}

export async function fetchDashboard(): Promise<HubStats> {
  return request<HubStats>(`${BASE}/dashboard`);
}
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/types/hub.ts web/src/lib/api.ts
git commit -m "feat: add frontend TypeScript types and API client

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 8: Frontend — Public layout and HubStatusBadge component

**Files:**
- Create: `web/src/components/PublicLayout.tsx`
- Create: `web/src/components/HubStatusBadge.tsx`

**Interfaces:**
- Produces: `<PublicLayout>` component (top nav, no sidebar), `<HubStatusBadge status={...}>` component

- [ ] **Step 1: Create `web/src/components/PublicLayout.tsx`**

```tsx
import { Link, Outlet } from 'react-router-dom';
import { Server, PlusCircle } from 'lucide-react';

export default function PublicLayout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex h-14 items-center justify-between border-b bg-card px-6">
        <Link to="/" className="flex items-center gap-2 font-semibold text-lg hover:opacity-80">
          <Server className="h-5 w-5 text-primary" />
          Token Hub Directory
        </Link>
        <nav className="flex items-center gap-4">
          <Link
            to="/submit"
            className="inline-flex items-center gap-1.5 text-sm font-medium text-primary hover:underline"
          >
            <PlusCircle className="h-4 w-4" />
            Submit a Hub
          </Link>
        </nav>
      </header>

      <main className="max-w-6xl mx-auto px-4 py-8">
        <Outlet />
      </main>

      <footer className="border-t py-4 text-center text-xs text-muted-foreground">
        &copy; {new Date().getFullYear()} Token Hub Directory. All rights reserved.
      </footer>
    </div>
  );
}
```

- [ ] **Step 2: Create `web/src/components/HubStatusBadge.tsx`**

```tsx
import { cn } from '../lib/utils';

interface Props {
  status: 'unknown' | 'online' | 'offline';
}

export default function HubStatusBadge({ status }: Props) {
  const label = status === 'online' ? 'Online' : status === 'offline' ? 'Offline' : 'Unknown';
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium',
        status === 'online' && 'bg-green-100 text-green-800',
        status === 'offline' && 'bg-red-100 text-red-800',
        status === 'unknown' && 'bg-gray-100 text-gray-600',
      )}
    >
      <span
        className={cn(
          'h-2 w-2 rounded-full',
          status === 'online' && 'bg-green-500',
          status === 'offline' && 'bg-red-500',
          status === 'unknown' && 'bg-gray-400',
        )}
      />
      {label}
    </span>
  );
}
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/PublicLayout.tsx web/src/components/HubStatusBadge.tsx
git commit -m "feat: add PublicLayout and HubStatusBadge components

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 9: Frontend — Hub Directory page

**Files:**
- Create: `web/src/pages/HubDirectory.tsx`

**Interfaces:**
- Consumes: `fetchHubs` from api.ts, `HubStatusBadge`, types from hub.ts

- [ ] **Step 1: Create `web/src/pages/HubDirectory.tsx`**

```tsx
import { useEffect, useState, useCallback } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Search, ChevronLeft, ChevronRight } from 'lucide-react';

import { fetchHubs } from '../lib/api';
import { parseTags, type TokenHub, type HubListResponse } from '../types/hub';
import HubStatusBadge from '../components/HubStatusBadge';
import { Input } from '../components/ui/input';
import { Button } from '../components/ui/button';

export function HubDirectoryPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const page = Number(searchParams.get('page') || '1');
  const search = searchParams.get('search') || '';
  const tag = searchParams.get('tag') || '';
  const status = searchParams.get('status') || '';

  const [data, setData] = useState<HubListResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [searchInput, setSearchInput] = useState(search);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchHubs({ page, per_page: 50, search, tag, status: status || undefined });
      setData(result);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  }, [page, search, tag, status]);

  useEffect(() => {
    load();
  }, [load]);

  const totalPages = data ? Math.ceil(data.total / data.per_page) : 0;

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    if (key !== 'page') next.delete('page');
    setSearchParams(next);
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Token Hub Directory</h1>
        <p className="text-muted-foreground mt-1">
          Discover and monitor token hubs powered by new-api
        </p>
      </div>

      <div className="flex flex-wrap gap-3">
        <div className="relative flex-1 min-w-60">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search hubs..."
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') setParam('search', searchInput);
            }}
            className="pl-9"
          />
        </div>
        <select
          value={status}
          onChange={(e) => setParam('status', e.target.value)}
          className="rounded-md border bg-background px-3 py-2 text-sm"
        >
          <option value="">All status</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
        </select>
      </div>

      {error && <div className="text-sm text-red-600">Error: {error}</div>}

      {loading && <div className="text-sm text-muted-foreground">Loading...</div>}

      {data && data.hubs.length === 0 && !loading && (
        <div className="text-sm text-muted-foreground py-8 text-center">
          No hubs found. <Link to="/submit" className="text-primary underline">Submit one?</Link>
        </div>
      )}

      {data && data.hubs.length > 0 && (
        <>
          <div className="border rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-muted/50">
                <tr>
                  <th className="text-left px-4 py-3 font-medium">Name</th>
                  <th className="text-left px-4 py-3 font-medium">Description</th>
                  <th className="text-left px-4 py-3 font-medium">Status</th>
                  <th className="text-left px-4 py-3 font-medium">Latency</th>
                  <th className="text-left px-4 py-3 font-medium">Last Probed</th>
                </tr>
              </thead>
              <tbody>
                {data.hubs.map((hub: TokenHub) => (
                  <tr key={hub.id} className="border-t hover:bg-muted/30">
                    <td className="px-4 py-3">
                      <Link to={`/hubs/${hub.id}`} className="font-medium text-primary hover:underline">
                        {hub.name}
                      </Link>
                      {parseTags(hub.tags).length > 0 && (
                        <div className="flex flex-wrap gap-1 mt-1">
                          {parseTags(hub.tags).map((t) => (
                            <button
                              key={t}
                              onClick={() => setParam('tag', t)}
                              className="text-xs bg-muted rounded px-1.5 py-0.5 hover:bg-muted-foreground/20"
                            >
                              {t}
                            </button>
                          ))}
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground max-w-xs truncate">
                      {hub.description || '—'}
                    </td>
                    <td className="px-4 py-3">
                      <HubStatusBadge status={hub.healthStatus} />
                    </td>
                    <td className="px-4 py-3 tabular-nums">
                      {hub.healthLatencyMs > 0 ? `${hub.healthLatencyMs}ms` : '—'}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground text-xs">
                      {hub.lastProbedAt
                        ? new Date(hub.lastProbedAt).toLocaleString()
                        : '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {totalPages > 1 && (
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">
                Page {page} of {totalPages} ({data.total} hubs)
              </span>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page <= 1}
                  onClick={() => setParam('page', String(page - 1))}
                >
                  <ChevronLeft className="h-4 w-4" />
                  Prev
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page >= totalPages}
                  onClick={() => setParam('page', String(page + 1))}
                >
                  Next
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/HubDirectory.tsx
git commit -m "feat: add Hub Directory page with search, filter, pagination

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 10: Frontend — Hub Detail page

**Files:**
- Create: `web/src/pages/HubDetail.tsx`

**Interfaces:**
- Consumes: `fetchHub`, `fetchHubHealth` from api.ts, `uptimePercent` from hub.ts

- [ ] **Step 1: Create `web/src/pages/HubDetail.tsx`**

```tsx
import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { ArrowLeft, ExternalLink, Clock } from 'lucide-react';

import { fetchHub, fetchHubHealth } from '../lib/api';
import { parseTags, uptimePercent, type TokenHub, type HealthProbe } from '../types/hub';
import HubStatusBadge from '../components/HubStatusBadge';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card';

export function HubDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [hub, setHub] = useState<TokenHub | null>(null);
  const [probes, setProbes] = useState<HealthProbe[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;

    async function load() {
      setLoading(true);
      setError(null);
      try {
        const [hubData, healthData] = await Promise.all([
          fetchHub(Number(id)),
          fetchHubHealth(Number(id)),
        ]);
        if (!cancelled) {
          setHub(hubData);
          setProbes(healthData.probes);
        }
      } catch (e) {
        if (!cancelled) setError(String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => { cancelled = true; };
  }, [id]);

  if (loading) return <div className="text-sm text-muted-foreground">Loading...</div>;
  if (error) return <div className="text-sm text-red-600">Error: {error}</div>;
  if (!hub) return <div className="text-sm text-muted-foreground">Hub not found.</div>;

  const tags = parseTags(hub.tags);
  const uptime = uptimePercent(probes);
  const modelsData = hub.modelsInfo ? JSON.parse(hub.modelsInfo) as Array<{ id: string; name: string; price: number }> : [];

  return (
    <div className="space-y-6">
      <Link to="/" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" />
        Back to directory
      </Link>

      <div>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-bold">{hub.name}</h1>
          <HubStatusBadge status={hub.healthStatus} />
        </div>
        <a
          href={hub.url}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm text-primary hover:underline mt-1"
        >
          {hub.url}
          <ExternalLink className="h-3 w-3" />
        </a>
      </div>

      {hub.description && <p className="text-muted-foreground">{hub.description}</p>}

      {tags.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {tags.map((t) => (
            <span key={t} className="text-xs bg-muted rounded px-2 py-0.5">{t}</span>
          ))}
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Uptime (7d)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-semibold tabular-nums">{uptime}%</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Latency</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-semibold tabular-nums">
              {hub.healthLatencyMs > 0 ? `${hub.healthLatencyMs}ms` : '—'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium text-muted-foreground">Last Probed</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-sm">
              {hub.lastProbedAt
                ? new Date(hub.lastProbedAt).toLocaleString()
                : 'Never'}
            </div>
          </CardContent>
        </Card>
      </div>

      {probes.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Clock className="h-4 w-4" />
              Health Timeline (last 24h)
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex gap-0.5 h-16 items-end">
              {probes.map((p) => (
                <div
                  key={p.id}
                  title={`${new Date(p.probedAt).toLocaleTimeString()}: ${p.online ? 'Online' : 'Offline'} (${p.latencyMs}ms)`}
                  className={`flex-1 rounded-sm min-h-1 ${
                    p.online ? 'bg-green-500' : 'bg-red-500'
                  }`}
                  style={{ height: p.online ? '100%' : '25%' }}
                />
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {modelsData.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Available Models</CardTitle>
          </CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  <th className="text-left py-2 font-medium">Model</th>
                  <th className="text-left py-2 font-medium">Model ID</th>
                  <th className="text-right py-2 font-medium">Price</th>
                </tr>
              </thead>
              <tbody>
                {modelsData.map((m) => (
                  <tr key={m.id} className="border-b last:border-0">
                    <td className="py-2">{m.name || m.id}</td>
                    <td className="py-2 text-muted-foreground">{m.id}</td>
                    <td className="py-2 text-right tabular-nums">{m.price > 0 ? `$${m.price}` : 'Free'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/HubDetail.tsx
git commit -m "feat: add Hub Detail page with health timeline and models

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 11: Frontend — Submit Hub page

**Files:**
- Create: `web/src/pages/SubmitHub.tsx`

**Interfaces:**
- Consumes: `submitHub` from api.ts

- [ ] **Step 1: Create `web/src/pages/SubmitHub.tsx`**

```tsx
import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';

import { submitHub } from '../lib/api';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';

export function SubmitHubPage() {
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [description, setDescription] = useState('');
  const [tagsInput, setTagsInput] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSubmitting(true);

    try {
      const tags = tagsInput
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean);
      await submitHub({
        name: name.trim(),
        url: url.trim(),
        description: description.trim() || undefined,
        tags: JSON.stringify(tags),
      });
      setSuccess(true);
    } catch (err) {
      setError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  if (success) {
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-bold">Submitted!</h1>
        <p className="text-muted-foreground">
          Your hub has been submitted for review. An admin will approve it shortly.
        </p>
        <div className="flex gap-3">
          <Button asChild variant="outline">
            <Link to="/">Back to Directory</Link>
          </Button>
          <Button onClick={() => { setSuccess(false); setName(''); setUrl(''); setDescription(''); setTagsInput(''); }}>
            Submit Another
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-lg mx-auto space-y-6">
      <Link to="/" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ArrowLeft className="h-4 w-4" />
        Back to directory
      </Link>

      <div>
        <h1 className="text-2xl font-bold">Submit a Token Hub</h1>
        <p className="text-muted-foreground mt-1">
          Add a new-api token hub to the directory. It will be reviewed before appearing publicly.
        </p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="name">Hub Name *</Label>
          <Input
            id="name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My Token Hub"
            required
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="url">Hub URL *</Label>
          <Input
            id="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://api.example.com"
            required
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="description">Description</Label>
          <textarea
            id="description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Brief description of this hub..."
            rows={3}
            className="flex w-full rounded-md border bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="tags">Tags (comma-separated)</Label>
          <Input
            id="tags"
            value={tagsInput}
            onChange={(e) => setTagsInput(e.target.value)}
            placeholder="gpt-4, vision, cheap"
          />
        </div>

        {error && <div className="text-sm text-red-600">Error: {error}</div>}

        <Button type="submit" disabled={submitting}>
          {submitting ? 'Submitting...' : 'Submit for Review'}
        </Button>
      </form>
    </div>
  );
}
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/SubmitHub.tsx
git commit -m "feat: add Submit Hub page with form

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 12: Frontend — Admin pages (Submissions and Hubs management)

**Files:**
- Create: `web/src/pages/AdminSubmissions.tsx`
- Create: `web/src/pages/AdminHubs.tsx`

**Interfaces:**
- Consumes: `fetchSubmissions`, `approveSubmission`, `rejectSubmission`, `fetchAdminHubs`, `deleteHub` from api.ts

- [ ] **Step 1: Create `web/src/pages/AdminSubmissions.tsx`**

```tsx
import { useEffect, useState } from 'react';
import { Check, X, Clock } from 'lucide-react';

import { fetchSubmissions, approveSubmission, rejectSubmission } from '../lib/api';
import type { TokenHub } from '../types/hub';
import { Button } from '../components/ui/button';

export function AdminSubmissionsPage() {
  const [submissions, setSubmissions] = useState<TokenHub[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchSubmissions();
      setSubmissions(data.submissions);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const handleApprove = async (id: number) => {
    try {
      await approveSubmission(id);
      setSubmissions((prev) => prev.filter((s) => s.id !== id));
    } catch (e) {
      setError(String(e));
    }
  };

  const handleReject = async (id: number) => {
    try {
      await rejectSubmission(id);
      setSubmissions((prev) => prev.filter((s) => s.id !== id));
    } catch (e) {
      setError(String(e));
    }
  };

  if (loading) return <div className="text-sm text-muted-foreground">Loading...</div>;
  if (error) return <div className="text-sm text-red-600">Error: {error}</div>;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold flex items-center gap-2">
          <Clock className="h-5 w-5" />
          Pending Submissions
        </h1>
        <p className="text-muted-foreground mt-1">Review and approve or reject submitted hubs.</p>
      </div>

      {submissions.length === 0 ? (
        <div className="text-sm text-muted-foreground py-8 text-center">No pending submissions.</div>
      ) : (
        <div className="border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-3 font-medium">Name</th>
                <th className="text-left px-4 py-3 font-medium">URL</th>
                <th className="text-left px-4 py-3 font-medium">Description</th>
                <th className="text-left px-4 py-3 font-medium">Submitted</th>
                <th className="text-right px-4 py-3 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {submissions.map((hub) => (
                <tr key={hub.id} className="border-t hover:bg-muted/30">
                  <td className="px-4 py-3 font-medium">{hub.name}</td>
                  <td className="px-4 py-3 text-muted-foreground">{hub.url}</td>
                  <td className="px-4 py-3 text-muted-foreground max-w-xs truncate">
                    {hub.description || '—'}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground text-xs">
                    {new Date(hub.createdAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-2">
                      <Button size="sm" variant="outline" onClick={() => handleApprove(hub.id)}>
                        <Check className="h-4 w-4 text-green-600" />
                        Approve
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => handleReject(hub.id)}>
                        <X className="h-4 w-4 text-red-600" />
                        Reject
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Create `web/src/pages/AdminHubs.tsx`**

```tsx
import { useEffect, useState } from 'react';
import { Trash2, Server } from 'lucide-react';

import { fetchAdminHubs, deleteHub } from '../lib/api';
import { parseTags, type TokenHub } from '../types/hub';
import HubStatusBadge from '../components/HubStatusBadge';
import { Button } from '../components/ui/button';

export function AdminHubsPage() {
  const [hubs, setHubs] = useState<TokenHub[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await fetchAdminHubs();
      setHubs(data.hubs);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const handleDelete = async (id: number) => {
    if (!confirm('Delete this hub?')) return;
    try {
      await deleteHub(id);
      setHubs((prev) => prev.filter((h) => h.id !== id));
    } catch (e) {
      setError(String(e));
    }
  };

  if (loading) return <div className="text-sm text-muted-foreground">Loading...</div>;
  if (error) return <div className="text-sm text-red-600">Error: {error}</div>;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold flex items-center gap-2">
          <Server className="h-5 w-5" />
          All Hubs
        </h1>
        <p className="text-muted-foreground mt-1">Manage all token hubs in the directory.</p>
      </div>

      {hubs.length === 0 ? (
        <div className="text-sm text-muted-foreground py-8 text-center">No hubs yet.</div>
      ) : (
        <div className="border rounded-lg overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="text-left px-4 py-3 font-medium">Name</th>
                <th className="text-left px-4 py-3 font-medium">URL</th>
                <th className="text-left px-4 py-3 font-medium">Status</th>
                <th className="text-left px-4 py-3 font-medium">Health</th>
                <th className="text-right px-4 py-3 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {hubs.map((hub) => (
                <tr key={hub.id} className="border-t hover:bg-muted/30">
                  <td className="px-4 py-3">
                    <div className="font-medium">{hub.name}</div>
                    <div className="flex flex-wrap gap-1 mt-0.5">
                      {parseTags(hub.tags).map((t) => (
                        <span key={t} className="text-xs bg-muted rounded px-1.5 py-0.5">{t}</span>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-muted-foreground max-w-[200px] truncate">{hub.url}</td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium rounded px-2 py-0.5 ${
                      hub.status === 'approved' ? 'bg-green-100 text-green-800' :
                      hub.status === 'rejected' ? 'bg-red-100 text-red-800' :
                      'bg-yellow-100 text-yellow-800'
                    }`}>
                      {hub.status}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    <HubStatusBadge status={hub.healthStatus} />
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end">
                      <Button size="sm" variant="ghost" onClick={() => handleDelete(hub.id)}>
                        <Trash2 className="h-4 w-4 text-red-500" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/AdminSubmissions.tsx web/src/pages/AdminHubs.tsx
git commit -m "feat: add admin pages for submissions and hub management

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 13: Frontend — Update App.tsx routing, sidebar, and admin dashboard

**Files:**
- Modify: `web/src/App.tsx` — add public routes, admin routes, update sidebar and dashboard

**Interfaces:**
- Consumes: all page components, PublicLayout, fetchDashboard

- [ ] **Step 1: Rewrite `web/src/App.tsx`**

Replace the entire content of `web/src/App.tsx` with:

```tsx
import { useState } from 'react'
import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { BarChart3, Settings, Users, Server, Clock } from 'lucide-react'

import { cn } from './lib/utils'
import { GlobalProbabilityPage } from './pages/GlobalProbability'
import { SystemConfigPage } from './pages/SystemConfig'
import UserManagement from './pages/UserManagement'
import Login from './pages/Login'
import PrivateRoute from './components/PrivateRoute'
import PublicLayout from './components/PublicLayout'
import { HubDirectoryPage } from './pages/HubDirectory'
import { HubDetailPage } from './pages/HubDetail'
import { SubmitHubPage } from './pages/SubmitHub'
import { AdminSubmissionsPage } from './pages/AdminSubmissions'
import { AdminHubsPage } from './pages/AdminHubs'
import { Toaster } from './components/ui/toaster'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "./components/ui/alert-dialog"

const sidebarSections = [
  {
    title: 'Overview',
    items: [
      { to: '/dashboard', label: 'Dashboard', icon: BarChart3 },
    ]
  },
  {
    title: 'Hubs',
    items: [
      { to: '/submissions', label: 'Submissions', icon: Clock },
      { to: '/admin-hubs', label: 'Manage Hubs', icon: Server },
    ]
  },
  {
    title: 'Admin',
    items: [
      { to: '/user-management', label: 'Users', icon: Users },
      { to: '/system-config', label: 'Settings', icon: Settings },
    ]
  }
]

function AppLayout() {
  const [showLogoutConfirm, setShowLogoutConfirm] = useState(false)

  const handleLogout = async () => {
    try {
      await fetch('/api/logout', { method: 'POST' });
      window.location.href = '/login';
    } catch (err) {
      console.error('Logout failed:', err);
    }
  };

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="flex h-14 items-center justify-between border-b bg-card px-4">
        <div className="flex items-center gap-3">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/10">
            <BarChart3 className="h-4 w-4 text-primary" />
          </div>
          <div className="leading-tight">
            <div className="text-sm font-semibold">Admin Dashboard</div>
            <div className="text-xs text-muted-foreground">Token Hub Management</div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <div
            className="flex h-9 w-9 items-center justify-center rounded-full border bg-background text-foreground cursor-pointer hover:bg-muted transition-colors"
            onClick={() => setShowLogoutConfirm(true)}
            title="Logout"
          >
            <Users className="h-4 w-4" />
          </div>
        </div>
      </header>

      <AlertDialog open={showLogoutConfirm} onOpenChange={setShowLogoutConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirm Logout</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to log out?
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleLogout}>Logout</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <div className="flex min-h-[calc(100vh-3.5rem)]">
        <aside className="flex w-64 shrink-0 flex-col border-r bg-card p-4">
          <nav className="space-y-6">
            {sidebarSections.map((section) => (
              <div key={section.title}>
                <h4 className="mb-2 px-3 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  {section.title}
                </h4>
                <div className="space-y-1">
                  {section.items.map((it) => (
                    <NavLink
                      key={it.to}
                      to={it.to}
                      className={({ isActive }) =>
                        cn(
                          'flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors',
                          isActive
                            ? 'bg-primary text-primary-foreground font-medium shadow-sm'
                            : 'text-foreground hover:bg-muted',
                        )
                      }
                    >
                      <it.icon className="h-4 w-4" />
                      <span>{it.label}</span>
                    </NavLink>
                  ))}
                </div>
              </div>
            ))}
          </nav>
          <div className="mt-auto border-t pt-4 text-xs text-muted-foreground">
            <div className="font-medium text-foreground">Token Hub Admin</div>
            <div>Management Console</div>
          </div>
        </aside>

        <main className="flex min-h-[calc(100vh-3.5rem)] flex-1 flex-col p-6 bg-slate-50">
          <div className="flex-1">
            <Routes>
              <Route path="/" element={<Navigate to="/dashboard" replace />} />
              <Route path="/dashboard" element={<GlobalProbabilityPage />} />
              <Route path="/submissions" element={<AdminSubmissionsPage />} />
              <Route path="/admin-hubs" element={<AdminHubsPage />} />
              <Route path="/system-config" element={<SystemConfigPage />} />
              <Route path="/user-management" element={<UserManagement />} />
              <Route path="*" element={<Navigate to="/dashboard" replace />} />
            </Routes>
          </div>
          <footer className="mt-8 border-t pt-4 text-xs text-muted-foreground">
            &copy; {new Date().getFullYear()} Token Hub Admin. All rights reserved.
          </footer>
        </main>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <>
      <Routes>
        {/* Public routes */}
        <Route path="/login" element={<Login />} />
        <Route element={<PublicLayout />}>
          <Route path="/" element={<HubDirectoryPage />} />
          <Route path="/hubs/:id" element={<HubDetailPage />} />
          <Route path="/submit" element={<SubmitHubPage />} />
        </Route>

        {/* Admin routes */}
        <Route
          path="/*"
          element={
            <PrivateRoute>
              <AppLayout />
            </PrivateRoute>
          }
        />
      </Routes>
      <Toaster />
    </>
  )
}
```

- [ ] **Step 2: Update `GlobalProbabilityPage` to show hub stats**

Replace the content of `web/src/pages/GlobalProbability.tsx` with:

```tsx
import { useEffect, useState } from 'react'
import { Activity, BarChart3, Server, Clock } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/card'
import { fetchDashboard } from '../lib/api'
import type { HubStats } from '../types/hub'

export function GlobalProbabilityPage() {
  const [data, setData] = useState<HubStats | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetchDashboard()
      .then((d) => { if (!cancelled) setData(d) })
      .catch((e) => { if (!cancelled) setError(String(e)) })
    return () => { cancelled = true }
  }, [])

  if (error) return <div className="text-sm text-red-600">Error: {error}</div>

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2 text-xl font-medium text-muted-foreground bg-white p-4 rounded-lg border shadow-sm">
        <BarChart3 className="h-5 w-5" />
        Dashboard
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Server className="h-4 w-4 text-blue-500" />
              Total Hubs
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-semibold tabular-nums">
              {data ? data.total_hubs : '—'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Activity className="h-4 w-4 text-green-500" />
              Online
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-semibold tabular-nums">
              {data ? data.online_hubs : '—'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Activity className="h-4 w-4 text-red-500" />
              Offline
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-semibold tabular-nums">
              {data ? data.offline_hubs : '—'}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Clock className="h-4 w-4 text-yellow-500" />
              Pending
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-semibold tabular-nums">
              {data ? data.pending_hubs : '—'}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Run a full build to verify everything compiles together**

```bash
cd web && npm run build
```

Expected: build succeeds, no TypeScript errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/pages/GlobalProbability.tsx
git commit -m "feat: wire up routing, sidebar, and admin dashboard with hub stats

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 14: End-to-end verification and final build

**Files:**
- Modify: `Makefile` — update cmd path reference

**Goal:** Verify the complete application builds and runs correctly.

- [ ] **Step 1: Update root Makefile for renamed cmd path**

In `Makefile` line 12, change `./cmd/willing` to `./cmd/tokenhub`:

```
CGO_ENABLED=$(CGO_ENABLED) $(GO) build -tags embed -o $(OUTDIR)/$(BIN) ./cmd/tokenhub
```

Also on line 17 (dev target), change `./cmd/willing` to `./cmd/tokenhub`:

```
$(GO) run ./cmd/tokenhub serve
```

- [ ] **Step 2: Build the Go backend**

```bash
go build -o bin/app ./cmd/tokenhub
```

Expected: no errors.

- [ ] **Step 3: Build the frontend**

```bash
cd web && npm run build
```

Expected: build succeeds.

- [ ] **Step 4: Run the full application in dev mode**

```bash
go run ./cmd/tokenhub serve
```

Expected: server starts on `:8080`. Visit `http://localhost:8080/` — should see the hub directory page (empty). Visit `http://localhost:8080/login` — should see the login page. Log in with admin/admin.

- [ ] **Step 5: Manual smoke test**

With the server running:

```bash
# Test: submit a hub
curl -X POST http://localhost:8080/api/hubs/submit \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test Hub","url":"https://example.com","description":"A test","tags":"[\"test\"]"}'

# Test: list hubs (should be empty since not approved)
curl http://localhost:8080/api/hubs

# Test: health endpoint
curl http://localhost:8080/api/health
```

Expected: submit returns 201 with the hub, list returns empty (pending), health returns ok.

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "chore: update Makefile for renamed cmd path, final wiring

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

### Task 15: Add basic Go tests

**Files:**
- Create: `internal/db/hub_test.go`
- Create: `internal/admin/hub_public_test.go`

- [ ] **Step 1: Create `internal/db/hub_test.go`**

```go
package db

import (
	"context"
	"testing"
	"time"

	"tokenhub/internal/models"
)

func TestHubCRUD(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenConfig{Driver: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	// Create
	hub, err := store.CreateHub(ctx, models.TokenHub{
		Name: "Test Hub",
		URL:  "https://example.com",
	})
	if err != nil {
		t.Fatalf("create hub: %v", err)
	}
	if hub.ID == 0 {
		t.Fatal("expected hub ID to be set")
	}
	if hub.Status != "pending" {
		t.Fatalf("expected pending status, got %s", hub.Status)
	}

	// Get
	got, err := store.GetHubByID(ctx, hub.ID)
	if err != nil {
		t.Fatalf("get hub: %v", err)
	}
	if got.Name != "Test Hub" {
		t.Fatalf("expected 'Test Hub', got %q", got.Name)
	}

	// Approve
	if err := store.ApproveHub(ctx, hub.ID); err != nil {
		t.Fatalf("approve hub: %v", err)
	}
	got, _ = store.GetHubByID(ctx, hub.ID)
	if got.Status != "approved" {
		t.Fatalf("expected approved, got %s", got.Status)
	}

	// List approved
	hubs, total, err := store.ListApprovedHubs(ctx, ListHubsOptions{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatalf("list hubs: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected 1 hub, got %d", total)
	}
	if len(hubs) != 1 {
		t.Fatalf("expected 1 hub in result, got %d", len(hubs))
	}

	// Delete
	if err := store.DeleteHub(ctx, hub.ID); err != nil {
		t.Fatalf("delete hub: %v", err)
	}
	_, err = store.GetHubByID(ctx, hub.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestHubHealthProbes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenConfig{Driver: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	hub, _ := store.CreateHub(ctx, models.TokenHub{Name: "H", URL: "https://x.com"})
	_ = store.ApproveHub(ctx, hub.ID)

	// Update health
	if err := store.UpdateHubHealth(ctx, hub.ID, true, 42, ""); err != nil {
		t.Fatalf("update health: %v", err)
	}

	got, _ := store.GetHubByID(ctx, hub.ID)
	if got.HealthStatus != "online" {
		t.Fatalf("expected online, got %s", got.HealthStatus)
	}

	// Insert probe
	now := time.Now().UTC()
	probe := models.HealthProbe{HubID: hub.ID, Online: true, LatencyMs: 42, ProbedAt: now}
	if err := store.InsertHealthProbe(ctx, probe); err != nil {
		t.Fatalf("insert probe: %v", err)
	}

	probes, err := store.GetHealthProbes(ctx, hub.ID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("get probes: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}
}
```

Add `"time"` to the import block.

Actually, the probe timestamp needs to be handled more carefully. Let me fix:

```go
// Insert probe
now := time.Now().UTC()
probe := models.HealthProbe{HubID: hub.ID, Online: true, LatencyMs: 42, ProbedAt: now}
if err := store.InsertHealthProbe(ctx, probe); err != nil {
	t.Fatalf("insert probe: %v", err)
}

probes, err := store.GetHealthProbes(ctx, hub.ID, now.Add(-1*time.Hour))
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/db/ -v -run TestHub
```

Expected: both tests pass.

- [ ] **Step 3: Run all Go tests**

```bash
go test ./... -v
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/db/hub_test.go
git commit -m "test: add Go tests for hub CRUD and health probes

Co-Authored-By: Claude <noreply@anthropic.com>"
```

