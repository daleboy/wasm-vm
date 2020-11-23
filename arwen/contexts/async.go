package contexts

import (
	"encoding/json"
	"math"
	"math/big"

	"github.com/ElrondNetwork/arwen-wasm-vm/arwen"
	extramath "github.com/ElrondNetwork/arwen-wasm-vm/math"
	"github.com/ElrondNetwork/elrond-go/core/vmcommon"
)

var _ arwen.AsyncContext = (*asyncContext)(nil)

type asyncContext struct {
	host       arwen.VMHost
	stateStack []*asyncContext

	CallerAddr      []byte
	ReturnData      []byte
	AsyncCallGroups []*arwen.AsyncCallGroup
}

// NewAsyncContext creates a new asyncContext
func NewAsyncContext(host arwen.VMHost) *asyncContext {
	return &asyncContext{
		host:            host,
		stateStack:      nil,
		CallerAddr:      nil,
		ReturnData:      nil,
		AsyncCallGroups: make([]*arwen.AsyncCallGroup, 0),
	}
}

func (context *asyncContext) GetCallerAddress() []byte {
	return context.CallerAddr
}

func (context *asyncContext) GetReturnData() []byte {
	return context.ReturnData
}

func (context *asyncContext) GetCallGroup(groupID string) (*arwen.AsyncCallGroup, bool) {
	index, ok := context.findGroupByID(groupID)
	if ok {
		return context.AsyncCallGroups[index], true
	}
	return nil, false
}

func (context *asyncContext) AddCallGroup(group *arwen.AsyncCallGroup) error {
	_, exists := context.findGroupByID(group.Identifier)
	if exists {
		return arwen.ErrAsyncCallGroupExistsAlready
	}

	context.AsyncCallGroups = append(context.AsyncCallGroups, group)
	return nil
}

func (context *asyncContext) DeleteCallGroupByID(groupID string) {
	index, ok := context.findGroupByID(groupID)
	if !ok {
		return
	}

	context.DeleteCallGroup(index)
}

func (context *asyncContext) DeleteCallGroup(index int) {
	groups := context.AsyncCallGroups
	if len(groups) == 0 {
		return
	}

	last := len(groups) - 1
	if index < 0 || index > last {
		return
	}

	groups[index] = groups[last]
	groups = groups[:last]
	context.AsyncCallGroups = groups
}

func (context *asyncContext) AddCall(groupID string, call *arwen.AsyncCall) error {
	if context.host.IsBuiltinFunctionName(call.SuccessCallback) {
		return arwen.ErrCannotUseBuiltinAsCallback
	}
	if context.host.IsBuiltinFunctionName(call.ErrorCallback) {
		return arwen.ErrCannotUseBuiltinAsCallback
	}

	group, ok := context.GetCallGroup(groupID)
	if !ok {
		group = arwen.NewAsyncCallGroup(groupID)
		context.AddCallGroup(group)
	}

	group.AddAsyncCall(call)

	return nil
}

func (context *asyncContext) FindCall(destination []byte) (string, int, error) {
	for _, group := range context.AsyncCallGroups {
		callIndex, ok := group.FindByDestination(destination)
		if ok {
			return group.Identifier, callIndex, nil
		}
	}

	return "", -1, arwen.ErrAsyncCallNotFound
}

func (context *asyncContext) UpdateCurrentCallStatus() (*arwen.AsyncCall, error) {
	vmInput := context.host.Runtime().GetVMInput()
	if vmInput.CallType != vmcommon.AsynchronousCallBack {
		return nil, nil
	}

	if len(vmInput.Arguments) == 0 {
		return nil, arwen.ErrCannotInterpretCallbackArgs
	}

	// The first argument of the callback is the return code of the destination call
	destReturnCode := big.NewInt(0).SetBytes(vmInput.Arguments[0]).Uint64()

	groupID, index, err := context.FindCall(vmInput.CallerAddr)
	if err != nil {
		return nil, err
	}

	group, _ := context.GetCallGroup(groupID)
	call := group.AsyncCalls[index]
	call.UpdateStatus(vmcommon.ReturnCode(destReturnCode))

	return call, nil
}

