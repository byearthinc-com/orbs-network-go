// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

package consensuscontext

import (
	"bytes"
	"context"
	"github.com/orbs-network/crypto-lib-go/crypto/validators"
	"github.com/orbs-network/orbs-network-go/config"
	"github.com/orbs-network/orbs-network-go/services/processor/native/repository/Triggers"
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/orbs-spec/types/go/protocol"
	"github.com/orbs-network/orbs-spec/types/go/services"
	"github.com/orbs-network/scribe/log"
	"github.com/pkg/errors"
	"time"
)

type txValidator func(ctx context.Context, vctx *txValidatorContext) error

// TODO v3 consider changing the way the validator works not to have a "context" see issue https://github.com/orbs-network/orbs-network-go/issues/1555
type txValidatorContext struct {
	virtualChainId         primitives.VirtualChainId
	allowedTimestampJitter time.Duration
	input                  *services.ValidateTransactionsBlockInput
}

func validateTxProtocolVersion(ctx context.Context, vctx *txValidatorContext) error {
	checkedProtocolVersion := vctx.input.TransactionsBlock.Header.ProtocolVersion()
	if checkedProtocolVersion > config.MAXIMAL_PROTOCOL_VERSION_SUPPORTED_VALUE {
		return errors.Wrapf(ErrMismatchedProtocolVersion, "maximal %v actual %v", config.MAXIMAL_PROTOCOL_VERSION_SUPPORTED_VALUE, checkedProtocolVersion)
	}
	if checkedProtocolVersion < config.MINIMAL_PROTOCOL_VERSION_SUPPORTED_VALUE {
		return errors.Wrapf(ErrMismatchedProtocolVersion, "minimal %v actual %v", config.MINIMAL_PROTOCOL_VERSION_SUPPORTED_VALUE, checkedProtocolVersion)
	}
	return nil
}

func validateTxVirtualChainID(ctx context.Context, vctx *txValidatorContext) error {
	expectedVirtualChainId := vctx.virtualChainId
	checkedVirtualChainId := vctx.input.TransactionsBlock.Header.VirtualChainId()
	if checkedVirtualChainId != vctx.virtualChainId {
		return errors.Wrapf(ErrMismatchedVirtualChainID, "expected %v actual %v", expectedVirtualChainId, checkedVirtualChainId)
	}
	return nil
}

func validateTxBlockHeight(ctx context.Context, vctx *txValidatorContext) error {
	expectedBlockHeight := vctx.input.CurrentBlockHeight
	checkedBlockHeight := vctx.input.TransactionsBlock.Header.BlockHeight()
	if checkedBlockHeight != expectedBlockHeight {
		return ErrMismatchedBlockHeight
	}
	return nil
}

func validateTxPrevBlockHashPtr(ctx context.Context, vctx *txValidatorContext) error {
	expectedPrevBlockHashPtr := vctx.input.PrevBlockHash
	prevBlockHashPtr := vctx.input.TransactionsBlock.Header.PrevBlockHashPtr()
	if !bytes.Equal(prevBlockHashPtr, expectedPrevBlockHashPtr) {
		return errors.Wrapf(ErrMismatchedPrevBlockHash, "expected %v actual %v", expectedPrevBlockHashPtr, prevBlockHashPtr)
	}
	return nil
}

func validateTxBlockProposer(ctx context.Context, vctx *txValidatorContext) error {
	expectedBlockProposer := vctx.input.BlockProposerAddress
	blockProposer := vctx.input.TransactionsBlock.Header.BlockProposerAddress()
	if len(blockProposer) > 0 { // If tx block header - block proposer is len 0 this is older version and for backward compatibility validate check is skipped
		if !bytes.Equal(blockProposer, expectedBlockProposer) {
			return errors.Wrapf(ErrMismatchedBlockProposer, "Tx block: expected %v actual %v", expectedBlockProposer, blockProposer)
		}
	}
	return nil
}

func validateTxTransactionsBlockTimestamp(ctx context.Context, vctx *txValidatorContext) error {
	prevBlockTimestamp := vctx.input.PrevBlockTimestamp
	currentBlockTimestamp := vctx.input.TransactionsBlock.Header.Timestamp()
	allowedTimestampJitter := vctx.allowedTimestampJitter
	if err := isValidBlockTimestamp(currentBlockTimestamp, prevBlockTimestamp,
		primitives.TimestampNano(time.Now().UnixNano()), primitives.TimestampNano(allowedTimestampJitter.Nanoseconds())); err != nil {
		return err
	}
	return nil
}

