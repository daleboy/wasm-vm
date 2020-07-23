package tests

import (
	"fmt"
	"io/ioutil"
	"math/big"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElrondNetwork/arwen-wasm-vm/arwen"
	"github.com/ElrondNetwork/arwen-wasm-vm/config"
	"github.com/ElrondNetwork/arwen-wasm-vm/ipc/common"
	"github.com/ElrondNetwork/arwen-wasm-vm/ipc/nodepart"
	"github.com/ElrondNetwork/arwen-wasm-vm/mock"
	logger "github.com/ElrondNetwork/elrond-go-logger"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
	"github.com/ElrondNetwork/elrond-vm-common/parsers"
	"github.com/stretchr/testify/require"
)

var arwenVirtualMachine = []byte{5, 0}

func TestArwenDriver_DiagnoseWait(t *testing.T) {
	blockchain := &mock.BlockchainHookStub{}
	driver := newDriver(t, blockchain)

	err := driver.DiagnoseWait(100)
	require.Nil(t, err)
}

func TestArwenDriver_DiagnoseWaitWithTimeout(t *testing.T) {
	blockchain := &mock.BlockchainHookStub{}
	driver := newDriver(t, blockchain)

	err := driver.DiagnoseWait(5000)
	require.True(t, common.IsCriticalError(err))
	require.Contains(t, err.Error(), "timeout")
	require.True(t, driver.IsClosed())
}

func TestArwenDriver_RestartsIfStopped(t *testing.T) {
	logger.ToggleLoggerName(true)
	_ = logger.SetLogLevel("*:TRACE")

	blockchain := &mock.BlockchainHookStub{}
	driver := newDriver(t, blockchain)

	blockchain.GetUserAccountCalled = func(address []byte) (vmcommon.UserAccountHandler, error) {
		return &mock.AccountMock{Code: bytecodeCounter}, nil
	}

	vmOutput, err := driver.RunSmartContractCreate(createDeployInput(bytecodeCounter))
	require.Nil(t, err)
	require.NotNil(t, vmOutput)
	vmOutput, err = driver.RunSmartContractCall(createCallInput("increment"))
	require.Nil(t, err)
	require.NotNil(t, vmOutput)

	require.False(t, driver.IsClosed())
	driver.Close()
	require.True(t, driver.IsClosed())

	// Per this request, Arwen is restarted
	vmOutput, err = driver.RunSmartContractCreate(createDeployInput(bytecodeCounter))
	require.Nil(t, err)
	require.NotNil(t, vmOutput)
	require.False(t, driver.IsClosed())
}

func BenchmarkArwenDriver_RestartsIfStopped(b *testing.B) {
	blockchain := &mock.BlockchainHookStub{}
	driver := newDriver(b, blockchain)

	for i := 0; i < b.N; i++ {
		driver.Close()
		require.True(b, driver.IsClosed())
		_ = driver.RestartArwenIfNecessary()
		require.False(b, driver.IsClosed())
	}
}

func BenchmarkArwenDriver_RestartArwenIfNecessary(b *testing.B) {
	blockchain := &mock.BlockchainHookStub{}
	driver := newDriver(b, blockchain)

	for i := 0; i < b.N; i++ {
		_ = driver.RestartArwenIfNecessary()
	}
}

