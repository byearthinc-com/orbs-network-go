// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

/*
Package memory provides an in-memory implementation of the Gossip Transport adapter, meant for usage in fast tests that
should not use the TCP-based adapter, such as acceptance tests or sociable unit tests, or in other in-process network use cases
*/
package memory

import (
	"context"
	"github.com/orbs-network/govnr"
	"github.com/orbs-network/orbs-network-go/instrumentation/logfields"
	"github.com/orbs-network/orbs-network-go/instrumentation/trace"
	"github.com/orbs-network/orbs-network-go/services/gossip/adapter"
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/orbs-spec/types/go/protocol/gossipmessages"
	"github.com/orbs-network/scribe/log"
	"sync"
	"time"
)

const SEND_QUEUE_MAX_MESSAGES = 1000
const LISTENER_HANDLE_TIMEOUT = 7 * time.Second

var LogTag = log.String("adapter", "gossip")

type message struct {
	payloads     [][]byte
	traceContext *trace.Context
}

type memoryTransport struct {
	sync.RWMutex
	govnr.TreeSupervisor
	peers    map[string]*peer
	transmit adapter.TransmitFunc
}

func NewTransport(ctx context.Context, logger log.Logger, nodes []primitives.NodeAddress) *memoryTransport {
	transport := &memoryTransport{
		peers:    make(map[string]*peer),
		transmit: nil,
	}
	transport.transmit = func(ctx context.Context, peerAddress primitives.NodeAddress, data *adapter.TransportData) {
		transport.peers[peerAddress.KeyForMap()].send(ctx, data)
	}

	transport.Lock()
	defer transport.Unlock()
	for _, node := range nodes {
		peer := newPeer(ctx, node, logger.WithTags(LogTag, log.Stringable("node", node)), len(nodes))
		transport.peers[node.KeyForMap()] = peer
		transport.Supervise(peer)
	}

	return transport
}

func (p *memoryTransport) UpdateTopology(bgCtx context.Context, newPeers adapter.TransportPeers) {
//	currently does nothing on purpose
}

func (p *memoryTransport) GracefulShutdown(shutdownContext context.Context) {
	p.Lock()
	defer p.Unlock()
	for _, peer := range p.peers {
		peer.cancel()
	}
}

func (p *memoryTransport) RegisterListener(listener adapter.TransportListener, nodeAddress primitives.NodeAddress) {
	p.Lock()
	defer p.Unlock()
	p.peers[string(nodeAddress)].attach(listener)
}

func defaultInterceptor(ctx context.Context, peerAddress primitives.NodeAddress, data *adapter.TransportData, transmit adapter.TransmitFunc) error {
	transmit(ctx, peerAddress, data)
	return nil
}

func (p *memoryTransport) SendWithInterceptor(ctx context.Context, data *adapter.TransportData, intercept adapter.InterceptorFunc) error {
	if intercept == nil {
		intercept = defaultInterceptor
	}

	var lastError error

	switch data.RecipientMode {

	case gossipmessages.RECIPIENT_LIST_MODE_BROADCAST:
		for key, peer := range p.peers {
			if key != data.SenderNodeAddress.KeyForMap() {
				err := intercept(ctx, peer.nodeAddress, data, p.transmit)
				if err != nil {
					lastError = err
				}
			}
		}

	case gossipmessages.RECIPIENT_LIST_MODE_LIST:
		for _, k := range data.RecipientNodeAddresses {
			err := intercept(ctx, k, data, p.transmit)
			if err != nil {
				lastError = err
			}
		}

	case gossipmessages.RECIPIENT_LIST_MODE_ALL_BUT_LIST:
		panic("Not implemented")
	}

	return lastError
}

func (p *memoryTransport) Send(ctx context.Context, data *adapter.TransportData) error {
	return p.SendWithInterceptor(ctx, data, nil)
}

type peer struct {
	govnr.TreeSupervisor
	socket      chan message
	listener    chan adapter.TransportListener
	logger      log.Logger
	cancel      context.CancelFunc
	nodeAddress primitives.NodeAddress
}

func newPeer(parent context.Context, nodeAddress primitives.NodeAddress, logger log.Logger, totalPeers int) *peer {
	ctx, cancel := context.WithCancel(parent)
	p := &peer{
		// channel is buffered on purpose, otherwise the whole network is synced on transport
		// we also multiply by number of peers because we have one logical "socket" for combined traffic from all peers together
		// we decided not to separate sockets between every 2 peers (like tcp transport) because:
		//  1) nodes in production tend to broadcast messages, so traffic is usually combined anyways
		//  2) the implementation complexity to mimic tcp transport isn't justified
		socket:      make(chan message, SEND_QUEUE_MAX_MESSAGES*totalPeers),
		listener:    make(chan adapter.TransportListener),
		logger:      logger,
		cancel:      cancel,
		nodeAddress: nodeAddress,
	}

	p.Supervise(govnr.Forever(ctx, "In-memory transport peer", logfields.GovnrErrorer(logger), func() {
		// wait till we have a listener attached
		select {
		case l := <-p.listener:
			logger.Info("connecting to listener", log.Stringable("listener", l))
			defer logger.Info("disconnecting from listener", log.Stringable("listener", l))

			p.acceptUsing(ctx, l)
		case <-ctx.Done():
			// fall through
		}
	}))

	return p
}

func (p *peer) attach(listener adapter.TransportListener) {
	p.listener <- listener
}

func (p *peer) send(ctx context.Context, data *adapter.TransportData) {
	p.logger.Info("depositing message into peer queue", trace.LogFieldFrom(ctx))
	before := time.Now()
	tracingContext, _ := trace.FromContext(ctx)
	select {
	case p.socket <- message{payloads: data.Payloads, traceContext: tracingContext}:
		p.logger.Info("deposited message into peer queue", trace.LogFieldFrom(ctx), log.Stringable("duration", time.Since(before)))
		return
	case <-ctx.Done():
		p.logger.Info("memory transport sending message after shutdown", log.Error(ctx.Err()))
		return
	default:
		p.logger.Error("memory transport send buffer is full")
		return
	}
}

func (p *peer) acceptUsing(bgCtx context.Context, listener adapter.TransportListener) {
	for {
		p.logger.Info("reading a message from socket", log.Int("socket-size", len(p.socket)))
		select {
		case message := <-p.socket:
			receive(bgCtx, listener, message)
		case <-bgCtx.Done():
			p.logger.Info("shutting down", log.Error(bgCtx.Err()), log.Int("socket-size", len(p.socket)))
			return
		}
	}
}

func receive(bgCtx context.Context, listener adapter.TransportListener, message message) {
	ctx, cancel := context.WithTimeout(bgCtx, LISTENER_HANDLE_TIMEOUT)
	defer cancel()
	traceContext := contextFrom(ctx, message)
	listener.OnTransportMessageReceived(traceContext, message.payloads)
}

func contextFrom(ctx context.Context, message message) context.Context {
	if message.traceContext == nil {
		return trace.NewContext(ctx, "memory-transport")
	} else {
		return trace.PropagateContext(ctx, message.traceContext)
	}
}
