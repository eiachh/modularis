package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	// New layered: application/agent service, transport/* , hub for WS (internal/ws removed; singular conn handler via hub).
	agentapp "github.com/modularis/modularis/internal/application/agent"
	"github.com/modularis/modularis/internal/hub"
	"github.com/modularis/modularis/internal/registry"
	httptransport "github.com/modularis/modularis/internal/transport/http" // alias to avoid std http conflict
	websocket "github.com/modularis/modularis/internal/transport/websocket"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Setup registries/hubs (hub singular for all WS conns/clients/displays).
	agentRegistry := registry.New()
	agentHub := hub.NewHub(log, "agent")
	displayHub := hub.NewHub(log, "display") // displayRegistry not needed; hub handles

	// Wire application service (agent-only; display broadcast via hub as original).
	agentService := agentapp.NewService(agentRegistry, agentHub, displayHub, log)

	// Thin transport handlers: only decode/upgrade + delegate to service.
	agentWSHandler := &websocket.AgentHandler{
		Service: agentService,
		Log:     log,
	}
	displayWSHandler := &websocket.DisplayHandler{
		Service:    agentService,
		DisplayHub: displayHub,
		Log:        log,
	}
	capabilitiesHTTPHandler := &httptransport.CapabilitiesHandler{
		Service: agentService,
	}

	// Setup routes using layered handlers.
	router := gin.Default()
	router.GET("/connect", agentWSHandler.Handle)
	router.GET("/display", displayWSHandler.Handle)
	// /capabilities, /invoke via http transport.
	router.GET("/capabilities", capabilitiesHTTPHandler.Handle)
	router.POST("/invoke", capabilitiesHTTPHandler.HandleInvoke)

	// Server config.
	addr := envOr("LISTEN_ADDR", ":8080")
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	// Graceful shutdown.
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