// GetPendingOnly creates a new asyncContext containing only
// the pending AsyncCallGroups, without deleting anything from the initial asyncContext
func (context *asyncContext) GetPendingOnly() []*arwen.AsyncCallGroup {
	pendingGroups := make([]*arwen.AsyncCallGroup, 0)
	var pendingGroup *arwen.AsyncCallGroup
	for _, group := range context.AsyncCallGroups {
		pendingGroup = nil
		for _, asyncCall := range group.AsyncCalls {
			if asyncCall.Status != arwen.AsyncCallPending {
				continue
			}

			if pendingGroup == nil {
				pendingGroup = arwen.NewAsyncCallGroup(group.Identifier)
				pendingGroups = append(pendingGroups, pendingGroup)
			}

			pendingGroup.AsyncCalls = append(pendingGroup.AsyncCalls, asyncCall)
		}
	}

	return pendingGroups
}

func (context *asyncContext) InitState() {
	context.CallerAddr = make([]byte, 0)
	context.ReturnData = make([]byte, 0)
	context.AsyncCallGroups = make([]*arwen.AsyncCallGroup, 0)
}

func (context *asyncContext) SetCaller(caller []byte) {
	context.CallerAddr = caller
}

func (context *asyncContext) PushState() {
	// TODO the call groups must be cloned, not just referenced
	newState := &asyncContext{
		CallerAddr:      context.CallerAddr,
		ReturnData:      context.ReturnData,
		AsyncCallGroups: context.AsyncCallGroups,
	}

	context.stateStack = append(context.stateStack, newState)
}

func (context *asyncContext) PopDiscard() {
}

func (context *asyncContext) PopSetActiveState() {
}

func (context *asyncContext) PopMergeActiveState() {
}

func (context *asyncContext) ClearStateStack() {
	context.stateStack = make([]*asyncContext, 0)
}

// Execute is the entry-point of the async calling mechanism; it is called by
// host.ExecuteOnDestContext() and host.callSCMethod(). When Execute()
// finishes, there should be no remaining AsyncCalls that can be executed
// synchronously, and all AsyncCalls that require asynchronous execution must
// already have corresponding entries in vmOutput.OutputAccounts, to be
// dispatched across shards.
//
// Execute() does NOT handle the callbacks of cross-shard AsyncCalls. See
// PostprocessCrossShardCallback() for that.
//
// Note that Execute() is mutually recursive with host.ExecuteOnDestContext(),
// because synchronous AsyncCalls are executed with
// host.ExecuteOnDestContext(), which, in turn, calls asyncContext.Execute() to
// resolve AsyncCalls generated by the AsyncCalls, and so on.
func (context *asyncContext) Execute() error {
	if context.IsComplete() {
		return nil
	}

	// Step 1: execute all AsyncCalls that can be executed synchronously
	// (includes smart contracts and built-in functions in the same shard)
	err := context.setupAsyncCallsGas()
	if err != nil {
		return err
	}

	for groupIndex, group := range context.AsyncCallGroups {
		// Execute the call group strictly synchronously (no asynchronous calls allowed)
		err := context.executeCallGroup(group, true)
		if err != nil {
			return err
		}

		if group.IsCompleted() {
			context.DeleteCallGroup(groupIndex)
		}
	}

	// Step 2: redistribute unspent gas; then, in one combined step, do the
	// following:
	// * locally execute built-in functions with cross-shard
	//   destinations, whereby the cross-shard OutputAccount entries are generated
	// * call host.sendAsyncCallCrossShard() for each pending AsyncCall, to
	//   generate the corresponding cross-shard OutputAccount entries
	err = context.setupAsyncCallsGas()
	if err != nil {
		return err
	}

	for _, group := range context.AsyncCallGroups {
		// Execute the call group allowing asynchronous (cross-shard) calls as well
		err = context.executeCallGroup(group, false)
		if err != nil {
			return err
		}
	}

	context.DeleteCallGroupByID(arwen.LegacyAsyncCallGroupID)

	err = context.Save()
	if err != nil {
		return err
	}

	return nil
}

