package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/eiachh/Modularis/internal/auth"
	"github.com/eiachh/Modularis/internal/policy"
)

// PolicyHandler serves policy/role management endpoints (SU-only).
type PolicyHandler struct {
	SUManager *auth.SUManager
	Store     *policy.Store
}

// requireSU checks Authorization: Bearer <token> and verifies it's an SU token.
// Returns true if OK, otherwise writes 401/403 and returns false.
func (h *PolicyHandler) requireSU(c *gin.Context) bool {
	authz := c.GetHeader("Authorization")
	if authz == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
		return false
	}
	// Expect "Bearer <token>"
	const prefix = "Bearer "
	if len(authz) < len(prefix) || authz[:len(prefix)] != prefix {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization format"})
		return false
	}
	token := authz[len(prefix):]
	if !h.SUManager.IsSUToken(token) {
		c.JSON(http.StatusForbidden, gin.H{"error": "SU token required"})
		return false
	}
	return true
}

// HandleCreateRole handles POST /policy/role (SU only).
func (h *PolicyHandler) HandleCreateRole(c *gin.Context) {
	if !h.requireSU(c) {
		return
	}
	var r policy.Role
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if r.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role name required"})
		return
	}
	h.Store.SetRole(&r)
	c.JSON(http.StatusCreated, gin.H{"ok": true, "role": r})
}

// HandleCreatePolicy handles POST /policy (SU only).
func (h *PolicyHandler) HandleCreatePolicy(c *gin.Context) {
	if !h.requireSU(c) {
		return
	}
	var p policy.Policy
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if p.Identity == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identity required"})
		return
	}
	h.Store.SetPolicy(&p)
	c.JSON(http.StatusCreated, gin.H{"ok": true, "policy": p})
}

// HandleGetPolicy handles GET /policy/:identity (SU only).
func (h *PolicyHandler) HandleGetPolicy(c *gin.Context) {
	if !h.requireSU(c) {
		return
	}
	identity := c.Param("identity")
	p := h.Store.GetPolicy(identity)
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "policy not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// HandleListRoles handles GET /policy/roles (SU only).
func (h *PolicyHandler) HandleListRoles(c *gin.Context) {
	if !h.requireSU(c) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": h.Store.ListRoles()})
}

// HandleListPolicies handles GET /policies (SU only).
func (h *PolicyHandler) HandleListPolicies(c *gin.Context) {
	if !h.requireSU(c) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"policies": h.Store.ListPolicies()})
}
