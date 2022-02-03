package contracts

import (
	"math/big"
	"testing"

	"github.com/ElrondNetwork/arwen-wasm-vm/v1_4/arwen"
	arwenHost "github.com/ElrondNetwork/arwen-wasm-vm/v1_4/arwen/host"
	"github.com/ElrondNetwork/arwen-wasm-vm/v1_4/arwen/mock"
	gasSchedules "github.com/ElrondNetwork/arwen-wasm-vm/v1_4/arwenmandos/gasSchedules"
	worldmock "github.com/ElrondNetwork/arwen-wasm-vm/v1_4/mock/world"
	test "github.com/ElrondNetwork/arwen-wasm-vm/v1_4/testcommon"
	"github.com/ElrondNetwork/elrond-go-core/data/vm"
	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
	"github.com/ElrondNetwork/elrond-vm-common/parsers"
	"github.com/stretchr/testify/require"
)

const GasProvided = 0xFFFFFFFFFFFFFFFF

func Test_EgldEsdtSwap(t *testing.T) {
	deployerAddress := []byte("deployer")
	contractAddress := test.MakeTestSCAddress("egld-esdt-swap")
	contractCode := test.GetSCCode("../../test/sc-bridge/egld-esdt-swap.wasm")
	tokenIdentifier := []byte("WEGLD")

	gasMap, err := gasSchedules.LoadGasScheduleConfig(gasSchedules.GetV4())
	require.Nil(t, err)

	world := worldmock.NewMockWorld()
	world.InitBuiltinFunctions(gasMap)

	deployer := &worldmock.Account{
		Address: deployerAddress,
		Nonce:   1024,
		Balance: big.NewInt(42),
	}

	world.AcctMap.PutAccount(deployer)
	world.NewAddressMocks = append(world.NewAddressMocks, &worldmock.NewAddressMock{
		CreatorAddress: deployer.Address,
		CreatorNonce:   deployer.Nonce,
		NewAddress:     contractAddress,
	})

	esdtTransferParser, _ := parsers.NewESDTTransferParser(worldmock.WorldMarshalizer)
	host, err := arwenHost.NewArwenVM(world, &arwen.VMHostParameters{
		VMType:                   test.DefaultVMType,
		BlockGasLimit:            uint64(1000),
		GasSchedule:              gasMap,
		BuiltInFuncContainer:     world.BuiltinFuncs.Container,
		ElrondProtectedKeyPrefix: []byte("ELROND"),
		ESDTTransferParser:       esdtTransferParser,
		EpochNotifier:            &mock.EpochNotifierStub{},
	})
	require.Nil(t, err)

	deployInput := &vmcommon.ContractCreateInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:  deployerAddress,
			Arguments:   [][]byte{tokenIdentifier},
			CallValue:   big.NewInt(0),
			CallType:    vm.DirectCall,
			GasProvided: GasProvided,
		},
		ContractCode: contractCode,
	}

	deployer.Nonce++
	vmOutput, err := host.RunSmartContractCreate(deployInput)
	require.Nil(t, err)
	require.Equal(t, "", vmOutput.ReturnMessage)
	require.Equal(t, uint64(1546300), GasProvided-vmOutput.GasRemaining)
	_ = world.UpdateAccounts(vmOutput.OutputAccounts, nil)

	// set roles
	contract := world.AcctMap.GetAccount(contractAddress)
	contract.SetTokenRolesAsStrings(tokenIdentifier, []string{"ESDTRoleLocalMint", "ESDTRoleLocalBurn"})

	// getWrappedEgldTokenId
	vmOutput, err = host.RunSmartContractCall(&vmcommon.ContractCallInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:  deployerAddress,
			Arguments:   [][]byte{},
			CallValue:   big.NewInt(0),
			CallType:    vm.DirectCall,
			GasProvided: GasProvided,
		},
		RecipientAddr: contractAddress,
		Function:      "getWrappedEgldTokenId",
	})
	require.Nil(t, err)
	require.Equal(t, "", vmOutput.ReturnMessage)
	require.Equal(t, tokenIdentifier, vmOutput.ReturnData[0])
	require.Equal(t, uint64(1435575), GasProvided-vmOutput.GasRemaining)
	_ = world.UpdateAccounts(vmOutput.OutputAccounts, nil)

	// wrapEgld
	vmOutput, err = host.RunSmartContractCall(&vmcommon.ContractCallInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:  deployerAddress,
			Arguments:   [][]byte{},
			CallValue:   big.NewInt(1),
			CallType:    vm.DirectCall,
			GasProvided: GasProvided,
		},
		RecipientAddr: contractAddress,
		Function:      "wrapEgld",
	})
	require.Nil(t, err)
	require.Equal(t, "", vmOutput.ReturnMessage)
	_ = world.UpdateAccounts(vmOutput.OutputAccounts, nil)
}