func isValidBlockTimestamp(currentBlockTimestamp, prevBlockTimestamp, now, allowedTimestampJitter primitives.TimestampNano) error {
	upperJitterLimitNano := now + allowedTimestampJitter
	lowerJitterLimitNano := now - allowedTimestampJitter

	if prevBlockTimestamp >= currentBlockTimestamp {
		prevBlockTimestampTime := time.Unix(0, int64(prevBlockTimestamp))
		currentBlockTimestampTime := time.Unix(0, int64(currentBlockTimestamp))
		return errors.Errorf("the previous block's timestamp is same or later than current block's timestamp: prevBlockTimestamp=%d (%s) currentBlockTimestamp=%d (%s)", prevBlockTimestamp, prevBlockTimestampTime, currentBlockTimestamp, currentBlockTimestampTime)
	}
	if currentBlockTimestamp > upperJitterLimitNano {
		currentBlockTimestampTime := time.Unix(0, int64(currentBlockTimestamp))
		upperJitterLimit := time.Unix(0, int64(upperJitterLimitNano))
		return errors.Errorf("current block's timestamp is later than latest timestamp allowed (upper jitter limit): currentBlockTimestamp=%d (%s) upperJitterLimitNano=%d (%s)", currentBlockTimestamp, currentBlockTimestampTime, upperJitterLimitNano, upperJitterLimit)
	}

	if currentBlockTimestamp < lowerJitterLimitNano {
		currentBlockTimestampTime := time.Unix(0, int64(currentBlockTimestamp))
		lowerJitterLimit := time.Unix(0, int64(lowerJitterLimitNano))
		return errors.Errorf("current block's timestamp is earlier than earliest timestamp allowed (lower jitter limit): currentBlockTimestamp=%d (%s) lowerJitterLimitNano=%d (%s)", currentBlockTimestamp, currentBlockTimestampTime, lowerJitterLimitNano, lowerJitterLimit)
	}
	return nil
}

func validateTransactionsBlockMerkleRoot(ctx context.Context, vctx *txValidatorContext) error {
	return validators.ValidateTransactionsBlockMerkleRoot(&validators.BlockValidatorContext{
		TransactionsBlock: vctx.input.TransactionsBlock,
	})
}

func validateTransactionsBlockMetadataHash(ctx context.Context, vctx *txValidatorContext) error {
	return validators.ValidateTransactionsBlockMetadataHash(&validators.BlockValidatorContext{
		TransactionsBlock: vctx.input.TransactionsBlock,
	})
}

type validateForOrdering func(ctx context.Context, input *services.ValidateTransactionsForOrderingInput) (*services.ValidateTransactionsForOrderingOutput, error)

func validateTxTransactionOrdering(ctx context.Context, cfg config.ConsensusContextConfig, validateForOrderingFunc validateForOrdering, transactionBlock *protocol.TransactionsBlockContainer) error {
	txs := transactionBlock.SignedTransactions
	if cfg.ConsensusContextTriggersEnabled() {
		if len(txs) == 0 {
			return ErrTriggerEnabledAndTriggerMissing
		}
		txs = txs[:len(txs)-1]
	}

	validationInput := &services.ValidateTransactionsForOrderingInput{
		BlockProtocolVersion:      transactionBlock.Header.ProtocolVersion(),
		CurrentBlockHeight:        transactionBlock.Header.BlockHeight(),
		CurrentBlockTimestamp:     transactionBlock.Header.Timestamp(),
		CurrentBlockReferenceTime: transactionBlock.Header.ReferenceTime(),
		SignedTransactions:        txs,
	}
	_, err := validateForOrderingFunc(ctx, validationInput)
	if err != nil {
		return errors.Wrapf(ErrFailedTransactionOrdering, "%v", err)
	}
	return nil
}

func validateProposeBlockReferenceTime(prevBlockReferenceTime primitives.TimestampSeconds, proposedBlockReferenceTime primitives.TimestampSeconds,
	validatorCurrentReferenceTime primitives.TimestampSeconds) error {
	if proposedBlockReferenceTime < prevBlockReferenceTime {
		return errors.Errorf("proposed block time reference %d cannot be smaller than previous block %d", proposedBlockReferenceTime, prevBlockReferenceTime)
	}

	if validatorCurrentReferenceTime < proposedBlockReferenceTime {
		return errors.Errorf("proposed block time reference %d is too far in the future compared to validator current time reference %d", proposedBlockReferenceTime, validatorCurrentReferenceTime)
	}

	return nil
}

func validateProposeBlockProtocolVersionWithManagement(blockProtocolVersion primitives.ProtocolVersion, managementProtocolVersion primitives.ProtocolVersion) error {
	if blockProtocolVersion != managementProtocolVersion {
		return errors.Errorf("proposed block protocol version %d mismatch management protocol version %d", blockProtocolVersion, managementProtocolVersion)
	}

	return nil
}

