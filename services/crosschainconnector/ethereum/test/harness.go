package test

import (
	"context"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/orbs-network/orbs-network-go/instrumentation/log"
	"github.com/orbs-network/orbs-network-go/services/crosschainconnector/ethereum"
	"github.com/orbs-network/orbs-network-go/services/crosschainconnector/ethereum/adapter"
	"github.com/orbs-network/orbs-network-go/services/crosschainconnector/ethereum/contract"
	"github.com/orbs-network/orbs-spec/types/go/services"
	"github.com/pkg/errors"
	"os"
	"strings"
)

type harness struct {
	adapter   *adapter.EthereumSimulator
	connector services.CrosschainConnector
	logger    log.BasicLogger
	address   string
}

type ethereumConnectorConfigForTests struct {
	endpoint string
}

func (c *ethereumConnectorConfigForTests) EthereumEndpoint() string {
	return c.endpoint
}

func (h *harness) deployStorageContract(ctx context.Context, text string) error {
	address, err := h.adapter.DeploySimpleStorageContract(h.adapter.GetAuth(), text)
	h.adapter.Commit()
	if err != nil {
		return err
	}

	h.address = hexutil.Encode(address[:])
	return nil
}

func (h *harness) getAddress() string {
	return h.address
}

func newEthereumConnectorHarness() *harness {
	logger := log.GetLogger().WithOutput(log.NewFormattingOutput(os.Stdout, log.NewHumanReadableFormatter()))
	conn := adapter.NewEthereumSimulatorConnection(logger)

	return &harness{
		adapter:   conn,
		logger:    logger,
		connector: ethereum.NewEthereumCrosschainConnector(conn, logger),
	}
}

func ethereumPackInputArguments(jsonAbi string, method string, args []interface{}) ([]byte, error) {
	if parsedABI, err := abi.JSON(strings.NewReader(jsonAbi)); err != nil {
		return nil, errors.WithStack(err)
	} else {
		return parsedABI.Pack(method, args...)
	}
}

func ethereumUnpackOutput(data []byte, method string, out interface{}) error {
	if parsedABI, err := abi.JSON(strings.NewReader(contract.SimpleStorageABI)); err != nil {
		return errors.WithStack(err)
	} else {
		return parsedABI.Unpack(out, method, data)
	}
}