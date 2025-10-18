package asruntime

import (
	"context"
	"time"
	"unicode/utf16"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"go.uber.org/zap"
)

const (
	i32 = api.ValueTypeI32
	i64 = api.ValueTypeI64
	f64 = api.ValueTypeF64
)

// simplified from
// https://github.com/wazero/wazero/blob/996b80304486a71e83ab67c7ba6f9ebccc8f3af0/imports/assemblyscript/assemblyscript.go#L1

func AssemblyscriptExports(envBuilder wazero.HostModuleBuilder, pluginID string, traceEnabled bool) {
	_abort := func(_ context.Context, mod api.Module, stack []uint64) {
		mem := mod.Memory()

		message := uint32(stack[0])
		fileName := uint32(stack[1])
		lineNumber := uint32(stack[2])
		columnNumber := uint32(stack[3])

		// Don't panic if there was a problem reading the message
		if msg, msgOk := readAssemblyScriptString(mem, message); msgOk {
			if fn, fnOk := readAssemblyScriptString(mem, fileName); fnOk {
				zap.L().WithOptions(zap.WithCaller(false)).Warn(msg, zap.String("pluginID", pluginID), zap.String("function", fn), zap.Uint32("linenumber", lineNumber), zap.Uint32("columnnumber", columnNumber))
			}
		}

	}
	_abortParams := []api.ValueType{i32, i32, i32, i32}
	_abortParamNames := []string{"message", "fileName", "lineNumber", "columnNumber"}

	envBuilder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(_abort), _abortParams, []api.ValueType{}).
		WithParameterNames(_abortParamNames...).
		Export("abort")

	_trace := func(_ context.Context, mod api.Module, params []uint64) {
		if !traceEnabled {
			return
		}
		message := uint32(params[0])

		msg, ok := readAssemblyScriptString(mod.Memory(), message)
		if !ok {
			return // don't panic if unable to trace
		}
		zap.L().Named("pluginas").WithOptions(zap.WithCaller(false)).Info(msg, zap.String("pluginID", pluginID))

	}
	_traceParams := []api.ValueType{i32, i32, f64, f64, f64, f64, f64}
	_traceParamNames := []string{"message", "nArgs", "arg0", "arg1", "arg2", "arg3", "arg4"}

	envBuilder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(_trace), _traceParams, []api.ValueType{}).
		WithParameterNames(_traceParamNames...).
		Export("trace")

	_now := func(_ context.Context, mod api.Module, stack []uint64) {
		n := time.Now()
		t := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
		stack[0] = uint64(n.UnixNano() - t.UnixNano())
	}
	_nowParams := []api.ValueType{}
	_nowParamNames := []string{}
	envBuilder.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(_now), _nowParams, []api.ValueType{i64}).
		WithParameterNames(_nowParamNames...).
		Export("nanosecond")

}

// readAssemblyScriptString reads a UTF-16 string created by AssemblyScript.
func readAssemblyScriptString(mem api.Memory, offset uint32) (string, bool) {
	// Length is four bytes before pointer.
	byteCount, ok := mem.ReadUint32Le(offset - 4)
	if !ok || byteCount%2 != 0 {
		return "", false
	}
	buf, ok := mem.Read(offset, byteCount)
	if !ok {
		return "", false
	}
	return decodeUTF16(buf), true
}

func decodeUTF16(b []byte) string {
	u16s := make([]uint16, len(b)/2)

	lb := len(b)
	for i := 0; i < lb; i += 2 {
		u16s[i/2] = uint16(b[i]) + (uint16(b[i+1]) << 8)
	}

	return string(utf16.Decode(u16s))
}
