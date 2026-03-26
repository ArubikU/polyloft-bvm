package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gioapp "gioui.org/app"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/diagnostic"
	"github.com/ArubikU/polyloft-bvm/internal/modules"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

type runOptions struct {
	jitThreshold *int
	jitLog       bool
}


func main() {
	go func() {
		defer os.Exit(0)
		mainImpl()
	}()
	gioapp.Main()
}

func mainImpl() {
	if len(os.Args) >= 2 && os.Args[1] == "types" {
		fatal(typesTarget(os.Args[2:]))
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "runline" {
		fatal(runlineTarget(os.Args[2:]))
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "check" {
		fatal(checkTarget(os.Args[2:]))
		return
	}
	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	command := os.Args[1]
	if command == "run" {
		path, opts, err := parseRunArgs(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fatal(runTarget(path, opts))
		return
	}
	var path string
	var outFile string

	if (command == "dump" || command == "compile") && len(os.Args) >= 4 && os.Args[2] == "-o" {
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "missing output file after -o")
			os.Exit(1)
		}
		outFile = os.Args[3]
		path = os.Args[4]
	} else {
		path = os.Args[2]
	}

	switch command {
	case "check":
		fatal(checkPathTarget(path))
	case "dump":
		fatal(dumpTarget(path, outFile))
	case "compile":
		fatal(compileTarget(path, outFile))
	default:
		usage()
		os.Exit(1)
	}
}

func runTarget(path string, opts runOptions) error {
	if strings.HasSuffix(path, ".pfbc") {
		fn, err := modules.LoadCompiledModule(path)
		if err != nil {
			return err
		}
		registry := bvmruntime.NewRegistry()
		bvmruntime.InstallCoreGlobals(registry, os.Stdout)
		machine := vm.NewWithRegistry(os.Stdout, registry)
		applyRunOptions(machine, fn, opts)
		_, err = machine.Run(fn)
		return err
	}
	if strings.HasSuffix(path, ".pfx") {
		fn, registry, err := modules.LoadBundle(path, os.Stdout)
		if err != nil {
			return err
		}
		machine := vm.NewWithRegistry(os.Stdout, registry)
		applyRunOptions(machine, fn, opts)
		_, err = machine.Run(fn)
		return err
	}
	fn, registry, err := modules.CompileSource(path, os.Stdout)
	if err != nil {
		return err
	}
	machine := vm.NewWithRegistry(os.Stdout, registry)
	applyRunOptions(machine, fn, opts)
	_, err = machine.Run(fn)
	return err
}

func applyRunOptions(machine *vm.VM, fn *bytecode.Function, opts runOptions) {
	if machine == nil {
		return
	}
	if !shouldEnableJITForFunction(fn, opts) {
		return
	}
	if opts.jitThreshold != nil {
		machine.SetJITWarmupThreshold(*opts.jitThreshold)
	}
	if opts.jitLog {
		machine.SetJITLogger(os.Stderr)
	}
}

func shouldEnableJITForFunction(fn *bytecode.Function, opts runOptions) bool {
	return fn != nil && opts.jitThreshold != nil
}

func parseRunArgs(args []string) (string, runOptions, error) {
	opts := runOptions{}
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--jit":
			threshold := 1
			if opts.jitThreshold == nil {
				opts.jitThreshold = &threshold
			}
		case "--jit-threshold":
			i++
			if i >= len(args) {
				return "", runOptions{}, fmt.Errorf("missing value after --jit-threshold")
			}
			threshold, err := strconv.Atoi(args[i])
			if err != nil || threshold < 1 {
				return "", runOptions{}, fmt.Errorf("invalid --jit-threshold %q", args[i])
			}
			opts.jitThreshold = &threshold
		case "--jit-log":
			opts.jitLog = true
		default:
			positionals = append(positionals, args[i])
		}
	}
	if len(positionals) != 1 {
		return "", runOptions{}, fmt.Errorf("usage: polyloft-bvm run [--jit] [--jit-threshold <n>] [--jit-log] <path>")
	}
	return positionals[0], opts, nil
}

func checkPathTarget(path string) error {
	return modules.CheckSource(path, io.Discard)
}

func runlineTarget(args []string) error {
	source, logicalPath, checkOnly, err := parseInlineArgs(args)
	if err != nil {
		return err
	}
	if checkOnly {
		return modules.CheckInlineSource(logicalPath, source, io.Discard)
	}
	fn, registry, err := modules.CompileInlineSource(logicalPath, source, os.Stdout)
	if err != nil {
		return err
	}
	machine := vm.NewWithRegistry(os.Stdout, registry)
	_, err = machine.Run(fn)
	return err
}

func checkTarget(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: polyloft-bvm check <path>")
	}
	return checkPathTarget(args[0])
}

