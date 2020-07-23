package host

import (
	"math/big"
	"testing"

	"github.com/ElrondNetwork/arwen-wasm-vm/ipc/arwenpart"
	"github.com/ElrondNetwork/arwen-wasm-vm/mock"
)

func TestDelegation_ManyNodes(t *testing.T) {
	newAddress := []byte("new smartcontract")

	blockchainMock := &mock.BlockchainHookMock{}
	cryptoHook := arwenpart.NewCryptoHookGateway()

	host, err := DefaultTestArwen(t, blockchainMock, cryptoHook)

	input := DefaultTestContractCreateInput()
	input.CallValue = big.NewInt(88)
	input.GasProvided = 1000
	input.ContractCode = GetTestSCCode("init-correct", "../../")
	input.Arguments = [][]byte{{0}}

}
