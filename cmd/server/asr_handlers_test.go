package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alex/codegateway/internal/config"
	"github.com/gin-gonic/gin"
)

func TestHandleASRStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := &config.Config{ASR: config.ASRConfig{Enabled: false}}
	r.GET("/v1/asr/status", handleASRStatus(cfg))

	req := httptest.NewRequest("GET", "/v1/asr/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"browser_preferred":true`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestHandleASRDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := &config.Config{ASR: config.ASRConfig{Enabled: false}}
	r.POST("/v1/asr", handleASR(cfg))

	req := httptest.NewRequest("POST", "/v1/asr", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
