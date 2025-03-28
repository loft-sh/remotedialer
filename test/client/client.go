package client

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/loft-sh/remotedialer"
	"k8s.io/klog/v2"
)

func Start(ctx context.Context, clientID, serverAddress string) {
	headers := http.Header{
		"X-Tunnel-ID": []string{clientID},
	}

	go func() {
		for {
			err := remotedialer.ClientConnect(ctx, serverAddress, headers, nil, func(string, string) bool { return true }, nil)
			if err != nil && !errors.Is(err, context.Canceled) {
				klog.FromContext(ctx).Error(err, "Failed to connect to proxy")
				os.Exit(1)
			}
			klog.FromContext(ctx).Info("Client connect done")
		}
	}()
}
