package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/account"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/gin-gonic/gin"
)

func TestRequireSessionOrAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	mgr := account.NewManager(database.DB)
	admin, err := mgr.EnsureDefault()
	if err != nil {
		t.Fatal(err)
	}
	const apiKey = "sk-gateway-auth-test-key"
	_, err = database.Exec(`
		INSERT INTO tokens (user_id, name, key, status, remain_quota, unlimited_quota, created_at)
		VALUES (?, 'k', ?, 1, -1, 1, ?)
	`, admin.ID, apiKey, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := mgr.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	mw := requireSessionOrAPIKey(mgr)
	hit := func(headers map[string]string) int {
		r := gin.New()
		r.Use(mw)
		r.GET("/x", func(c *gin.Context) {
			id, ok := requireAuthUserID(c)
			if !ok {
				return
			}
			c.JSON(200, gin.H{"user_id": id})
		})
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := hit(nil); code != http.StatusUnauthorized {
		t.Fatalf("no creds: %d", code)
	}
	if code := hit(map[string]string{"Authorization": "Bearer " + sess.Token}); code != 200 {
		t.Fatalf("session bearer: %d", code)
	}
	if code := hit(map[string]string{"X-Session-Token": sess.Token}); code != 200 {
		t.Fatalf("session header: %d", code)
	}
	if code := hit(map[string]string{"Authorization": "Bearer " + apiKey}); code != 200 {
		t.Fatalf("api key bearer: %d", code)
	}
	if code := hit(map[string]string{"X-API-Key": apiKey}); code != 200 {
		t.Fatalf("x-api-key: %d", code)
	}
	if code := hit(map[string]string{"Authorization": "Bearer sk-wrong"}); code != http.StatusUnauthorized {
		t.Fatalf("bad key: %d", code)
	}
}
