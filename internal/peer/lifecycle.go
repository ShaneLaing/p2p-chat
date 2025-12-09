package peer

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Start launches background goroutines and optional UIs.
func (a *App) Start() {
	if a == nil {
		return
	}
	a.startOnce.Do(func() {
		rt := a.runtime
		if rt == nil {
			return
		}
		if a.enableTUI && a.tui != nil {
			go func() {
				if err := a.tui.Run(rt.Context()); err != nil {
					log.Printf("tui error: %v", err)
				}
			}()
		}
		if a.enableCLI {
			go rt.ReadCLIInput(os.Stdin)
		}
		if web := rt.Web(); web != nil {
			go web.Run(rt.Context())
		}

		if err := rt.RegisterSelf(); err != nil {
			log.Printf("register failed: %v", err)
		}
		rt.ConnectToBootstrapPeers()
		rt.BroadcastHandshake()

		go rt.Dialer().Run(rt.Context())
		go rt.HandleIncoming()
		go rt.PollBootstrapLoop()
		go rt.GossipLoop()
		go rt.UpdatePeerListLoop()
		go rt.PresenceHeartbeatLoop()
	})
}

// Shutdown stops background goroutines and releases resources.
func (a *App) Shutdown() {
	if a == nil {
		return
	}
	a.shutdownOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		rt := a.runtime
		if rt == nil {
			return
		}
		if web := rt.Web(); web != nil {
			web.Close()
		}
		if dialer := rt.Dialer(); dialer != nil {
			dialer.Close()
		}
		if ack := rt.AckTracker(); ack != nil {
			ack.Stop()
		}
		if cm := rt.ConnManager(); cm != nil {
			cm.Stop()
		}
		if store := rt.Store(); store != nil {
			store.Close()
		}
		if files := rt.Files(); files != nil {
			files.Close()
		}
	})
}

// WaitForShutdown blocks until an interrupt signal arrives, then shuts down the peer gracefully.
func WaitForShutdown(app *App) {
	if app == nil {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	app.Shutdown()
}
