package elections_systemcontract

import (
	"bytes"
	"fmt"
	"github.com/orbs-network/orbs-contract-sdk/go/sdk/v1/ethereum"
	"github.com/orbs-network/orbs-contract-sdk/go/sdk/v1/safemath/safeuint64"
	"github.com/orbs-network/orbs-contract-sdk/go/sdk/v1/state"
	"math/big"
)

/***
 * processing
 */
func processVoting() uint64 {
	currentBlock := ethereum.GetBlockNumber()
	if !_isAfterElectionMirroring(currentBlock) {
		panic(fmt.Sprintf("mirror period (%d - %d) did not end (now %d). cannot start processing", _getCurrentElectionBlockNumber(), _getCurrentElectionBlockNumber()+VOTE_MIRROR_PERIOD_LENGTH_IN_BLOCKS, currentBlock))
	}

	electedValidators := _processVotingStateMachine()
	if electedValidators != nil {
		_setElectedValidators(electedValidators)
		_setCurrentElectionBlockNumber(safeuint64.Add(_getCurrentElectionBlockNumber(), ELECTION_PERIOD_LENGTH_IN_BLOCKS))
		return 1
	} else {
		return 0
	}
}

func _processVotingStateMachine() [][20]byte {
	processState := _getVotingProcessState()
	if processState == "" {
		_readValidatorsFromEthereumToState()
		_nextProcessVotingState(VOTING_PROCESS_STATE_GUARDIANS)
		return nil
	} else if processState == VOTING_PROCESS_STATE_GUARDIANS {
		_readGuardiansFromEthereumToState()
		_nextProcessVotingState(VOTING_PROCESS_STATE_VALIDATORS)
		return nil
	} else if processState == VOTING_PROCESS_STATE_VALIDATORS {
		if _collectNextValidatorDataFromEthereum() {
			_nextProcessVotingState(VOTING_PROCESS_STATE_VOTERS)
		}
		return nil
	} else if processState == VOTING_PROCESS_STATE_VOTERS {
		if _collectNextVoterStakeFromEthereum() {
			_nextProcessVotingState(VOTING_PROCESS_STATE_DELEGATORS)
		}
		return nil
	} else if processState == VOTING_PROCESS_STATE_DELEGATORS {
		if _collectNextDelegatorStakeFromEthereum() {
			_nextProcessVotingState(VOTING_PROCESS_STATE_CALCULATIONS)
		}
		return nil
	} else if processState == VOTING_PROCESS_STATE_CALCULATIONS {
		candidateVotes, totalVotes, participantStakes, guardiansAccumulatedStake := _calculateVotes()
		elected := _processValidatorsSelection(candidateVotes, totalVotes)
		_processRewards(totalVotes, elected, participantStakes, guardiansAccumulatedStake)
		_clearGuardians()
		_setVotingProcessState("")
		return elected
	}
	return nil
}

func _nextProcessVotingState(stage string) {
	_setVotingProcessItem(0)
	_setVotingProcessState(stage)
	fmt.Printf("elections %10d: moving to state %s\n", _getCurrentElectionBlockNumber(), stage)
}

func _readValidatorsFromEthereumToState() {
	var validators [][20]byte
	ethereum.CallMethodAtBlock(_getCurrentElectionBlockNumber(), getValidatorsEthereumContractAddress(), getValidatorsAbi(), "getValidatorsBytes20", &validators)

	_setValidators(validators)
}

func _readGuardiansFromEthereumToState() {
	var guardians [][20]byte
	pos := int64(0)
	pageSize := int64(50)
	for {
		var gs [][20]byte
		ethereum.CallMethodAtBlock(_getCurrentElectionBlockNumber(), getGuardiansEthereumContractAddress(), getGuardiansAbi(), "getGuardiansBytes20", &gs, big.NewInt(pos), big.NewInt(pageSize))
		guardians = append(guardians, gs...)
		if len(gs) < 50 {
			break
		}
		pos += pageSize
	}

	_setGuardians(guardians)
}

func _collectNextValidatorDataFromEthereum() (isDone bool) {
	nextIndex := _getVotingProcessItem()
	_collectOneValidatorDataFromEthereum(nextIndex)
	nextIndex++
	_setVotingProcessItem(nextIndex)
	return nextIndex >= _getNumberOfValidators()
}

