package activitylog

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Activity is a generic record for any call/activity in the system.
// It is designed to be flexible enough to store invocation records,
// display events, agent registrations, or any future activity type.
type Activity struct {
	// ID is a unique identifier for this activity (e.g., invocation ID).
	ID string `json:"id"`

	// Type categorizes the activity (e.g., "invoke", "display", "register").
	Type string `json:"type"`

	// Timestamp is when the activity occurred.
	Timestamp time.Time `json:"timestamp"`

	// Data holds flexible, type-specific details about the activity.
	// Examples:
	//   - For "invoke": {"agent_name": "...", "function_name": "..."}
	//   - For "display": {"agent_name": "...", "title": "..."}
	//   - For future types: any serializable metadata.
	Data map[string]any `json:"data,omitempty"`
}

// Log is a thread-safe in-memory store for Activity records.
// It supports O(1) lookup by ID and preserves insertion order for listing.
type Log struct {
	mu         sync.RWMutex
	activities map[string]Activity
	order      []string // insertion order of IDs
}

// New creates an empty activity Log ready for use.
func New() *Log {
	return &Log{
		activities: make(map[string]Activity),
		order:      make([]string, 0),
	}
}

// Record adds an Activity to the log. If an activity with the same ID
// already exists, it is overwritten (idempotent upsert).
func (l *Log) Record(a Activity) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.activities[a.ID]; !exists {
		l.order = append(l.order, a.ID)
	}
	l.activities[a.ID] = a
}

// Get retrieves an Activity by ID. Returns the Activity and true if found,
// or an empty Activity and false otherwise.
func (l *Log) Get(id string) (Activity, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	a, ok := l.activities[id]
	return a, ok
}

// List returns a snapshot of all activities in insertion order.
func (l *Log) List() []Activity {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]Activity, 0, len(l.order))
	for _, id := range l.order {
		out = append(out, l.activities[id])
	}
	return out
}

// Count returns the number of stored activities.
func (l *Log) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.activities)
}

// Clear removes all activities (useful for tests).
func (l *Log) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.activities = make(map[string]Activity)
	l.order = l.order[:0]
}

// -----------------------------------------------------------------------
// Gin Middleware
// -----------------------------------------------------------------------

// ContextKey is the type for context keys used by this package.
type ContextKey string

const (
	// ActivityIDKey is the gin.Context key where the activity/invocation ID is stored.
	ActivityIDKey ContextKey = "activity_id"
)

// Middleware returns a Gin middleware that generates a unique activity ID
// for each request, records a generic Activity entry in the provided Log,
// and stores the ID in gin.Context for downstream handlers.
//
// The recorded Activity has:
//   - ID: newly generated UUID
//   - Type: the provided activityType (e.g., "invoke", "display", "call")
//   - Timestamp: time.Now().UTC()
//   - Data: includes "path", "method", and optionally request-specific fields
//
// Example:
//
//	activityLog := activitylog.New()
//	router.Use(activitylog.Middleware(activityLog, "request"))
func Middleware(log *Log, activityType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := uuid.New().String()

		// Record generic activity before the handler runs
		act := Activity{
			ID:        id,
			Type:      activityType,
			Timestamp: time.Now().UTC(),
			Data: map[string]any{
				"path":   c.Request.URL.Path,
				"method": c.Request.Method,
			},
		}
		log.Record(act)

		// Store ID in context for downstream use
		c.Set(string(ActivityIDKey), id)

		c.Next()
	}
}

// GetActivityID extracts the activity ID from gin.Context if present.
// Returns the ID and true, or empty string and false.
func GetActivityID(c *gin.Context) (string, bool) {
	if v, ok := c.Get(string(ActivityIDKey)); ok {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}
