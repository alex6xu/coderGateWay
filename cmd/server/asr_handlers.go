package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/gin-gonic/gin"
)

func handleASRStatus(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled := cfg.ASR.Enabled && strings.TrimSpace(cfg.ASR.BaseURL) != ""
		c.JSON(http.StatusOK, gin.H{
			"enabled":           enabled,
			"provider":          cfg.ASR.Provider,
			"model":             cfg.ASR.Model,
			"language":          cfg.ASR.Language,
			"browser_preferred": true,
		})
	}
}

// handleASR accepts multipart audio (field "file") and proxies to an
// OpenAI-compatible Whisper transcription endpoint (e.g. local faster-whisper / speaches).
func handleASR(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.ASR.Enabled || strings.TrimSpace(cfg.ASR.BaseURL) == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "服务端 ASR 未启用。默认请使用浏览器 Web Speech（Chrome）；或配置 asr.base_url 指向 Whisper 兼容服务。",
			})
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 25<<20)
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing audio file field 'file'"})
			return
		}
		defer file.Close()

		language := strings.TrimSpace(c.PostForm("language"))
		if language == "" {
			language = cfg.ASR.Language
		}
		if language == "" {
			language = "zh"
		}
		model := cfg.ASR.Model
		if model == "" {
			model = "whisper-1"
		}

		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		fw, err := w.CreateFormFile("file", sanitizeFilename(header.Filename))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if _, err := io.Copy(fw, file); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read upload"})
			return
		}
		_ = w.WriteField("model", model)
		_ = w.WriteField("language", language)
		_ = w.WriteField("response_format", "json")
		_ = w.Close()

		base := strings.TrimRight(cfg.ASR.BaseURL, "/")
		url := base + "/audio/transcriptions"
		req, err := http.NewRequestWithContext(c.Request.Context(), "POST", url, &body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		if cfg.ASR.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.ASR.APIKey)
		}

		client := &http.Client{Timeout: 120 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("ASR upstream error: %v", err)})
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": fmt.Sprintf("ASR upstream status %d: %s", resp.StatusCode, truncateStr(string(raw), 400)),
			})
			return
		}

		// OpenAI-compatible: {"text":"..."}
		text := extractJSONText(raw)
		if text == "" {
			text = strings.TrimSpace(string(raw))
		}
		c.JSON(http.StatusOK, gin.H{
			"text":     text,
			"provider": cfg.ASR.Provider,
			"model":    model,
		})
	}
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" {
		return "speech.webm"
	}
	return name
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func extractJSONText(raw []byte) string {
	type resp struct {
		Text string `json:"text"`
	}
	var r resp
	if err := json.Unmarshal(raw, &r); err != nil {
		return ""
	}
	return strings.TrimSpace(r.Text)
}
