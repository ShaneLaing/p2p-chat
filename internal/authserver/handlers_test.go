package authserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"golang.org/x/crypto/bcrypt"

	"p2p-chat/internal/authutil"
)

func TestHealthHandlerWithoutDB(t *testing.T) {
	srv := New(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.healthHandler()(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestHealthHandlerWithDB(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	srv := New(db)
	mock.ExpectPing()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.healthHandler()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRegisterHandlerSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	srv := New(db)
	mock.ExpectExec("INSERT INTO users").WithArgs("alice", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	body := bytes.NewBufferString(`{"username":"alice","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/register", body)
	rr := httptest.NewRecorder()
	srv.registerHandler()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLoginHandlerSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	srv := New(db)
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	mock.ExpectQuery("SELECT password_hash FROM users WHERE username=\\$1").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(string(hash)))
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	rr := httptest.NewRecorder()
	srv.loginHandler()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp loginResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp.Username != "alice" || resp.Token == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestStoreMessageHandlerValidatesSender(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	srv := New(db)
	mock.ExpectExec("INSERT INTO messages").WithArgs("alice", nil, "hi").WillReturnResult(sqlmock.NewResult(1, 1))
	req := httptest.NewRequest(http.MethodPost, "/messages", strings.NewReader(`{"sender":"alice","content":"hi"}`))
	req = req.WithContext(newAuthContext(req.Context(), "alice"))
	rr := httptest.NewRecorder()
	srv.storeMessageHandler()(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestHistoryHandlerRejectsOtherUsers(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	srv := New(db)
	req := httptest.NewRequest(http.MethodGet, "/history?user=bob", nil)
	req = req.WithContext(newAuthContext(req.Context(), "alice"))
	rr := httptest.NewRecorder()
	srv.historyHandler()(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHistoryHandlerReturnsRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	srv := New(db)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"sender", "receiver", "content", "timestamp"}).AddRow("alice", nil, "hi", now)
	mock.ExpectQuery("(?s)SELECT.+FROM messages").WithArgs("alice").WillReturnRows(rows)
	req := httptest.NewRequest(http.MethodGet, "/history", nil)
	req = req.WithContext(newAuthContext(req.Context(), "alice"))
	rr := httptest.NewRecorder()
	srv.historyHandler()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAuthenticatedMiddleware(t *testing.T) {
	token, err := authutil.IssueToken("alice")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	srv := New(nil)
	nextCalled := false
	handler := srv.authenticated()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if !nextCalled {
		t.Fatalf("expected next handler to be invoked")
	}
}

func TestAuthenticatedMiddlewareRejectsInvalidToken(t *testing.T) {
	srv := New(nil)
	handler := srv.authenticated()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func newAuthContext(parent context.Context, user string) context.Context {
	return context.WithValue(parent, ctxUserKey{}, user)
}
