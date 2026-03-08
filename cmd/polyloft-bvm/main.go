package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/modules"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/value"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
	"github.com/BurntSushi/toml"
)

type PfDependency struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
	Include *bool  `toml:"include"`
}

type Config struct {
	Project struct {
		Name       string `toml:"name"`
		Version    string `toml:"version"`
		EntryPoint string `toml:"entry_point"`
	} `toml:"project"`
	Dependencies struct {
		Pf []PfDependency `toml:"pf"`
	} `toml:"dependencies"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "run":
		runCommand(os.Args[2:])
	case "dump":
		dumpCommand(os.Args[2:])
	case "compile":
		compileCommand(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func runCommand(args []string) {
	runCmd := flag.NewFlagSet("run", flag.ContinueOnError)
	if err := runCmd.Parse(args); err != nil {
		os.Exit(1)
	}

	var path string
	if runCmd.NArg() < 1 {
		path = "."
	} else {
		path = runCmd.Arg(0)
	}

	var fn *bytecode.Function
	var registry *bvmruntime.Registry
	var err error

	// Detect if it's a .pfx ZIP bundle
	if strings.HasSuffix(path, ".pfx") {
		fn, registry, err = runFromBundle(path)
	} else {
		// Existing logic for files/directories
		if info, err := os.Stat(path); err == nil && (info.IsDir() || strings.HasSuffix(path, ".toml")) {
			configPath := path
			if info.IsDir() {
				configPath = filepath.Join(path, "polyloft.toml")
			}
			cfg, err := loadConfig(configPath)
			if err != nil {
				fatal(err)
			}
			path = filepath.Join(filepath.Dir(configPath), cfg.Project.EntryPoint)
		}
		fn, registry, err = prepareAndCompile(path)
	}

	if err != nil {
		fatal(err)
	}

	machine := vm.NewWithRegistry(os.Stdout, registry)
	if _, err := machine.Run(fn); err != nil {
		fatal(err)
	}
}

func runFromBundle(path string) (*bytecode.Function, *bvmruntime.Registry, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()

	var tomlContent []byte

	for _, f := range r.File {
		if f.Name == "polyloft.toml" {
			rc, err := f.Open()
			if err != nil {
				return nil, nil, err
			}
			tomlContent, _ = io.ReadAll(rc)
			rc.Close()
		}
	}

	if tomlContent == nil {
		return nil, nil, fmt.Errorf("invalid .pfx bundle: missing polyloft.toml")
	}

	var cfg Config
	if _, err := toml.Decode(string(tomlContent), &cfg); err != nil {
		return nil, nil, fmt.Errorf("failed to parse polyloft.toml in bundle: %v", err)
	}

	loader := &modules.Loader{
		Stdout:       os.Stdout,
		WorkspaceDir: ".",
		Cache:        make(map[string]*modules.LoadedModule),
		Loading:      make(map[string]bool),
		Archive:      r.File, // Pass the zip archive files directly to the loader
	}

	entryPoint := cfg.Project.EntryPoint
	fn, registry, err := loader.PrepareFromBundle(entryPoint)
	if err != nil {
		return nil, nil, err
	}
	return fn, registry, nil
}

func dumpCommand(args []string) {
	dumpCmd := flag.NewFlagSet("dump", flag.ContinueOnError)
	outFile := dumpCmd.String("o", "", "output file (.pfbc)")
	disassemble := dumpCmd.Bool("d", false, "disassemble to text")
	if err := dumpCmd.Parse(args); err != nil {
		os.Exit(1)
	}

	if dumpCmd.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "missing file to dump")
		os.Exit(1)
	}
	path := dumpCmd.Arg(0)

	fn, registry, err := prepareAndCompile(path)
	if err != nil {
		fatal(err)
	}

	if *outFile != "" {
		// If disassemble is requested OR if the user didn't specify .pfbc,
		// or if they explicitly asked for text, we write text.
		// However, the rule we established is that .pfbc is binary.
		if *disassemble {
			dumpContent := fn.Chunk.Disassemble(fn.Name)
			if err := os.WriteFile(*outFile, []byte(dumpContent), 0644); err != nil {
				fatal(err)
			}
		} else {
			// Binary dump
			if !strings.HasSuffix(*outFile, ".pfbc") {
				*outFile += ".pfbc"
			}
			f, err := os.Create(*outFile)
			if err != nil {
				fatal(err)
			}
			defer f.Close()
			codec := value.NewBinaryCodec()
			if err := fn.WriteBinary(f, codec); err != nil {
				fatal(err)
			}
		}
	} else {
		_ = registry // registry is used in prepareAndCompile
		fmt.Print(fn.Chunk.Disassemble(fn.Name))
	}
}

func compileCommand(args []string) {
	compileCmd := flag.NewFlagSet("compile", flag.ContinueOnError)
	outFile := compileCmd.String("o", "", "output file (.pfx)")
	if err := compileCmd.Parse(args); err != nil {
		os.Exit(1)
	}

	var path string
	if compileCmd.NArg() < 1 {
		path = "."
	} else {
		path = compileCmd.Arg(0)
	}

	info, err := os.Stat(path)
	if err != nil {
		fatal(err)
	}

	configPath := path
	if info.IsDir() {
		configPath = filepath.Join(path, "polyloft.toml")
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "compile command requires a polyloft.toml file. Not found at: %s\n", configPath)
		os.Exit(1)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fatal(err)
	}

	entryPoint := filepath.Join(filepath.Dir(configPath), cfg.Project.EntryPoint)

	// Use a shared loader to capture all compiled modules in cache
	workspaceDir := filepath.Dir(configPath)
	loader := &modules.Loader{
		Stdout:       os.Stdout,
		WorkspaceDir: workspaceDir,
		Cache:        make(map[string]*modules.LoadedModule),
		Loading:      make(map[string]bool),
	}

	program, registry, err := loader.Prepare(entryPoint)
	if err != nil {
		fatal(err)
	}
	if err := sema.Check(program, registry); err != nil {
		fatal(err)
	}
	mainFn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		fatal(err)
	}

	target := *outFile
	if target == "" {
		target = cfg.Project.Name + ".pfx"
		if target == ".pfx" {
			target = "out.pfx"
		}
	}

	// Create ZIP bundle
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	// Add polyloft.toml
	tf, err := zw.Create("polyloft.toml")
	if err != nil {
		fatal(err)
	}
	confData, _ := os.ReadFile(configPath)
	tf.Write(confData)

	// Helper to get relative path for zip entry names
	absConfigDir, _ := filepath.Abs(filepath.Dir(configPath))
	rel := func(p string) string {
		absP, _ := filepath.Abs(p)
		r, err := filepath.Rel(absConfigDir, absP)
		if err != nil {
			return p // fallback
		}
		return strings.ReplaceAll(r, "\\", "/")
	}

	// Collect exports for the main module
	mainExports, _ := loader.CollectExports(program, nil, mainFn, registry) // machine nil here but it's okay for static exports

	// Add entry point module
	if err := saveModuleToZip(zw, rel(entryPoint), mainFn, mainExports); err != nil {
		fatal(err)
	}

	// Helper to check if a module belongs to an excluded dependency
	isExcluded := func(modPath string) bool {
		if modules.IsStdlibModulePath(modPath) {
			for _, dep := range cfg.Dependencies.Pf {
				if dep.Name == "stdlib" {
					if dep.Include != nil {
						return !*dep.Include
					}
					return true // default false for stdlib if not explicitly included
				}
			}
			return true // default false for stdlib if omitted from toml
		}

		for _, dep := range cfg.Dependencies.Pf {
			if dep.Name == "stdlib" {
				continue
			}
			if dep.Include != nil && !*dep.Include {
				parts := strings.Split(filepath.ToSlash(modPath), "/")
				for _, p := range parts {
					if p == dep.Name {
						return true
					}
				}
			}
		}
		return false
	}

	// Add all imported modules from cache
	for _, mod := range loader.Cache {
		if isExcluded(mod.Path) {
			continue
		}
		entryName := rel(mod.Path)
		if modules.IsStdlibModulePath(mod.Path) {
			entryName = modules.TrimStdlibModulePath(mod.Path)
		}
		if err := saveModuleToZip(zw, entryName, mod.Function, mod.Exports); err != nil {
			fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		fatal(err)
	}

	if err := os.WriteFile(target, buf.Bytes(), 0644); err != nil {
		fatal(err)
	}

	fmt.Printf("Compiled and bundled to %s\n", target)
}

// saveModuleToZip writes the compiled function and its metadata purely into the .pfbc file.
func saveModuleToZip(zw *zip.Writer, relPath string, fn *bytecode.Function, exports map[string]modules.ExportMetadata) error {
	basePath := strings.TrimSuffix(relPath, filepath.Ext(relPath))

	f, err := zw.Create(basePath + ".pfbc")
	if err != nil {
		return err
	}

	codec := value.NewBinaryCodec()
	if err := fn.WriteBinary(f, codec); err != nil {
		return err
	}

	if exports == nil {
		exports = make(map[string]modules.ExportMetadata)
	}

	// We only need the Spec and Visibility for bundle loading (evaluating ast.Visibility and type checking)
	// We CANNOT serialize Value, so we make sure it's nil
	exportMeta := make(map[string]modules.ExportMetadata)
	for k, meta := range exports {
		exportMeta[k] = modules.ExportMetadata{
			Spec:       flattenSpec(meta.Spec, 0),
			Visibility: meta.Visibility,
		}
	}

	// Wait, we need to defer f.Close() to close the file or else the zip writer won't flush the file.
	// Oh, I opened `f`, but `zw.Create` returns an io.Writer. We don't close it!

	exportData, err := json.Marshal(exportMeta)
	if err != nil {
		return err
	}

	if err := binary.Write(f, binary.LittleEndian, uint32(len(exportData))); err != nil {
		return err
	}

	if _, err := f.Write(exportData); err != nil {
		return err
	}

	return nil
}

func prepareAndCompile(path string) (*bytecode.Function, *bvmruntime.Registry, error) {
	program, registry, err := modules.Prepare(path, os.Stdout)
	if err != nil {
		return nil, nil, err
	}
	if registry == nil {
		registry = bvmruntime.NewRegistry()
		bvmruntime.InstallCoreGlobals(registry, os.Stdout)
	}
	if err := sema.Check(program, registry); err != nil {
		return nil, nil, err
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	return fn, registry, err
}

func loadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.Project.EntryPoint == "" {
		return nil, fmt.Errorf("project.entry_point is required in polyloft.toml")
	}
	return &cfg, nil
}

func usage() {
	fmt.Println("Usage: polyloft-bvm <run|dump|compile> [options] <file|project>")
	fmt.Println("\nCommands:")
	fmt.Println("  run <file.pf|project_dir|file.pfx>")
	fmt.Println("      Runs a single file, a project dir, or a compiled bundle")
	fmt.Println("  dump [-d] [-o <file.pfbc>] <file.pf>")
	fmt.Println("      Prints disassembly or saves binary bytecode (-o) to a file")
	fmt.Println("      Use -d with -o to save text disassembly instead of binary")
	fmt.Println("  compile [-o <file.pfx>] [project_dir]")
	fmt.Println("      Compiles a project to a .pfx bundle (requires polyloft.toml)")
}

func flattenSpec(s bvmruntime.Spec, depth int) bvmruntime.Spec {
	if depth > 2 {
		return bvmruntime.Spec{Name: s.Name, TypeName: s.TypeName, IsAbstract: s.IsAbstract, IsSealed: s.IsSealed, IsInterface: s.IsInterface, IsRecord: s.IsRecord}
	}
	res := bvmruntime.Spec{
		Name:                  s.Name,
		TypeName:              s.TypeName,
		TypeParams:            s.TypeParams,
		ConstructorVisibility: s.ConstructorVisibility,
		IsAbstract:            s.IsAbstract,
		IsSealed:              s.IsSealed,
		IsInterface:           s.IsInterface,
		IsRecord:              s.IsRecord,
		Permits:               s.Permits,
	}
	if s.Callable != nil {
		res.Callable = &bvmruntime.CallableSpec{
			Params:   s.Callable.Params,
			Return:   s.Callable.Return,
			Variadic: s.Callable.Variadic,
		}
	}
	if s.Members != nil {
		res.Members = make(map[string]bvmruntime.Spec, len(s.Members))
		for k, v := range s.Members {
			res.Members[k] = flattenSpec(v, depth+1)
		}
	}
	if s.InstanceMembers != nil {
		res.InstanceMembers = make(map[string]bvmruntime.Spec, len(s.InstanceMembers))
		for k, v := range s.InstanceMembers {
			res.InstanceMembers[k] = flattenSpec(v, depth+1)
		}
	}
	if s.Module != nil {
		res.Module = &bvmruntime.ModuleSpec{Name: s.Module.Name}
		if s.Module.Members != nil {
			res.Module.Members = make(map[string]bvmruntime.Spec, len(s.Module.Members))
			for k, v := range s.Module.Members {
				res.Module.Members[k] = flattenSpec(v, depth+1)
			}
		}
	}
	return res
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
