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
	"github.com/modularis/modularis/internal/service"
	"github.com/modularis/modularis/internal/ws"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	agentRegistry := registry.New()
	displayRegistry := registry.NewDisplayRegistry()

	hubs := &ws.Hubs{
		Agent:   ws.NewHub(log, "agent"),
		Display: ws.NewHub(log, "display"),
	}

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
		Registry: agentRegistry,
		Hub:      hubs.Agent,
		Log:      log,
	}

	agentHandler := &handler.AgentHandler{
		Service: agentSvc,
		Log:     log,
	}

	displayHandler := &handler.DisplayHandler{
		Service: displaySvc,
		Log:     log,
	}

	capabilitiesHandler := &handler.CapabilitiesHandler{
		Service: capabilitiesSvc,
		Log:     log,
	}

	router := gin.Default()
	router.GET("/connect", agentHandler.Handle)
	router.GET("/display", displayHandler.Handle)
	router.GET("/capabilities", capabilitiesHandler.Handle)
	router.POST("/invoke", capabilitiesHandler.HandleInvoke)

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