func _collectOneValidatorDataFromEthereum(i int) {
	validator := _getValidatorEthereumAddressAtIndex(i)

	var orbsAddress [20]byte
	ethereum.CallMethodAtBlock(_getCurrentElectionBlockNumber(), getValidatorsRegistryEthereumContractAddress(), getValidatorsRegistryAbi(), "getOrbsAddress", &orbsAddress, validator)
	stake := _getStakeAtElection(validator)

	_setValidatorStake(validator[:], stake)
	_setValidatorOrbsAddress(validator[:], orbsAddress[:])
	fmt.Printf("elections %10d: from ethereum validator %x, stake %d orbsAddress %x\n", _getCurrentElectionBlockNumber(), validator, stake, orbsAddress)
}

func _collectNextVoterStakeFromEthereum() bool {
	nextIndex := _getVotingProcessItem()
	_collectOneVoterStakeFromEthereum(nextIndex)
	nextIndex++
	_setVotingProcessItem(nextIndex)
	return nextIndex >= _getNumberOfVoters()
}

func _collectOneVoterStakeFromEthereum(i int) {
	voter := _getVoterAtIndex(i)
	stake := uint64(0)
	if _isGuardian(voter) {
		voteBlockNumber := state.ReadUint64(_formatVoterBlockNumberKey(voter[:]))
		if voteBlockNumber != 0 && safeuint64.Add(voteBlockNumber, VOTE_VALID_PERIOD_LENGTH_IN_BLOCKS) > _getCurrentElectionBlockNumber() {
			stake = _getStakeAtElection(voter)
		} else {
			fmt.Printf("elections %10d: from ethereum guardian %x vote is too old, will ignore\n", _getCurrentElectionBlockNumber(), voter)
		}
	} else {
		fmt.Printf("elections %10d: from ethereum guardian %x is not actually a guardian, will ignore\n", _getCurrentElectionBlockNumber(), voter)
	}
	_setVoterStake(voter[:], stake)
	fmt.Printf("elections %10d: from ethereum guardian %x, stake %d\n", _getCurrentElectionBlockNumber(), voter, stake)
}

func _collectNextDelegatorStakeFromEthereum() bool {
	nextIndex := _getVotingProcessItem()
	_collectOneDelegatorStakeFromEthereum(nextIndex)
	nextIndex++
	_setVotingProcessItem(nextIndex)
	return nextIndex >= _getNumberOfDelegators()
}

func _collectOneDelegatorStakeFromEthereum(i int) {
	delegator := _getDelegatorAtIndex(i)
	stake := uint64(0)
	if !_isGuardian(delegator) {
		stake = _getStakeAtElection(delegator)
	} else {
		fmt.Printf("elections %10d: from ethereum delegator %x is actually a guardian, will ignore\n", _getCurrentElectionBlockNumber(), delegator)
	}
	state.WriteUint64(_formatDelegatorStakeKey(delegator[:]), stake)
	fmt.Printf("elections %10d: from ethereum delegator %x , stake %d\n", _getCurrentElectionBlockNumber(), delegator, stake)
}

func _getStakeAtElection(ethAddr [20]byte) uint64 {
	stake := new(*big.Int)
	ethereum.CallMethodAtBlock(_getCurrentElectionBlockNumber(), getTokenEthereumContractAddress(), getTokenAbi(), "balanceOf", stake, ethAddr)
	return ((*stake).Div(*stake, ETHEREUM_STAKE_FACTOR)).Uint64()
}

func _calculateVotes() (candidateVotes map[[20]byte]uint64, totalVotes uint64, participantStakes map[[20]byte]uint64, guardianAccumulatedStakes map[[20]byte]uint64) {
	guardians := _getGuardians()
	guardianStakes := _collectGuardiansStake(guardians)
	delegatorStakes := _collectDelegatorsStake(guardians)
	guardianToDelegators := _findGuardianDelegators(delegatorStakes)
	candidateVotes, totalVotes, participantStakes, guardianAccumulatedStakes = _guardiansCastVotes(guardianStakes, guardianToDelegators, delegatorStakes)
	return
}

