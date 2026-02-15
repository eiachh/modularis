package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/modularis/modularis/internal/handler"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	agentRegistry := registry.New()
	agentHub := ws.NewHub(log, "agent")

	displayRegistry := registry.NewDisplayRegistry()
	displayHub := ws.NewHub(log, "display")

	connectHandler := &handler.ConnectHandler{
		Hub:        agentHub,
		DisplayHub: displayHub,
		Registry:   agentRegistry,
		Log:        log,
	}

	displayHandler := &handler.DisplayHandler{
		DisplayHub:      displayHub,
		DisplayRegistry: displayRegistry,
		Log:             log,
	}

	// CapabilitiesHandler exposes registered agent capabilities (agent_name,
	// function_name, schema) for clients *and* handles /invoke (forwards to
	// agent WS via hub). Reflects runtime registrations only.
	capabilitiesHandler := &handler.CapabilitiesHandler{
		Registry: agentRegistry,
		// Hub for forwarding commands to agents (echo etc.).
		Hub: agentHub,
		Log: log,
	}

	router := gin.Default()
	router.GET("/connect", connectHandler.Handle)
	router.GET("/display", displayHandler.Handle)
	// GET /capabilities: discovery.
	// POST /invoke: assemble/forward command to agent (client → orch → agent).
	router.GET("/capabilities", capabilitiesHandler.Handle)
	router.POST("/invoke", capabilitiesHandler.HandleInvoke)

	addr := envOr("LISTEN_ADDR", ":8080")
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
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
