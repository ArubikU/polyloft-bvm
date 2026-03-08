package main

import (
	"fmt"
	"os"

	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/modules"
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
	program, registry, err := modules.Prepare(path, os.Stdout)
	if err != nil {
		fatal(err)
	}
	if registry == nil {
		registry = bvmruntime.NewRegistry()
		bvmruntime.InstallCoreGlobals(registry, os.Stdout)
	}
	if err := sema.Check(program, registry); err != nil {
		fatal(err)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
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
