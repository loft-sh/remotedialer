package remotedialer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestBasic(t *testing.T) {
	// start a server
	PrintTunnelData = true
	handler := New(authorizer, DefaultErrorWriter)
	handler.ClientConnectAuthorizer = func(proto, address string) bool {
		return true
	}

	fmt.Println("Listening on ", "127.0.0.1:22222")
	errChan := make(chan error, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	router := mux.NewRouter()
	router.Handle("/connect", handler)
	wsServer := &http.Server{
		Handler: router,
		Addr:    "127.0.0.1:22222",
	}

	go func() {
		defer cancel()

		err := wsServer.ListenAndServe()
		if err != nil {
			errChan <- err
		}
	}()

	router2 := mux.NewRouter()
	router2.Handle("/hello", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprintf(writer, "Hi")
	}))
	helloServer := &http.Server{
		Handler: router2,
		Addr:    "127.0.0.1:22223",
	}

	go func() {
		defer cancel()

		err := helloServer.ListenAndServe()
		if err != nil {
			errChan <- err
		}
	}()

	go func() {
		err := ClientConnect(ctx, "ws://127.0.0.1:22222/connect", http.Header{
			"X-Tunnel-ID": []string{"foo"},
		}, nil, func(string, string) bool { return true }, func(ctx context.Context, session *Session) error {
			client := &http.Client{
				Transport: &http.Transport{
					DialContext: session.Dial,
				},
			}

			resp, err := client.Get("http://127.0.0.1:22223/hello")
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			out, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("read body: %w", err)
			} else if string(out) != "Hi" {
				return fmt.Errorf("unexpected answer: %s", string(out))
			}

			errChan <- nil
			return nil
		})
		errChan <- err
	}()

	err := <-errChan
	_ = wsServer.Close()
	_ = helloServer.Close()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// start a server
	PrintTunnelData = true
	peer1, peer1Server := startServer("peer-1", "127.0.0.1:22222")
	defer peer1Server.Close()
	peer2, peer2Server := startServer("peer-2", "127.0.0.1:22223")
	defer peer2Server.Close()
	startHttpServer("127.0.0.1:22224")

	// connect peer1 -> peer2
	peer1.AddPeer(ctx, "ws://127.0.0.1:22223/connect", "peer-2")

	// client connect to peer 1
	go func() {
		err := ClientConnect(ctx, "ws://127.0.0.1:22222/connect", http.Header{
			"X-Tunnel-ID": []string{"to-peer-1"},
		}, nil, func(string, string) bool { return true }, func(ctx context.Context, session *Session) error {
			return nil
		})
		if err != nil {
			panic(err)
		}
	}()

	// test request
	time.Sleep(time.Second * 1)
	testHttpRequest(peer2.Dialer("to-peer-1"))

	fmt.Println("OTHER WAY AROUND")

	// connect peer2 -> peer1
	peer2.AddPeer(ctx, "ws://127.0.0.1:22222/connect", "peer-1")

	// client connect to peer 2
	go func() {
		err := ClientConnect(ctx, "ws://127.0.0.1:22223/connect", http.Header{
			"X-Tunnel-ID": []string{"to-peer-2"},
		}, nil, func(string, string) bool { return true }, func(ctx context.Context, session *Session) error {
			return nil
		})
		if err != nil {
			panic(err)
		}
	}()

	// test connections
	time.Sleep(time.Second * 1)
	testHttpRequest(peer1.Dialer("to-peer-2"))
}

func testHttpRequest(dial Dialer) {
	waitGroup := sync.WaitGroup{}
	for i := 0; i < 15; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			httpClient := &http.Client{
				Transport: &http.Transport{
					DialContext:           dial,
					ForceAttemptHTTP2:     true,
					MaxIdleConns:          100,
					IdleConnTimeout:       90 * time.Second,
					TLSHandshakeTimeout:   10 * time.Second,
					ExpectContinueTimeout: 1 * time.Second,
				},
			}

			resp, err := httpClient.Get("http://127.0.0.1:22224/hello")
			if err != nil {
				panic(err)
			}

			out, err := io.ReadAll(resp.Body)
			if err != nil {
				panic(err)
			} else if string(out) != "Hi" {
				panic(string(out))
			}
		}()
	}

	waitGroup.Wait()
}

func startHttpServer(address string) {
	router2 := mux.NewRouter()
	router2.Handle("/hello", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = fmt.Fprintf(writer, "Hi")
	}))
	helloServer := &http.Server{
		Handler: router2,
		Addr:    address,
	}

	go func() {
		err := helloServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
}

func startServer(peerID, address string) (*Server, *http.Server) {
	handler := New(authorizer, DefaultErrorWriter)
	handler.ClientConnectAuthorizer = func(proto, address string) bool {
		return true
	}
	handler.PeerID = peerID
	handler.PeerToken = peerID
	handler.PeerAuthorizer = func(req *http.Request, id, token string) bool { return true }

	router := mux.NewRouter()
	router.Handle("/connect", handler)
	wsServer := &http.Server{
		Handler: router,
		Addr:    address,
	}

	go func() {
		err := wsServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	return handler, wsServer
}

func authorizer(req *http.Request) (string, bool, error) {
	id := req.Header.Get("x-tunnel-id")
	return id, id != "", nil
}
