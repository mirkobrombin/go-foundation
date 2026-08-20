package plugin

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

type wasmFunction struct {
	name       string
	parameters []api.ValueType
	results    []api.ValueType
}

var wasmFunctions = []wasmFunction{
	{name: wasmExportVersion, results: []api.ValueType{api.ValueTypeI64}},
	{name: wasmExportAllocate, parameters: []api.ValueType{api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI32}},
	{name: wasmExportFree, parameters: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}},
	{name: wasmExportMetadata, results: []api.ValueType{api.ValueTypeI64}},
	{name: wasmExportStart, results: []api.ValueType{api.ValueTypeI32}},
	{name: wasmExportStop, results: []api.ValueType{api.ValueTypeI32}},
	{name: wasmExportCall, parameters: []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, results: []api.ValueType{api.ValueTypeI64}},
	{name: wasmExportLastError, results: []api.ValueType{api.ValueTypeI64}},
}

var wasmHostFunctions = map[string]struct{}{
	wasmImportHostCall:   {},
	wasmImportResultLen:  {},
	wasmImportResultRead: {},
	wasmImportErrorLen:   {},
	wasmImportErrorRead:  {},
}

func (plugin *WasmPlugin) validateContract() error {
	for _, imported := range plugin.compiled.ImportedFunctions() {
		module, name, ok := imported.Import()
		if !ok {
			continue
		}
		if module == wasmHostModule {
			if _, allowed := wasmHostFunctions[name]; allowed {
				continue
			}
			return fmt.Errorf("%w: unknown host import %s.%s", ErrWasmInvalidModule, module, name)
		}
		if plugin.config.wasi && module == wasiModuleName {
			continue
		}
		return fmt.Errorf("%w: import %s.%s is not allowed", ErrWasmInvalidModule, module, name)
	}
	if len(plugin.compiled.ImportedMemories()) > 0 {
		return fmt.Errorf("%w: imported memory is not allowed", ErrWasmInvalidModule)
	}
	if _, ok := plugin.compiled.ExportedMemories()["memory"]; !ok {
		return fmt.Errorf("%w: memory export is missing", ErrWasmInvalidModule)
	}
	for _, signature := range wasmFunctions {
		function, ok := plugin.compiled.ExportedFunctions()[signature.name]
		if !ok {
			return fmt.Errorf("%w: export %s is missing", ErrWasmInvalidModule, signature.name)
		}
		if !equalValueTypes(function.ParamTypes(), signature.parameters) || !equalValueTypes(function.ResultTypes(), signature.results) {
			return fmt.Errorf("%w: export %s has the wrong signature", ErrWasmInvalidModule, signature.name)
		}
	}
	return nil
}

const wasiModuleName = "wasi_snapshot_preview1"

func equalValueTypes(left, right []api.ValueType) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (plugin *WasmPlugin) instantiateHost(ctx context.Context) error {
	builder := plugin.runtime.NewHostModuleBuilder(wasmHostModule)
	builder.NewFunctionBuilder().WithFunc(plugin.hostCall).Export(wasmImportHostCall)
	builder.NewFunctionBuilder().WithFunc(plugin.hostResponseLen).Export(wasmImportResultLen)
	builder.NewFunctionBuilder().WithFunc(plugin.hostResponseRead).Export(wasmImportResultRead)
	builder.NewFunctionBuilder().WithFunc(plugin.hostErrorLen).Export(wasmImportErrorLen)
	builder.NewFunctionBuilder().WithFunc(plugin.hostErrorRead).Export(wasmImportErrorRead)
	if _, err := builder.Instantiate(ctx); err != nil {
		return fmt.Errorf("%w: instantiate host ABI: %v", ErrWasmInvalidModule, err)
	}
	return nil
}

func (plugin *WasmPlugin) hostCall(ctx context.Context, module api.Module, capabilityPointer, capabilityLength, operationPointer, operationLength, inputPointer, inputLength uint32) uint32 {
	plugin.hostResponse = nil
	plugin.hostError = nil
	if capabilityLength > maxWasmNameSize || operationLength > maxWasmNameSize || inputLength > plugin.config.payloadLimit {
		plugin.setHostError("request exceeds its size limit")
		return uint32(WasmHostPayloadTooLarge)
	}
	capability, ok := readModuleBytes(module, capabilityPointer, capabilityLength)
	if !ok {
		plugin.setHostError("capability is outside guest memory")
		return uint32(WasmHostInvalidRequest)
	}
	operation, ok := readModuleBytes(module, operationPointer, operationLength)
	if !ok {
		plugin.setHostError("operation is outside guest memory")
		return uint32(WasmHostInvalidRequest)
	}
	input, ok := readModuleBytes(module, inputPointer, inputLength)
	if !ok {
		plugin.setHostError("input is outside guest memory")
		return uint32(WasmHostInvalidRequest)
	}
	capabilityName := string(capability)
	if validateWasmName(capabilityName) != nil || validateWasmName(string(operation)) != nil {
		plugin.setHostError("capability or operation name is invalid")
		return uint32(WasmHostInvalidRequest)
	}
	if _, declared := plugin.declared[capabilityName]; !declared {
		plugin.setHostError("capability was not declared by the plugin")
		return uint32(WasmHostDenied)
	}
	handler, granted := plugin.config.capabilities[capabilityName]
	if !granted {
		plugin.setHostError("capability was not granted by the host")
		return uint32(WasmHostDenied)
	}
	response, err := handler.Call(ctx, string(operation), input)
	if err != nil {
		plugin.setHostError(err.Error())
		return uint32(WasmHostHandlerError)
	}
	if len(response) > int(plugin.config.payloadLimit) {
		plugin.setHostError("response exceeds the payload limit")
		return uint32(WasmHostPayloadTooLarge)
	}
	plugin.hostResponse = append(plugin.hostResponse[:0], response...)
	return uint32(WasmHostOK)
}

func (plugin *WasmPlugin) hostResponseLen() uint32 {
	return uint32(len(plugin.hostResponse))
}

func (plugin *WasmPlugin) hostResponseRead(_ context.Context, module api.Module, pointer, capacity uint32) int32 {
	return writeModuleBytes(module, pointer, capacity, plugin.hostResponse)
}

func (plugin *WasmPlugin) hostErrorLen() uint32 {
	return uint32(len(plugin.hostError))
}

func (plugin *WasmPlugin) hostErrorRead(_ context.Context, module api.Module, pointer, capacity uint32) int32 {
	return writeModuleBytes(module, pointer, capacity, plugin.hostError)
}

func (plugin *WasmPlugin) setHostError(message string) {
	limit := int(plugin.config.payloadLimit)
	if len(message) > limit {
		message = message[:limit]
	}
	plugin.hostError = append(plugin.hostError[:0], message...)
}

func readModuleBytes(module api.Module, pointer, length uint32) ([]byte, bool) {
	if length == 0 {
		return nil, true
	}
	data, ok := module.Memory().Read(pointer, length)
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func writeModuleBytes(module api.Module, pointer, capacity uint32, data []byte) int32 {
	if uint32(len(data)) > capacity {
		return -int32(len(data))
	}
	if len(data) == 0 {
		return 0
	}
	if !module.Memory().Write(pointer, data) {
		return -1
	}
	return int32(len(data))
}