// PrepareLegacyAsyncCall builds an AsyncCall struct from its arguments, sets it as
// the default async call and informs Wasmer to stop contract execution with BreakpointAsyncCall
func (context *asyncContext) PrepareLegacyAsyncCall(address []byte, data []byte, value []byte) error {
	legacyGroupID := arwen.LegacyAsyncCallGroupID
	legacyCallback := []byte(arwen.CallbackFunctionName)

	_, exists := context.GetCallGroup(legacyGroupID)
	if exists {
		return arwen.ErrOnlyOneLegacyAsyncCallAllowed
	}

	err := context.CreateAndAddCall(legacyGroupID,
		address,
		data,
		value,
		legacyCallback,
		legacyCallback,
		math.MaxUint64,
	)
	if err != nil {
		return err
	}

	context.host.Runtime().SetRuntimeBreakpointValue(arwen.BreakpointAsyncCall)

	return nil
}

// CreateAndAddAsyncCall creates a new AsyncCall from its arguments and adds it
// to the specified group
func (context *asyncContext) CreateAndAddCall(
	groupID string,
	address []byte,
	data []byte,
	value []byte,
	successCallback []byte,
	errorCallback []byte,
	gas uint64,
) error {

	gasToLock, err := context.prepareGasForAsyncCall()
	if err != nil {
		return err
	}

	if gas == math.MaxUint64 {
		metering := context.host.Metering()
		gas = metering.GasLeft()
	}

	return context.AddCall(groupID, &arwen.AsyncCall{
		Status:          arwen.AsyncCallPending,
		Destination:     address,
		Data:            data,
		ValueBytes:      value,
		SuccessCallback: string(successCallback),
		ErrorCallback:   string(errorCallback),
		ProvidedGas:     gas,
		GasLocked:       gasToLock,
	})
}

func (context *asyncContext) prepareGasForAsyncCall() (uint64, error) {
	metering := context.host.Metering()
	err := metering.UseGasForAsyncStep()
	if err != nil {
		return 0, err
	}

	var shouldLockGas bool

	if !context.host.IsDynamicGasLockingEnabled() {
		// Legacy mode: static gas locking, always enabled
		shouldLockGas = true
	} else {
		// Dynamic mode: lock only if callBack() exists
		shouldLockGas = context.host.Runtime().HasCallbackMethod()
	}

	gasToLock := uint64(0)
	if shouldLockGas {
		gasToLock = metering.ComputeGasLockedForAsync()
		err = metering.UseGasBounded(gasToLock)
		if err != nil {
			return 0, err
		}
	}

	return gasToLock, nil
}

/**
 * postprocessCrossShardCallback() is called by host.callSCMethod() after it
 * has locally executed the callback of a returning cross-shard AsyncCall,
 * which means that the AsyncContext corresponding to the original transaction
 * must be loaded from storage, and then the corresponding AsyncCall must be
 * deleted from the current AsyncContext.

 * TODO because individual AsyncCalls are contained by AsyncCallGroups, we
 * must verify whether the containing AsyncCallGroup has any remaining calls
 * pending. If not, the final callback of the containing AsyncCallGroup must be
 * executed as well.
 */
func (context *asyncContext) PostprocessCrossShardCallback() error {
	runtime := context.host.Runtime()
	if runtime.Function() == arwen.CallbackFunctionName {
		// Legacy callbacks do not require postprocessing.
		return nil
	}

	// TODO FindAsyncCallByDestination() only returns the first matched AsyncCall
	// by destination, but there could be multiple matches in an AsyncContext.
	vmInput := runtime.GetVMInput()
	currentGroupID, asyncCallIndex, err := context.FindCall(vmInput.CallerAddr)
	if err != nil {
		return err
	}

	currentCallGroup, ok := context.GetCallGroup(currentGroupID)
	if !ok {
		return arwen.ErrCallBackFuncNotExpected
	}

	currentCallGroup.DeleteAsyncCall(asyncCallIndex)
	if currentCallGroup.HasPendingCalls() {
		return nil
	}

	context.DeleteCallGroupByID(currentGroupID)
	// Are we still waiting for callbacks to return?
	if context.HasPendingCallGroups() {
		return nil
	}

	err = context.Delete()
	if err != nil {
		return err
	}

	return context.executeAsyncContextCallback()
}

