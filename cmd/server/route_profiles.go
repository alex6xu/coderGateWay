package server

import (
    "errors"
    "net/http"
    "strconv"

    "github.com/alex/codegateway/internal/db"
    "github.com/alex/codegateway/internal/gateway/profile"
    "github.com/gin-gonic/gin"
)

type routeProfileRequest struct {
    Name string `json:"name"`
    Purpose profile.Purpose `json:"purpose"`
    Models []string `json:"models"`
}

func routeProfileStore(database *db.DB) *profile.Store { return profile.NewStore(database.DB) }

func handleListRouteProfiles(database *db.DB) gin.HandlerFunc { return func(c *gin.Context) {
    accountID, ok := requireAccountID(c); if !ok { return }
    profiles, err := routeProfileStore(database).List(accountID)
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list route profiles"}); return }
    c.JSON(http.StatusOK, gin.H{"route_profiles": profiles})
} }

func handleCreateRouteProfile(database *db.DB) gin.HandlerFunc { return func(c *gin.Context) {
    accountID, ok := requireAccountID(c); if !ok { return }
    var req routeProfileRequest; if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route profile"}); return }
    created, err := routeProfileStore(database).Create(accountID, profile.CreateInput{Name: req.Name, Purpose: req.Purpose, Models: req.Models})
    if errors.Is(err, profile.ErrConflict) { c.JSON(http.StatusConflict, gin.H{"error": "route profile already exists"}); return }
    if errors.Is(err, profile.ErrInvalid) { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create route profile"}); return }
    c.JSON(http.StatusCreated, created)
} }

func handleUpdateRouteProfile(database *db.DB) gin.HandlerFunc { return func(c *gin.Context) {
    accountID, ok := requireAccountID(c); if !ok { return }
    id, err := strconv.ParseInt(c.Param("id"), 10, 64); if err != nil || id < 1 { c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route profile id"}); return }
    var req routeProfileRequest; if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route profile"}); return }
    updated, err := routeProfileStore(database).Update(accountID, id, profile.CreateInput{Name: req.Name, Purpose: req.Purpose, Models: req.Models})
    if errors.Is(err, profile.ErrNotFound) { c.JSON(http.StatusNotFound, gin.H{"error": "route profile not found"}); return }
    if errors.Is(err, profile.ErrConflict) { c.JSON(http.StatusConflict, gin.H{"error": "route profile already exists"}); return }
    if errors.Is(err, profile.ErrInvalid) { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update route profile"}); return }
    c.JSON(http.StatusOK, updated)
} }

func handleDeleteRouteProfile(database *db.DB) gin.HandlerFunc { return func(c *gin.Context) {
    accountID, ok := requireAccountID(c); if !ok { return }
    id, err := strconv.ParseInt(c.Param("id"), 10, 64); if err != nil || id < 1 { c.JSON(http.StatusBadRequest, gin.H{"error": "invalid route profile id"}); return }
    err = routeProfileStore(database).Delete(accountID, id)
    if errors.Is(err, profile.ErrNotFound) { c.JSON(http.StatusNotFound, gin.H{"error": "route profile not found"}); return }
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete route profile"}); return }
    c.Status(http.StatusNoContent)
} }
