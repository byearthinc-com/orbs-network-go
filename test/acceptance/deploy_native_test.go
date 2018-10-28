package acceptance

import (
	"context"
	"github.com/orbs-network/orbs-network-go/test/contracts"
	"github.com/orbs-network/orbs-network-go/test/harness"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNonLeaderDeploysNativeContract(t *testing.T) {
	harness.Network(t).Start(func(ctx context.Context, network harness.InProcessNetwork) {

		t.Log("testing", network.Description()) // leader is nodeIndex 0, validator is nodeIndex 1

		counterStart := contracts.MOCK_COUNTER_CONTRACT_START_FROM

		t.Log("deploying contract")

		<-network.SendDeployCounterContract(ctx, 1)
		require.EqualValues(t, counterStart, <-network.CallCounterGet(ctx, 0), "get counter after deploy")

		t.Log("transacting with contract")

		<-network.SendCounterAdd(ctx, 1, 17)
		require.EqualValues(t, counterStart+17, <-network.CallCounterGet(ctx, 0), "get counter after transaction")

	})
}