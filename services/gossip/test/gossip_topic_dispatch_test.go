package test

import (
	"context"
	"github.com/orbs-network/go-mock"
	"github.com/orbs-network/orbs-network-go/config"
	"github.com/orbs-network/orbs-network-go/services/gossip"
	"github.com/orbs-network/orbs-network-go/services/gossip/adapter"
	"github.com/orbs-network/orbs-network-go/services/gossip/adapter/memory"
	"github.com/orbs-network/orbs-network-go/services/gossip/codec"
	"github.com/orbs-network/orbs-network-go/test"
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/orbs-spec/types/go/protocol/gossipmessages"
	"github.com/orbs-network/orbs-spec/types/go/services/gossiptopics"
	"github.com/orbs-network/scribe/log"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type conf struct {
}

func (c *conf) NodeAddress() primitives.NodeAddress {
	return []byte{0x01}
}

func (c *conf) VirtualChainId() primitives.VirtualChainId {
	return 42
}

func TestDifferentTopicsDoNotBlockEachOtherForSamePeer(t *testing.T) {
	test.WithContext(func(ctx context.Context) {
		logger := log.DefaultTestingLogger(t)
		nodeAddresses := []primitives.NodeAddress{{0x01}, {0x02}}
		cfg := &conf{}

		genesisValidatorNodes := make(map[string]config.ValidatorNode)
		for _, address := range nodeAddresses {
			genesisValidatorNodes[address.KeyForMap()] = config.NewHardCodedValidatorNode(primitives.NodeAddress(address))
		}
		transport := memory.NewTransport(ctx, logger, genesisValidatorNodes)
		g := gossip.NewGossip(transport, cfg, logger)

		trh := &gossiptopics.MockTransactionRelayHandler{}
		bsh := &gossiptopics.MockBlockSyncHandler{}

		g.RegisterTransactionRelayHandler(trh)
		g.RegisterBlockSyncHandler(bsh)

		bsh.When("HandleBlockAvailabilityRequest", mock.Any, mock.Any).Call(func(nested context.Context, input *gossiptopics.BlockAvailabilityRequestInput) {
			time.Sleep(1 * time.Hour)
		})

		trh.When("HandleForwardedTransactions", mock.Any, mock.Any).Times(1).Return(&gossiptopics.EmptyOutput{}, nil)

		require.NoError(t, transport.Send(ctx, &adapter.TransportData{
			SenderNodeAddress: cfg.NodeAddress(),
			RecipientMode:     gossipmessages.RECIPIENT_LIST_MODE_BROADCAST,
			Payloads:          aBlockSyncRequest(),
		}))

		require.NoError(t, transport.Send(ctx, &adapter.TransportData{
			SenderNodeAddress: cfg.NodeAddress(),
			RecipientMode:     gossipmessages.RECIPIENT_LIST_MODE_BROADCAST,
			Payloads:          aTransactionRelayRequest(),
		}))

		require.NoError(t, test.EventuallyVerify(1*time.Second, trh, bsh), "mocks were not invoked as expected")

	})
}

func aBlockSyncRequest() [][]byte {
	message := &gossipmessages.BlockAvailabilityRequestMessage{
		SignedBatchRange: (&gossipmessages.BlockSyncRangeBuilder{
			BlockType:                gossipmessages.BLOCK_TYPE_BLOCK_PAIR,
			FirstBlockHeight:         1001,
			LastBlockHeight:          2001,
			LastCommittedBlockHeight: 3001,
		}).Build(),
		Sender: (&gossipmessages.SenderSignatureBuilder{
			SenderNodeAddress: []byte{0x01, 0x02, 0x03},
			Signature:         []byte{0x04, 0x05, 0x06},
		}).Build(),
	}
	payloads, _ := codec.EncodeBlockAvailabilityRequest((&gossipmessages.HeaderBuilder{}).Build(), message)
	return payloads
}

func aTransactionRelayRequest() [][]byte {
	header := (&gossipmessages.HeaderBuilder{
		Topic:            gossipmessages.HEADER_TOPIC_TRANSACTION_RELAY,
		TransactionRelay: gossipmessages.TRANSACTION_RELAY_FORWARDED_TRANSACTIONS,
		RecipientMode:    gossipmessages.RECIPIENT_LIST_MODE_BROADCAST,
		VirtualChainId:   42,
	}).Build()

	payloads, _ := codec.EncodeForwardedTransactions(header, &gossipmessages.ForwardedTransactionsMessage{})
	return payloads
}
