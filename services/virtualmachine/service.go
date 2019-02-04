package virtualmachine

import (
	"context"
	"github.com/orbs-network/orbs-network-go/instrumentation/log"
	"github.com/orbs-network/orbs-network-go/instrumentation/trace"
	"github.com/orbs-network/orbs-network-go/services/processor/native"
	"github.com/orbs-network/orbs-spec/types/go/protocol"
	"github.com/orbs-network/orbs-spec/types/go/services"
	"github.com/orbs-network/orbs-spec/types/go/services/handlers"
	"github.com/pkg/errors"
)

var LogTag = log.Service("virtual-machine")

type service struct {
	stateStorage         services.StateStorage
	processors           map[protocol.ProcessorType]services.Processor
	crosschainConnectors map[protocol.CrosschainConnectorType]services.CrosschainConnector
	logger               log.BasicLogger

	contexts *executionContextProvider
}

func NewVirtualMachine(
	stateStorage services.StateStorage,
	processors map[protocol.ProcessorType]services.Processor,
	crosschainConnectors map[protocol.CrosschainConnectorType]services.CrosschainConnector,
	logger log.BasicLogger,
) services.VirtualMachine {

	s := &service{
		processors:           processors,
		crosschainConnectors: crosschainConnectors,
		stateStorage:         stateStorage,
		logger:               logger.WithTags(LogTag),

		contexts: newExecutionContextProvider(),
	}

	for _, processor := range processors {
		processor.RegisterContractSdkCallHandler(s)
	}

	return s
}

func (s *service) ProcessQuery(ctx context.Context, input *services.ProcessQueryInput) (*services.ProcessQueryOutput, error) {
	logger := s.logger.WithTags(trace.LogFieldFrom(ctx))

	if input.BlockHeight != 0 {
		s.logger.Panic("Run local method with specific block height is not yet supported")
	}

	committedBlockHeight, committedBlockTimestamp, err := s.getRecentCommittedBlockHeight(ctx)
	if err != nil {
		return &services.ProcessQueryOutput{
			CallResult:              protocol.EXECUTION_RESULT_ERROR_UNEXPECTED,
			OutputArgumentArray:     []byte{},
			ReferenceBlockHeight:    committedBlockHeight,
			ReferenceBlockTimestamp: committedBlockTimestamp,
		}, err
	}

	logger.Info("running local method", log.Stringable("contract", input.SignedQuery.Query().ContractName()), log.Stringable("method", input.SignedQuery.Query().MethodName()), log.BlockHeight(committedBlockHeight))
	callResult, outputArgs, outputEvents, err := s.runMethod(ctx, committedBlockHeight, committedBlockHeight, committedBlockTimestamp, input.SignedQuery.Query(), protocol.ACCESS_SCOPE_READ_ONLY, nil)
	if outputArgs == nil {
		outputArgs = (&protocol.ArgumentArrayBuilder{}).Build()
	}
	if outputEvents == nil {
		outputEvents = (&protocol.EventsArrayBuilder{}).Build()
	}

	return &services.ProcessQueryOutput{
		CallResult:              callResult,
		OutputEventsArray:       outputEvents.RawEventsArray(),
		OutputArgumentArray:     outputArgs.RawArgumentsArray(),
		ReferenceBlockHeight:    committedBlockHeight,
		ReferenceBlockTimestamp: committedBlockTimestamp,
	}, err
}

func (s *service) ProcessTransactionSet(ctx context.Context, input *services.ProcessTransactionSetInput) (*services.ProcessTransactionSetOutput, error) {
	logger := s.logger.WithTags(trace.LogFieldFrom(ctx))

	logger.Info("processing transaction set", log.Int("num-transactions", len(input.SignedTransactions)), log.BlockHeight(input.CurrentBlockHeight))
	receipts, stateDiffs := s.processTransactionSet(ctx, input.CurrentBlockHeight, input.CurrentBlockTimestamp, input.SignedTransactions)

	return &services.ProcessTransactionSetOutput{
		TransactionReceipts: receipts,
		ContractStateDiffs:  stateDiffs,
	}, nil
}

func (s *service) TransactionSetPreOrder(ctx context.Context, input *services.TransactionSetPreOrderInput) (*services.TransactionSetPreOrderOutput, error) {
	logger := s.logger.WithTags(trace.LogFieldFrom(ctx))

	statuses := make([]protocol.TransactionStatus, len(input.SignedTransactions))

	// check subscription
	err := s.callGlobalPreOrderSystemContract(ctx, input.CurrentBlockHeight, input.CurrentBlockTimestamp)
	if err != nil {
		for i := 0; i < len(statuses); i++ {
			statuses[i] = protocol.TRANSACTION_STATUS_REJECTED_GLOBAL_PRE_ORDER
		}
	} else {
		// check signatures
		s.verifyTransactionSignatures(input.SignedTransactions, statuses)
	}

	if err != nil {
		logger.Info("performed pre order checks", log.Error(err), log.BlockHeight(input.CurrentBlockHeight), log.Int("num-statuses", len(statuses)))
	} else {
		logger.Info("performed pre order checks", log.BlockHeight(input.CurrentBlockHeight), log.Int("num-statuses", len(statuses)))
	}

	return &services.TransactionSetPreOrderOutput{
		PreOrderResults: statuses,
	}, err
}

func (s *service) HandleSdkCall(ctx context.Context, input *handlers.HandleSdkCallInput) (*handlers.HandleSdkCallOutput, error) {
	var output []*protocol.Argument
	var err error

	executionContext := s.contexts.loadExecutionContext(input.ContextId)
	if executionContext == nil {
		return nil, errors.Errorf("invalid execution context %s", input.ContextId)
	}

	switch input.OperationName {
	case native.SDK_OPERATION_NAME_STATE:
		output, err = s.handleSdkStateCall(ctx, executionContext, input.MethodName, input.InputArguments, input.PermissionScope)
	case native.SDK_OPERATION_NAME_SERVICE:
		output, err = s.handleSdkServiceCall(ctx, executionContext, input.MethodName, input.InputArguments, input.PermissionScope)
	case native.SDK_OPERATION_NAME_EVENTS:
		output, err = s.handleSdkEventsCall(ctx, executionContext, input.MethodName, input.InputArguments, input.PermissionScope)
	case native.SDK_OPERATION_NAME_ETHEREUM:
		output, err = s.handleSdkEthereumCall(ctx, executionContext, input.MethodName, input.InputArguments, input.PermissionScope)
	case native.SDK_OPERATION_NAME_ADDRESS:
		output, err = s.handleSdkAddressCall(ctx, executionContext, input.MethodName, input.InputArguments, input.PermissionScope)
	case native.SDK_OPERATION_NAME_ENV:
		output, err = s.handleSdkEnvCall(ctx, executionContext, input.MethodName, input.InputArguments, input.PermissionScope)
	default:
		return nil, errors.Errorf("unknown SDK call operation: %s", input.OperationName)
	}

	if err != nil {
		return nil, err
	}

	return &handlers.HandleSdkCallOutput{
		OutputArguments: output,
	}, nil
}