func _collectGuardiansStake(guardians map[[20]byte]bool) (guardianStakes map[[20]byte]uint64) {
	guardianStakes = make(map[[20]byte]uint64)
	numOfVoters := _getNumberOfVoters()
	for i := 0; i < numOfVoters; i++ {
		voter := _getVoterAtIndex(i)
		if guardians[voter] {
			voteBlockNumber := state.ReadUint64(_formatVoterBlockNumberKey(voter[:]))
			if voteBlockNumber != 0 && safeuint64.Add(voteBlockNumber, VOTE_VALID_PERIOD_LENGTH_IN_BLOCKS) > _getCurrentElectionBlockNumber() {
				stake := getGuardianStake(voter[:])
				guardianStakes[voter] = stake
				fmt.Printf("elections %10d: guardian %x, stake %d\n", _getCurrentElectionBlockNumber(), voter, stake)
			} else {
				fmt.Printf("elections %10d: guardian %x voted at %d is too old, ignoring as guardian \n", _getCurrentElectionBlockNumber(), voter, voteBlockNumber)
			}
		} else {
			fmt.Printf("elections %10d: guardian %x is not a guardian ignoring\n", _getCurrentElectionBlockNumber(), voter)
		}
	}
	return
}

func _collectDelegatorsStake(guardians map[[20]byte]bool) (delegatorStakes map[[20]byte]uint64) {
	delegatorStakes = make(map[[20]byte]uint64)
	numOfDelegators := _getNumberOfDelegators()
	for i := 0; i < numOfDelegators; i++ {
		delegator := _getDelegatorAtIndex(i)
		if !guardians[delegator] {
			stake := state.ReadUint64(_formatDelegatorStakeKey(delegator[:]))
			delegatorStakes[delegator] = stake
			fmt.Printf("elections %10d: delegator %x, stake %d\n", _getCurrentElectionBlockNumber(), delegator, stake)
		} else {
			fmt.Printf("elections %10d: delegator %x ignored as it is also a guardian\n", _getCurrentElectionBlockNumber(), delegator)
		}
	}
	return
}

func _findGuardianDelegators(delegatorStakes map[[20]byte]uint64) (guardianToDelegators map[[20]byte][][20]byte) {
	guardianToDelegators = make(map[[20]byte][][20]byte)

	for delegator := range delegatorStakes {
		guardian := _getDelegatorGuardian(delegator[:])
		if !bytes.Equal(guardian[:], delegator[:]) {
			fmt.Printf("elections %10d: delegator %x, guardian/agent %x\n", _getCurrentElectionBlockNumber(), delegator, guardian)
			guardianDelegatorList, ok := guardianToDelegators[guardian]
			if !ok {
				guardianDelegatorList = [][20]byte{}
			}
			guardianDelegatorList = append(guardianDelegatorList, delegator)
			guardianToDelegators[guardian] = guardianDelegatorList
		}
	}
	return
}

func _guardiansCastVotes(guardianStakes map[[20]byte]uint64, guardianDelegators map[[20]byte][][20]byte, delegatorStakes map[[20]byte]uint64) (candidateVotes map[[20]byte]uint64, totalVotes uint64, participantStakes map[[20]byte]uint64, guardainsAccumulatedStakes map[[20]byte]uint64) {
	totalVotes = uint64(0)
	candidateVotes = make(map[[20]byte]uint64)
	participantStakes = make(map[[20]byte]uint64)
	guardainsAccumulatedStakes = make(map[[20]byte]uint64)
	for guardian, guardianStake := range guardianStakes {
		participantStakes[guardian] = guardianStake
		fmt.Printf("elections %10d: guardian %x, self-voting stake %d\n", _getCurrentElectionBlockNumber(), guardian, guardianStake)
		stake := safeuint64.Add(guardianStake, _calculateOneGuardianVoteRecursive(guardian, guardianDelegators, delegatorStakes, participantStakes))
		guardainsAccumulatedStakes[guardian] = stake
		_setGuardianVotingWeight(guardian[:], stake)
		totalVotes = safeuint64.Add(totalVotes, stake)
		fmt.Printf("elections %10d: guardian %x, voting stake %d\n", _getCurrentElectionBlockNumber(), guardian, stake)

		candidateList := _getCandidates(guardian[:])
		for _, candidate := range candidateList {
			fmt.Printf("elections %10d: guardian %x, voted for candidate %x\n", _getCurrentElectionBlockNumber(), guardian, candidate)
			candidateVotes[candidate] = safeuint64.Add(candidateVotes[candidate], stake)
		}
	}
	fmt.Printf("elections %10d: total voting stake %d\n", _getCurrentElectionBlockNumber(), totalVotes)
	_setTotalStake(totalVotes)
	return
}