func parseInlineArgs(args []string) (string, string, bool, error) {
	checkOnly := false
	readFromStdin := false
	logicalPath := ""
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			checkOnly = true
		case "--stdin":
			readFromStdin = true
		case "--path":
			i++
			if i >= len(args) {
				return "", "", false, fmt.Errorf("missing value after --path")
			}
			logicalPath = args[i]
		default:
			positionals = append(positionals, args[i])
		}
	}
	var source string
	if readFromStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", false, err
		}
		source = string(data)
	} else {
		if len(positionals) != 1 {
			return "", "", false, fmt.Errorf("usage: polyloft-bvm runline [--check] [--stdin] [--path <logical-path>] <inline-script>")
		}
		source = positionals[0]
	}
	if strings.TrimSpace(source) == "" {
		return "", "", false, fmt.Errorf("inline source is empty")
	}
	return source, logicalPath, checkOnly, nil
}

func dumpTarget(path string, outFile string) error {
	var disassembly string
	if strings.HasSuffix(path, ".pfbc") {
		fn, err := modules.LoadCompiledModule(path)
		if err != nil {
			return err
		}
		disassembly = fn.Chunk.Disassemble(fn.Name)
	} else if strings.HasSuffix(path, ".pfx") {
		fn, _, err := modules.LoadBundle(path, os.Stdout)
		if err != nil {
			return err
		}
		disassembly = fn.Chunk.Disassemble(fn.Name)
	} else {
		fn, _, err := modules.CompileSource(path, os.Stdout)
		if err != nil {
			return err
		}
		disassembly = fn.Chunk.Disassemble(fn.Name)
	}
	if outFile == "" {
		fmt.Print(disassembly)
		return nil
	}
	return os.WriteFile(outFile, []byte(disassembly), 0o644)
}

func compileTarget(path string, outFile string) error {
	target, err := defaultCompileTarget(path, outFile)
	if err != nil {
		return err
	}
	f, err := os.Create(target)
	if err != nil {
		return err
	}
	defer f.Close()
	if isProjectInput(path) {
		return modules.BuildProjectBundle(path, os.Stdout, f)
	}
	return modules.WriteCompiledModule(path, os.Stdout, f)
}

func defaultCompileTarget(path string, outFile string) (string, error) {
	if outFile != "" {
		return outFile, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if isProjectInput(path) {
		if strings.EqualFold(filepath.Base(absPath), "polyloft.toml") {
			absPath = filepath.Dir(absPath)
		}
		return absPath + ".pfx", nil
	}
	if strings.HasSuffix(absPath, ".pf") {
		return strings.TrimSuffix(absPath, ".pf") + ".pfbc", nil
	}
	return absPath + ".pfbc", nil
}

func isProjectInput(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err := os.Stat(filepath.Join(path, "polyloft.toml"))
		return err == nil
	}
	return strings.EqualFold(filepath.Base(path), "polyloft.toml")
}

func usage() {
	fmt.Println("Usage: polyloft-bvm <run|check|dump|compile> [options] <path>")
	fmt.Println("       polyloft-bvm runline [--check] [--stdin] [--path <logical-path>] <inline-script>")
	fmt.Println("       polyloft-bvm types <primitives|runtime|stdlib> [-o <file>]")
	fmt.Println("  run             execute source (.pf), compiled module (.pfbc), bundle (.pfx), or project")
	fmt.Println("  run --jit       force JIT hotness threshold to 1 for immediate compilation attempts")
	fmt.Println("  run --jit-threshold <n>  set the JIT hotness threshold explicitly")
	fmt.Println("  run --jit-log   write JIT compilation/execution logs to stderr")
	fmt.Println("  check           parse and type-check a source (.pf), bundle entry, or project without running it")
	fmt.Println("  runline         execute an inline Polyloft script")
	fmt.Println("  runline --check parse and type-check an inline script without running it")
	fmt.Println("  runline --stdin read the inline script from stdin")
	fmt.Println("  runline --path  provide a logical file path for diagnostics and import resolution")
	fmt.Println("  dump            print disassembly from source or compiled artifact")
	fmt.Println("  dump -o <file>  write disassembly text to a file")
	fmt.Println("  compile         write a compiled module (.pfbc) or project bundle (.pfx)")
	fmt.Println("  compile -o <file>   override output path")
	fmt.Println("  types primitives    write the primitive types manifest")
	fmt.Println("  types runtime       write the runtime/native manifest")
	fmt.Println("  types stdlib        write the stdlib types manifest")
}

func typesTarget(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing types target")
	}
	target := args[0]
	outFile := ""
	if len(args) == 3 && args[1] == "-o" {
		outFile = args[2]
	} else if len(args) != 1 {
		return fmt.Errorf("usage: polyloft-bvm types <primitives|runtime|stdlib> [-o <file>]")
	}
	writer, closeFn, err := typesOutputWriter(outFile)
	if err != nil {
		return err
	}
	defer closeFn()
	switch target {
	case "primitives":
		return modules.WritePrimitiveTypesManifest(writer)
	case "runtime":
		return modules.WriteRuntimeTypesManifest(writer)
	case "stdlib":
		return modules.WriteStdlibTypesManifest(writer)
	default:
		return fmt.Errorf("unknown types target %q", target)
	}
}

func typesOutputWriter(path string) (io.Writer, func(), error) {
	if path == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, diagnostic.Format(err))
	os.Exit(1)
}
