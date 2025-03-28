package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/loft-sh/remotedialer"
	"github.com/loft-sh/remotedialer/test/client"
	"github.com/loft-sh/remotedialer/test/server"
	"k8s.io/klog/v2"
)

var (
	debug bool

	startPort = 8123
	count     = 3
)

func main() {
	flag.BoolVar(&debug, "debug", true, "Debug logging")
	flag.Parse()

	if debug {
		klogFlagSet := &flag.FlagSet{}
		klog.InitFlags(klogFlagSet)
		if err := klogFlagSet.Set("v", "10"); err != nil {
			klog.TODO().Error(err, "failed to set klog verbosity level")
			os.Exit(1)
		}
		if err := klogFlagSet.Parse([]string{}); err != nil {
			klog.TODO().Error(err, "failed to parse klog flags")
			os.Exit(1)
		}
		remotedialer.PrintTunnelData = true
	}

	ctx := context.Background()

	// start dummy server
	startDummy(":8122")

	// start multiple servers
	startServers(ctx, startPort, count)

	// wait for servers to be ready
	time.Sleep(time.Millisecond * 100)

	// start client
	client.Start(ctx, "client1", "ws://localhost:8123/connect")

	// wait for context to be done
	doRequests(ctx, startPort, count)
}

func doRequests(ctx context.Context, startPort int, count int) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second * 2):
			// get random port between startPort and startPort+count
			port := rand.Intn(count) + startPort

			// now make a request to the client 1
			url := fmt.Sprintf("http://localhost:%d/client/client1/http/127.0.0.1:8122", port)
			resp, err := http.Get(url)
			if err != nil {
				klog.TODO().Error(err, "Failed to make request")
				os.Exit(1)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				klog.TODO().Error(err, "Failed to read response")
				os.Exit(1)
			}

			klog.Info("Response: ", url, string(body))
		}
	}
}

func startServers(ctx context.Context, startPort int, count int) {
	for i := 0; i < count; i++ {
		// build peers
		peers := []string{}
		for j := 0; j < count; j++ {
			if j == i {
				continue
			}
			peers = append(peers, fmt.Sprintf("server%d:ws://localhost:%d/connect", j, startPort+j))
		}

		server.MustStart(ctx, fmt.Sprintf(":%d", startPort+i), fmt.Sprintf("server%d", i), fmt.Sprintf("token%d", i), strings.Join(peers, ","))
	}
}

func startDummy(listen string) {
	log.Println("listening ", listen)
	go func() {
		err := http.ListenAndServe(listen, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			rw.Write([]byte("Hello, World!"))
		}))
		if err != nil {
			klog.TODO().Error(err, "Failed to listen and serve")
			os.Exit(1)
		}
	}()
}
