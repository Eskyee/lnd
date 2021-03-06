// +build kvdb_etcd

package etcd

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/coreos/etcd/embed"
)

const (
	// readyTimeout is the time until the embedded etcd instance should start.
	readyTimeout = 10 * time.Second

	// defaultEtcdPort is the start of the range for listening ports of
	// embedded etcd servers. Ports are monotonically increasing starting
	// from this number and are determined by the results of getFreePort().
	defaultEtcdPort = 2379
)

var (
	// lastPort is the last port determined to be free for use by a new
	// embedded etcd server. It should be used atomically.
	lastPort uint32 = defaultEtcdPort
)

// getFreePort returns the first port that is available for listening by a new
// embedded etcd server. It panics if no port is found and the maximum available
// TCP port is reached.
func getFreePort() int {
	port := atomic.AddUint32(&lastPort, 1)
	for port < 65535 {
		// If there are no errors while attempting to listen on this
		// port, close the socket and return it as available.
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		l, err := net.Listen("tcp4", addr)
		if err == nil {
			err := l.Close()
			if err == nil {
				return int(port)
			}
		}
		port = atomic.AddUint32(&lastPort, 1)
	}

	// No ports available? Must be a mistake.
	panic("no ports available for listening")
}

// NewEmbeddedEtcdInstance creates an embedded etcd instance for testing,
// listening on random open ports. Returns the backend config and a cleanup
// func that will stop the etcd instance.
func NewEmbeddedEtcdInstance(path string) (*BackendConfig, func(), error) {
	cfg := embed.NewConfig()
	cfg.Dir = path

	// To ensure that we can submit large transactions.
	cfg.MaxTxnOps = 8192
	cfg.MaxRequestBytes = 16384 * 1024

	// Listen on random free ports.
	clientURL := fmt.Sprintf("127.0.0.1:%d", getFreePort())
	peerURL := fmt.Sprintf("127.0.0.1:%d", getFreePort())
	cfg.LCUrls = []url.URL{{Host: clientURL}}
	cfg.LPUrls = []url.URL{{Host: peerURL}}

	etcd, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, nil, err
	}

	select {
	case <-etcd.Server.ReadyNotify():
	case <-time.After(readyTimeout):
		etcd.Close()
		return nil, nil,
			fmt.Errorf("etcd failed to start after: %v", readyTimeout)
	}

	ctx, cancel := context.WithCancel(context.Background())

	connConfig := &BackendConfig{
		Ctx:                ctx,
		Host:               "http://" + peerURL,
		User:               "user",
		Pass:               "pass",
		InsecureSkipVerify: true,
	}

	return connConfig, func() {
		cancel()
		etcd.Close()
	}, nil
}