// executeAsyncContextCallback will either execute a sync call (in-shard) to
// the original caller by invoking its callback directly, or will dispatch a
// cross-shard callback to it.
func (context *asyncContext) executeAsyncContextCallback() error {
	execMode, err := context.DetermineExecutionMode(context.CallerAddr, context.ReturnData)
	if err != nil {
		return err
	}

	if execMode != arwen.SyncExecution {
		return context.sendContextCallbackToOriginalCaller()
	}

	// The caller is in the same shard, execute its callback
	callbackCallInput := context.createSyncContextCallbackInput()

	callbackVMOutput, callBackErr := context.host.ExecuteOnDestContext(callbackCallInput)
	context.finishSyncExecution(callbackVMOutput, callBackErr)

	return nil
}

func (context *asyncContext) sendContextCallbackToOriginalCaller() error {
	host := context.host
	runtime := host.Runtime()
	output := host.Output()
	metering := host.Metering()
	currentCall := runtime.GetVMInput()

	err := output.Transfer(
		context.CallerAddr,
		runtime.GetSCAddress(),
		metering.GasLeft(),
		0,
		currentCall.CallValue,
		context.ReturnData,
		vmcommon.AsynchronousCallBack,
	)
	if err != nil {
		metering.UseGas(metering.GasLeft())
		runtime.FailExecution(err)
		return err
	}

	return nil
}

func (context *asyncContext) createSyncContextCallbackInput() *vmcommon.ContractCallInput {
	host := context.host
	runtime := host.Runtime()
	metering := host.Metering()

	_, arguments, err := host.CallArgsParser().ParseData(string(context.ReturnData))
	if err != nil {
		arguments = [][]byte{context.ReturnData}
	}

	// TODO ensure a new value for VMInput.CurrentTxHash
	input := &vmcommon.ContractCallInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:     runtime.GetSCAddress(),
			Arguments:      arguments,
			CallValue:      runtime.GetVMInput().CallValue,
			CallType:       vmcommon.AsynchronousCallBack,
			GasPrice:       runtime.GetVMInput().GasPrice,
			GasProvided:    metering.GasLeft(),
			CurrentTxHash:  runtime.GetCurrentTxHash(),
			OriginalTxHash: runtime.GetOriginalTxHash(),
			PrevTxHash:     runtime.GetPrevTxHash(),
		},
		RecipientAddr: context.CallerAddr,
		Function:      arwen.CallbackFunctionName, // TODO currently default; will customize in AsynContext
	}
	return input
}

func (context *asyncContext) executeCallGroup(
	group *arwen.AsyncCallGroup,
	syncExecutionOnly bool,
) error {
	for _, asyncCall := range group.AsyncCalls {
		err := context.executeCall(asyncCall, syncExecutionOnly)
		if err != nil {
			return err
		}
	}

	group.DeleteCompletedAsyncCalls()

	// If ALL the AsyncCalls in the AsyncCallGroup were executed synchronously,
	// then the AsyncCallGroup can have its callback executed.
	if group.IsCompleted() {
		// TODO reenable this, after allowing a gas limit for it and deciding what
		// arguments it receives (this method is currently a NOP and returns nil)
		return context.executeAsyncCallGroupCallback(group)
	}

	return nil
}

