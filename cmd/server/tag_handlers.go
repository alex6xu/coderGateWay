package server

import (
	"net/http"
	"strconv"

	"github.com/alex/codegateway/internal/agent/tags"
	"github.com/gin-gonic/gin"
)

func handleListTags(tagSvc *tags.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		kind := c.Query("kind")
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		list, err := tagSvc.ListTags(accountID, kind, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tags": list})
	}
}

func handleTagOverview(tagSvc *tags.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		topN, _ := strconv.Atoi(c.DefaultQuery("top", "12"))
		perTag, _ := strconv.Atoi(c.DefaultQuery("per_tag", "5"))
		groups, err := tagSvc.Overview(accountID, topN, perTag)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"groups": groups})
	}
}

func handleGetTagMessages(tagSvc *tags.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		slug := c.Param("slug")
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		group, err := tagSvc.ListMessagesByTag(accountID, slug, limit)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, group)
	}
}

func handleRetagMessages(tagSvc *tags.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		n, err := tagSvc.RetagRecent(accountID, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tagged": n})
	}
}
