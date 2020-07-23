package host

import (
	"fmt"
	"math/big"
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
	deployInput.GasProvided = 1_000_000
	deployInput.ContractCode = GetTestSCCode("delegation", "../../")
	deployInput.Arguments = [][]byte{
		auction_mock.Address,
		{0x03, 0xE8}, // 1000
		{0x03, 0xE8}, // 1000
		{0x03, 0xE8}, // 1000
	}

	blockchainMock.NewAddr = []byte("delegation_contract_____________")
	vmOutput, err := host.RunSmartContractCreate(deployInput)
	require.Nil(t, err)
	require.NotNil(t, vmOutput)
	require.Equal(t, vmcommon.Ok, vmOutput.ReturnCode)

	blockchainMock.UpdateAccounts(vmOutput.OutputAccounts)
	require.Equal(t, 3, len(blockchainMock.Accounts))

	// Part 2: send a transaction to the Delegation contract containing a request
	// to add 1400+ nodes
	// txData := GetFileContents("../../test/delegation-at-genesis.txt")
	// parser := parsers.NewCallArgsParser()
	// callInput := DefaultTestContractCallInput()
	// callInput.VMInput.CallerAddr = delegation_owner.Address

}