// TODO split into two different functions, for sync execution and async
// execution, and remove parameter syncExecutionOnly
func (context *asyncContext) executeCall(
	asyncCall *arwen.AsyncCall,
	syncExecutionOnly bool,
) error {
	execMode, err := context.DetermineExecutionMode(asyncCall.Destination, asyncCall.Data)
	if err != nil {
		return err
	}

	if execMode == arwen.SyncExecution {
		vmOutput, err := context.executeSyncCall(asyncCall)

		// The vmOutput instance returned by host.executeSyncCall() is never nil,
		// by design. Using it without checking for err is safe here.
		asyncCall.UpdateStatus(vmOutput.ReturnCode)

		// TODO host.executeSyncCallback() returns a vmOutput produced by executing
		// the callback. Information from this vmOutput should be preserved in the
		// pending AsyncCallGroup, and made available to the callback of the
		// AsyncCallGroup (currently not implemented).
		callbackVMOutput, callbackErr := context.executeSyncCallback(asyncCall, vmOutput, err)
		context.finishSyncExecution(callbackVMOutput, callbackErr)
		return nil
	}

	if syncExecutionOnly {
		return nil
	}

	if execMode == arwen.AsyncBuiltinFunc {
		// Built-in functions will handle cross-shard calls themselves, by
		// generating entries in vmOutput.OutputAccounts, but they need to be
		// executed synchronously to do that. It is not necessary to call
		// sendAsyncCallCrossShard(). The vmOutput produced by the built-in
		// function, containing the cross-shard call, has ALREADY been merged into
		// the main output by the inner call to host.ExecuteOnDestContext().  The
		// status of the AsyncCall is not updated here - it will be updated by
		// postprocessCrossShardCallback(), when the cross-shard call returns.
		vmOutput, err := context.executeSyncCall(asyncCall)
		if err != nil {
			return err
		}

		if vmOutput.ReturnCode != vmcommon.Ok {
			asyncCall.UpdateStatus(vmOutput.ReturnCode)
			callbackVMOutput, callbackErr := context.executeSyncCallback(asyncCall, vmOutput, err)
			context.finishSyncExecution(callbackVMOutput, callbackErr)
		}

		return nil
	}

	if execMode == arwen.AsyncUnknown {
		return context.sendAsyncCallCrossShard(asyncCall)
	}

	return nil
}

func (context *asyncContext) executeSyncCall(asyncCall *arwen.AsyncCall) (*vmcommon.VMOutput, error) {
	destinationCallInput, err := context.createSyncCallInput(asyncCall)
	if err != nil {
		return nil, err
	}

	return context.host.ExecuteOnDestContext(destinationCallInput)
}

func (context *asyncContext) executeSyncCallback(
	asyncCall *arwen.AsyncCall,
	vmOutput *vmcommon.VMOutput,
	err error,
) (*vmcommon.VMOutput, error) {

	callbackInput, err := context.createSyncCallbackInput(asyncCall, vmOutput, err)
	if err != nil {
		return nil, err
	}

	return context.host.ExecuteOnDestContext(callbackInput)
}

func (context *asyncContext) executeAsyncCallGroupCallback(group *arwen.AsyncCallGroup) error {
	// TODO implement this
	return nil
}

func (context *asyncContext) sendAsyncCallCrossShard(asyncCall arwen.AsyncCallHandler) error {
	host := context.host
	runtime := host.Runtime()
	output := host.Output()

	err := output.Transfer(
		asyncCall.GetDestination(),
		runtime.GetSCAddress(),
		asyncCall.GetGasLimit(),
		asyncCall.GetGasLocked(),
		big.NewInt(0).SetBytes(asyncCall.GetValueBytes()),
		asyncCall.GetData(),
		vmcommon.AsynchronousCall,
	)
	if err != nil {
		metering := host.Metering()
		metering.UseGas(metering.GasLeft())
		runtime.FailExecution(err)
		return err
	}

	return nil
}

func (context *asyncContext) createSyncCallInput(asyncCall arwen.AsyncCallHandler) (*vmcommon.ContractCallInput, error) {
	host := context.host
	runtime := host.Runtime()
	sender := runtime.GetSCAddress()

	function, arguments, err := host.CallArgsParser().ParseData(string(asyncCall.GetData()))
	if err != nil {
		return nil, err
	}

	gasLimit := asyncCall.GetGasLimit()
	gasToUse := host.Metering().GasSchedule().ElrondAPICost.AsyncCallStep
	if gasLimit <= gasToUse {
		return nil, arwen.ErrNotEnoughGas
	}
	gasLimit -= gasToUse

	contractCallInput := &vmcommon.ContractCallInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:     sender,
			Arguments:      arguments,
			CallValue:      big.NewInt(0).SetBytes(asyncCall.GetValueBytes()),
			CallType:       vmcommon.AsynchronousCall,
			GasPrice:       runtime.GetVMInput().GasPrice,
			GasProvided:    gasLimit,
			CurrentTxHash:  runtime.GetCurrentTxHash(),
			OriginalTxHash: runtime.GetOriginalTxHash(),
			PrevTxHash:     runtime.GetPrevTxHash(),
		},
		RecipientAddr: asyncCall.GetDestination(),
		Function:      function,
	}

	return contractCallInput, nil
}

