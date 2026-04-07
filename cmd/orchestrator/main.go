package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/eiachh/Modularis/internal/activitylog"
	"github.com/eiachh/Modularis/internal/auth"
	"github.com/eiachh/Modularis/internal/handler"
	"github.com/eiachh/Modularis/internal/invokeresult"
	"github.com/eiachh/Modularis/internal/policy"
	"github.com/eiachh/Modularis/internal/registry"
	"github.com/eiachh/Modularis/internal/service"
	"github.com/eiachh/Modularis/internal/ws"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	agentRegistry := registry.New()
	displayRegistry := registry.NewDisplayRegistry()

	hubs := &ws.Hubs{
		Agent:   ws.NewHub(log, "agent"),
		Display: ws.NewHub(log, "display"),
	}

	// Activity log for tracking all call activities (invocations, etc.)
	activityLog := activitylog.New()

	// Store for invocation results (for GetInvokeResult blocking)
	resultStore := invokeresult.New()

	// SU token manager: Ed25519 keys + one-time SU token generation
	suManager := auth.NewSUManager()

	// Policy store (in-memory): roles and per-identity policies
	policyStore := policy.NewStore()

	agentSvc := &service.AgentService{
		Registry: agentRegistry,
		Hubs:     hubs,
		Log:      log,
	}

	displaySvc := &service.DisplayService{
		Registry: displayRegistry,
		Hubs:     hubs,
		Log:      log,
	}

	capabilitiesSvc := &service.CapabilitiesService{
		Registry:    agentRegistry,
		Hub:         hubs.Agent,
		Log:         log,
		ResultStore: resultStore,
		Policy:      policyStore,
	}

	agentHandler := &handler.AgentHandler{
		Service:     agentSvc,
		Log:         log,
		ResultStore: resultStore,
		Policy:      policyStore,
	}

	displayHandler := &handler.DisplayHandler{
		Service: displaySvc,
		Log:     log,
	}

	capabilitiesHandler := &handler.CapabilitiesHandler{
		Service:     capabilitiesSvc,
		Log:         log,
		ActivityLog: activityLog,
		SUManager:   suManager,
	}

	authHandler := &handler.AuthHandler{
		SUManager: suManager,
	}

	policyHandler := &handler.PolicyHandler{
		SUManager: suManager,
		Store:     policyStore,
	}

	router := gin.Default()
	router.GET("/connect", agentHandler.Handle)
	router.GET("/display", displayHandler.Handle)
	router.GET("/capabilities", capabilitiesHandler.Handle)
	router.GET("/activities", capabilitiesHandler.HandleListActivities)
	// Apply activity logging middleware specifically to /invoke route
	router.POST("/invoke", activitylog.Middleware(activityLog, "invoke"), capabilitiesHandler.HandleInvoke)
	router.GET("/invoke/result/:id", capabilitiesHandler.HandleInvokeResult)

	// SU token endpoint: generates Ed25519-signed super-user token (one-time only)
	router.POST("/su/token", authHandler.HandleGenerateSUToken)

	// Default token endpoint: clients request an opaque token (no permissions by default)
	router.POST("/token", authHandler.HandleGenerateDefaultToken)

	// Policy admin endpoints (SU-only via Authorization: Bearer <SU token>)
	router.POST("/policy/role", policyHandler.HandleCreateRole)
	router.POST("/policy", policyHandler.HandleCreatePolicy)
	router.GET("/policy/roles", policyHandler.HandleListRoles)
	router.GET("/policy/:identity", policyHandler.HandleGetPolicy)
	router.GET("/policies", policyHandler.HandleListPolicies)

	addr := envOr("LISTEN_ADDR", ":8080")
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("orchestrator listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	if err := srv.Shutdown(context.Background()); err != nil {
		log.Error("shutdown error", "error", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
