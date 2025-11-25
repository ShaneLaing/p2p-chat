package peer

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"p2p-chat/internal/crypto"
	"p2p-chat/internal/network"
)

// App encapsulates the peer runtime components.
type App struct {
	Cfg *Config

	ctx    context.Context
	cancel context.CancelFunc

	ConnMgr   *network.ConnManager
	Scheduler *dialScheduler
	Cache     *msgCache
	History   *historyBuffer
	Store     *historyStore
	Files     *fileStore
	Blocklist *blockList
	Directory *peerDirectory
	Metrics   *metrics
	Ack       *ackTracker
	Identity  *identity
	SelfAddr  string

	sink displaySink
	web  *webBridge
}

// NewApp wires all peer dependencies according to the provided config.
func NewApp(cfg *Config) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())

	box, err := crypto.NewBox(cfg.Secret)
	if err != nil {
		cancel()
		return nil, err
	}

	cm := network.NewConnManager(cfg.ListenAddr, box)
	if err := cm.StartListen(); err != nil {
		cancel()
		return nil, err
	}
	log.Printf("peer listening on %s (encryption:%t)", cfg.ListenAddr, cm.EncryptionEnabled())

	cache := newMsgCache(10 * time.Minute)
	history := newHistory(cfg.HistorySize)

	store, err := openHistoryStore(cfg.HistoryDB)
	if err != nil {
		log.Printf("history db unavailable (%v), running without persistence", err)
	}

	var files *fileStore
	if cfg.UseWeb {
		files, err = openFileStore(cfg.FilesDB, cfg.FilesDir)
		if err != nil {
			cancel()
			return nil, err
		}
	}

	identity := newIdentity(cfg.Nick, cfg.ListenAddr)
	if cfg.Username != "" && cfg.Token != "" {
		identity.SetAuth(cfg.Username, cfg.Token)
	}

	blocklist := newBlockList()
	directory := newPeerDirectory()
	metrics := newMetrics()
	dialer := newDialScheduler(cm, cfg.ListenAddr)
	ack := newAckTracker(cm)

	app := &App{
		Cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		ConnMgr:   cm,
		Scheduler: dialer,
		Cache:     cache,
		History:   history,
		Store:     store,
		Files:     files,
		Blocklist: blocklist,
		Directory: directory,
		Metrics:   metrics,
		Ack:       ack,
		Identity:  identity,
		SelfAddr:  cfg.ListenAddr,
	}

	if name := identity.Get(); name != "" {
		directory.Record(name, cfg.ListenAddr)
	}

	return app, nil
}

// Start launches the peer runtime loops and user interfaces.
func (a *App) Start() {
	var sinks []displaySink
	if a.Cfg.UseCLI {
		sinks = append(sinks, a.StartCLI())
	}
	if a.Cfg.UseTUI {
		if sink := a.StartTUI(); sink != nil {
			sinks = append(sinks, sink)
		}
	}
	if a.Cfg.UseWeb {
		if sink := a.StartWebUI(); sink != nil {
			sinks = append(sinks, sink)
		}
	}
	if len(sinks) == 0 {
		sinks = append(sinks, a.StartCLI())
	}
	a.sink = newMultiSink(sinks...)

	if err := a.registerSelf(); err != nil {
		log.Printf("register failed: %v", err)
	}
	a.connectToBootstrapPeers()
	a.broadcastHandshake()

	go a.Scheduler.Run(a.ctx)
	go a.runInboundHandler()
	go a.runBootstrapLoop()
	go a.runGossipLoop()
	go a.runPeerListLoop()
	go a.runPresenceLoop()

	if a.Cfg.UseCLI {
		go a.readCLIInput()
	}
}

// Shutdown stops all background routines and releases resources.
func (a *App) Shutdown() {
	a.cancel()
	if a.web != nil {
		a.web.Close()
	}
	if a.Scheduler != nil {
		a.Scheduler.Close()
	}
	if a.Ack != nil {
		a.Ack.Stop()
	}
	if a.ConnMgr != nil {
		a.ConnMgr.Stop()
	}
	if a.Store != nil {
		_ = a.Store.Close()
	}
	if a.Files != nil {
		_ = a.Files.Close()
	}
}

// WaitForShutdown blocks until SIGINT/SIGTERM then stops the app.
func WaitForShutdown(app *App) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down...")
	app.Shutdown()
}