func (context *asyncContext) createSyncCallbackInput(
	asyncCall *arwen.AsyncCall,
	vmOutput *vmcommon.VMOutput,
	destinationErr error,
) (*vmcommon.ContractCallInput, error) {
	metering := context.host.Metering()
	runtime := context.host.Runtime()

	// always provide return code as the first argument to callback function
	retCodeBytes := big.NewInt(int64(vmOutput.ReturnCode)).Bytes()
	if len(retCodeBytes) == 0 {
		retCodeBytes = []byte{0}
	}

	arguments := [][]byte{retCodeBytes}
	if destinationErr == nil {
		// when execution went Ok, callBack arguments are:
		// [0, result1, result2, ....]
		arguments = append(arguments, vmOutput.ReturnData...)
	} else {
		// when execution returned error, callBack arguments are:
		// [error code, error message]
		arguments = append(arguments, []byte(vmOutput.ReturnMessage))
	}

	callbackFunction := asyncCall.GetCallbackName()

	gasLimit := vmOutput.GasRemaining + asyncCall.GetGasLocked()
	dataLength := computeDataLengthFromArguments(callbackFunction, arguments)

	gasToUse := metering.GasSchedule().ElrondAPICost.AsyncCallStep
	gasToUse += metering.GasSchedule().BaseOperationCost.DataCopyPerByte * uint64(dataLength)
	if gasLimit <= gasToUse {
		return nil, arwen.ErrNotEnoughGas
	}
	gasLimit -= gasToUse

	// Return to the sender SC, calling its specified callback method.
	contractCallInput := &vmcommon.ContractCallInput{
		VMInput: vmcommon.VMInput{
			CallerAddr:     asyncCall.Destination,
			Arguments:      arguments,
			CallValue:      big.NewInt(0),
			CallType:       vmcommon.AsynchronousCallBack,
			GasPrice:       runtime.GetVMInput().GasPrice,
			GasProvided:    gasLimit,
			CurrentTxHash:  runtime.GetCurrentTxHash(),
			OriginalTxHash: runtime.GetOriginalTxHash(),
			PrevTxHash:     runtime.GetPrevTxHash(),
		},
		RecipientAddr: runtime.GetSCAddress(),
		Function:      callbackFunction,
	}

	return contractCallInput, nil
}

func (context *asyncContext) finishSyncExecution(vmOutput *vmcommon.VMOutput, err error) {
	if err == nil {
		return
	}

	runtime := context.host.Runtime()
	output := context.host.Output()

	runtime.GetVMInput().GasProvided = 0

	if vmOutput == nil {
		vmOutput = output.CreateVMOutputInCaseOfError(err)
	}

	output.SetReturnMessage(vmOutput.ReturnMessage)

	output.Finish([]byte(vmOutput.ReturnCode.String()))
	output.Finish(runtime.GetCurrentTxHash())
}

func (context *asyncContext) HasPendingCallGroups() bool {
	return len(context.AsyncCallGroups) > 0
}

func (context *asyncContext) IsComplete() bool {
	return len(context.AsyncCallGroups) == 0
}

func (context *asyncContext) Save() error {
	if len(context.AsyncCallGroups) == 0 {
		return nil
	}

	storage := context.host.Storage()
	runtime := context.host.Runtime()

	storageKey := arwen.CustomStorageKey(arwen.AsyncDataPrefix, runtime.GetPrevTxHash())
	data, err := context.serialize()
	if err != nil {
		return err
	}

	_, err = storage.SetStorage(storageKey, data)
	if err != nil {
		return err
	}

	return nil
}

