package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/modularis/modularis/internal/service"
	"github.com/modularis/modularis/pkg"
)

// CapabilitiesHandler serves capability discovery and invocation.
type CapabilitiesHandler struct {
	Service *service.CapabilitiesService
	Log     *slog.Logger
}

// Handle returns all registered capabilities.
func (h *CapabilitiesHandler) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, h.Service.ListSummaries())
}

// HandleInvoke validates and forwards an invocation request.
func (h *CapabilitiesHandler) HandleInvoke(c *gin.Context) {
	var req pkg.InvokeCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		h.Log.Warn("invalid invoke request", "error", err)
		c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	result, err := h.Service.Invoke(req)
	if err != nil {
		status := http.StatusInternalServerError
		switch result.Error {
		case "":
		default:
			if result.Error != "" && !result.Success {
				status = resolveInvokeStatus(result.Error)
			}
		}
		c.JSON(status, result)
		return
	}

	c.JSON(http.StatusOK, result)
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
