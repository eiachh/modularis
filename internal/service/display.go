package service

import (
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/modularis/modularis/internal/domain"
	"github.com/modularis/modularis/internal/registry"
	"github.com/modularis/modularis/internal/ws"
)

// DisplayService handles display registration and lifecycle.
type DisplayService struct {
	Registry *registry.DisplayRegistry
	Hubs     *ws.Hubs
	Log      *slog.Logger
}

// Register creates and persists a new display, attaching it to the hub.
func (s *DisplayService) Register(conn *ws.Conn, name, displayType string) (*domain.Display, error) {
	display := &domain.Display{
		ID:           uuid.New().String(),
		Name:         name,
		Type:         displayType,
		RegisteredAt: time.Now().UTC(),
	}

	if err := s.Registry.Add(display); err != nil {
		s.Log.Error("display registry add failed", "error", err)
		return nil, err
	}

	conn.Metadata = display
	s.Hubs.Display.Register(display.ID, conn)

	s.Log.Info("display registered",
		"display_id", display.ID,
		"name", display.Name,
		"type", display.Type,
	)

	return display, nil
}

// Deregister removes the display from the hub and registry.
func (s *DisplayService) Deregister(displayID string) {
	s.Hubs.Display.Unregister(displayID)
	s.Registry.Remove(displayID)
	s.Log.Info("display deregistered", "display_id", displayID)
}
