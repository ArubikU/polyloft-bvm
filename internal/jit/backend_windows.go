//go:build windows

package jit

import "github.com/ArubikU/polyloft-bvm/internal/value"

type windowsBackend struct{}

func selectBackend() backend {
	return windowsBackend{}
}

func (windowsBackend) name() string {
	return "windows-stackabi-v1"
}

func (windowsBackend) encode(fnName string, arity int, localCount int, usesReceiver bool, maxStack int, constants []value.Value, metadata []any, ir []irInstruction) *Program {
	ops, _ := opcodeSetForBackend("windows-stackabi-v1")
	return buildProgram(fnName, "windows-stackabi-v1", ops, arity, localCount, usesReceiver, maxStack, constants, metadata, ir)
}

func (windowsBackend) instructions() []InstructionSpec {
	catalog, _ := InstructionCatalogForBackend("windows-stackabi-v1")
	return catalog
}
