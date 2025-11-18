package authserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"p2p-chat/internal/authutil"
)

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

type healthPayload struct {
	Status    string `json:"status"`
	DBEnabled bool   `json:"dbEnabled"`
	Message   string `json:"message"`
}

func (s *Server) databaseUnavailable(w http.ResponseWriter) {
	http.Error(w, "database unavailable: set DATABASE_URL to enable persistence", http.StatusServiceUnavailable)
}

func (s *Server) writeHealthJSON(w http.ResponseWriter, status int, dbEnabled bool, message string) {
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
		s.databaseUnavailable(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(bytes); err != nil {
		log.Printf("health write error: %v", err)
	}
}

func (s *Server) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.metrics.HealthChecks.Add(1)
		if s.DB == nil {
			s.writeHealthJSON(w, http.StatusServiceUnavailable, false, "database unavailable: set DATABASE_URL to enable persistence")
			return
		}
		if err := s.DB.PingContext(r.Context()); err != nil {
			log.Printf("health ping failed: %v", err)
			s.writeHealthJSON(w, http.StatusServiceUnavailable, false, err.Error())
			return
		}
		s.writeHealthJSON(w, http.StatusOK, true, "ok")
	}
}

func (s *Server) registerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.metrics.RegisterAttempts.Add(1)
		if s.DB == nil {
			s.databaseUnavailable(w)
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
		_, err = s.DB.Exec(`INSERT INTO users (username, password_hash) VALUES ($1, $2)`, req.Username, string(hash))
		if err != nil {
			http.Error(w, "username exists", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func (s *Server) loginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.metrics.LoginAttempts.Add(1)
		if s.DB == nil {
			s.metrics.StatelessModeLogins.Add(1)
			s.databaseUnavailable(w)
			return
		}
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		var storedHash string
		if err := s.DB.QueryRow(`SELECT password_hash FROM users WHERE username=$1`, req.Username).Scan(&storedHash); err != nil {
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
		s.metrics.PersistentModeLogins.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(loginResponse{Token: token, Username: req.Username})
	}
}

func (s *Server) storeMessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.DB == nil {
			s.databaseUnavailable(w)
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
		_, err := s.DB.Exec(`INSERT INTO messages (sender, receiver, content) VALUES ($1, $2, $3)`, req.Sender, req.Receiver, req.Content)
		if err != nil {
			http.Error(w, "store failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) historyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.DB == nil {
			s.databaseUnavailable(w)
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
		rows, err := s.DB.Query(`
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

func (s *Server) authenticated() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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
}

func parseTokenFromHeader(h string) string {
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func (s *Server) databaseConn() *sql.DB {
	return s.DB
}
