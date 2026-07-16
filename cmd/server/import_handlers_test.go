package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestReadImportMarkdownJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/p", func(c *gin.Context) {
		content, title, err := readImportMarkdown(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"content_len": len(content), "title": title})
	})

	payload, _ := json.Marshal(map[string]string{
		"content": "# Demo\n\n## User\n你好\n\n## Assistant\n好的\n",
		"title":   "自定义标题",
	})
	req := httptest.NewRequest("POST", "/p", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"title":"自定义标题"`)) {
		t.Fatalf("body=%s", w.Body.String())
	}
}
