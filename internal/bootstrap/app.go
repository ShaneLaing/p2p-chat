package bootstrap

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"p2p-chat/internal/peerlist"
)

// App wraps the bootstrap HTTP server and peer registry state.
type App struct {
	Cfg   *Config
	Store *peerlist.Store
	srv   *http.Server
}

// NewApp wires the dependencies required to run the bootstrap server.
func NewApp(cfg *Config) *App {
	return &App{
		Cfg:   cfg,
		Store: peerlist.NewStore(cfg.PeerTTL),
	}
}

// Start configures the HTTP routes and begins serving requests.
func (a *App) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/register", a.handleRegister)
	mux.HandleFunc("/peers", a.handlePeers)

	a.srv = &http.Server{
		Addr:    a.Cfg.Addr,
		Handler: mux,
	}

	go func() {
		if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("bootstrap server stopped: %v", err)
		}
	}()

	log.Printf("bootstrap server listening on %s", a.Cfg.Addr)
	return nil
}

// Shutdown gracefully stops the HTTP server.
func (a *App) Shutdown(ctx context.Context) error {
	if a.srv == nil {
		return nil
	}
	return a.srv.Shutdown(ctx)
}

// WaitForShutdown blocks on SIGINT/SIGTERM and then shuts down the app.
func WaitForShutdown(app *App) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("bootstrap shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
