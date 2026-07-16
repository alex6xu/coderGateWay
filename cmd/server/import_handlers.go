package server

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/agent/tags"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/importexport/mdchat"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// handleImportMDPreview parses markdown without writing to DB.
func handleImportMDPreview() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := requireAccountID(c); !ok {
			return
		}
		content, titleHint, err := readImportMarkdown(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		parsed := mdchat.Parse(content)
		if titleHint != "" {
			parsed.Title = titleHint
		}
		if err := mdchat.Validate(parsed); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "parsed": parsed})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"title":         parsed.Title,
			"message_count": len(parsed.Messages),
			"messages":      parsed.Messages,
		})
	}
}

// handleImportMDSession imports a markdown transcript as a new session.
func handleImportMDSession(database *db.DB, tagSvc *tags.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		content, titleHint, err := readImportMarkdown(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		parsed := mdchat.Parse(content)
		if titleHint != "" {
			parsed.Title = titleHint
		}
		if err := mdchat.Validate(parsed); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		doTag := true
		if v := strings.TrimSpace(c.PostForm("tag")); v == "0" || strings.EqualFold(v, "false") {
			doTag = false
		}
		// JSON body may include tag flag
		if c.ContentType() == "application/json" {
			// already consumed? readImportMarkdown handles json — tag from query
			if v := c.Query("tag"); v == "0" || strings.EqualFold(v, "false") {
				doTag = false
			}
		}

		sessionID := uuid.NewString()
		now := time.Now()
		title := parsed.Title
		if title == "" {
			title = "Imported chat"
		}
		if len([]rune(title)) > 80 {
			title = string([]rune(title)[:80])
		}

		_, err = database.Exec(`
			INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at)
			VALUES (?, ?, ?, 'import', 0, ?, ?)
		`, sessionID, accountID, title, now, now)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
			return
		}

		count := 0
		base := now.Add(-time.Duration(len(parsed.Messages)) * time.Second)
		for i, m := range parsed.Messages {
			role := m.Role
			if role != "user" && role != "assistant" && role != "system" {
				continue
			}
			msgID := uuid.NewString()
			ts := base.Add(time.Duration(i) * time.Second)
			_, err := database.Exec(`
				INSERT INTO messages (id, session_id, role, content, provider, created_at)
				VALUES (?, ?, ?, ?, 'md-import', ?)
			`, msgID, sessionID, role, m.Content, ts)
			if err != nil {
				continue
			}
			count++
			if doTag && role == "user" && tagSvc != nil {
				_, _ = tagSvc.TagMessage(accountID, msgID, m.Content)
			}
		}

		_, _ = database.Exec(`
			UPDATE sessions SET message_count = ?, updated_at = ? WHERE id = ?
		`, count, now, sessionID)

		c.JSON(http.StatusOK, gin.H{
			"session": gin.H{
				"id":            sessionID,
				"title":         title,
				"platform":      "import",
				"message_count": count,
				"created_at":    now,
				"updated_at":    now,
			},
			"imported": count,
			"tags":     doTag,
		})
	}
}

func readImportMarkdown(c *gin.Context) (content, title string, err error) {
	ct := c.ContentType()
	if strings.HasPrefix(ct, "multipart/") {
		file, err := c.FormFile("file")
		if err != nil {
			// try raw field
			content = strings.TrimSpace(c.PostForm("content"))
			title = strings.TrimSpace(c.PostForm("title"))
			if content == "" {
				return "", "", errMissingMD
			}
			return content, title, nil
		}
		f, err := file.Open()
		if err != nil {
			return "", "", err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 8<<20))
		if err != nil {
			return "", "", err
		}
		content = string(raw)
		title = strings.TrimSpace(c.PostForm("title"))
		if title == "" {
			title = strings.TrimSuffix(file.Filename, ".md")
			title = strings.TrimSuffix(title, ".markdown")
		}
		return content, title, nil
	}

	var body struct {
		Content  string `json:"content"`
		Title    string `json:"title"`
		Markdown string `json:"markdown"`
		Tag      *bool  `json:"tag"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		return "", "", errMissingMD
	}
	content = strings.TrimSpace(body.Content)
	if content == "" {
		content = strings.TrimSpace(body.Markdown)
	}
	if content == "" {
		return "", "", errMissingMD
	}
	return content, strings.TrimSpace(body.Title), nil
}

type importErr string

func (e importErr) Error() string { return string(e) }

const errMissingMD importErr = "请提供 Markdown 内容（JSON content 字段或 multipart file）"
