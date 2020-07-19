// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

package bootstrap

import (
	"context"
	"fmt"
	"github.com/orbs-network/crypto-lib-go/crypto/signer"
	"github.com/orbs-network/govnr"
	"github.com/orbs-network/orbs-network-go/config"
	"github.com/orbs-network/orbs-network-go/instrumentation/metric"
	"github.com/orbs-network/orbs-network-go/instrumentation/reporters"
	"github.com/orbs-network/orbs-network-go/instrumentation/trace"
	"github.com/orbs-network/orbs-network-go/services/blockstorage"
	blockStorageAdapter "github.com/orbs-network/orbs-network-go/services/blockstorage/adapter"
	"github.com/orbs-network/orbs-network-go/services/blockstorage/servicesync"
	"github.com/orbs-network/orbs-network-go/services/consensusalgo/benchmarkconsensus"
	"github.com/orbs-network/orbs-network-go/services/consensusalgo/leanhelixconsensus"
	"github.com/orbs-network/orbs-network-go/services/consensuscontext"
	"github.com/orbs-network/orbs-network-go/services/crosschainconnector/ethereum"
	ethereumAdapter "github.com/orbs-network/orbs-network-go/services/crosschainconnector/ethereum/adapter"
	"github.com/orbs-network/orbs-network-go/services/gossip"
	gossipAdapter "github.com/orbs-network/orbs-network-go/services/gossip/adapter"
	"github.com/orbs-network/orbs-network-go/services/management"
	"github.com/orbs-network/orbs-network-go/services/processor/native"
	nativeProcessorAdapter "github.com/orbs-network/orbs-network-go/services/processor/native/adapter"
	"github.com/orbs-network/orbs-network-go/services/publicapi"
	"github.com/orbs-network/orbs-network-go/services/statestorage"
	stateStorageAdapter "github.com/orbs-network/orbs-network-go/services/statestorage/adapter"
	"github.com/orbs-network/orbs-network-go/services/transactionpool"
	txPoolAdapter "github.com/orbs-network/orbs-network-go/services/transactionpool/adapter"
	"github.com/orbs-network/orbs-network-go/services/virtualmachine"
	"github.com/orbs-network/orbs-spec/types/go/protocol"
	"github.com/orbs-network/orbs-spec/types/go/protocol/consensus"
	"github.com/orbs-network/orbs-spec/types/go/services"
	"github.com/orbs-network/scribe/log"
	"github.com/pkg/errors"
)

type NodeLogic interface {
	govnr.ShutdownWaiter
	PublicApi() services.PublicApi
}

type nodeLogic struct {
	govnr.TreeSupervisor
	publicApi      services.PublicApi
	consensusAlgos []services.ConsensusAlgo
}

