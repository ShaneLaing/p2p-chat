package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/go-chi/httplog"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"

	"p2p-chat/internal/authutil"
)

var metrics authMetrics

type authMetrics struct {
	authRequests         atomic.Uint64
	loginAttempts        atomic.Uint64
	registerAttempts     atomic.Uint64
	healthChecks         atomic.Uint64
	statelessModeLogins  atomic.Uint64
	persistentModeLogins atomic.Uint64
}

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

func loggingMiddleware(db *sql.DB) func(http.Handler) http.Handler {
	stateless := db == nil
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.authRequests.Add(1)
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(recorder, r)
			entry := logEntry{
				Route:         routePattern(r),
				Method:        r.Method,
				Status:        recorder.status,
				DurationMS:    time.Since(start).Milliseconds(),
				StatelessMode: stateless,
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

func main() {
	logger := httplog.NewLogger("auth", httplog.Options{JSON: false})

	dbURL := os.Getenv("DATABASE_URL")
	var db *sql.DB
	if dbURL == "" {
		log.Print("DATABASE_URL not set; running without PostgreSQL persistence")
	} else {
		var err error
		db, err = sql.Open("pgx", dbURL)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
		defer db.Close()
		if err := db.Ping(); err != nil {
			log.Fatalf("db ping: %v", err)
		}
		if err := runMigrations(db); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	}

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(loggingMiddleware(db))

	r.Post("/register", registerHandler(db))
	r.Post("/login", loginHandler(db))
	r.Get("/healthz", healthHandler(db))
	authenticated := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := parseTokenFromHeader(r.Header.Get("Authorization"))
			username, err := authutil.ValidateToken(token)
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserKey{}, username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	r.With(authenticated).Post("/messages", storeMessageHandler(db))
	r.With(authenticated).Get("/history", historyHandler(db))

	addr := ":8089"
	log.Printf("Auth server running at %s", addr)
	if err := http.ListenAndServe(addr, httplog.RequestLogger(logger)(r)); err != nil {
		log.Fatalf("auth server stopped: %v", err)
	}
}

type ctxUserKey struct{}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
}

type messageRecord struct {
	Sender    string    `json:"sender"`
	Receiver  *string   `json:"receiver,omitempty"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// databaseUnavailable centralizes the 503 response so handlers
// always emit the same guidance about configuring DATABASE_URL.
func databaseUnavailable(w http.ResponseWriter) {
	http.Error(w, "database unavailable: set DATABASE_URL to enable persistence", http.StatusServiceUnavailable)
}

// healthHandler reports JSON status along with 200/503 so operators can detect
// stateless mode programmatically.
func healthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics.healthChecks.Add(1)
		if db == nil {
			writeHealthJSON(w, http.StatusServiceUnavailable, false, "database unavailable: set DATABASE_URL to enable persistence")
			return
		}
		if err := db.PingContext(r.Context()); err != nil {
			log.Printf("health ping failed: %v", err)
			writeHealthJSON(w, http.StatusServiceUnavailable, false, err.Error())
			return
		}
		writeHealthJSON(w, http.StatusOK, true, "ok")
	}
}

type healthPayload struct {
	Status    string `json:"status"`
	DBEnabled bool   `json:"dbEnabled"`
	Message   string `json:"message"`
}

func writeHealthJSON(w http.ResponseWriter, status int, dbEnabled bool, message string) {
	state := "ok"
	if status >= 400 {
		state = "error"
	}
	payload := healthPayload{
		Status:    state,
		DBEnabled: dbEnabled,
		Message:   message,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("health marshal error: %v", err)
		databaseUnavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(bytes); err != nil {
		log.Printf("health write error: %v", err)
	}
}

func registerHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics.registerAttempts.Add(1)
		if db == nil {
			databaseUnavailable(w)
			return
		}
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		if req.Username == "" || req.Password == "" {
			http.Error(w, "username/password required", http.StatusBadRequest)
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "hash error", http.StatusInternalServerError)
			return
		}
		_, err = db.Exec(`INSERT INTO users (username, password_hash) VALUES ($1, $2)`, req.Username, string(hash))
		if err != nil {
			http.Error(w, "username exists", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func loginHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		metrics.loginAttempts.Add(1)
		if db == nil {
			metrics.statelessModeLogins.Add(1)
			databaseUnavailable(w)
			return
		}
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var storedHash string
		if err := db.QueryRow(`SELECT password_hash FROM users WHERE username=$1`, req.Username).Scan(&storedHash); err != nil {
			http.Error(w, "invalid username", http.StatusBadRequest)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.Password)); err != nil {
			http.Error(w, "wrong password", http.StatusBadRequest)
			return
		}
		token, err := authutil.IssueToken(req.Username)
		if err != nil {
			http.Error(w, "token error", http.StatusInternalServerError)
			return
		}
		metrics.persistentModeLogins.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loginResponse{Token: token, Username: req.Username})
	}
}

func storeMessageHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			databaseUnavailable(w)
			return
		}
		user := r.Context().Value(ctxUserKey{}).(string)
		var req struct {
			Sender   string  `json:"sender"`
			Receiver *string `json:"receiver"`
			Content  string  `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if req.Content == "" {
			http.Error(w, "content required", http.StatusBadRequest)
			return
		}
		if req.Sender == "" {
			req.Sender = user
		}
		if req.Sender != user {
			http.Error(w, "sender mismatch", http.StatusForbidden)
			return
		}
		_, err := db.Exec(`INSERT INTO messages (sender, receiver, content) VALUES ($1, $2, $3)`, req.Sender, req.Receiver, req.Content)
		if err != nil {
			http.Error(w, "store failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func historyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			databaseUnavailable(w)
			return
		}
		user := r.Context().Value(ctxUserKey{}).(string)
		target := r.URL.Query().Get("user")
		if target == "" {
			target = user
		}
		if target != user {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		rows, err := db.Query(`
			SELECT sender, receiver, content, COALESCE(timestamp, NOW())
			FROM messages
			WHERE receiver IS NULL OR receiver=$1 OR sender=$1
			ORDER BY id DESC
			LIMIT 200
		`, target)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var records []messageRecord
		for rows.Next() {
			var rec messageRecord
			if err := rows.Scan(&rec.Sender, &rec.Receiver, &rec.Content, &rec.Timestamp); err != nil {
				http.Error(w, "scan failed", http.StatusInternalServerError)
				return
			}
			records = append(records, rec)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(records)
	}
}

func runMigrations(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id SERIAL PRIMARY KEY,
			sender TEXT NOT NULL,
			receiver TEXT,
			content TEXT NOT NULL,
			timestamp TIMESTAMPTZ DEFAULT NOW()
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func parseTokenFromHeader(h string) string {
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}
