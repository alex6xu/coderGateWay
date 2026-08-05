package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gatewaylog"
	"github.com/gin-gonic/gin"
)

func gatewayLogStore(database *db.DB) *gatewaylog.Store {
	return gatewaylog.NewStore(database.DB)
}

func handleListRequestLogs(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		statusCode, _ := strconv.Atoi(c.Query("status"))
		filter := gatewaylog.ListFilter{
			Model:      strings.TrimSpace(c.Query("model")),
			StatusCode: statusCode,
			Limit:      limit,
			Offset:     offset,
		}
		logs, err := gatewayLogStore(database).List(accountID, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if logs == nil {
			logs = []gatewaylog.Entry{}
		}
		c.JSON(http.StatusOK, gin.H{"logs": logs})
	}
}

func handleGetRequestLog(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		id := c.Param("id")
		entry, err := gatewayLogStore(database).Get(accountID, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if entry == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "request log not found"})
			return
		}
		c.JSON(http.StatusOK, entry)
	}
}
