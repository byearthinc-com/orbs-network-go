// Copyright 2019 the orbs-network-go authors
// This file is part of the orbs-network-go library in the Orbs project.
//
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
// The above notice should be included in all copies or substantial portions of the software.

package committee_systemcontract

import (
	"encoding/binary"
	"github.com/orbs-network/orbs-contract-sdk/go/sdk/v1/state"
	. "github.com/orbs-network/orbs-contract-sdk/go/testing/unit"
	"github.com/orbs-network/orbs-network-go/crypto/hash"
	"github.com/stretchr/testify/require"
	"sort"
	"testing"
)

func TestOrbsCommitteeContract_getOrderedCommittee_withoutReputation(t *testing.T) {
	addrs := makeNodeAddressArray(10)
	blockHeight := 155

	InServiceScope(nil, nil, func(m Mockery) {
		_init()

		// prepare
		m.MockEnvBlockHeight(blockHeight)

		// sort with simplified calculation
		seed := make([]byte, 8)
		binary.LittleEndian.PutUint64(seed, uint64(blockHeight))
		copyOfAddrs := make([][]byte, 0, len(addrs)) // must copy list to avoid double sorting.
		scoresUint32 := make([]uint32, 0, len(addrs))
		for _, addr := range addrs {
			copyOfAddrs = append(copyOfAddrs, addr)
			scoresUint32 = append(scoresUint32, binary.LittleEndian.Uint32(hash.CalcSha256(addr, seed)[hash.SHA256_HASH_SIZE_BYTES-4:]))
		}
		toSort := testSort{addresses: copyOfAddrs, scores: scoresUint32}
		sort.Sort(toSort)

		// run
		ordered := _getOrderedCommitteeArray(addrs)

		//assert
		require.EqualValues(t, toSort.addresses, ordered)
	})
}

func TestOrbsCommitteeContract_getOrderedCommittee_SimpleReputationMarkDown(t *testing.T) {
	addrs := makeNodeAddressArray(3)
	blockHeight := 155

	InServiceScope(nil, nil, func(m Mockery) {
		_init()

		// prepare
		m.MockEnvBlockHeight(blockHeight)
		state.WriteUint32(_formatMisses(addrs[0]), 10)

		// sort with simplified calculation
		seed := make([]byte, 8)
		binary.LittleEndian.PutUint64(seed, uint64(blockHeight))
		copyOfAddrs := make([][]byte, 0, len(addrs)) // must copy list to avoid double sorting.
		scoresUint32 := make([]uint32, 0, len(addrs))
		for i, addr := range addrs {
			copyOfAddrs = append(copyOfAddrs, addr)
			scoresUint32 = append(scoresUint32, binary.LittleEndian.Uint32(hash.CalcSha256(addr, seed)[hash.SHA256_HASH_SIZE_BYTES-4:]))
			if i == 0 {
				scoresUint32[0] = scoresUint32[0] / 1024
			}
		}
		toSort := testSort{addresses: copyOfAddrs, scores: scoresUint32}
		sort.Sort(toSort)

		// run
		ordered := _getOrderedCommitteeArray(addrs)

		//assert
		require.EqualValues(t, toSort.addresses, ordered)
	})
}

func TestOrbsCommitteeContract_orderList_noReputation_noSeed(t *testing.T) {
	addrs := makeNodeAddressArray(10)

	InServiceScope(nil, nil, func(m Mockery) {
		_init()

		// Prepare do calculation in similar way
		copyOfAddrs := make([][]byte, 0, len(addrs)) // must copy list to avoid double sorting.
		scoresUint32 := make([]uint32, 0, len(addrs))
		for _, addr := range addrs {
			copyOfAddrs = append(copyOfAddrs, addr)
			scoresUint32 = append(scoresUint32, binary.LittleEndian.Uint32(hash.CalcSha256(addr)[hash.SHA256_HASH_SIZE_BYTES-4:]))
		}
		toSort := testSort{addresses: copyOfAddrs, scores: scoresUint32}
		sort.Sort(toSort)

		// run with empty seed
		ordered := _orderList(addrs, []byte{})

		//assert
		require.EqualValues(t, toSort.addresses, ordered)
	})
}

type testSort struct {
	addresses [][]byte
	scores    []uint32
}

func (s testSort) Len() int {
	return len(s.addresses)
}

func (s testSort) Swap(i, j int) {
	s.addresses[i], s.addresses[j] = s.addresses[j], s.addresses[i]
	s.scores[i], s.scores[j] = s.scores[j], s.scores[i]
}

// descending order
func (s testSort) Less(i, j int) bool {
	return s.scores[i] > s.scores[j]
}

func TestOrbsCommitteeContract_calculateScore(t *testing.T) {
	addr := []byte{0xa1, 0x33}
	var emptySeed = []byte{}
	nonEmptySeed := []byte{0x44}
	nonEmptySeedOneBitDiff := []byte{0x43}

	scoreWithEmpty := _calculateScore(addr, emptySeed)
	scoreWithNonEmpty := _calculateScore(addr, nonEmptySeed)
	scoreWithNonEmptyOneBitDiff := _calculateScore(addr, nonEmptySeedOneBitDiff)

	shaOfAddrWithNoSeed := hash.CalcSha256(addr)
	expectedScoreWithEmpty :=  binary.LittleEndian.Uint32(shaOfAddrWithNoSeed[hash.SHA256_HASH_SIZE_BYTES-4:])

	require.Equal(t, expectedScoreWithEmpty, scoreWithEmpty, "for score with empty seed doesn't match expected")
	require.NotEqual(t, scoreWithNonEmpty, scoreWithEmpty, "for score with and without seed must not match")
	require.NotEqual(t, scoreWithNonEmpty, scoreWithNonEmptyOneBitDiff, "score is diff even with one bit difference in seed")
}
