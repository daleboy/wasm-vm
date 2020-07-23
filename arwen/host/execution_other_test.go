package host

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ElrondNetwork/arwen-wasm-vm/mock"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
	"github.com/ElrondNetwork/elrond-vm-common/parsers"
	"github.com/stretchr/testify/require"
)

func TestDelegation_ManyNodes(t *testing.T) {
	// Part 1: prepare the Auction Mock and the Delegation SC
	blockchainMock := mock.NewBlockchainHookMock()
	cryptoHook := mock.NewCryptoHook()
	host, err := DefaultTestArwen(t, blockchainMock, cryptoHook)
	require.NotNil(t, host)
	require.Nil(t, err)

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
	callInput.VMInput.Arguments = arguments
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
