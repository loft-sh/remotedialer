package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/loft-sh/remotedialer"
	"k8s.io/klog/v2"
)

var (
	clients = map[string]*http.Client{}
	l       sync.Mutex
	counter int64
)

func authorizer(req *http.Request) (string, bool, error) {
	id := req.Header.Get("x-tunnel-id")
	return id, id != "", nil
}

func Client(server *remotedialer.Server, rw http.ResponseWriter, req *http.Request) {
	timeout := req.URL.Query().Get("timeout")
	if timeout == "" {
		timeout = "15"
	}

	vars := mux.Vars(req)
	clientKey := vars["id"]
	url := fmt.Sprintf("%s://%s%s", vars["scheme"], vars["host"], vars["path"])
	client := getClient(server, clientKey, timeout)

	id := atomic.AddInt64(&counter, 1)

	logger := klog.FromContext(req.Context())
	logger.Info("Handling request", "id", id, "timeout", timeout, "url", url)

	resp, err := client.Get(url)
	if err != nil {
		logger.Error(err, "Failed to make request", "id", id, "timeout", timeout, "url", url)
		remotedialer.DefaultErrorWriter(rw, req, 500, err)
		return
	}
	defer resp.Body.Close()

	logger.Info("Handling request", "id", id, "timeout", timeout, "url", url)
	for k, v := range resp.Header {
		for _, h := range v {
			if rw.Header().Get(k) == "" {
				rw.Header().Set(k, h)
			} else {
				rw.Header().Add(k, h)
			}
		}
	}
	rw.WriteHeader(resp.StatusCode)
	_, err = io.Copy(rw, resp.Body)
	if err != nil {
		logger.Error(err, "Failed to copy response body", "id", id, "timeout", timeout, "url", url)
		remotedialer.DefaultErrorWriter(rw, req, 500, err)
		return
	}

	logger.Info("Done handling request", "id", id, "timeout", timeout, "url", url)
}

func getClient(server *remotedialer.Server, clientKey, timeout string) *http.Client {
	l.Lock()
	defer l.Unlock()

	key := fmt.Sprintf("%s/%s", clientKey, timeout)
	client := clients[key]
	if client != nil {
		return client
	}

	dialer := server.Dialer(clientKey)
	client = &http.Client{
		Transport: &http.Transport{
			DialContext: dialer,
		},
	}
	if timeout != "" {
		t, err := strconv.Atoi(timeout)
		if err == nil {
			client.Timeout = time.Duration(t) * time.Second
		}
	}

	clients[key] = client
	return client
}

func MustStart(ctx context.Context, listenAddress, serverID, serverToken, peers string) {
	err := Start(ctx, listenAddress, serverID, serverToken, peers)
	if err != nil {
		klog.TODO().Error(err, "Failed to start server")
		os.Exit(1)
	}
}

func Start(ctx context.Context, listenAddress, serverID, serverToken, peers string) error {
	handler := remotedialer.New(authorizer, remotedialer.DefaultErrorWriter)
	handler.PeerToken = serverToken
	handler.PeerID = serverID
	handler.PeerAuthorizer = func(req *http.Request, peer, token string) bool {
		return true
	}

	for _, peer := range strings.Split(peers, ",") {
		parts := strings.SplitN(strings.TrimSpace(peer), ":", 2)
		if len(parts) != 2 {
			continue
		}
		handler.AddPeer(ctx, parts[1], parts[0])
	}

	router := mux.NewRouter()
	router.Handle("/connect", handler)
	router.HandleFunc("/client/{id}/{scheme}/{host}{path:.*}", func(rw http.ResponseWriter, req *http.Request) {
		Client(handler, rw, req)
	})

	fmt.Println("Listening on ", listenAddress)
	go func() {
		if err := http.ListenAndServe(listenAddress, router); err != nil {
			klog.TODO().Error(err, "Failed to listen and serve")
			os.Exit(1)
		}
	}()

	return nil
}
