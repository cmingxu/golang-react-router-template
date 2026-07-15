package admin

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/sessions"

	"tokenhub/internal/db"
	"tokenhub/internal/models"
	"tokenhub/webui"
)

var store = sessions.NewCookieStore([]byte("change-me-to-a-random-secret"))

func isSecureRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Ssl"), "on") {
		return true
	}
	return false
}

func authMiddleware(cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = cfg

		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") {
			if p == "/api/login" {
				c.Next()
				return
			}

			session, _ := store.Get(c.Request, "session-name")
			if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				c.Abort()
				return
			}

			var userID int64
			switch v := session.Values["userID"].(type) {
			case int64:
				userID = v
			case int:
				userID = int64(v)
			case int32:
				userID = int64(v)
			case float64:
				userID = int64(v)
			}
			if userID <= 0 {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				c.Abort()
				return
			}

			c.Set("userID", userID)
			c.Next()
			return
		}

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
	}
}

type Config struct {
	DB *db.Store
}

func New(cfg Config) http.Handler {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(authMiddleware(cfg))

	r.POST("/api/login", func(c *gin.Context) {
		var req struct {
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		user, err := cfg.DB.GetUserByNickname(c.Request.Context(), req.Nickname)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		if user.Password != req.Password {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		session, _ := store.Get(c.Request, "session-name")
		session.Values["authenticated"] = true
		session.Values["userID"] = user.ID
		secure := isSecureRequest(c.Request)
		session.Options = &sessions.Options{
			Path:     "/",
			MaxAge:   86400 * 30,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		}
		if err := session.Save(c.Request, c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save session"})
			return
		}

		log.Printf("[Auth] User '%s' logged in successfully from %s", user.Nickname, c.ClientIP())
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	r.POST("/api/logout", func(c *gin.Context) {
		session, _ := store.Get(c.Request, "session-name")
		session.Values["authenticated"] = false
		secure := isSecureRequest(c.Request)
		session.Options = &sessions.Options{
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		}
		if err := session.Save(c.Request, c.Writer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save session"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	api := r.Group("/api")

	api.GET("/me", func(c *gin.Context) {
		userID, exists := c.Get("userID")
		if !exists {
			session, _ := store.Get(c.Request, "session-name")
			if uid, ok := session.Values["userID"].(int64); ok {
				userID = uid
			}
		}
		c.JSON(http.StatusOK, gin.H{"userID": userID})
	})

	api.GET("/users", func(c *gin.Context) {
		users, err := cfg.DB.ListUsers(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, users)
	})

	api.POST("/users", func(c *gin.Context) {
		var req struct {
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		user, err := cfg.DB.CreateUser(c.Request.Context(), models.User{
			Nickname: req.Nickname,
			Password: req.Password,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, user)
	})

	api.PUT("/users/:id/password", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
			return
		}

		var req struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := cfg.DB.UpdateUserPassword(c.Request.Context(), id, req.Password); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	api.DELETE("/users/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user id"})
			return
		}

		if err := cfg.DB.DeleteUser(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	r.GET("/api/health", func(c *gin.Context) {
		status := gin.H{
			"ok":   true,
			"time": time.Now().UTC().Format(time.RFC3339Nano),
		}
		if cfg.DB != nil {
			if err := cfg.DB.Ping(c.Request.Context()); err != nil {
				status["db_ok"] = false
				status["db_error"] = err.Error()
				c.JSON(http.StatusServiceUnavailable, status)
				return
			}
			status["db_ok"] = true
		} else {
			status["db_ok"] = nil
		}
		c.JSON(http.StatusOK, status)
	})

	api.GET("/dashboard", func(c *gin.Context) {
		var userCount int64
		if cfg.DB != nil {
			s := cfg.DB
			users, err := s.ListUsers(c.Request.Context())
			if err == nil {
				userCount = int64(len(users))
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"user_count": userCount,
			"time":       time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	api.GET("/system-config", func(c *gin.Context) {
		if cfg.DB == nil {
			c.JSON(http.StatusOK, gin.H{"items": gin.H{}})
			return
		}
		sc, err := cfg.DB.GetSystemConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"items": map[string]string{
				"warn_text": sc.WarnText,
			},
		})
	})

	type setConfigRequest struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	api.PUT("/system-config", func(c *gin.Context) {
		if cfg.DB == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "db not configured"})
			return
		}
		var req setConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var upd db.SystemConfigUpdate
		switch strings.ToLower(strings.TrimSpace(req.Key)) {
		case "warn_text":
			upd.WarnText = &req.Value
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown key"})
			return
		}

		if _, err := cfg.DB.UpdateSystemConfig(c.Request.Context(), upd); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	webui.Register(r)

	return r
}
