//go:build !windows

package jit

import "github.com/ArubikU/polyloft-bvm/internal/value"

type posixBackend struct{}

func selectBackend() backend {
	return posixBackend{}
}

func (posixBackend) name() string {
	return "posix-stackabi-v1"
}

func (posixBackend) encode(fnName string, arity int, localCount int, usesReceiver bool, maxStack int, constants []value.Value, metadata []any, ir []irInstruction) *Program {
	ops, _ := opcodeSetForBackend("posix-stackabi-v1")
	return buildProgram(fnName, "posix-stackabi-v1", ops, arity, localCount, usesReceiver, maxStack, constants, metadata, ir)
}

func (posixBackend) instructions() []InstructionSpec {
	catalog, _ := InstructionCatalogForBackend("posix-stackabi-v1")
	return catalog
}
