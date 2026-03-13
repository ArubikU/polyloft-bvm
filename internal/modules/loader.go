package modules

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/classfile"
	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/diagnostic"
	"github.com/ArubikU/polyloft-bvm/internal/lexer"
	"github.com/ArubikU/polyloft-bvm/internal/parser"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/token"
	"github.com/ArubikU/polyloft-bvm/internal/value"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

type ExportMetadata struct {
	Value          value.Value
	Spec           bvmruntime.Spec
	Visibility     ast.Visibility
	IsNative       bool
	TextInsertKind string
	TextInsert     string
}

type LoadedModule struct {
	Path     string
	Dir      string
	Source   string
	Function *bytecode.Function
	Exports  map[string]ExportMetadata
	Imports  []string
}

type Loader struct {
	Stdout       io.Writer
	WorkspaceDir string
	Cache        map[string]*LoadedModule
	Loading      map[string]bool
	Archive      []*zip.File
	ProjectMeta  map[string]map[string]ExportMetadata
}

func Prepare(path string, stdout io.Writer) (*ast.Program, *bvmruntime.Registry, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	workspaceDir, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	loader := &Loader{
		Stdout:       stdout,
		WorkspaceDir: workspaceDir,
		Cache:        make(map[string]*LoadedModule),
		Loading:      make(map[string]bool),
	}
	return loader.Prepare(absPath)
}

func (l *Loader) Prepare(absPath string) (*ast.Program, *bvmruntime.Registry, error) {
	program, _, err := l.parseFile(absPath)
	if err != nil {
		return nil, nil, err
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, l.Stdout)
	if err := l.loadImportsIntoRegistry(program, absPath, registry); err != nil {
		return nil, nil, err
	}
	return program, registry, nil
}

func (l *Loader) PrepareSource(absPath string, source string) (*ast.Program, *bvmruntime.Registry, error) {
	program, _, err := l.parseSource(absPath, source)
	if err != nil {
		return nil, nil, err
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, l.Stdout)
	if err := l.loadImportsIntoRegistry(program, absPath, registry); err != nil {
		return nil, nil, err
	}
	return program, registry, nil
}

func (l *Loader) PrepareFromBundle(entryPoint string) (*bytecode.Function, *bvmruntime.Registry, error) {
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, l.Stdout)

	// We'll use a single VM for all module initializations
	machine := vm.NewWithRegistry(l.Stdout, registry)

	// Collect all modules from archive to initialize them
	type modInfo struct {
		mod     *LoadedModule
		modPath string
		parts   []string
	}
	var toInit []modInfo

	// Normalize entryPoint for comparison
	canonicalEntry := strings.TrimSuffix(strings.ReplaceAll(entryPoint, "\\", "/"), ".pf")

	for _, f := range l.Archive {
		if strings.HasSuffix(f.Name, ".pfbc") {
			modPath := strings.TrimSuffix(f.Name, ".pfbc")

			// Skip stdlib modules
			if IsStdlibModulePath(modPath) {
				continue
			}

			mod, err := l.loadModule(modPath)
			if err != nil {
				continue
			}

			// Normalize modPath for logical namespace
			displayPath := strings.TrimPrefix(modPath, "/")
			displayPath = strings.TrimPrefix(displayPath, "src/")
			displayPath = strings.TrimPrefix(displayPath, "lib/")
			displayPath = strings.TrimPrefix(displayPath, "libs/")
			displayPath = strings.TrimPrefix(displayPath, "stdlib/")
			displayPath = strings.TrimSuffix(displayPath, "/index")

			rawParts := strings.Split(displayPath, "/")
			parts := make([]string, 0, len(rawParts))
			for _, p := range rawParts {
				if p != "" {
					parts = append(parts, p)
				}
			}
			if len(parts) == 0 {
				continue
			}

			toInit = append(toInit, modInfo{mod: mod, modPath: modPath, parts: parts})
		}
	}

	// 1. Initial binding of namespaces
	for _, info := range toInit {
		rootModule, rootSpec := l.buildNamespaceModule(info.parts, info.mod.Exports)
		rootName := info.parts[0]
		if _, ok := registry.Globals()[rootName]; !ok {
			registry.DefineWithSpec(rootName, value.ObjectValue(rootModule), *rootSpec)
		} else {
			mergeNamespace(registry, rootModule, rootSpec)
		}
	}

	// 2. Run all module functions to initialize classes/globals
	globals := registry.Globals()
	for _, info := range toInit {
		canonicalPath := strings.TrimSuffix(strings.ReplaceAll(info.modPath, "\\", "/"), ".pf")
		if canonicalPath == canonicalEntry {
			// Skip main module for now, it will be run by the caller
			continue
		}

		if _, err := machine.Run(info.mod.Function); err != nil {
			return nil, nil, fmt.Errorf("failed to initialize module %s: %v", info.modPath, err)
		}

		// 3. Persist initialized values.
		// When loaded from source, GlobalSlotNames contains all global names.
		// When loaded from binary, GlobalSlotNames may be empty, so we also
		// fall back to iterating Exports keys directly against machine.globals.
		persistedBySlot := make(map[string]bool)
		for _, name := range info.mod.Function.GlobalSlotNames {
			val, ok := machine.ResolveGlobal(info.mod.Function, name)
			if !ok {
				continue
			}
			if existing, exists := globals[name]; exists {
				if _, isMod := existing.AsModule(); isMod {
					continue
				}
			}
			globals[name] = val
			persistedBySlot[name] = true
			if exp, exists := info.mod.Exports[name]; exists {
				exp.Value = val
				info.mod.Exports[name] = exp
			} else {
				info.mod.Exports[name] = ExportMetadata{Value: val}
			}
		}

		// Fallback for binary-loaded modules where GlobalSlotNames is empty:
		// look up each exported name in the VM's flat globals map.
		vmGlobals := machine.Globals()
		for name, exp := range info.mod.Exports {
			if persistedBySlot[name] || exp.Value.Kind != value.Nil {
				continue
			}
			if val, ok := vmGlobals[name]; ok && val.Kind != value.Nil {
				exp.Value = val
				info.mod.Exports[name] = exp
				if _, exists := globals[name]; !exists {
					globals[name] = val
				}
			}
		}

		// Overwrite the registry with the updated module structure.
		rootModule, rootSpec := l.buildNamespaceModule(info.parts, info.mod.Exports)
		mergeNamespace(registry, rootModule, rootSpec)
	}

	// 4. Initialize only the stdlib modules that bundled modules actually import.
	initializedStdlib := make(map[string]bool)
	var initStdlibModule func(string) error
	initStdlibModule = func(modPath string) error {
		if initializedStdlib[modPath] {
			return nil
		}
		mod, err := l.loadModule(modPath)
		if err != nil {
			return err
		}
		for _, imported := range mod.Imports {
			if IsStdlibModulePath(imported) {
				if err := initStdlibModule(imported); err != nil {
					return err
				}
			}
		}
		if err := injectStdlibNativeMembers(registry, mod.Path); err != nil {
			return err
		}
		if _, err := machine.Run(mod.Function); err != nil {
			return err
		}
		persistedBySlot := make(map[string]bool)
		for _, name := range mod.Function.GlobalSlotNames {
			val, ok := machine.ResolveGlobal(mod.Function, name)
			if !ok {
				continue
			}
			globals[name] = val
			persistedBySlot[name] = true
			if exp, exists := mod.Exports[name]; exists {
				exp.Value = val
				mod.Exports[name] = exp
			}
		}
		for name, exp := range mod.Exports {
			if persistedBySlot[name] || exp.Value.Kind != value.Nil {
				continue
			}
			if val, ok := machine.Globals()[name]; ok && val.Kind != value.Nil {
				exp.Value = val
				mod.Exports[name] = exp
				globals[name] = val
			}
		}
		parts := namespacePartsForModule(modPath)
		if len(parts) > 0 {
			rootModule, rootSpec := l.buildNamespaceModule(parts, mod.Exports)
			mergeNamespace(registry, rootModule, rootSpec)
		}
		initializedStdlib[modPath] = true
		return nil
	}
	for _, info := range toInit {
		for _, imported := range info.mod.Imports {
			if !IsStdlibModulePath(imported) {
				continue
			}
			if err := initStdlibModule(imported); err != nil {
				return nil, nil, fmt.Errorf("failed to initialize stdlib module %s: %v", imported, err)
			}
		}
	}

	loaded, err := l.loadModule(entryPoint)
	if err != nil {
		return nil, nil, err
	}

	return loaded.Function, registry, nil
}

