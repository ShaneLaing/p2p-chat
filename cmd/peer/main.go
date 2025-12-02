package main

import (
	"log"

	"p2p-chat/internal/peer"
)

func main() {
	cfg := peer.LoadConfig()
	app, err := peer.NewApp(cfg)
	if err != nil {
		log.Fatalf("init error: %v", err)
	}
	app.Start()
	peer.WaitForShutdown(app)
}
