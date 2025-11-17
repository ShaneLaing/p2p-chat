package network

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"p2p-chat/internal/crypto"
	"p2p-chat/internal/message"
)

// ConnManager manages inbound and outbound peer connections.
type ConnManager struct {
	addr     string
	listener net.Listener
	secure   *crypto.Box

	connsMu sync.RWMutex
	conns   map[string]net.Conn

	Incoming chan message.Message
	quit     chan struct{}
}

// NewConnManager returns a configured manager for addr.
func NewConnManager(addr string, box *crypto.Box) *ConnManager {
	return &ConnManager{
		addr:     addr,
		secure:   box,
		conns:    make(map[string]net.Conn),
		Incoming: make(chan message.Message, 128),
		quit:     make(chan struct{}),
	}
}

// StartListen starts accepting inbound peers.
func (cm *ConnManager) StartListen() error {
	ln, err := net.Listen("tcp", cm.addr)
	if err != nil {
		return err
	}
	cm.listener = ln
	go cm.acceptLoop()
	return nil
}

func (cm *ConnManager) acceptLoop() {
	for {
		conn, err := cm.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case <-cm.quit:
				return
			default:
				log.Printf("accept error: %v", err)
			}
			continue
		}
		remote := conn.RemoteAddr().String()
		cm.addConn(remote, conn)
		go cm.handleConn(conn, remote)
	}
}

// ConnectToPeer dials an outbound connection if missing.
func (cm *ConnManager) ConnectToPeer(peerAddr string) error {
	if peerAddr == cm.addr {
		return nil
	}
	cm.connsMu.RLock()
	_, exists := cm.conns[peerAddr]
	cm.connsMu.RUnlock()
	if exists {
		return nil
	}
	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		return err
	}
	cm.addConn(peerAddr, conn)
	go cm.handleConn(conn, peerAddr)
	return nil
}

func (cm *ConnManager) handleConn(conn net.Conn, key string) {
	defer func() {
		cm.removeConn(key)
		_ = conn.Close()
	}()

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read error from %s: %v", key, err)
			}
			return
		}
		payload := bytes.TrimSpace(line)
		if len(payload) == 0 {
			continue
		}
		if cm.secure != nil {
			payload, err = cm.secure.Decrypt(payload)
			if err != nil {
				log.Printf("decrypt error from %s: %v", key, err)
				continue
			}
		}
		var msg message.Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			log.Printf("json decode error from %s: %v", key, err)
			continue
		}
		cm.Incoming <- msg
	}
}

// Broadcast sends a message to all peers except the provided address.
func (cm *ConnManager) Broadcast(msg message.Message, except string) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal message error: %v", err)
		return
	}
	if cm.secure != nil {
		data, err = cm.secure.Encrypt(data)
		if err != nil {
			log.Printf("encrypt message error: %v", err)
			return
		}
	}
	data = append(data, '\n')

	cm.connsMu.RLock()
	defer cm.connsMu.RUnlock()
	for addr, conn := range cm.conns {
		if addr == except {
			continue
		}
		if _, err := conn.Write(data); err != nil {
			log.Printf("write error to %s: %v", addr, err)
			go cm.removeConn(addr)
		}
	}
}

func (cm *ConnManager) addConn(addr string, conn net.Conn) {
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()
	if old, ok := cm.conns[addr]; ok {
		_ = old.Close()
	}
	cm.conns[addr] = conn
}

// ConnsList returns current peer addresses.
func (cm *ConnManager) ConnsList() []string {
	cm.connsMu.RLock()
	defer cm.connsMu.RUnlock()
	list := make([]string, 0, len(cm.conns))
	for addr := range cm.conns {
		list = append(list, addr)
	}
	return list
}

func (cm *ConnManager) removeConn(addr string) {
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()
	if conn, ok := cm.conns[addr]; ok {
		_ = conn.Close()
		delete(cm.conns, addr)
	}
}

// Stop shuts down listener and connections.
func (cm *ConnManager) Stop() {
	close(cm.quit)
	if cm.listener != nil {
		_ = cm.listener.Close()
	}
	cm.connsMu.Lock()
	for addr, conn := range cm.conns {
		_ = conn.Close()
		delete(cm.conns, addr)
	}
	cm.connsMu.Unlock()
	close(cm.Incoming)
}

// Addr exposes listening addr.
func (cm *ConnManager) Addr() string {
	return cm.addr
}

// EncryptionEnabled returns state.
func (cm *ConnManager) EncryptionEnabled() bool {
	return cm.secure != nil
}

// DialAddr formats host:port helper.
func DialAddr(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
