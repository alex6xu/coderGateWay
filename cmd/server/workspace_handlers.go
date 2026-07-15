package server

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alex/codegateway/internal/workspace"
	"github.com/gin-gonic/gin"
)

func handleListWorkspaces(mgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		list, err := mgr.List(accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list workspaces"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"workspaces": list})
	}
}

func handleGetWorkspace(mgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		ws, err := mgr.Get(accountID, c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"workspace": ws})
	}
}

func handleDeleteWorkspace(mgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if err := mgr.Delete(accountID, c.Param("id")); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "deleted"})
	}
}

func handleWorkspaceTree(mgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		ws, err := mgr.Get(accountID, c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
			return
		}
		path := c.Query("path")
		recursive := c.Query("recursive") != "0"
		entries, err := mgr.ListTree(ws, path, recursive)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"entries": entries})
	}
}

func handleDownloadWorkspace(mgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		ws, err := mgr.Get(accountID, c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
			return
		}
		filename := ws.Name + ".zip"
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		if err := mgr.ZipTo(ws, c.Writer); err != nil {
			// headers may already be sent
			return
		}
	}
}

// handleUploadWorkspace accepts multipart files with relative paths (webkitdirectory)
// or a single zip archive field named "archive".
func handleUploadWorkspace(mgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		name := strings.TrimSpace(c.PostForm("name"))
		if name == "" {
			name = "project"
		}

		// Cap upload size ~200MB
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 200<<20)
		if err := c.Request.ParseMultipartForm(200 << 20); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form or upload too large"})
			return
		}

		ws, err := mgr.CreateEmpty(accountID, name)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Prefer zip archive if present
		if file, err := c.FormFile("archive"); err == nil && file != nil {
			f, err := file.Open()
			if err != nil {
				_ = mgr.Delete(accountID, ws.ID)
				c.JSON(http.StatusBadRequest, gin.H{"error": "failed to open archive"})
				return
			}
			defer f.Close()

			tmpZip := filepath.Join(ws.RootPath, ".upload.zip")
			out, err := createFile(tmpZip)
			if err != nil {
				f.Close()
				_ = mgr.Delete(accountID, ws.ID)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if _, err := io.Copy(out, f); err != nil {
				out.Close()
				_ = mgr.Delete(accountID, ws.ID)
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			out.Close()

			if err := unzipInto(tmpZip, ws.RootPath); err != nil {
				_ = mgr.Delete(accountID, ws.ID)
				c.JSON(http.StatusBadRequest, gin.H{"error": "failed to extract zip: " + err.Error()})
				return
			}
			_ = removeFile(tmpZip)
		} else {
			form := c.Request.MultipartForm
			files := form.File["files"]
			if len(files) == 0 {
				// also accept "file"
				files = form.File["file"]
			}
			if len(files) == 0 {
				_ = mgr.Delete(accountID, ws.ID)
				c.JSON(http.StatusBadRequest, gin.H{"error": "no files uploaded; select a directory or zip"})
				return
			}

			for _, fh := range files {
				rel := fh.Filename
				if rel == "" {
					continue
				}
				// Skip junk
				base := filepath.Base(rel)
				if base == ".DS_Store" || strings.HasPrefix(base, "._") {
					continue
				}
				src, err := fh.Open()
				if err != nil {
					continue
				}
				err = mgr.WriteRelativeFile(ws, rel, src)
				src.Close()
				if err != nil {
					_ = mgr.Delete(accountID, ws.ID)
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
			}
		}

		if err := mgr.RefreshStats(ws); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":   "uploaded",
			"workspace": ws,
		})
	}
}