func (l *Loader) parseFile(path string) (*ast.Program, string, error) {
	source, err := readModuleSource(path)
	if err != nil {
		return nil, source, diagnostic.Wrap(err, diagnostic.KindParse, path, source)
	}
	return l.parseSource(path, source)
}

func (l *Loader) parseSource(path string, source string) (*ast.Program, string, error) {
	tokens, err := lexer.Scan(source)
	if err != nil {
		return nil, source, diagnostic.Wrap(err, diagnostic.KindParse, path, source)
	}
	program, err := parser.Parse(tokens)
	if err != nil {
		return nil, source, diagnostic.Wrap(err, diagnostic.KindParse, path, source)
	}
	return program, source, nil
}

func readModuleSource(path string) (string, error) {
	var (
		data []byte
		err  error
	)
	if IsStdlibModulePath(path) {
		data, err = stdlibFS.ReadFile(TrimStdlibModulePath(path))
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (l *Loader) loadImportsIntoRegistry(program *ast.Program, currentPath string, registry *bvmruntime.Registry) error {
	for _, stmt := range program.Statements {
		importStmt, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		modulePath, err := l.resolveImport(currentPath, importStmt)
		if err != nil {
			return err
		}
		loaded, err := l.loadModule(modulePath)
		if err != nil {
			return err
		}
		if err := l.bindImport(registry, importStmt, loaded, currentPath); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loader) resolveImport(currentPath string, importStmt *ast.ImportStmt) (string, error) {
	parts := make([]string, len(importStmt.Path))
	for i, part := range importStmt.Path {
		parts[i] = part.Lexeme
	}
	if isStdlibImport(parts) {
		if modulePath, ok := resolveStdlibModule(parts); ok {
			return modulePath, nil
		}
	}

	if l.Archive != nil {
		// Try to resolve within the archive
		relPath := strings.Join(parts, "/")
		for _, f := range l.Archive {
			name := strings.TrimSuffix(f.Name, ".pfbc")
			if name == relPath || strings.HasSuffix(name, "/"+relPath) {
				return name, nil
			}
		}
		// If it's a stdlib import but wasn't found in Archive (which only contains compiled project files)
		// it should have been handled by isStdlibImport above.
	}

	for _, candidate := range l.packageCandidates(currentPath, parts) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("module not found: %s", strings.Join(parts, "."))
}

func (l *Loader) packageCandidates(currentPath string, parts []string) []string {
	relPath := filepath.Join(parts...)
	baseName := parts[len(parts)-1]
	roots := l.packageRoots(currentPath)
	candidates := make([]string, 0, len(roots)*3)
	seen := make(map[string]bool)
	for _, root := range roots {
		for _, candidate := range []string{
			filepath.Join(root, relPath+".pf"),
			filepath.Join(root, relPath, "index.pf"),
			filepath.Join(root, relPath, baseName+".pf"),
		} {
			if seen[candidate] {
				continue
			}
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func (l *Loader) packageRoots(currentPath string) []string {
	currentDir := filepath.Dir(currentPath)
	roots := make([]string, 0, 12)
	seen := make(map[string]bool)
	addRoot := func(path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if seen[clean] {
			return
		}
		seen[clean] = true
		roots = append(roots, clean)
	}
	if projectRoot, ok := l.findProjectRoot(currentDir); ok {
		addRoot(filepath.Join(projectRoot, "src"))
		addRoot(filepath.Join(projectRoot, "lib"))
		addRoot(filepath.Join(projectRoot, "libs"))
		addRoot(projectRoot)
	}
	for dir := currentDir; dir != ""; dir = filepath.Dir(dir) {
		addRoot(filepath.Join(dir, "src"))
		addRoot(filepath.Join(dir, "lib"))
		addRoot(filepath.Join(dir, "libs"))
		addRoot(dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	addRoot(filepath.Join(l.WorkspaceDir, "src"))
	addRoot(filepath.Join(l.WorkspaceDir, "lib"))
	addRoot(filepath.Join(l.WorkspaceDir, "libs"))
	addRoot(l.WorkspaceDir)
	return roots
}

func (l *Loader) findProjectRoot(startDir string) (string, bool) {
	for dir := startDir; dir != ""; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "polyloft.toml")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", false
}

func (l *Loader) loadModule(path string) (*LoadedModule, error) {
	if loaded, ok := l.Cache[path]; ok {
		return loaded, nil
	}
	if l.Loading[path] {
		return nil, fmt.Errorf("circular import detected: %s", path)
	}
	l.Loading[path] = true

	if l.Archive != nil {
		cleanPath := strings.ReplaceAll(path, "\\", "/")
		if IsStdlibModulePath(cleanPath) {
			cleanPath = TrimStdlibModulePath(cleanPath)
		}
		cleanPath = strings.TrimSuffix(cleanPath, ".pf")

		var bcFile *zip.File
		for _, f := range l.Archive {
			if f.Name == cleanPath+".pfbc" {
				bcFile = f
				break
			}
		}

		if bcFile != nil {
			rc, err := bcFile.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			fn, metadata, err := classfile.ReadModule(rc)
			if err != nil {
				return nil, err
			}

			exports, err := decodeSerializedExports(metadata)
			if err != nil {
				return nil, err
			}

			if exports == nil {
				exports = make(map[string]ExportMetadata)
			}

			loaded := &LoadedModule{
				Path:     path,
				Dir:      filepath.Dir(path),
				Function: fn,
				Exports:  exports,
				Imports:  decodeSerializedImports(metadata),
			}
			l.Cache[path] = loaded
			return loaded, nil
		}

		// Fallback: If not found in archive, check if it's a stdlib module
		if !IsStdlibModulePath(path) {
			// If it's not stdlib and not in archive, we can't load it
			return nil, fmt.Errorf("module %s not found in archive", path)
		}
		// If it IS stdlib, we fall through to the normal source loading logic below
	}

	defer delete(l.Loading, path)

	program, source, err := l.parseFile(path)
	if err != nil {
		return nil, err
	}
	imports, err := l.collectModuleImports(program, path)
	if err != nil {
		return nil, err
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, l.Stdout)
	if err := l.loadImportsIntoRegistry(program, path, registry); err != nil {
		return nil, err
	}
	if err := sema.Check(program, registry); err != nil {
		return nil, err
	}
	fn, err := compiler.CompileModuleWithRegistry(program, registry)
	if err != nil {
		return nil, err
	}
	machine := vm.NewWithRegistry(l.Stdout, registry)
	if IsStdlibModulePath(path) {
		cleanPath := TrimStdlibModulePath(path)
		parts := strings.Split(cleanPath, "/")
		if len(parts) >= 3 && parts[0] == "stdlib" && parts[1] == "polyloft" {
			base := strings.TrimSuffix(parts[2], ".pf")
			modName := strings.ToUpper(base[:1]) + base[1:]
			if nativeVal, ok := registry.Globals()[modName]; ok {
				if nativeMod, ok := nativeVal.AsModule(); ok {
					for k, v := range nativeMod.Members {
						machine.Globals()[k] = v
					}
				}
			}
		}
	}

	if _, err := machine.Run(fn); err != nil {
		return nil, err
	}
	exports, err := l.CollectExports(program, machine, fn, registry, source)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Base(path), "index.pf") {
		if err := l.augmentIndexExports(program, path, exports); err != nil {
			return nil, err
		}
	}
	loaded := &LoadedModule{Path: path, Dir: filepath.Dir(path), Source: source, Function: fn, Exports: exports, Imports: imports}
	l.Cache[path] = loaded
	return loaded, nil
}

func (l *Loader) augmentIndexExports(program *ast.Program, currentPath string, exports map[string]ExportMetadata) error {
	for _, stmt := range program.Statements {
		importStmt, ok := stmt.(*ast.ImportStmt)
		if !ok || len(importStmt.Names) == 0 {
			continue
		}
		modulePath, err := l.resolveImport(currentPath, importStmt)
		if err != nil {
			return err
		}
		loaded, err := l.loadModule(modulePath)
		if err != nil {
			return err
		}
		allowed := l.visibleExports(loaded, currentPath)
		for _, name := range importStmt.Names {
			if _, exists := exports[name.Lexeme]; exists {
				continue
			}
			exported, ok := allowed[name.Lexeme]
			if !ok {
				return fmt.Errorf("symbol %s is not accessible from %s", name.Lexeme, strings.Join(pathTokens(importStmt.Path), "."))
			}
			exports[name.Lexeme] = exported
		}
	}
	return nil
}

func (l *Loader) collectModuleImports(program *ast.Program, currentPath string) ([]string, error) {
	imports := make([]string, 0)
	seen := make(map[string]bool)
	for _, stmt := range program.Statements {
		importStmt, ok := stmt.(*ast.ImportStmt)
		if !ok {
			continue
		}
		resolved, err := l.resolveImport(currentPath, importStmt)
		if err != nil {
			return nil, err
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		imports = append(imports, resolved)
	}
	return imports, nil
}

func namespacePartsForModule(modPath string) []string {
	displayPath := strings.TrimPrefix(strings.ReplaceAll(modPath, "\\", "/"), "/")
	if IsStdlibModulePath(displayPath) {
		displayPath = TrimStdlibModulePath(displayPath)
	}
	displayPath = strings.TrimPrefix(displayPath, "src/")
	displayPath = strings.TrimPrefix(displayPath, "lib/")
	displayPath = strings.TrimPrefix(displayPath, "libs/")
	displayPath = strings.TrimPrefix(displayPath, "stdlib/")
	displayPath = strings.TrimSuffix(displayPath, ".pf")
	displayPath = strings.TrimSuffix(displayPath, "/index")
	rawParts := strings.Split(displayPath, "/")
	parts := make([]string, 0, len(rawParts))
	for _, p := range rawParts {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func (l *Loader) CollectExports(program *ast.Program, machine *vm.VM, fn *bytecode.Function, registry *bvmruntime.Registry, source string) (map[string]ExportMetadata, error) {
	exports := make(map[string]ExportMetadata)
	builder := newSpecBuilder(program, machine, fn, registry)
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.LetStmt:
			var resolved value.Value
			if machine != nil {
				var ok bool
				resolved, ok = machine.ResolveGlobal(fn, node.Name.Lexeme)
				if !ok {
					return nil, fmt.Errorf("module export %s is undefined at runtime", node.Name.Lexeme)
				}
			} else {
				resolved = value.NilValue()
			}
			spec := bvmruntime.InferSpec(node.Name.Lexeme, resolved)
			if node.Type != nil && spec.TypeName == bvmruntime.TypeAny {
				spec.TypeName = builder.normalizeTypeRef(node.Type)
			}
			exports[node.Name.Lexeme] = ExportMetadata{Value: resolved, Spec: spec, Visibility: node.Visibility}
		case *ast.DestructureLetStmt:
			for _, target := range node.Targets {
				var resolved value.Value
				if machine != nil {
					var ok bool
					resolved, ok = machine.ResolveGlobal(fn, target.Lexeme)
					if !ok {
						return nil, fmt.Errorf("module export %s is undefined at runtime", target.Lexeme)
					}
				} else {
					resolved = value.NilValue()
				}
				exports[target.Lexeme] = ExportMetadata{Value: resolved, Spec: bvmruntime.InferSpec(target.Lexeme, resolved), Visibility: node.Visibility}
			}
		case *ast.FunctionStmt:
			var resolved value.Value
			if machine != nil {
				var ok bool
				resolved, ok = machine.ResolveGlobal(fn, node.Name.Lexeme)
				if !ok {
					return nil, fmt.Errorf("module export %s is undefined at runtime", node.Name.Lexeme)
				}
			} else {
				resolved = value.NilValue()
			}
			spec := bvmruntime.InferSpec(node.Name.Lexeme, resolved)
			spec.TypeParams = typeParamNames(node.TypeParams)
			if spec.Callable == nil {
				spec.Callable = &bvmruntime.CallableSpec{}
			}
			params := make([]string, len(node.Params))
			for i, param := range node.Params {
				params[i] = builder.normalizeTypeRef(param.Type)
			}
			spec.Callable.Params = params
			if node.ReturnType != nil && spec.Callable != nil {
				spec.Callable.Return = builder.normalizeTypeRef(node.ReturnType)
				if returnSpec, ok := builder.resolveDeclaredTypeSpec(node.ReturnType); ok {
					if len(returnSpec.InstanceMembers) > 0 {
						spec.InstanceMembers = returnSpec.InstanceMembers
					} else if len(returnSpec.Members) > 0 {
						spec.InstanceMembers = returnSpec.Members
					}
				}
			}
			exports[node.Name.Lexeme] = ExportMetadata{
				Value:          resolved,
				Spec:           spec,
				Visibility:     node.Visibility,
				IsNative:       node.IsNative,
				TextInsertKind: functionTextInsertKind(node),
				TextInsert:     functionTextInsert(node, source),
			}
		case *ast.ClassStmt:
			var resolved value.Value
			if machine != nil {
				var ok bool
				resolved, ok = machine.ResolveGlobal(fn, node.Name.Lexeme)
				if !ok {
					return nil, fmt.Errorf("module export %s is undefined at runtime", node.Name.Lexeme)
				}
			} else {
				resolved = value.NilValue()
			}
			spec, err := builder.classDeclSpec(node, resolved)
			if err != nil {
				return nil, err
			}
			exports[node.Name.Lexeme] = ExportMetadata{Value: resolved, Spec: spec, Visibility: node.Visibility, TextInsertKind: classTextInsertKind(node), TextInsert: sourceTextInsert(node.Annotations, node.SourceSpan, source)}
		case *ast.InterfaceStmt:
			spec, err := builder.interfaceDeclSpec(node)
			if err != nil {
				return nil, err
			}
			exports[node.Name.Lexeme] = ExportMetadata{Value: value.NilValue(), Spec: spec, Visibility: node.Visibility, TextInsertKind: interfaceTextInsertKind(node), TextInsert: sourceTextInsert(node.Annotations, node.SourceSpan, source)}
		case *ast.TypeAliasStmt:
			continue
		}
	}
	return exports, nil
}

func functionTextInsert(fn *ast.FunctionStmt, source string) string {
	if fn == nil {
		return ""
	}
	if fn.IsNative {
		return nativeFunctionTextInsert(fn)
	}
	return sourceTextInsert(fn.Annotations, fn.SourceSpan, source)
}

func functionTextInsertKind(fn *ast.FunctionStmt) string {
	if fn == nil {
		return ""
	}
	if fn.IsNative {
		return "native_def"
	}
	if hasAnnotation(fn.Annotations, "Lintclude") {
		return "function_source"
	}
	return ""
}

func classTextInsertKind(classStmt *ast.ClassStmt) string {
	if classStmt == nil || !hasAnnotation(classStmt.Annotations, "Lintclude") {
		return ""
	}
	switch {
	case classStmt.IsEnum:
		return "enum_source"
	case classStmt.IsRecord:
		return "record_source"
	default:
		return "class_source"
	}
}

func interfaceTextInsertKind(iface *ast.InterfaceStmt) string {
	if iface == nil || !hasAnnotation(iface.Annotations, "Lintclude") {
		return ""
	}
	return "interface_source"
}

func nativeFunctionTextInsert(fn *ast.FunctionStmt) string {
	if fn == nil || !fn.IsNative {
		return ""
	}
	var builder strings.Builder
	switch fn.Visibility {
	case ast.VisibilityPublic:
		builder.WriteString("public ")
	case ast.VisibilityProtected:
		builder.WriteString("protected ")
	}
	builder.WriteString("native def ")
	builder.WriteString(fn.Name.Lexeme)
	builder.WriteByte('(')
	for i, param := range fn.Params {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(param.Name.Lexeme)
		if param.Type != nil {
			builder.WriteString(": ")
			builder.WriteString(typeRefText(param.Type))
		}
	}
	builder.WriteByte(')')
	if fn.ReturnType != nil {
		builder.WriteString(" -> ")
		builder.WriteString(typeRefText(fn.ReturnType))
	}
	return builder.String()
}

func sourceTextInsert(annotations []ast.Annotation, span ast.SourceSpan, source string) string {
	if !hasAnnotation(annotations, "Lintclude") || span.StartLine <= 0 || span.EndLine < span.StartLine || source == "" {
		return ""
	}
	lines := strings.Split(source, "\n")
	if len(lines) == 0 || span.StartLine > len(lines) {
		return ""
	}
	end := span.EndLine
	if end > len(lines) {
		end = len(lines)
	}
	start := expandLeadingCommentBlock(lines, span.StartLine)
	return strings.TrimRight(strings.Join(lines[start-1:end], "\n"), "\n")
}

func expandLeadingCommentBlock(lines []string, start int) int {
	idx := start - 1
	sawComment := false
	for idx > 0 {
		trimmed := strings.TrimSpace(lines[idx-1])
		switch {
		case trimmed == "":
			if !sawComment {
				return idx + 1
			}
			idx--
		case strings.HasPrefix(trimmed, "//"), strings.HasPrefix(trimmed, "/*"), strings.HasPrefix(trimmed, "*"), strings.HasPrefix(trimmed, "*/"):
			sawComment = true
			idx--
		default:
			return idx + 1
		}
	}
	return 1
}

func hasAnnotation(annotations []ast.Annotation, name string) bool {
	for _, annotation := range annotations {
		if strings.EqualFold(annotation.Name.Lexeme, name) {
			return true
		}
	}
	return false
}

func typeRefText(typeRef *ast.TypeRef) string {
	if typeRef == nil {
		return "any"
	}
	if len(typeRef.Union) > 0 {
		parts := make([]string, 0, len(typeRef.Union))
		for _, option := range typeRef.Union {
			parts = append(parts, typeRefText(option))
		}
		return strings.Join(parts, " | ")
	}
	if typeRef.Wildcard {
		if typeRef.Bound == nil {
			return "?"
		}
		if typeRef.BoundKind == token.Super {
			return "? super " + typeRefText(typeRef.Bound)
		}
		return "? extends " + typeRefText(typeRef.Bound)
	}
	if len(typeRef.Args) == 0 {
		return typeRef.Name.Lexeme
	}
	args := make([]string, 0, len(typeRef.Args))
	for _, arg := range typeRef.Args {
		args = append(args, typeRefText(arg))
	}
	return typeRef.Name.Lexeme + "<" + strings.Join(args, ", ") + ">"
}

type specBuilder struct {
	program        *ast.Program
	machine        *vm.VM
	fn             *bytecode.Function
	registry       *bvmruntime.Registry
	typeAliases    map[string]*ast.TypeRef
	classCache     map[string]*bvmruntime.Spec
	interfaceCache map[string]*bvmruntime.Spec
}

func newSpecBuilder(program *ast.Program, machine *vm.VM, fn *bytecode.Function, registry *bvmruntime.Registry) *specBuilder {
	return &specBuilder{
		program:        program,
		machine:        machine,
		fn:             fn,
		registry:       registry,
		typeAliases:    collectProgramTypeAliases(program),
		classCache:     make(map[string]*bvmruntime.Spec),
		interfaceCache: make(map[string]*bvmruntime.Spec),
	}
}

func (b *specBuilder) resolveDeclaredTypeSpec(typeRef *ast.TypeRef) (bvmruntime.Spec, bool) {
	if typeRef == nil {
		return bvmruntime.Spec{}, false
	}
	if alias, ok := b.typeAliases[typeRef.Name.Lexeme]; ok {
		return b.resolveDeclaredTypeSpec(alias)
	}
	for _, stmt := range b.program.Statements {
		switch node := stmt.(type) {
		case *ast.ClassStmt:
			if node.Name.Lexeme != typeRef.Name.Lexeme {
				continue
			}
			resolved, ok := b.machine.ResolveGlobal(b.fn, node.Name.Lexeme)
			if !ok {
				return bvmruntime.Spec{}, false
			}
			spec, err := b.classDeclSpec(node, resolved)
			if err != nil {
				return bvmruntime.Spec{}, false
			}
			return spec, true
		case *ast.InterfaceStmt:
			if node.Name.Lexeme != typeRef.Name.Lexeme {
				continue
			}
			spec, err := b.interfaceDeclSpec(node)
			if err != nil {
				return bvmruntime.Spec{}, false
			}
			return spec, true
		}
	}
	if b.registry == nil {
		return bvmruntime.Spec{}, false
	}
	spec, ok := b.registry.Specs()[typeRef.Name.Lexeme]
	return spec, ok
}

func (l *Loader) bindImport(registry *bvmruntime.Registry, importStmt *ast.ImportStmt, loaded *LoadedModule, importerPath string) error {
	allowed := l.visibleExports(loaded, importerPath)
	parts := pathTokens(importStmt.Path)
	if err := injectStdlibNativeMembers(registry, loaded.Path); err != nil {
		return err
	}
	if IsStdlibModulePath(loaded.Path) {
		for name, exported := range allowed {
			registry.DefineWithSpec(name, exported.Value, exported.Spec)
		}
	}
	if len(importStmt.Names) > 0 {
		for _, name := range importStmt.Names {
			exported, ok := allowed[name.Lexeme]
			if !ok {
				return fmt.Errorf("symbol %s is not accessible from %s", name.Lexeme, strings.Join(parts, "."))
			}
			registry.DefineWithSpec(name.Lexeme, exported.Value, exported.Spec)
		}
		return nil
	}

	// For stdlib imports like 'polyloft.common', we might need to build the namespace
	if IsStdlibModulePath(loaded.Path) && parts[0] == "polyloft" {
		moduleValue, moduleSpec := l.buildNamespaceModule(parts, allowed)
		return mergeNamespace(registry, moduleValue, moduleSpec)
	}

	moduleValue, moduleSpec := l.buildNamespaceModule(parts, allowed)
	return mergeNamespace(registry, moduleValue, moduleSpec)
}

func injectStdlibNativeMembers(registry *bvmruntime.Registry, modulePath string) error {
	if !IsStdlibModulePath(modulePath) {
		return nil
	}
	cleanPath := TrimStdlibModulePath(modulePath)
	parts := strings.Split(cleanPath, "/")
	if len(parts) < 3 || parts[0] != "stdlib" || parts[1] != "polyloft" {
		return nil
	}
	base := strings.TrimSuffix(parts[2], ".pf")
	if base == "" {
		return nil
	}
	moduleName := strings.ToUpper(base[:1]) + base[1:]
	nativeVal, ok := registry.Globals()[moduleName]
	if !ok {
		return nil
	}
	nativeMod, ok := nativeVal.AsModule()
	if !ok {
		return nil
	}
	moduleSpec, _ := registry.Specs()[moduleName]
	for name, member := range nativeMod.Members {
		spec := bvmruntime.Spec{Name: name, TypeName: bvmruntime.TypeAny}
		if moduleSpec.Module != nil {
			if memberSpec, ok := moduleSpec.Module.Members[name]; ok {
				spec = memberSpec
			}
		}
		registry.DefineWithSpec(name, member, spec)
	}
	return nil
}

func (l *Loader) visibleExports(loaded *LoadedModule, importerPath string) map[string]ExportMetadata {
	allowed := make(map[string]ExportMetadata)
	importerDir := filepath.Dir(importerPath)
	for name, exported := range loaded.Exports {
		switch exported.Visibility {
		case ast.VisibilityPublic:
			allowed[name] = exported
		case ast.VisibilityProtected:
			if l.sameModuleDir(importerDir, loaded.Dir) {
				allowed[name] = exported
			}
		case ast.VisibilityPrivate:
			if importerPath == loaded.Path {
				allowed[name] = exported
			}
		}
	}
	return allowed
}

func (l *Loader) sameModuleDir(left string, right string) bool {
	if IsStdlibModulePath(left) || IsStdlibModulePath(right) {
		leftClean := strings.ReplaceAll(TrimStdlibModulePath(left), "\\", "/")
		rightClean := strings.ReplaceAll(TrimStdlibModulePath(right), "\\", "/")
		return leftClean == rightClean
	}
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func pathTokens(tokens []token.Token) []string {
	parts := make([]string, len(tokens))
	for i, token := range tokens {
		parts[i] = token.Lexeme
	}
	return parts
}

func (l *Loader) buildNamespaceModule(parts []string, exports map[string]ExportMetadata) (*value.Module, *bvmruntime.Spec) {
	leaf := &value.Module{Name: parts[len(parts)-1], Members: make(map[string]value.Value)}
	leafSpec := &bvmruntime.Spec{Name: parts[len(parts)-1], TypeName: bvmruntime.TypeModule, Module: &bvmruntime.ModuleSpec{Name: parts[len(parts)-1], Members: make(map[string]bvmruntime.Spec)}}
	for name, exported := range exports {
		leaf.Members[name] = exported.Value
		leafSpec.Module.Members[name] = exported.Spec
	}
	currentValue := leaf
	currentSpec := leafSpec
	for i := len(parts) - 2; i >= 0; i-- {
		wrappedValue := &value.Module{Name: parts[i], Members: map[string]value.Value{parts[i+1]: value.ObjectValue(currentValue)}}
		wrappedSpec := &bvmruntime.Spec{Name: parts[i], TypeName: bvmruntime.TypeModule, Module: &bvmruntime.ModuleSpec{Name: parts[i], Members: map[string]bvmruntime.Spec{parts[i+1]: *currentSpec}}}
		currentValue = wrappedValue
		currentSpec = wrappedSpec
	}
	return currentValue, currentSpec
}

func mergeNamespace(registry *bvmruntime.Registry, incoming *value.Module, spec *bvmruntime.Spec) error {
	rootName := incoming.Name
	if existing, ok := registry.Globals()[rootName]; ok {
		existingModule, moduleOk := existing.AsModule()
		if !moduleOk {
			return fmt.Errorf("cannot merge namespace %s into non-module symbol", rootName)
		}
		mergeModuleValue(existingModule, incoming)
		existingSpec := registry.Specs()[rootName]
		mergeModuleSpec(existingSpec.Module, spec.Module)
		registry.DefineWithSpec(rootName, value.ObjectValue(existingModule), existingSpec)
		return nil
	}
	registry.DefineWithSpec(rootName, value.ObjectValue(incoming), *spec)
	return nil
}

func mergeModuleValue(dst *value.Module, src *value.Module) {
	for name, member := range src.Members {
		if existing, ok := dst.Members[name]; ok {
			existingModule, existingIsModule := existing.AsModule()
			newModule, newIsModule := member.AsModule()
			if existingIsModule && newIsModule {
				mergeModuleValue(existingModule, newModule)
				continue
			}
		}
		dst.Members[name] = member
	}
}

func mergeModuleSpec(dst *bvmruntime.ModuleSpec, src *bvmruntime.ModuleSpec) {
	if dst == nil || src == nil {
		return
	}
	for name, member := range src.Members {
		existing, ok := dst.Members[name]
		if ok && existing.Module != nil && member.Module != nil {
			mergeModuleSpec(existing.Module, member.Module)
			dst.Members[name] = existing
			continue
		}
		dst.Members[name] = member
	}
}

func (b *specBuilder) interfaceDeclSpec(iface *ast.InterfaceStmt) (bvmruntime.Spec, error) {
	if cached, ok := b.interfaceCache[iface.Name.Lexeme]; ok {
		return *cached, nil
	}
	spec := bvmruntime.Spec{
		Name:        iface.Name.Lexeme,
		TypeName:    iface.Name.Lexeme,
		TypeParams:  typeParamNames(iface.TypeParams),
		Members:     make(map[string]bvmruntime.Spec),
		IsInterface: true,
		IsSealed:    iface.IsSealed,
		Permits:     permitNames(iface.Permits),
	}
	b.interfaceCache[iface.Name.Lexeme] = &spec
	for _, base := range iface.Extends {
		baseSpec, ok := b.resolveDeclaredTypeSpec(base)
		if !ok {
			continue
		}
		baseSpec = b.applySpecTypeArgs(baseSpec, base.Args)
		for name, member := range baseSpec.Members {
			spec.Members[name] = member
		}
	}
	for _, method := range iface.Methods {
		params := make([]string, len(method.Params))
		for i, param := range method.Params {
			params[i] = b.normalizeTypeRef(param.Type)
		}
		spec.Members[method.Name.Lexeme] = bvmruntime.Spec{
			Name:     iface.Name.Lexeme + "." + method.Name.Lexeme,
			TypeName: bvmruntime.TypeFunction,
			Callable: &bvmruntime.CallableSpec{Params: params, Return: b.normalizeTypeRef(method.ReturnType)},
		}
	}
	b.interfaceCache[iface.Name.Lexeme] = &spec
	return spec, nil
}

func (b *specBuilder) applySpecTypeArgs(spec bvmruntime.Spec, args []*ast.TypeRef) bvmruntime.Spec {
	if len(args) == 0 || len(spec.TypeParams) == 0 {
		return spec
	}
	mapping := make(map[string]string, len(spec.TypeParams))
	for i, param := range spec.TypeParams {
		if i >= len(args) {
			break
		}
		mapping[param] = b.normalizeTypeRef(args[i])
	}
	return substituteSpecTypeParams(spec, mapping)
}

func substituteSpecTypeParams(spec bvmruntime.Spec, mapping map[string]string) bvmruntime.Spec {
	if len(mapping) == 0 {
		return spec
	}
	copySpec := spec
	copySpec.TypeName = substituteSpecTypeName(copySpec.TypeName, mapping)
	copySpec.TypeParams = nil
	if copySpec.Callable != nil {
		params := make([]string, len(copySpec.Callable.Params))
		for i, param := range copySpec.Callable.Params {
			params[i] = substituteSpecTypeName(param, mapping)
		}
		callableCopy := &bvmruntime.CallableSpec{
			Params:     params,
			Return:     substituteSpecTypeName(copySpec.Callable.Return, mapping),
			Variadic:   copySpec.Callable.Variadic,
			Overloaded: copySpec.Callable.Overloaded,
		}
		if len(copySpec.Callable.Overloads) > 0 {
			callableCopy.Overloads = make([]*bvmruntime.CallableSpec, 0, len(copySpec.Callable.Overloads))
			for _, overload := range copySpec.Callable.Overloads {
				if overload == nil {
					continue
				}
				overloadParams := make([]string, len(overload.Params))
				for i, param := range overload.Params {
					overloadParams[i] = substituteSpecTypeName(param, mapping)
				}
				callableCopy.Overloads = append(callableCopy.Overloads, &bvmruntime.CallableSpec{
					Params:     overloadParams,
					Return:     substituteSpecTypeName(overload.Return, mapping),
					Variadic:   overload.Variadic,
					Overloaded: overload.Overloaded,
				})
			}
		}
		copySpec.Callable = callableCopy
	}
	if len(copySpec.ConstructorOverloads) > 0 {
		overloads := make([]*bvmruntime.CallableSpec, 0, len(copySpec.ConstructorOverloads))
		for _, overload := range copySpec.ConstructorOverloads {
			if overload == nil {
				continue
			}
			params := make([]string, len(overload.Params))
			for i, param := range overload.Params {
				params[i] = substituteSpecTypeName(param, mapping)
			}
			overloads = append(overloads, &bvmruntime.CallableSpec{
				Params:   params,
				Return:   substituteSpecTypeName(overload.Return, mapping),
				Variadic: overload.Variadic,
			})
		}
		copySpec.ConstructorOverloads = overloads
	}
	if len(copySpec.Members) > 0 {
		members := make(map[string]bvmruntime.Spec, len(copySpec.Members))
		for name, member := range copySpec.Members {
			members[name] = substituteSpecTypeParams(member, mapping)
		}
		copySpec.Members = members
	}
	if len(copySpec.InstanceMembers) > 0 {
		members := make(map[string]bvmruntime.Spec, len(copySpec.InstanceMembers))
		for name, member := range copySpec.InstanceMembers {
			members[name] = substituteSpecTypeParams(member, mapping)
		}
		copySpec.InstanceMembers = members
	}
	if copySpec.Module != nil && len(copySpec.Module.Members) > 0 {
		moduleMembers := make(map[string]bvmruntime.Spec, len(copySpec.Module.Members))
		for name, member := range copySpec.Module.Members {
			moduleMembers[name] = substituteSpecTypeParams(member, mapping)
		}
		copySpec.Module = &bvmruntime.ModuleSpec{Name: copySpec.Module.Name, Members: moduleMembers}
	}
	return copySpec
}

func substituteSpecTypeName(typeName string, mapping map[string]string) string {
	trimmed := strings.TrimSpace(typeName)
	if trimmed == "" {
		return trimmed
	}
	if replacement, ok := mapping[trimmed]; ok {
		return replacement
	}
	if strings.HasPrefix(trimmed, "? extends ") {
		return "? extends " + substituteSpecTypeName(strings.TrimPrefix(trimmed, "? extends "), mapping)
	}
	if strings.HasPrefix(trimmed, "? super ") {
		return "? super " + substituteSpecTypeName(strings.TrimPrefix(trimmed, "? super "), mapping)
	}
	if trimmed == "?" {
		return trimmed
	}
	if parts := splitTopLevelTypeList(trimmed, '|'); len(parts) > 0 {
		resolved := make([]string, len(parts))
		for i, part := range parts {
			resolved[i] = substituteSpecTypeName(part, mapping)
		}
		return joinTypeParts(resolved, " | ")
	}
	base, args := parseGenericType(trimmed)
	if len(args) == 0 {
		return trimmed
	}
	resolvedArgs := make([]string, len(args))
	for i, arg := range args {
		resolvedArgs[i] = substituteSpecTypeName(arg, mapping)
	}
	return base + "<" + joinTypeParts(resolvedArgs, ", ") + ">"
}

func (b *specBuilder) classDeclSpec(classStmt *ast.ClassStmt, resolved value.Value) (bvmruntime.Spec, error) {
	if cached, ok := b.classCache[classStmt.Name.Lexeme]; ok {
		return *cached, nil
	}
	spec := bvmruntime.InferSpec(classStmt.Name.Lexeme, resolved)
	spec.TypeParams = typeParamNames(classStmt.TypeParams)
	spec.IsAbstract = classStmt.IsAbstract
	spec.IsSealed = classStmt.IsSealed
	spec.IsRecord = classStmt.IsRecord
	spec.Permits = permitNames(classStmt.Permits)
	spec.ConstructorVisibility = string(ast.VisibilityPublic)
	if spec.Members == nil {
		spec.Members = make(map[string]bvmruntime.Spec)
	}
	if spec.InstanceMembers == nil {
		spec.InstanceMembers = make(map[string]bvmruntime.Spec)
	}
	b.classCache[classStmt.Name.Lexeme] = &spec
	for _, method := range classStmt.Methods {
		if method.IsConstructor {
			spec.ConstructorVisibility = string(method.Visibility)
			continue
		}
		params := make([]string, len(method.Params))
		for i, param := range method.Params {
			params[i] = b.normalizeTypeRef(param.Type)
		}
		memberSpec := bvmruntime.Spec{
			Name:       classStmt.Name.Lexeme + "." + method.Name.Lexeme,
			TypeName:   bvmruntime.TypeFunction,
			Callable:   &bvmruntime.CallableSpec{Params: params, Return: b.normalizeTypeRef(method.ReturnType)},
			IsAbstract: method.IsAbstract,
		}
		if returnSpec, ok := b.resolveDeclaredTypeSpec(method.ReturnType); ok {
			if len(returnSpec.InstanceMembers) > 0 {
				memberSpec.InstanceMembers = returnSpec.InstanceMembers
			} else if len(returnSpec.Members) > 0 {
				memberSpec.InstanceMembers = returnSpec.Members
			}
		}
		if method.Static {
			if existing, ok := spec.Members[method.Name.Lexeme]; ok {
				memberSpec = mergeCallableSpecOverload(existing, memberSpec)
			}
			spec.Members[method.Name.Lexeme] = memberSpec
			continue
		}
		if existing, ok := spec.InstanceMembers[method.Name.Lexeme]; ok {
			memberSpec = mergeCallableSpecOverload(existing, memberSpec)
		}
		spec.InstanceMembers[method.Name.Lexeme] = memberSpec
	}
	return spec, nil
}

func cloneCallableSpec(spec *bvmruntime.CallableSpec) *bvmruntime.CallableSpec {
	if spec == nil {
		return nil
	}
	copySpec := &bvmruntime.CallableSpec{
		Params:     append([]string(nil), spec.Params...),
		Return:     spec.Return,
		Variadic:   spec.Variadic,
		Overloaded: spec.Overloaded,
	}
	if len(spec.Overloads) > 0 {
		copySpec.Overloads = make([]*bvmruntime.CallableSpec, 0, len(spec.Overloads))
		for _, overload := range spec.Overloads {
			if overload == nil {
				continue
			}
			copySpec.Overloads = append(copySpec.Overloads, &bvmruntime.CallableSpec{
				Params:     append([]string(nil), overload.Params...),
				Return:     overload.Return,
				Variadic:   overload.Variadic,
				Overloaded: overload.Overloaded,
			})
		}
	}
	return copySpec
}

func mergeCallableSpecOverload(existing bvmruntime.Spec, current bvmruntime.Spec) bvmruntime.Spec {
	if existing.Callable == nil || current.Callable == nil {
		return current
	}
	overloads := make([]*bvmruntime.CallableSpec, 0, 2)
	if len(existing.Callable.Overloads) > 0 {
		for _, overload := range existing.Callable.Overloads {
			if overload == nil {
				continue
			}
			overloads = append(overloads, cloneCallableSpec(overload))
		}
	} else {
		overloads = append(overloads, cloneCallableSpec(existing.Callable))
	}
	if len(current.Callable.Overloads) > 0 {
		for _, overload := range current.Callable.Overloads {
			if overload == nil {
				continue
			}
			overloads = append(overloads, cloneCallableSpec(overload))
		}
	} else {
		overloads = append(overloads, cloneCallableSpec(current.Callable))
	}
	current.Callable = cloneCallableSpec(current.Callable)
	current.Callable.Overloaded = len(overloads) > 1
	current.Callable.Overloads = overloads
	return current
}

func permitNames(permits []*ast.TypeRef) []string {
	if len(permits) == 0 {
		return nil
	}
	names := make([]string, 0, len(permits))
	for _, permit := range permits {
		names = append(names, permit.Name.Lexeme)
	}
	return names
}

func collectProgramTypeAliases(program *ast.Program) map[string]*ast.TypeRef {
	aliases := make(map[string]*ast.TypeRef)
	for _, stmt := range program.Statements {
		if alias, ok := stmt.(*ast.TypeAliasStmt); ok {
			aliases[alias.Name.Lexeme] = alias.Target
		}
	}
	return aliases
}

func (b *specBuilder) normalizeTypeRef(typeRef *ast.TypeRef) string {
	if typeRef == nil {
		return bvmruntime.TypeAny
	}
	if len(typeRef.Union) > 0 {
		parts := make([]string, len(typeRef.Union))
		for i, option := range typeRef.Union {
			parts[i] = b.normalizeTypeRef(option)
		}
		return joinTypeParts(parts, " | ")
	}
	if typeRef.Wildcard {
		return bvmruntime.TypeAny
	}
	if alias, ok := b.typeAliases[typeRef.Name.Lexeme]; ok {
		return b.normalizeTypeRef(alias)
	}
	base := normalizeScalarTypeName(typeRef.Name.Lexeme)
	if len(typeRef.Args) == 0 {
		return base
	}
	args := make([]string, len(typeRef.Args))
	for i, arg := range typeRef.Args {
		args[i] = b.normalizeTypeRef(arg)
	}
	return base + "<" + joinTypeParts(args, ", ") + ">"
}

func normalizeScalarTypeName(name string) string {
	switch name {
	case "int":
		return bvmruntime.TypeInt
	case "float":
		return bvmruntime.TypeFloat
	case "number":
		return bvmruntime.TypeNumber
	case "bool":
		return bvmruntime.TypeBool
	case "char":
		return bvmruntime.TypeChar
	case "String":
		return bvmruntime.TypeString
	case "string":
		return bvmruntime.TypeString
	case "void":
		return bvmruntime.TypeVoid
	case "nil":
		return bvmruntime.TypeNil
	case "array":
		return bvmruntime.TypeArray
	case "map":
		return bvmruntime.TypeMap
	case "tuple":
		return bvmruntime.TypeTuple
	case "range":
		return bvmruntime.TypeRange
	case "any", "":
		return bvmruntime.TypeAny
	case "Any":
		return bvmruntime.TypeAny
	default:
		return name
	}
}

func joinTypeParts(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	joined := parts[0]
	for i := 1; i < len(parts); i++ {
		joined += sep + parts[i]
	}
	return joined
}

func splitTopLevelTypeList(input string, sep rune) []string {
	depth := 0
	parts := make([]string, 0, 2)
	start := 0
	for i, r := range input {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if r == sep && depth == 0 {
				parts = append(parts, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	if start == 0 {
		return nil
	}
	parts = append(parts, strings.TrimSpace(input[start:]))
	return parts
}

func parseGenericType(typeName string) (string, []string) {
	start := -1
	for i, r := range typeName {
		if r == '<' {
			start = i
			break
		}
	}
	if start < 0 || len(typeName) == 0 || typeName[len(typeName)-1] != '>' {
		return typeName, nil
	}
	return strings.TrimSpace(typeName[:start]), splitTopLevelTypeList(typeName[start+1:len(typeName)-1], ',')
}

func typeParamNames(params []ast.TypeParam) []string {
	if len(params) == 0 {
		return nil
	}
	names := make([]string, len(params))
	for i, param := range params {
		names[i] = param.Name.Lexeme
	}
	return names
}
