package remotedialer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/loft-sh/remotedialer/metrics"
	"k8s.io/klog/v2"
)

var (
	Token = "X-API-Tunnel-Token"
	ID    = "X-API-Tunnel-ID"
)

func (s *Server) AddPeer(ctx context.Context, url, id string) {
	if s.PeerID == "" || s.PeerToken == "" {
		return
	}

	ctx, cancel := context.WithCancel(ctx)
	peer := peer{
		url:    url,
		id:     id,
		cancel: cancel,
	}

	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	if p, ok := s.peers[id]; ok {
		if p.equals(peer) {
			return
		}
		p.cancel()
	}

	klog.FromContext(ctx).Info("Adding peer", "url", url, "id", id)

	s.peers[id] = peer
	go peer.start(ctx, s)
}

func (s *Server) RemovePeer(ctx context.Context, id string) {
	s.peerLock.Lock()
	defer s.peerLock.Unlock()

	if p, ok := s.peers[id]; ok {
		klog.FromContext(ctx).Info("Removing peer", "id", id)
		p.cancel()
	}
	delete(s.peers, id)
}

type peer struct {
	url, id string
	cancel  context.CancelFunc
}

func (p peer) equals(other peer) bool {
	return p.url == other.url &&
		p.id == other.id
}

func (p *peer) start(ctx context.Context, s *Server) {
	headers := http.Header{
		ID:    {s.PeerID},
		Token: {s.PeerToken},
	}

	dialer := &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		HandshakeTimeout: HandshakeTimeOut,
	}

	logger := klog.FromContext(ctx)
	logger.Info("Peer started", "id", p.id, "url", p.url)

outer:
	for {
		select {
		case <-ctx.Done():
			break outer
		default:
		}

		logger.Info("Connect to peer", "id", p.id, "url", p.url)
		metrics.IncSMTotalAddPeerAttempt(p.id)
		ws, _, err := dialer.Dial(p.url, headers)
		if err != nil {
			logger.Error(err, "Failed to connect to peer", "url", p.url, "id", s.PeerID)
			time.Sleep(5 * time.Second)
			continue
		}
		metrics.IncSMTotalPeerConnected(p.id)

		session, err := NewClientSession(ctx, func(string, string) bool { return true }, ws)
		if err != nil {
			logger.Error(err, "Failed to connect to peer", "url", p.url)
			time.Sleep(5 * time.Second)
			continue
		}

		session.dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			parts := strings.SplitN(network, "::", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid clientKey/proto: %s", network)
			}
			d := s.Dialer(parts[0])
			return d(ctx, parts[1], address)
		}

		s.sessions.addListener(session)
		_, err = session.Serve(ctx)
		s.sessions.removeListener(session)
		session.Close()
		if err != nil {
			logger.Error(err, "Failed to serve peer connection", "id", p.id)
		}

		_ = ws.Close()
		time.Sleep(5 * time.Second)
	}

	logger.Info("Peer done", "id", p.id, "url", p.url)
}