func validateTransactionsBlockTriggerCompliance(ctx context.Context, cfg config.ConsensusContextConfig, transactionsBlock *protocol.TransactionsBlockContainer) error {
	txs := transactionsBlock.SignedTransactions
	if cfg.ConsensusContextTriggersEnabled() {
		txCount := len(txs)
		if txCount == 0 || !validateTransactionsBlockIsTxTrigger(txs[txCount-1]) {
			return ErrTriggerEnabledAndTriggerMissing
		}

		if !validateTransactionsBlockTxTriggerIsValidTime(txs[txCount-1], transactionsBlock.Header.Timestamp()) {
			return ErrTriggerEnabledAndTriggerInvalidTime
		}
		if !validateTransactionsBlockTxTriggerIsValid(txs[txCount-1], transactionsBlock.Header.ProtocolVersion(), cfg) {
			return ErrTriggerEnabledAndTriggerInvalid
		}

		for i := 0; i < txCount-2; i++ {
			if validateTransactionsBlockIsTxTrigger(txs[i]) {
				return ErrTriggerEnabledAndTriggerNotLast
			}
		}
	} else {
		for _, tx := range txs {
			if validateTransactionsBlockIsTxTrigger(tx) {
				return ErrTriggerDisabledAndTriggerExists
			}
		}
	}
	return nil
}

func validateTransactionsBlockIsTxTrigger(signedTransaction *protocol.SignedTransaction) bool {
	transaction := signedTransaction.Transaction()
	if transaction.ContractName().Equal(primitives.ContractName(triggers_systemcontract.CONTRACT_NAME)) &&
		transaction.MethodName().Equal(primitives.MethodName(triggers_systemcontract.METHOD_TRIGGER)) {
		return true
	}
	return false
}

func validateTransactionsBlockTxTriggerIsValidTime(signedTransaction *protocol.SignedTransaction, blockTime primitives.TimestampNano) bool {
	return signedTransaction.Transaction().Timestamp() == blockTime
}

func validateTransactionsBlockTxTriggerIsValid(signedTransaction *protocol.SignedTransaction, blockProtocolVersion primitives.ProtocolVersion, cfg config.ConsensusContextConfig) bool {
	if len(signedTransaction.Signature()) != 0 {
		return false
	}

	transaction := signedTransaction.Transaction()
	if transaction.ProtocolVersion() != blockProtocolVersion ||
		transaction.VirtualChainId() != cfg.VirtualChainId() ||
		len(transaction.InputArgumentArray()) != 0 ||
		len(transaction.Signer().Raw()) != 0 {
		return false
	}

	return true
}

func (s *service) ValidateTransactionsBlock(ctx context.Context, input *services.ValidateTransactionsBlockInput) (*services.ValidateTransactionsBlockOutput, error) {
	vctx := &txValidatorContext{
		virtualChainId:         s.config.VirtualChainId(),
		allowedTimestampJitter: s.config.ConsensusContextSystemTimestampAllowedJitter(),
		input:                  input,
	}

	ref, err := s.management.GetCurrentReference(ctx, &services.GetCurrentReferenceInput{SystemTime: input.PrevBlockReferenceTime})
	if err != nil {
		s.logger.Error("management.GetCurrentReference should not return error", log.Error(err))
		return nil, err
	}

	prevBlockReferenceTime, err := s.adjustPrevReference(ctx, input.CurrentBlockHeight, input.PrevBlockReferenceTime) // For completeness, can't really fail
	if err != nil {
		return nil, errors.Wrapf(ErrFailedGenesisRefTime, "ValidateTransactionsBlock failed genesis time %s", err)
	}

	if err2 := validateProposeBlockReferenceTime(prevBlockReferenceTime, input.TransactionsBlock.Header.ReferenceTime(), ref.CurrentReference); err2 != nil {
		return nil, err2
	}

	pvOutput, err3 := s.management.GetProtocolVersion(ctx, &services.GetProtocolVersionInput{Reference: input.TransactionsBlock.Header.ReferenceTime()})
	if err3 != nil {
		s.logger.Error("management.GetProtocolVersion should not return error", log.Error(err))
		return nil, err3
	}
	if err4 := validateProposeBlockProtocolVersionWithManagement(input.TransactionsBlock.Header.ProtocolVersion(), pvOutput.ProtocolVersion); err4 != nil {
		return nil, err4
	}

	validators := []txValidator{
		validateTxProtocolVersion,
		validateTxVirtualChainID,
		validateTxBlockHeight,
		validateTxPrevBlockHashPtr,
		validateTxTransactionsBlockTimestamp,
		validateTxBlockProposer,
		validateTransactionsBlockMerkleRoot,
		validateTransactionsBlockMetadataHash,
	}
	for _, v := range validators {
		if err := v(ctx, vctx); err != nil {
			return nil, err
		}
	}

	if err6 := validateTransactionsBlockTriggerCompliance(ctx, s.config, input.TransactionsBlock); err6 != nil { // trigger validator must be before ordering validator
		return nil, err6
	}

	if err7 := validateTxTransactionOrdering(ctx, s.config, s.transactionPool.ValidateTransactionsForOrdering, input.TransactionsBlock); err7 != nil { // trigger validator must be before ordering validtaor
		return nil, err7
	}

	return &services.ValidateTransactionsBlockOutput{}, nil
}
