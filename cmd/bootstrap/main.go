package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"p2p-chat/internal/peerlist"
)

type registerRequest struct {
	Addr string `json:"addr"`
}

func main() {
	addr := flag.String("addr", ":8000", "address bootstrap listens on")
	flag.Parse()

	store := peerlist.NewStore(2 * time.Minute)

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Addr == "" {
			http.Error(w, "missing addr", http.StatusBadRequest)
			return
		}
		store.Register(req.Addr)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	http.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		peers := store.List()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(peers); err != nil {
			log.Printf("encode peers: %v", err)
		}
	})

	log.Printf("bootstrap server listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
