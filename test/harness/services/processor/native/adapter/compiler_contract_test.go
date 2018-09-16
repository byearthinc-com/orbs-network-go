package adapter

import (
	"fmt"
	"github.com/orbs-network/orbs-contract-sdk/go/sdk"
	"github.com/orbs-network/orbs-network-go/instrumentation/log"
	"github.com/orbs-network/orbs-network-go/services/processor/native/adapter"
	"github.com/orbs-network/orbs-network-go/test/contracts"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestContract_Compile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping compilation of contracts in short mode")
	}

	t.Run("FakeCompiler", compileTest(aFakeCompiler))
	t.Run("NativeCompiler", compileTest(aNativeCompiler))
}

func compileTest(newHarness func(t *testing.T) *compilerContractHarness) func(*testing.T) {
	return func(t *testing.T) {
		h := newHarness(t)
		defer h.cleanup()

		t.Log("Compiling a valid contract")

		code := string(contracts.SourceCodeForCounter(contracts.MOCK_COUNTER_CONTRACT_START_FROM))
		contractInfo, err := h.compiler.Compile(code)

		require.NoError(t, err, "compile should succeed")
		require.NotNil(t, contractInfo, "loaded object should not be nil")
		require.Equal(t, fmt.Sprintf("CounterFrom%d", contracts.MOCK_COUNTER_CONTRACT_START_FROM), contractInfo.Name, "loaded object should be valid")

		// instantiate the "start()" function of the contract and call it
		ci := contractInfo.InitSingleton(nil)
		res := reflect.ValueOf(contractInfo.Methods["start"].Implementation).Call([]reflect.Value{reflect.ValueOf(ci), reflect.ValueOf(sdk.Context(0))})
		require.Equal(t, contracts.MOCK_COUNTER_CONTRACT_START_FROM, res[0].Interface().(uint64), "result of calling start() should match")

		t.Log("Compiling an invalid contract")

		invalidCode := "invalid code example"
		_, err = h.compiler.Compile(invalidCode)
		require.Error(t, err, "compile should fail")
	}
}

type compilerContractHarness struct {
	compiler adapter.Compiler
	cleanup  func()
}

func aNativeCompiler(t *testing.T) *compilerContractHarness {
	tmpDir := createTempTestDir(t)
	cfg := &config{artifactPath: tmpDir}
	log := log.GetLogger().WithOutput(log.NewOutput(os.Stdout).WithFormatter(log.NewHumanReadableFormatter()))
	compiler := adapter.NewNativeCompiler(cfg, log)
	return &compilerContractHarness{
		compiler: compiler,
		cleanup: func() {
			os.RemoveAll(tmpDir)
		},
	}
}

func aFakeCompiler(t *testing.T) *compilerContractHarness {
	compiler := NewFakeCompiler()
	code := string(contracts.SourceCodeForCounter(contracts.MOCK_COUNTER_CONTRACT_START_FROM))
	compiler.ProvideFakeContract(contracts.MockForCounter(), code)
	return &compilerContractHarness{
		compiler: compiler,
		cleanup:  func() {},
	}
}

func createTempTestDir(t *testing.T) string {
	prefix := strings.Replace(t.Name(), "/", "__", -1)
	tmpDir, err := ioutil.TempDir("/tmp", prefix)
	if err != nil {
		panic("could not create temp dir for test")
	}
	return tmpDir
}

type config struct {
	artifactPath string
}

func (c *config) ProcessorArtifactPath() string {
	return c.artifactPath
}
