package main

import (
	"log"

	"p2p-chat/internal/bootstrap"
)

func main() {
	cfg := bootstrap.LoadConfig()
	app := bootstrap.NewApp(cfg)
	if err := app.Start(); err != nil {
		log.Fatalf("bootstrap start: %v", err)
	}
	bootstrap.WaitForShutdown(app)
}
