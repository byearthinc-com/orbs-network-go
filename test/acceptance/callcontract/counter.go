package callcontract

import (
	"context"
	"fmt"
	"github.com/orbs-network/orbs-network-go/test/builders"
	"github.com/orbs-network/orbs-network-go/test/contracts"
	"github.com/orbs-network/orbs-network-go/test/crypto/keys"
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/orbs-spec/types/go/protocol"
	"github.com/orbs-network/orbs-spec/types/go/protocol/client"
)

type CounterClient interface {
	DeployNativeCounterContract(ctx context.Context, nodeIndex int, fromAddressIndex int) (*client.SendTransactionResponse, primitives.Sha256)
	CounterAdd(ctx context.Context, nodeIndex int, amount uint64) (*client.SendTransactionResponse, primitives.Sha256)
	CounterGet(ctx context.Context, nodeIndex int) uint64
}

func (c *contractClient) DeployNativeCounterContract(ctx context.Context, nodeIndex int, fromAddressIndex int) (*client.SendTransactionResponse, primitives.Sha256) {
	counterStart := contracts.MOCK_COUNTER_CONTRACT_START_FROM

	tx := builders.Transaction().
		WithMethod("_Deployments", "deployService").
		WithArgs(
			fmt.Sprintf("CounterFrom%d", counterStart),
			uint32(protocol.PROCESSOR_TYPE_NATIVE),
			[]byte(contracts.NativeSourceCodeForCounter(counterStart)),
		).
		WithEd25519Signer(keys.Ed25519KeyPairForTests(fromAddressIndex)).
		Builder()

	return c.API.SendTransaction(ctx, tx, nodeIndex)
}

func (c *contractClient) CounterAdd(ctx context.Context, nodeIndex int, amount uint64) (*client.SendTransactionResponse, primitives.Sha256) {
	counterStart := contracts.MOCK_COUNTER_CONTRACT_START_FROM

	tx := builders.Transaction().
		WithMethod(primitives.ContractName(fmt.Sprintf("CounterFrom%d", counterStart)), "add").
		WithArgs(amount).
		Builder()

	return c.API.SendTransaction(ctx, tx, nodeIndex)
}

func (c *contractClient) CounterGet(ctx context.Context, nodeIndex int) uint64 {
	counterStart := contracts.MOCK_COUNTER_CONTRACT_START_FROM

	query := builders.Query().
		WithMethod(primitives.ContractName(fmt.Sprintf("CounterFrom%d", counterStart)), "get").
		WithArgs().
		Builder()

	out := c.API.RunQuery(ctx, query, nodeIndex)
	argsArray := builders.PackedArgumentArrayDecode(out.QueryResult().RawOutputArgumentArrayWithHeader())
	return argsArray.ArgumentsIterator().NextArguments().Uint64Value()
}