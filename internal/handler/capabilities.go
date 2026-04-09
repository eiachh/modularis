package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/eiachh/Modularis/internal/activitylog"
	"github.com/eiachh/Modularis/internal/auth"
	"github.com/eiachh/Modularis/internal/service"
	"github.com/eiachh/Modularis/pkg"
)

// CapabilitiesHandler serves capability discovery and invocation.
type CapabilitiesHandler struct {
	Service     *service.CapabilitiesService
	Log         *slog.Logger
	ActivityLog *activitylog.Log
	SUManager   *auth.SUManager // for validating token signatures (opaque tokens)
}

// Handle returns all registered capabilities.
func (h *CapabilitiesHandler) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, h.Service.ListSummaries())
}

// HandleListActivities returns all recorded activities from the activity log.
// If ActivityLog is not configured, returns an empty array.
func (h *CapabilitiesHandler) HandleListActivities(c *gin.Context) {
	if h.ActivityLog == nil {
		c.JSON(http.StatusOK, []activitylog.Activity{})
		return
	}
	c.JSON(http.StatusOK, h.ActivityLog.List())
}

// HandleInvokeResult blocks until the result for the given invocation ID is
// available, then returns it. If the store is not configured or ID not found,
// returns 404.
func (h *CapabilitiesHandler) HandleInvokeResult(c *gin.Context) {
	if h.Service.ResultStore == nil {
		c.JSON(http.StatusNotFound, map[string]any{"error": "result store not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, map[string]any{"error": "missing invocation id"})
		return
	}
	entry := h.Service.ResultStore.Get(id)
	if entry == nil {
		c.JSON(http.StatusNotFound, map[string]any{"error": "invocation not found"})
		return
	}
	// Block until result is ready
	entry.Wait()
	r := entry.Result()
	if r == nil {
		c.JSON(http.StatusOK, map[string]any{"status": "acknowledged"})
		return
	}
	c.JSON(http.StatusOK, r)
}

// HandleInvoke validates and forwards an invocation request.
// Requires Authorization: Bearer <token>. Token is opaque — only signature is verified.
// Identity for policy lookup is the token string itself.
func (h *CapabilitiesHandler) HandleInvoke(c *gin.Context) {
	// Extract identity from Authorization header (opaque token)
	identity := extractIdentityFromAuth(c, h.SUManager)
	if identity == "" {
		c.JSON(http.StatusUnauthorized, map[string]any{"success": false, "error": "missing or invalid token"})
		return
	}

	var req pkg.InvokeCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		h.Log.Warn("invalid invoke request", "error", err)
		c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	// Extend the activity record with invocation details (middleware already
	// logged a base entry with the activity ID in context).
	if h.ActivityLog != nil {
		if id, ok := activitylog.GetActivityID(c); ok {
			// Record an enhanced activity (upsert by same ID) with invoke details.
			h.ActivityLog.Record(activitylog.Activity{
				ID:        id,
				Type:      "invoke",
				Timestamp: time.Now().UTC(),
				Data: map[string]any{
					"path":          c.Request.URL.Path,
					"method":        c.Request.Method,
					"agent_name":    req.AgentName,
					"function_name": req.FunctionName,
				},
			})
		}
	}

	result, err := h.Service.Invoke(req, identity)
	if err != nil {
		status := http.StatusInternalServerError
		switch result.Error {
		case "":
		default:
			if result.Error != "" && !result.Success {
				status = resolveInvokeStatus(result.Error)
			}
		}
		// Map forbidden to 403
		if result.Error == "forbidden" {
			status = http.StatusForbidden
		}
		c.JSON(status, result)
		return
	}

	c.JSON(http.StatusOK, result)
}

// extractIdentityFromAuth reads Authorization: Bearer <token>, validates signature,
// and returns the token string as the identity. Returns "" if missing or invalid.
func extractIdentityFromAuth(c *gin.Context, su *auth.SUManager) string {
	authz := c.GetHeader("Authorization")
	if authz == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return ""
	}
	token := strings.TrimPrefix(authz, prefix)
	if token == "" {
		return ""
	}
	if su == nil {
		// If no SUManager configured, accept any non-empty token (dev mode)
		return token
	}
	if err := su.ValidateTokenSignature(token); err != nil {
		return ""
	}
	// Token is valid (signature OK). Identity = token string (opaque).
	return token
}

// resolveInvokeStatus maps known error messages to appropriate HTTP status codes.
func resolveInvokeStatus(errMsg string) int {
	switch errMsg {
	case "agent WS connection lost":
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}