func TestDelegation_ManyNodes(t *testing.T) {
	// Part 1: prepare the Auction Mock and the Delegation SC
	blockchainMock := mock.NewBlockchainHookMock()
	host := newDriver(t, blockchainMock)
	require.NotNil(t, host)

	auction_mock := mock.AccountMock{
		Address: []byte("auction_contract________________"),
		Nonce:   0,
		Code:    GetTestSCCode("auction-mock", "../../"),
		Storage: make(map[string][]byte, 1),
	}
	auction_mock.Storage["stake_per_node"] = big.NewInt(1000).Bytes()
	blockchainMock.AddAccount(&auction_mock)

	delegation_owner := mock.AccountMock{
		Address: []byte("delegation_owner________________"),
		Nonce:   0,
		Balance: big.NewInt(5_000_000),
	}
	blockchainMock.AddAccount(&delegation_owner)

	require.Equal(t, 2, len(blockchainMock.Accounts))

	deployInput := DefaultTestContractCreateInput()
	deployInput.VMInput.CallerAddr = delegation_owner.Address
	deployInput.GasProvided = 999_000_000_000
	deployInput.ContractCode = GetTestSCCode("delegation", "../../")
	deployInput.Arguments = [][]byte{
		auction_mock.Address,
		{0x03, 0xE8}, // 1000
		{0x03, 0xE8}, // 1000
		{0x03, 0xE8}, // 1000
	}

	delegation_contract_address := []byte("delegation_contract_____________")
	blockchainMock.NewAddr = delegation_contract_address
	vmOutput, err := host.RunSmartContractCreate(deployInput)
	require.Nil(t, err)
	require.NotNil(t, vmOutput)
	require.Equal(t, vmcommon.Ok, vmOutput.ReturnCode)

	blockchainMock.UpdateAccounts(vmOutput.OutputAccounts)
	require.Equal(t, 3, len(blockchainMock.Accounts))

	// Part 2: send a transaction to the Delegation contract containing a request
	// to add 1400+ nodes
	parser := parsers.NewCallArgsParser()
	txData := txDataLine()
	fmt.Println("txData length", len(txData))
	function, arguments, err := parser.ParseData(txData)
	require.Nil(t, err)
	fmt.Println("txData argument count", len(arguments))

	callInput := DefaultTestContractCallInput()
	callInput.VMInput.CallerAddr = delegation_owner.Address
	callInput.VMInput.Arguments = arguments[0:258]
	callInput.RecipientAddr = delegation_contract_address
	callInput.Function = function
	callInput.GasProvided = 999_000_000_000

	vmOutput, err = host.RunSmartContractCall(callInput)
	require.Nil(t, err)
	require.NotNil(t, vmOutput)
	fmt.Println(vmOutput.ReturnMessage)

	require.Equal(t, vmcommon.Ok, vmOutput.ReturnCode)
	storageUpdates := vmOutput.OutputAccounts[string(delegation_contract_address)].StorageUpdates
	fmt.Println(len(storageUpdates))

	keyTypeCounts := make(map[string]uint)

	for key := range storageUpdates {
		prefix := resolvePrefix(key)
		if prefix == "unknown" {
			fmt.Println(key)
		}
		_, ok := keyTypeCounts[prefix]
		if !ok {
			keyTypeCounts[prefix] = 0
		}
		keyTypeCounts[prefix] += 1
	}

	for prefix, count := range keyTypeCounts {
		fmt.Println(prefix, ": ", count)
	}
}

var keyPrefixes = []string{"node_id_to_bls", "node_state", "node_signature", "node_bls_to_id", "owner", "num_nodes"}

func resolvePrefix(key string) string {
	for _, prefix := range keyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return prefix
		}
	}

	return "unknown"
}

func newDriver(tb testing.TB, blockchain vmcommon.BlockchainHook) *nodepart.ArwenDriver {
	driver, err := nodepart.NewArwenDriver(
		blockchain,
		common.ArwenArguments{
			VMHostParameters: arwen.VMHostParameters{
				VMType:                   arwenVirtualMachine,
				BlockGasLimit:            uint64(10000000),
				GasSchedule:              config.MakeGasMapForTests(),
				ElrondProtectedKeyPrefix: []byte("ELROND"),
			},
		},
		nodepart.Config{MaxLoopTime: 1000},
	)
	require.Nil(tb, err)
	require.NotNil(tb, driver)
	require.False(tb, driver.IsClosed())
	return driver
}

// GetFileContents retrieves the bytecode of a WASM module from a file
func GetFileContents(fileName string) []byte {
	code, err := ioutil.ReadFile(filepath.Clean(fileName))
	if err != nil {
		panic(fmt.Sprintf("GetFileContents(): %s", fileName))
	}

	return code
}

// GetTestSCCode retrieves the bytecode of a WASM testing module
func GetTestSCCode(scName string, prefixToTestSCs string) []byte {
	pathToSC := prefixToTestSCs + "test/contracts/" + scName + "/output/" + scName + ".wasm"
	return GetFileContents(pathToSC)
}

// DefaultTestContractCreateInput creates a vmcommon.ContractCreateInput struct with default values
func DefaultTestContractCreateInput() *vmcommon.ContractCreateInput {
	return &vmcommon.ContractCreateInput{
		VMInput: vmcommon.VMInput{
			CallerAddr: []byte("caller"),
			Arguments: [][]byte{
				[]byte("argument 1"),
				[]byte("argument 2"),
			},
			CallValue:   big.NewInt(0),
			CallType:    vmcommon.DirectCall,
			GasPrice:    0,
			GasProvided: 0,
		},
		ContractCode: []byte("contract"),
	}
}

// DefaultTestContractCallInput creates a vmcommon.ContractCallInput struct with default values
func DefaultTestContractCallInput() *vmcommon.ContractCallInput {
	return &vmcommon.ContractCallInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:  nil,
			Arguments:   make([][]byte, 0),
			CallValue:   big.NewInt(0),
			CallType:    vmcommon.DirectCall,
			GasPrice:    0,
			GasProvided: 0,
		},
		RecipientAddr: nil,
		Function:      "function",
	}
}
