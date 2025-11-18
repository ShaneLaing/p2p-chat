package authserver

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

// Server bundles all auth HTTP handlers, middleware, and metrics.
type Server struct {
	DB      *sql.DB
	metrics *Metrics
}

// New creates a Server with the provided DB (may be nil for stateless mode).
func New(db *sql.DB) *Server {
	return &Server{
		DB:      db,
		metrics: &Metrics{},
	}
}

// MetricsSnapshot exposes the current counters (useful for tests/logging).
func (s *Server) MetricsSnapshot() Metrics {
	return *s.metrics
}

// Router wires up chi routes, middleware, and handlers ready for http.ListenAndServe.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(s.loggingMiddleware())

	r.Post("/register", s.registerHandler())
	r.Post("/login", s.loginHandler())
	r.Get("/healthz", s.healthHandler())

	r.With(s.authenticated()).Post("/messages", s.storeMessageHandler())
	r.With(s.authenticated()).Get("/history", s.historyHandler())

	return r
}