// Note : important that first call is to guardian ... otherwise not all delegators will be added to participants
func _calculateOneGuardianVoteRecursive(currentLevelGuardian [20]byte, guardianToDelegators map[[20]byte][][20]byte, delegatorStakes map[[20]byte]uint64, participantStakes map[[20]byte]uint64) uint64 {
	guardianDelegatorList, ok := guardianToDelegators[currentLevelGuardian]
	currentVotes := delegatorStakes[currentLevelGuardian]
	if ok {
		for _, delegate := range guardianDelegatorList {
			participantStakes[delegate] = delegatorStakes[delegate]
			currentVotes = safeuint64.Add(currentVotes, _calculateOneGuardianVoteRecursive(delegate, guardianToDelegators, delegatorStakes, participantStakes))
		}
	}
	return currentVotes
}

func _processValidatorsSelection(candidateVotes map[[20]byte]uint64, totalVotes uint64) [][20]byte {
	validators := _getValidators()
	voteOutThreshhold := safeuint64.Div(safeuint64.Mul(totalVotes, VOTE_OUT_WEIGHT_PERCENT), 100)
	fmt.Printf("elections %10d: %d is vote out threshhold\n", _getCurrentElectionBlockNumber(), voteOutThreshhold)

	winners := make([][20]byte, 0, len(validators))
	for _, validator := range validators {
		voted, ok := candidateVotes[validator]
		_setValidatorVote(validator[:], voted)
		if !ok || voted < voteOutThreshhold {
			fmt.Printf("elections %10d: elected %x (got %d vote outs)\n", _getCurrentElectionBlockNumber(), validator, voted)
			winners = append(winners, validator)
		} else {
			fmt.Printf("elections %10d: candidate %x voted out by %d votes\n", _getCurrentElectionBlockNumber(), validator, voted)
		}
	}
	if len(winners) < MIN_ELECTED_VALIDATORS {
		fmt.Printf("elections %10d: not enought validators left after vote using all validators %v\n", _getCurrentElectionBlockNumber(), validators)
		return validators
	} else {
		return winners
	}
}

func _formatTotalVotingStakeKey() []byte {
	return []byte("Total_Voting_Weight")
}

func getTotalStake() uint64 {
	return state.ReadUint64(_formatTotalVotingStakeKey())
}

func _setTotalStake(weight uint64) {
	state.WriteUint64(_formatTotalVotingStakeKey(), weight)
}

const VOTING_PROCESS_STATE_VALIDATORS = "validators"
const VOTING_PROCESS_STATE_GUARDIANS = "guardians"
const VOTING_PROCESS_STATE_DELEGATORS = "delegators"
const VOTING_PROCESS_STATE_VOTERS = "voters"
const VOTING_PROCESS_STATE_CALCULATIONS = "calculations"
const VOTING_PROCESS_STATE_CLEANUP = "cleanUp"

func _formatVotingProcessStateKey() []byte {
	return []byte("Voting_Process_State")
}

func _getVotingProcessState() string {
	return state.ReadString(_formatVotingProcessStateKey())
}

func _setVotingProcessState(name string) {
	state.WriteString(_formatVotingProcessStateKey(), name)
}

func _formatVotingProcessItemIteratorKey() []byte {
	return []byte("Voting_Process_Item")
}

func _getVotingProcessItem() int {
	return int(state.ReadUint32(_formatVotingProcessItemIteratorKey()))
}

func _setVotingProcessItem(i int) {
	state.WriteUint32(_formatVotingProcessItemIteratorKey(), uint32(i))
}