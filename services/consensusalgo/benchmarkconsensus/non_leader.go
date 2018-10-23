package benchmarkconsensus

import (
	"context"
	"github.com/orbs-network/orbs-network-go/crypto/hash"
	"github.com/orbs-network/orbs-network-go/crypto/signature"
	"github.com/orbs-network/orbs-network-go/instrumentation/log"
	"github.com/orbs-network/orbs-spec/types/go/protocol"
	"github.com/orbs-network/orbs-spec/types/go/protocol/gossipmessages"
	"github.com/orbs-network/orbs-spec/types/go/services/gossiptopics"
	"github.com/pkg/errors"
)

func (s *service) nonLeaderHandleCommit(ctx context.Context, blockPair *protocol.BlockPairContainer) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	err := s.nonLeaderValidateBlockUnderMutex(blockPair)
	if err != nil {
		s.logger.Error("non leader failed to validate block", log.Error(err))
		return
	}
	err = s.nonLeaderCommitAndReplyUnderMutex(ctx, blockPair)
	if err != nil {
		s.logger.Error("non leader failed to commit and reply vote", log.Error(err))
		return
	}
}

func (s *service) nonLeaderValidateBlockUnderMutex(blockPair *protocol.BlockPairContainer) error {
	// block height
	blockHeight := blockPair.TransactionsBlock.Header.BlockHeight()
	if blockHeight != blockPair.ResultsBlock.Header.BlockHeight() {
		return errors.Errorf("invalid block: block height of tx %s is not equal rx %s", blockHeight, blockPair.ResultsBlock.Header.BlockHeight())
	}
	if blockHeight > s.lastCommittedBlockHeightUnderMutex()+1 {
		return errors.Errorf("invalid block: future block height %s", blockHeight)
	}

	// block consensus
	var prevCommittedBlockPair *protocol.BlockPairContainer = nil
	if s.lastCommittedBlockUnderMutex != nil && blockHeight == s.lastCommittedBlockHeightUnderMutex()+1 {
		// in this case we also want to validate match to the prev (prev hashes)
		prevCommittedBlockPair = s.lastCommittedBlockUnderMutex
	}
	err := s.validateBlockConsensus(blockPair, prevCommittedBlockPair)
	if err != nil {
		return err
	}

	return nil
}

func (s *service) nonLeaderCommitAndReplyUnderMutex(ctx context.Context, blockPair *protocol.BlockPairContainer) error {
	// save the block to block storage
	err := s.saveToBlockStorage(ctx, blockPair)
	if err != nil {
		return err
	}

	// remember the block in our last committed state variable
	if blockPair.TransactionsBlock.Header.BlockHeight() == s.lastCommittedBlockHeightUnderMutex()+1 {
		s.lastCommittedBlockUnderMutex = blockPair
	}

	// sign the committed message we're about to send
	status := (&gossipmessages.BenchmarkConsensusStatusBuilder{
		LastCommittedBlockHeight: s.lastCommittedBlockHeightUnderMutex(),
	}).Build()
	signedData := hash.CalcSha256(status.Raw())
	sig, err := signature.SignEd25519(s.config.NodePrivateKey(), signedData)
	if err != nil {
		return err
	}

	// prepare the message
	message := &gossipmessages.BenchmarkConsensusCommittedMessage{
		Status: status,
		Sender: (&gossipmessages.SenderSignatureBuilder{
			SenderPublicKey: s.config.NodePublicKey(),
			Signature:       sig,
		}).Build(),
	}

	// send committed back to leader via gossip
	recipient := blockPair.ResultsBlock.BlockProof.BenchmarkConsensus().Sender().SenderPublicKey()
	s.logger.Info("replying committed with last committed height", log.BlockHeight(s.lastCommittedBlockHeightUnderMutex()), log.Stringable("signed-data", signedData))
	_, err = s.gossip.SendBenchmarkConsensusCommitted(ctx, &gossiptopics.BenchmarkConsensusCommittedInput{
		RecipientPublicKey: recipient,
		Message:            message,
	})
	return err
}
