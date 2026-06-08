// Package client contains client-related structures and logic.
package client

import (
	"github.com/AdguardTeam/dnsproxy/proxy"
)

// Client is an interface for DNS clients.
type Client interface {
	// Upstreams returns the upstream configuration for the client.  uc must not
	// be nil.
	Upstreams() (uc *proxy.CustomUpstreamConfig)
}

// StaticClient is a [Client] implementation that returns a static upstream
// config.
type StaticClient struct {
	// conf is an upstream configuration.  It must not be nil.
	conf *proxy.CustomUpstreamConfig
}

// NewStaticClient creates a new properly initialized StaticClient.  conf must
// be valid.
func NewStaticClient(conf *proxy.CustomUpstreamConfig) (sc *StaticClient) {
	return &StaticClient{
		conf: conf,
	}
}

// type check
var _ Client = (*StaticClient)(nil)

// Upstreams implements the [Client] interface for *StaticClient.
func (s *StaticClient) Upstreams() (uc *proxy.CustomUpstreamConfig) {
	return s.conf
}
