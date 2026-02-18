package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modularis/modularis/internal/application/agent"
	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/pkg"
)

// CapabilitiesHandler is thin HTTP transport for /capabilities and /invoke:
// binds JSON, delegates to application service, responds. No business logic.
type CapabilitiesHandler struct {
	Service *agent.Service
}

// Handle GET /capabilities: delegates to service for list.
func (h *CapabilitiesHandler) Handle(c *gin.Context) {
	summaries := h.Service.GetCapabilities()
	c.JSON(http.StatusOK, summaries)
}

// HandleInvoke POST /invoke: binds, delegates to service.
func (h *CapabilitiesHandler) HandleInvoke(c *gin.Context) {
	var req pkg.InvokeCommand
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, domain.CommandResultPayload{Success: false, Error: err.Error()})
		return
	}

	result, _ := h.Service.InvokeCommand(req) // err handled in result
	c.JSON(http.StatusOK, result)
}