func (context *asyncContext) Load() error {
	runtime := context.host.Runtime()
	storage := context.host.Storage()

	storageKey := arwen.CustomStorageKey(arwen.AsyncDataPrefix, runtime.GetPrevTxHash())
	data := storage.GetStorage(storageKey)
	if len(data) == 0 {
		return arwen.ErrNoStoredAsyncContextFound
	}

	loadedContext, err := context.deserialize(data)
	if err != nil {
		return err
	}

	context.CallerAddr = loadedContext.CallerAddr
	context.ReturnData = loadedContext.ReturnData
	context.AsyncCallGroups = loadedContext.AsyncCallGroups

	return nil
}

func (context *asyncContext) Delete() error {
	runtime := context.host.Runtime()
	storage := context.host.Storage()

	storageKey := arwen.CustomStorageKey(arwen.AsyncDataPrefix, runtime.GetPrevTxHash())
	_, err := storage.SetStorage(storageKey, nil)
	return err
}

func (context *asyncContext) DetermineExecutionMode(destination []byte, data []byte) (arwen.AsyncCallExecutionMode, error) {
	runtime := context.host.Runtime()
	blockchain := context.host.Blockchain()

	// If ArgParser cannot read the Data field, then this is neither a SC call,
	// nor a built-in function call.
	functionName, _, err := context.host.CallArgsParser().ParseData(string(data))
	if err != nil {
		return arwen.AsyncUnknown, err
	}

	shardOfSC := blockchain.GetShardOfAddress(runtime.GetSCAddress())
	shardOfDest := blockchain.GetShardOfAddress(destination)
	sameShard := shardOfSC == shardOfDest

	if sameShard {
		return arwen.SyncExecution, nil
	}

	if context.host.IsBuiltinFunctionName(functionName) {
		return arwen.AsyncBuiltinFunc, nil
	}

	return arwen.AsyncUnknown, nil
}

func (context *asyncContext) setupAsyncCallsGas() error {
	gasLeft := context.host.Metering().GasLeft()
	gasNeeded := uint64(0)
	callsWithZeroGas := uint64(0)

	for _, group := range context.AsyncCallGroups {
		for _, asyncCall := range group.AsyncCalls {
			var err error
			gasNeeded, err = extramath.AddUint64(gasNeeded, asyncCall.ProvidedGas)
			if err != nil {
				return err
			}

			if gasNeeded > gasLeft {
				return arwen.ErrNotEnoughGas
			}

			if asyncCall.ProvidedGas == 0 {
				callsWithZeroGas++
				continue
			}

			asyncCall.GasLimit = asyncCall.ProvidedGas
		}
	}

	if callsWithZeroGas == 0 {
		return nil
	}

	if gasLeft <= gasNeeded {
		return arwen.ErrNotEnoughGas
	}

	gasShare := (gasLeft - gasNeeded) / callsWithZeroGas
	for _, group := range context.AsyncCallGroups {
		for _, asyncCall := range group.AsyncCalls {
			if asyncCall.ProvidedGas == 0 {
				asyncCall.GasLimit = gasShare
			}
		}
	}

	return nil
}

func (context *asyncContext) serialize() ([]byte, error) {
	serializableContext := &asyncContext{
		host:            nil,
		stateStack:      nil,
		CallerAddr:      context.CallerAddr,
		ReturnData:      context.ReturnData,
		AsyncCallGroups: context.AsyncCallGroups,
	}
	return json.Marshal(serializableContext)
}

func (context *asyncContext) deserialize(data []byte) (*asyncContext, error) {
	deserializedContext := &asyncContext{}
	err := json.Unmarshal(data, deserializedContext)
	if err != nil {
		return nil, err
	}

	return deserializedContext, nil
}

func (context *asyncContext) findGroupByID(groupID string) (int, bool) {
	for index, group := range context.AsyncCallGroups {
		if group.Identifier == groupID {
			return index, true
		}
	}
	return -1, false
}

func computeDataLengthFromArguments(function string, arguments [][]byte) int {
	// Calculate what length would the Data field have, were it of the
	// form "callback@arg1@arg4...

	// TODO this needs tests, especially for the case when the arguments slice
	// contains an empty []byte
	numSeparators := len(arguments)
	dataLength := len(function) + numSeparators
	for _, element := range arguments {
		dataLength += len(element)
	}

	return dataLength
}