func NewNodeLogic(parentCtx context.Context,
	gossipTransport gossipAdapter.Transport,
	blockPersistence blockStorageAdapter.BlockPersistence,
	statePersistence stateStorageAdapter.StatePersistence,
	stateBlockHeightReporter stateStorageAdapter.BlockHeightReporter,
	transactionPoolBlockHeightReporter transactionpool.BlockHeightReporter,
	maybeClock txPoolAdapter.Clock, nativeCompiler nativeProcessorAdapter.Compiler,
	managementProvider management.Provider,
	logger log.Logger, metricRegistry metric.Registry, nodeConfig config.NodeConfig,
	ethereumConnection ethereumAdapter.EthereumConnection) NodeLogic {

	ctx := trace.ContextWithNodeId(parentCtx, nodeConfig.NodeAddress().String())

	err := config.ValidateNodeLogic(nodeConfig)
	if err != nil {
		logger.Error("Node logic error cannot start", log.Error(err))
		panic(err)
	}

	processors := make(map[protocol.ProcessorType]services.Processor)
	processors[protocol.PROCESSOR_TYPE_NATIVE] = native.NewNativeProcessor(nativeCompiler, nodeConfig, logger, metricRegistry)
	addExtraProcessors(processors, nodeConfig, logger)

	crosschainConnectors := make(map[protocol.CrosschainConnectorType]services.CrosschainConnector)
	crosschainConnectors[protocol.CROSSCHAIN_CONNECTOR_TYPE_ETHEREUM] = ethereum.NewEthereumCrosschainConnector(ethereumConnection, nodeConfig, logger, metricRegistry)

	signer, err := signer.New(nodeConfig)
	if err != nil {
		logger.Error("Node logic signer error cannot start", log.Error(err))
		panic(fmt.Sprintf("Node logic signer error cannot start: %s", err))
	}

	gossipService := gossip.NewGossip(ctx, gossipTransport, nodeConfig, logger, metricRegistry)
	management := management.NewManagement(ctx, nodeConfig, managementProvider, gossipService, logger, metricRegistry)
	stateStorageService := statestorage.NewStateStorage(nodeConfig, statePersistence, stateBlockHeightReporter, logger, metricRegistry)
	virtualMachineService := virtualmachine.NewVirtualMachine(stateStorageService, processors, crosschainConnectors, management, nodeConfig, logger)
	transactionPoolService := transactionpool.NewTransactionPool(ctx, maybeClock, gossipService, virtualMachineService, signer, transactionPoolBlockHeightReporter, nodeConfig, logger, metricRegistry)
	serviceSyncCommitters := []servicesync.BlockPairCommitter{servicesync.NewStateStorageCommitter(stateStorageService), servicesync.NewTxPoolCommitter(transactionPoolService)}
	blockStorageService := blockstorage.NewBlockStorage(ctx, nodeConfig, blockPersistence, gossipService, logger, metricRegistry, serviceSyncCommitters)
	publicApiService := publicapi.NewPublicApi(nodeConfig, transactionPoolService, virtualMachineService, blockStorageService, logger, metricRegistry)
	consensusContextService := consensuscontext.NewConsensusContext(transactionPoolService, virtualMachineService, stateStorageService, management, nodeConfig, logger, metricRegistry)

	consensusAlgo := createConsensusAlgo(nodeConfig)(ctx, gossipService, blockStorageService, consensusContextService, management, signer, logger, metricRegistry)

	logger.Info("Node started")

	node := &nodeLogic{
		publicApi:      publicApiService,
		consensusAlgos: []services.ConsensusAlgo{consensusAlgo},
	}

	node.Supervise(management)
	node.Supervise(gossipService)
	node.Supervise(blockStorageService)
	node.Supervise(consensusAlgo)
	node.Supervise(reporters.NewSystemReporter(ctx, metricRegistry, logger))
	node.Supervise(reporters.NewRuntimeReporter(ctx, metricRegistry, logger))
	node.Supervise(metricRegistry.PeriodicallyRotate(ctx, logger))
	if nodeConfig.NTPEndpoint() != "" {
		node.Supervise(reporters.NewNtpReporter(ctx, metricRegistry, logger, nodeConfig.NTPEndpoint()))
	}

	return node
}

type consensusAlgo interface {
	services.ConsensusAlgo
	govnr.ShutdownWaiter
}

func createConsensusAlgo(nodeConfig config.NodeConfig) func(ctx context.Context,
	gossip services.Gossip,
	blockStorage services.BlockStorage,
	consensusContext services.ConsensusContext,
	management services.Management,
	signer signer.Signer,
	parentLogger log.Logger,
	metricFactory metric.Factory) consensusAlgo {

	return func(ctx context.Context, gossip services.Gossip, blockStorage services.BlockStorage, consensusContext services.ConsensusContext, management services.Management, signer signer.Signer, parentLogger log.Logger, metricFactory metric.Factory) consensusAlgo {
		switch nodeConfig.ActiveConsensusAlgo() {
		case consensus.CONSENSUS_ALGO_TYPE_LEAN_HELIX:
			return leanhelixconsensus.NewLeanHelixConsensusAlgo(ctx, gossip, blockStorage, consensusContext, signer, parentLogger, nodeConfig, metricFactory)
		case consensus.CONSENSUS_ALGO_TYPE_BENCHMARK_CONSENSUS:
			// TODO https://github.com/orbs-network/orbs-network-go/issues/1602 improve connection between benchmark and management
			ref, err := management.GetCurrentReference(ctx, &services.GetCurrentReferenceInput{})
			if err != nil {
				panic(errors.Errorf("benchmark cannot start with no current ref %s", err))
			}
			committee, err := management.GetCommittee(ctx, &services.GetCommitteeInput{Reference: ref.CurrentReference})
			if err != nil {
				panic(errors.Errorf("benchmark cannot start with no committee %s", err))
			}
			return benchmarkconsensus.NewBenchmarkConsensusAlgo(ctx, gossip, blockStorage, consensusContext, committee.Members, signer, parentLogger, nodeConfig, metricFactory)
		default:
			panic(errors.Errorf("unknown consensus algo type %s", nodeConfig.ActiveConsensusAlgo()))
		}
	}
}

func (n *nodeLogic) PublicApi() services.PublicApi {
	return n.publicApi
}
