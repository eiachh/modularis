package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/eiachh/Modularis/internal/auth"
)

// AuthHandler provides HTTP endpoints for authentication-related features.
type AuthHandler struct {
	SUManager *auth.SUManager
}

// HandleGenerateSUToken handles POST /su/token.
// On first call: generates, stores, and returns the SU token.
// On subsequent calls: returns 409 Conflict indicating the token already exists.
func (h *AuthHandler) HandleGenerateSUToken(c *gin.Context) {
	if h.SUManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "SU manager not configured"})
		return
	}

	token, err := h.SUManager.GenerateSUToken()
	if err != nil {
		if err.Error() == "SU token already generated" {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token": token,
	})
}

// HandleGenerateDefaultToken handles POST /token.
// Generates and returns a default/guest token (opaque, no permissions).
// Clients use this token for identification; SU grants permissions via policy.
func (h *AuthHandler) HandleGenerateDefaultToken(c *gin.Context) {
	if h.SUManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "SU manager not configured"})
		return
	}
	token, err := h.SUManager.GenerateDefaultToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"token": token,
	})
}

// HandleListTokens handles GET /tokens (SU only).
// Returns all generated client tokens for grant management.
func (h *AuthHandler) HandleListTokens(c *gin.Context) {
	if h.SUManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "SU manager not configured"})
		return
	}

	// Require SU token
	authz := c.GetHeader("Authorization")
	if authz == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
		return
	}
	const prefix = "Bearer "
	if len(authz) < len(prefix) || authz[:len(prefix)] != prefix {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization format"})
		return
	}
	token := authz[len(prefix):]
	if !h.SUManager.IsSUToken(token) {
		c.JSON(http.StatusForbidden, gin.H{"error": "SU token required"})
		return
	}

	// Get all tokens
	tokens := h.SUManager.ListTokens()

	// Also include SU token if generated
	suTokenInfo := h.SUManager.GetSUTokenInfo()
	if suTokenInfo != nil {
		tokens = append([]auth.TokenInfo{*suTokenInfo}, tokens...)
	}

	c.JSON(http.StatusOK, gin.H{"tokens": tokens})
}
