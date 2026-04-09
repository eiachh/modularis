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
