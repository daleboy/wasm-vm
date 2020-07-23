package common

import (
	"math/big"

	vmcommon "github.com/ElrondNetwork/elrond-vm-common"
)

// MessageContractDeployRequest is deploy request message (from Node)
type MessageContractDeployRequest struct {
	Message
	CreateInput *vmcommon.ContractCreateInput
}

// NewMessageContractDeployRequest creates a message
func NewMessageContractDeployRequest(input *vmcommon.ContractCreateInput) *MessageContractDeployRequest {
	message := &MessageContractDeployRequest{}
	message.Kind = ContractDeployRequest
	message.CreateInput = input
	return message
}

// MessageContractCallRequest is call request message (from Node)
type MessageContractCallRequest struct {
	Message
	CallInput *vmcommon.ContractCallInput
}

// NewMessageContractCallRequest creates a message
func NewMessageContractCallRequest(input *vmcommon.ContractCallInput) *MessageContractCallRequest {
	message := &MessageContractCallRequest{}
	message.Kind = ContractCallRequest
	message.CallInput = input
	return message
}

// MessageContractResponse is a contract response message (from Arwen)
type MessageContractResponse struct {
	Message
	CorrectedVMOutput *CorrectedVMOutput
}

// NewMessageContractResponse creates a message
func NewMessageContractResponse(vmOutput *vmcommon.VMOutput, err error) *MessageContractResponse {
	message := &MessageContractResponse{}
	message.Kind = ContractResponse
	message.CorrectedVMOutput = NewCorrectedVMOutput(vmOutput)
	message.SetError(err)
	return message
}

type CorrectedVMOutput struct {
	ReturnData              [][]byte
	ReturnCode              vmcommon.ReturnCode
	ReturnMessage           string
	GasRemaining            uint64
	GasRefund               *big.Int
	CorrectedOutputAccounts []*CorrectedOutputAccount
	DeletedAccounts         [][]byte
	TouchedAccounts         [][]byte
	Logs                    []*vmcommon.LogEntry
}

func NewCorrectedVMOutput(vmOutput *vmcommon.VMOutput) *CorrectedVMOutput {
	result := &CorrectedVMOutput{
		ReturnData:              vmOutput.ReturnData,
		ReturnCode:              vmOutput.ReturnCode,
		ReturnMessage:           vmOutput.ReturnMessage,
		GasRemaining:            vmOutput.GasRemaining,
		GasRefund:               vmOutput.GasRefund,
		CorrectedOutputAccounts: make([]*CorrectedOutputAccount, 0, len(vmOutput.OutputAccounts)),
		DeletedAccounts:         vmOutput.DeletedAccounts,
		TouchedAccounts:         vmOutput.TouchedAccounts,
		Logs:                    vmOutput.Logs,
	}

	for _, account := range vmOutput.OutputAccounts {
		result.CorrectedOutputAccounts = append(result.CorrectedOutputAccounts, NewCorrectedOutputAccount(account))
	}

	return result
}

func (vmOutput *CorrectedVMOutput) GetVMOutput() *vmcommon.VMOutput {
	accountsMap := make(map[string]*vmcommon.OutputAccount)

	for _, item := range vmOutput.CorrectedOutputAccounts {
		accountsMap[string(item.Address)] = item.GetOutputAccount()
	}

	return &vmcommon.VMOutput{
		ReturnData:      vmOutput.ReturnData,
		ReturnCode:      vmOutput.ReturnCode,
		ReturnMessage:   vmOutput.ReturnMessage,
		GasRemaining:    vmOutput.GasRemaining,
		GasRefund:       vmOutput.GasRefund,
		OutputAccounts:  accountsMap,
		DeletedAccounts: vmOutput.DeletedAccounts,
		TouchedAccounts: vmOutput.TouchedAccounts,
		Logs:            vmOutput.Logs,
	}
}

type CorrectedOutputAccount struct {
	Address        []byte
	Nonce          uint64
	Balance        *big.Int
	BalanceDelta   *big.Int
	StorageUpdates []*vmcommon.StorageUpdate
	Code           []byte
	CodeMetadata   []byte
	Data           []byte
	GasLimit       uint64
	CallType       vmcommon.CallType
}

func NewCorrectedOutputAccount(account *vmcommon.OutputAccount) *CorrectedOutputAccount {
	result := &CorrectedOutputAccount{
		Address:        account.Address,
		Nonce:          account.Nonce,
		Balance:        account.Balance,
		BalanceDelta:   account.BalanceDelta,
		StorageUpdates: make([]*vmcommon.StorageUpdate, 0, len(account.StorageUpdates)),
		Code:           account.Code,
		CodeMetadata:   account.CodeMetadata,
		Data:           account.Data,
		GasLimit:       account.GasLimit,
		CallType:       account.CallType,
	}

	for _, storageUpdate := range account.StorageUpdates {
		result.StorageUpdates = append(result.StorageUpdates, storageUpdate)
	}

	return result
}

func (account *CorrectedOutputAccount) GetOutputAccount() *vmcommon.OutputAccount {
	updatesMap := make(map[string]*vmcommon.StorageUpdate)

	for _, item := range account.StorageUpdates {
		updatesMap[string(item.Offset)] = item
	}

	return &vmcommon.OutputAccount{
		Address:        account.Address,
		Nonce:          account.Nonce,
		Balance:        account.Balance,
		BalanceDelta:   account.BalanceDelta,
		StorageUpdates: updatesMap,
		Code:           account.Code,
		CodeMetadata:   account.CodeMetadata,
		Data:           account.Data,
		GasLimit:       account.GasLimit,
		CallType:       account.CallType,
	}
}
