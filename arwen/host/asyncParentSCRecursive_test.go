package host

import (
	"math/big"
	"testing"

	mock "github.com/ElrondNetwork/arwen-wasm-vm/mock/context"
	"github.com/ElrondNetwork/elrond-go/testscommon/txDataBuilder"
	"github.com/stretchr/testify/require"
)

func createTestAsyncRecursiveParentContract(t testing.TB, host *vmHost, imb *mock.InstanceBuilderMock, testConfig *asyncCallRecursiveTestConfig) {
	parentInstance := imb.CreateAndStoreInstanceMock(t, host, parentAddress, testConfig.parentBalance)
	addAsyncRecursiveParentMethodsToInstanceMock(parentInstance, testConfig)
}

func addAsyncRecursiveParentMethodsToInstanceMock(instanceMock *mock.InstanceMock, testConfig *asyncCallRecursiveTestConfig) {
	input := DefaultTestContractCallInput()
	input.GasProvided = testConfig.gasProvidedToChild

	instanceMock.AddMockMethod("forwardAsyncCall", func() *mock.InstanceMock {
		host := instanceMock.Host
		instance := mock.GetMockInstance(host)
		t := instance.T
		arguments := host.Runtime().Arguments()
		destination := arguments[0]
		function := string(arguments[1])
		value := big.NewInt(testConfig.transferFromParentToChild).Bytes()

		host.Metering().UseGas(testConfig.gasUsedByParent)

		// only one child call by default
		recursiveChildCalls := big.NewInt(1)
		if len(arguments) > 2 {
			recursiveChildCalls = big.NewInt(0).SetBytes(arguments[2])
		}

		callData := txDataBuilder.NewBuilder()
		callData.Func(function)
		callData.BigInt(recursiveChildCalls)

		err := host.Runtime().ExecuteAsyncCall(destination, callData.ToBytes(), value)
		require.Nil(t, err)

		return instance
	})

	instanceMock.AddMockMethod("callBack", func() *mock.InstanceMock {
		host := instanceMock.Host
		instance := mock.GetMockInstance(host)
		host.Metering().UseGas(testConfig.gasUsedByCallback)
		return instance
	})
}