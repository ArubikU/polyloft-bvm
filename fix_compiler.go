//go:build ignore

package main

import (
	"log"
	"os"
	"strings"
)

func main() {
	path := `internal\compiler\compiler.go`
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	content := string(data)

	// Use \r\n for Windows line endings if they exist, or just \n.
	// Actually I'll use a more flexible replacement.

	fix := func(target, replacement string) {
		if strings.Contains(content, target) {
			content = strings.Replace(content, target, replacement, 1)
		} else {
			// Try with \r\n
			targetRN := strings.ReplaceAll(target, "\n", "\r\n")
			replacementRN := strings.ReplaceAll(replacement, "\n", "\r\n")
			content = strings.Replace(content, targetRN, replacementRN, 1)
		}
	}

	// 1. VariableStmt OpDefineGlobal (depth 0)
	fix("c.emit(bytecode.OpDefineGlobal, node.Name.Line)\n\t\t\tc.emitUint16(name, node.Name.Line)\n\t\t\treturn nil",
		"c.emit(bytecode.OpDefineGlobal, node.Name.Line)\n\t\t\tc.emitUint16(name, node.Name.Line)\n\t\t\tc.emit(bytecode.OpPop, node.Name.Line)\n\t\t\treturn nil")

	// 2. VariableStmt OpSetLocal (function local)
	fix("c.emit(bytecode.OpSetLocal, node.Name.Line)\n\t\tc.emitByte(slot, node.Name.Line)\n\t\treturn nil",
		"c.emit(bytecode.OpSetLocal, node.Name.Line)\n\t\tc.emitByte(slot, node.Name.Line)\n\t\tc.emit(bytecode.OpPop, node.Name.Line)\n\t\treturn nil")

	// 3. AssignStmt OpSetGlobalSlot
	fix("c.emit(bytecode.OpSetGlobalSlot, node.Name.Line)\n\t\t\tc.emitByte(slot, node.Name.Line)\n\t\t\treturn nil",
		"c.emit(bytecode.OpSetGlobalSlot, node.Name.Line)\n\t\t\tc.emitByte(slot, node.Name.Line)\n\t\t\tc.emit(bytecode.OpPop, node.Name.Line)\n\t\t\treturn nil")

	// 4. AssignStmt OpSetGlobal
	fix("c.emit(bytecode.OpSetGlobal, node.Name.Line)\n\t\tc.emitUint16(name, node.Name.Line)\n\t\treturn nil",
		"c.emit(bytecode.OpSetGlobal, node.Name.Line)\n\t\tc.emitUint16(name, node.Name.Line)\n\t\tc.emit(bytecode.OpPop, node.Name.Line)\n\t\treturn nil")

	err = os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		log.Fatal(err)
	}
}
