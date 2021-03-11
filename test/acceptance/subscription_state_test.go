// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

// +build unsafetests

package acceptance

import (
	"context"
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/orbs-spec/types/go/protocol"
	"github.com/orbs-network/orbs-spec/types/go/protocol/consensus"
	"github.com/orbs-network/scribe/log"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSubscription_WhenSubscriptionNonActiveCreateEmptyBlocks(t *testing.T) {
	NewHarness().
		WithNumNodes(6).
		WithManagementPollingInterval(20*time.Millisecond).
		WithLogFilters(log.DiscardAll()).
		WithConsensusAlgos(consensus.CONSENSUS_ALGO_TYPE_LEAN_HELIX).
		Start(t, func(t testing.TB, ctx context.Context, network *Network) {
			token := network.DeployBenchmarkTokenContract(ctx, 5)

			response, txHash := token.Transfer(ctx, 0, 17, 5, 6)
			network.WaitForTransactionInNodeState(ctx, txHash, 0)
			require.Equal(t, response.TransactionStatus(), protocol.TRANSACTION_STATUS_COMMITTED)
			require.EqualValues(t, 17, token.GetBalance(ctx, 0, 6))
			txs, err := network.BlockPersistence(0).GetTransactionsBlock(response.RequestResult().BlockHeight())
			require.NoError(t, err)
			require.EqualValues(t, 2, txs.Header.NumSignedTransactions(), "should have 2 tx : transfer + trigger")

			t.Log("stop subscription")
			newRefTime := GenerateNewManagementReferenceTime(0)
			setSubscriptionActiveAndWait(t, ctx, network,  newRefTime, false)

			response, _ = token.Transfer(ctx, 0, 17, 5, 6)
			require.Equal(t, response.TransactionStatus(), protocol.TRANSACTION_STATUS_REJECTED_GLOBAL_PRE_ORDER)
			require.EqualValues(t, 17, token.GetBalance(ctx, 0, 6))
			txs, err = network.BlockPersistence(0).GetTransactionsBlock(response.RequestResult().BlockHeight())
			require.NoError(t, err)
			require.EqualValues(t, 1, txs.Header.NumSignedTransactions(), "should have 1 tx : trigger")

			t.Log("start subscription")
			newRefTime = GenerateNewManagementReferenceTime(newRefTime)
			setSubscriptionActiveAndWait(t, ctx, network,  newRefTime, false)

			response, txHash = token.Transfer(ctx, 0, 17, 5, 6)
			network.WaitForTransactionInNodeState(ctx, txHash, 0)
			require.Equal(t, response.TransactionStatus(), protocol.TRANSACTION_STATUS_COMMITTED)
			require.EqualValues(t, 34, token.GetBalance(ctx, 0, 6))

			t.Log("test done, shutting down")
		})
}

func TestSubscription_WhenSubscriptionNumKeysFullCreateEmptyBlocks(t *testing.T) {
	NewHarness().
		WithNumNodes(6).
		WithManagementPollingInterval(20*time.Millisecond).
		WithLogFilters(log.DiscardAll()).
		WithConsensusAlgos(consensus.CONSENSUS_ALGO_TYPE_LEAN_HELIX).
		Start(t, func(t testing.TB, ctx context.Context, network *Network) {
			token := network.DeployBenchmarkTokenContract(ctx, 5)

			response, txHash := token.Transfer(ctx, 0, 17, 5, 6)
			network.WaitForTransactionInNodeState(ctx, txHash, 0)
			require.Equal(t, response.TransactionStatus(), protocol.TRANSACTION_STATUS_COMMITTED)
			require.EqualValues(t, 17, token.GetBalance(ctx, 0, 6))
			balance := token.GetBalance(ctx, 0, 5)
			txs, err := network.BlockPersistence(0).GetTransactionsBlock(response.RequestResult().BlockHeight())
			require.NoError(t, err)
			require.EqualValues(t, 2, txs.Header.NumSignedTransactions(), "should have 2 tx : transfer + trigger")

			t.Log("make num keys 2")
			newRefTime := GenerateNewManagementReferenceTime(0)
			setSubscriptionSizeAndWait(t, ctx, network,  newRefTime, 2, 100)

			response, _ = token.Transfer(ctx, 0, 17, 5, 7)
			require.Equal(t, response.TransactionStatus(), protocol.TRANSACTION_STATUS_REJECTED_GLOBAL_PRE_ORDER)
			require.EqualValues(t, balance, token.GetBalance(ctx, 0, 5), "addr 5 didn't transfer")
			txs, err = network.BlockPersistence(0).GetTransactionsBlock(response.RequestResult().BlockHeight())
			require.NoError(t, err)
			require.EqualValues(t, 1, txs.Header.NumSignedTransactions(), "should have 1 tx : trigger")

			t.Log("make num keys 100")
			newRefTime = GenerateNewManagementReferenceTime(newRefTime)
			setSubscriptionSizeAndWait(t, ctx, network,  newRefTime, 100, 100)

			response, txHash = token.Transfer(ctx, 0, 17, 5, 7)
			network.WaitForTransactionInNodeState(ctx, txHash, 0)
			require.Equal(t, response.TransactionStatus(), protocol.TRANSACTION_STATUS_COMMITTED)
			require.EqualValues(t, 17, token.GetBalance(ctx, 0, 7))

			t.Log("test done, shutting down")
		})
}

func setSubscriptionActiveAndWait(t testing.TB, ctx context.Context, network *Network, refTime primitives.TimestampSeconds, isActive bool) primitives.BlockHeight {
	return setSubscriptionImplAndWait(t, ctx, network, refTime, isActive, 1000, 100)
}

func setSubscriptionSizeAndWait(t testing.TB, ctx context.Context, network *Network, refTime primitives.TimestampSeconds, maxKeys int, maxSize int) primitives.BlockHeight {
	return setSubscriptionImplAndWait(t, ctx, network, refTime, true, maxKeys, maxSize)
}

func setSubscriptionImplAndWait(t testing.TB, ctx context.Context, network *Network, refTime primitives.TimestampSeconds, isActive bool, maxKeys int, maxSizeMb int) primitives.BlockHeight {
	err := network.committeeProvider.AddSubscription(refTime, isActive, primitives.StorageKeys(maxKeys), primitives.StorageSizeMegabyte(maxSizeMb))
	require.NoError(t, err)

	bh, err := network.WaitForManagementChange(ctx, 0, refTime)
	require.NoError(t, err)
	return bh
}
