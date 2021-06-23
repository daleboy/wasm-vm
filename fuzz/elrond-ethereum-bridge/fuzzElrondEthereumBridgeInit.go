package elrond_ethereum_bridge

import (
	"fmt"
	"math/big"
	"strconv"
)

const (
	INIT_BALANCE = 10000000 // 10 million
)

type MultisigInitArgs struct {
	requiredStake *big.Int
	slashAmount   *big.Int
	quorum        int
	boardMembers  []string
}

type DeployChildContractsArgs struct {
	egldEsdtSwapCode       []byte
	multiTransferEsdtCode  []byte
	ethereumFeePrepayCode  []byte
	esdtSafeCode           []byte
	priceAggregatorAddress string
	wrappedEgldTokenId     string
	wrappedEthTokenId      string
	tokenWhitelist         []string
}

func (fe *fuzzExecutor) initData() error {
	fe.data = &fuzzData{
		actorAddresses: &ActorAddresses{
			owner:             "address:owner",
			relayers:          []string{},
			users:             []string{},
			multisig:          "sc:multisig",
			priceAggregator:   "sc:price_aggregator",
			egldEsdtSwap:      "sc:egld_esdt_swap",
			esdtSafe:          "sc:esdt_safe",
			ethereumFeePrepay: "sc:ethereum_fee_prepay",
			multiTransferEsdt: "sc:multi_transfer_esdt",
		},
		egldEsdtSwapState:      nil,
		esdtSafeState:          nil,
		ethereumFeePrepayState: nil,
		multiTransferEsdtState: nil,
		multisigState:          nil,
	}
	fe.world.Clear()

	return nil
}

func (fe *fuzzExecutor) initAccounts(nrRelayers int, nrUsers int, initialBalance *big.Int) error {
	fe.createAccount(fe.data.actorAddresses.owner, initialBalance)

	for i := 1; i <= nrRelayers; i++ {
		address := fe.relayerAddress(i)
		err := fe.createAccount(address, initialBalance)
		if err != nil {
			return err
		}

		fe.data.actorAddresses.relayers = append(fe.data.actorAddresses.relayers, address)
	}

	for i := 1; i <= nrUsers; i++ {
		address := fe.userAddress(i)
		err := fe.createAccount(address, initialBalance)
		if err != nil {
			return err
		}

		fe.data.actorAddresses.users = append(fe.data.actorAddresses.users, address)
	}

	return nil
}

func (fe *fuzzExecutor) deployMultisig(multisigInitArgs *MultisigInitArgs) error {
	bundledArguments := []string{
		multisigInitArgs.requiredStake.String(),
		multisigInitArgs.slashAmount.String(),
		strconv.Itoa(multisigInitArgs.quorum),
	}
	bundledArguments = append(bundledArguments, multisigInitArgs.boardMembers...)

	err := fe.deployContract(fe.data.actorAddresses.owner, fe.data.actorAddresses.multisig, "multisig.wasm",
		bundledArguments...)
	if err != nil {
		return err
	}

	return nil
}

func (fe *fuzzExecutor) setupChildContracts(
	deployChildContractsArgs *DeployChildContractsArgs) error {

	err := fe.setupAggregator()
	if err != nil {
		return err
	}

	/*
		err = fe.executeStep(fmt.Sprintf(`
			{
				"step": "setState",
				"accounts": {
					"%s": {
						"nonce": "0",
						"balance": "0",
						"storage": {}
					}
				}
			}`,
			fe.data.actorAddresses.owner,
		))
		if err != nil {
			return err
		}
	*/

	return nil
}

func (fe *fuzzExecutor) setupAggregator() error {
	err := fe.executeStep(fmt.Sprintf(`
		{
			"step": "externalSteps",
			"path": "%s"
		}`,
		"../../test/elrond-ethereum-bridge/price-aggregator/mandos/oracle_submit.scen.json",
	))
	if err != nil {
		return err
	}

	return nil
}
