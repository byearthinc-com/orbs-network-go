package consensuscontext

import (
	"github.com/orbs-network/orbs-spec/types/go/primitives"
	"github.com/orbs-network/orbs-spec/types/go/services"
	"github.com/stretchr/testify/require"
	"testing"
)

var federationNodes = []*federationNode{
	{publicKey: []byte("dfc06c5be24a67adee80b35ab4f147bb1a35c55ff85eda69f40ef827bddec173")},
	{publicKey: []byte("92d469d7c004cc0b24a192d9457836bf38effa27536627ef60718b00b0f33152")},
	{publicKey: []byte("a899b318e65915aa2de02841eeb72fe51fddad96014b73800ca788a547f8cce0")},
	{publicKey: []byte("58e7ed8169a151602b1349c990c84ca2fb2f62eb17378f9a94e49552fbafb9d8")},
	{publicKey: []byte("23f97918acf48728d3f25a39a5f091a1a9574c52ccb20b9bad81306bd2af4631")},
	{publicKey: []byte("07492c6612f78a47d7b6a18a17792a01917dec7497bdac1a35c477fbccc3303b")},
	{publicKey: []byte("43a4dbbf7a672c6689dbdd662fd89a675214b00d884bb7113d3410b502ecd826")},
	{publicKey: []byte("469bd276271aa6d59e387018cf76bd00f55c702931c13e80896eec8a32b22082")},
	{publicKey: []byte("102073b28749be1e3daf5e5947605ec7d43c3183edb48a3aac4c9542cdbaf748")},
	{publicKey: []byte("70d92324eb8d24b7c7ed646e1996f94dcd52934a031935b9ac2d0e5bbcfa357c")},
}

type federationNode struct {
	publicKey primitives.Ed25519PublicKey
}

func (n *federationNode) NodePublicKey() primitives.Ed25519PublicKey {
	return n.publicKey
}

func TestCommitteeSizeVSTotalNodesCount(t *testing.T) {

	federationSize := len(federationNodes)

	testCases := []struct {
		description            string
		requestedCommitteeSize int
		federationSize         int
		expectedCommitteeSize  int
	}{
		{"Requested committee size less than federation size", federationSize - 1, federationSize, federationSize - 1},
		{"Requested committee size same as federation size", federationSize, federationSize, federationSize},
		{"Requested committee size greater than federation size", federationSize + 1, federationSize, federationSize},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			actualCommitteeSize := CalculateCommitteeSize(testCase.requestedCommitteeSize, testCase.federationSize)
			require.Equal(t, testCase.expectedCommitteeSize, actualCommitteeSize,
				"Expected committee size is %d but the calculated committee size is %d",
				testCase.expectedCommitteeSize, actualCommitteeSize)
		})
	}
}

func TestChooseRandomCommitteeIndices(t *testing.T) {
	input := &services.RequestCommitteeInput{
		BlockHeight:      1,
		RandomSeed:       123456789,
		MaxCommitteeSize: 5,
	}

	t.Run("Receive same number of indices as requested", func(t *testing.T) {
		indices := ChooseRandomCommitteeIndices(input)
		indicesLen := uint32(len(indices))
		require.Equal(t, input.MaxCommitteeSize, indicesLen, "Expected to receive %d indices but got %d", input.MaxCommitteeSize, indicesLen)
	})

	t.Run("Receive unique indices", func(t *testing.T) {
		indices := ChooseRandomCommitteeIndices(input)
		uniqueIndices := unique(indices)
		uniqueIndicesLen := uint32(len(uniqueIndices))
		require.Equal(t, input.MaxCommitteeSize, uniqueIndicesLen, "Expected to receive %d unique indices but got %d", input.MaxCommitteeSize, uniqueIndicesLen)
	})

}

func unique(input []int) []int {
	u := make([]int, 0, len(input))
	m := make(map[int]bool)

	for _, val := range input {
		if _, ok := m[val]; !ok {
			m[val] = true
			u = append(u, val)
		}
	}
	return u
}
