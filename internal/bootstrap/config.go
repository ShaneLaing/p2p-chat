package bootstrap

import (
	"flag"
	"time"
)

// Config captures the bootstrap server settings derived from CLI flags.
type Config struct {
	Addr    string
	PeerTTL time.Duration
}

// LoadConfig parses CLI flags and builds a Config instance.
func LoadConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.Addr, "addr", ":8000", "address bootstrap listens on")
	flag.DurationVar(&cfg.PeerTTL, "peer-ttl", 2*time.Minute, "duration a peer stays registered without refresh")

	flag.Parse()

	if cfg.Addr == "" {
		cfg.Addr = ":8000"
	}
	return cfg
}
