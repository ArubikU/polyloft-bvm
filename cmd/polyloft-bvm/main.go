package main

import (
	"fmt"
	"os"

	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/lexer"
	"github.com/ArubikU/polyloft-bvm/internal/parser"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	command := os.Args[1]
	path := os.Args[2]
	source, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}

	tokens, err := lexer.Scan(string(source))
	if err != nil {
		fatal(err)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		fatal(err)
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, os.Stdout)
	if err := sema.Check(program, registry); err != nil {
		fatal(err)
	}
	fn, err := compiler.Compile(program)
	if err != nil {
		fatal(err)
	}

	switch command {
	case "run":
		machine := vm.NewWithRegistry(os.Stdout, registry)
		if _, err := machine.Run(fn); err != nil {
			fatal(err)
		}
	case "dump":
		fmt.Print(fn.Chunk.Disassemble(fn.Name))
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Usage: polyloft-bvm <run|dump> <file>")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
