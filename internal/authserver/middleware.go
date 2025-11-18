package authserver

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

type logEntry struct {
	Route         string `json:"route"`
	Method        string `json:"method"`
	Status        int    `json:"status"`
	DurationMS    int64  `json:"duration_ms"`
	StatelessMode bool   `json:"stateless_mode"`
	Client        string `json:"client"`
	Timestamp     string `json:"timestamp"`
}

func (s *Server) loggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.metrics.AuthRequests.Add(1)
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(recorder, r)
			entry := logEntry{
				Route:         routePattern(r),
				Method:        r.Method,
				Status:        recorder.status,
				DurationMS:    time.Since(start).Milliseconds(),
				StatelessMode: s.DB == nil,
				Client:        clientOrigin(r),
				Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
			}
			payload, err := json.Marshal(entry)
			if err != nil {
				log.Printf("log marshal error: %v", err)
				return
			}
			log.Print(string(payload))
		})
	}
}

func routePattern(r *http.Request) string {
	if ctx := chi.RouteContext(r.Context()); ctx != nil {
		if pattern := ctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return r.URL.Path
}

func clientOrigin(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return fwd
	}
	return r.RemoteAddr
}
