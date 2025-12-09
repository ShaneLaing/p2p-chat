package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"p2p-chat/internal/message"
)

func (r *Runtime) RegisterSelf() error {
	if r.bootstrapURL == "" {
		return nil
	}
	payload := map[string]string{"addr": r.selfAddr}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(strings.TrimRight(r.bootstrapURL, "/")+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func fetchPeers(url string) ([]string, error) {
	resp, err := http.Get(strings.TrimRight(url, "/") + "/peers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var peers []string
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func (r *Runtime) ConnectToBootstrapPeers() {
	if r.bootstrapURL == "" {
		return
	}
	peers, err := fetchPeers(r.bootstrapURL)
	if err != nil {
		log.Printf("fetch peers: %v", err)
		return
	}
	for _, peer := range peers {
		if peer == r.selfAddr {
			continue
		}
		r.dialer.Add(peer)
		if err := r.cm.ConnectToPeer(peer); err != nil {
			log.Printf("connect to %s: %v", peer, err)
		}
	}
}

func (r *Runtime) PollBootstrapLoop() {
	if r.bootstrapURL == "" {
		return
	}
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			peers, err := fetchPeers(r.bootstrapURL)
			if err != nil {
				log.Printf("poll peers: %v", err)
				continue
			}
			for _, peer := range peers {
				if peer == r.selfAddr {
					continue
				}
				r.dialer.Add(peer)
			}
		}
	}
}

func (r *Runtime) GossipLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			peers := r.dialer.Desired()
			if len(peers) == 0 {
				continue
			}
			msg := message.Message{
				MsgID:     NewMsgID(),
				Type:      MsgTypePeerSync,
				From:      r.identity.Get(),
				Origin:    r.selfAddr,
				Timestamp: time.Now(),
				PeerList:  peers,
			}
			r.cm.Broadcast(msg, "")
		}
	}
}

func (r *Runtime) UpdatePeerListLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			addrs := r.cm.ConnsList()
			r.directory.MarkActive(addrs)
			r.sink.UpdatePeers(r.directory.Snapshot())
		}
	}
}

func (r *Runtime) PresenceHeartbeatLoop() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.BroadcastHandshake()
		}
	}
}
