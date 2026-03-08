package modules

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/lexer"
	"github.com/ArubikU/polyloft-bvm/internal/parser"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/token"
	"github.com/ArubikU/polyloft-bvm/internal/value"
	"github.com/ArubikU/polyloft-bvm/internal/vm"
)

type exportedSymbol struct {
	Value      value.Value
	Spec       bvmruntime.Spec
	Visibility ast.Visibility
}

type loadedModule struct {
	Path    string
	Dir     string
	Exports map[string]exportedSymbol
}

type Loader struct {
	stdout       io.Writer
	workspaceDir string
	cache        map[string]*loadedModule
	loading      map[string]bool
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
		stdout:       stdout,
		workspaceDir: workspaceDir,
		cache:        make(map[string]*loadedModule),
		loading:      make(map[string]bool),
	}
	program, err := loader.parseFile(absPath)
	if err != nil {
		return nil, nil, err
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, stdout)
	if err := loader.loadImportsIntoRegistry(program, absPath, registry); err != nil {
		return nil, nil, err
	}
	return program, registry, nil
}

func (l *Loader) parseFile(path string) (*ast.Program, error) {
	var (
		source []byte
		err    error
	)
	if isStdlibModulePath(path) {
		source, err = stdlibFS.ReadFile(trimStdlibModulePath(path))
	} else {
		source, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	tokens, err := lexer.Scan(string(source))
	if err != nil {
		return nil, err
	}
	return parser.Parse(tokens)
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
	addRoot(filepath.Join(l.workspaceDir, "src"))
	addRoot(filepath.Join(l.workspaceDir, "lib"))
	addRoot(filepath.Join(l.workspaceDir, "libs"))
	addRoot(l.workspaceDir)
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

func (l *Loader) loadModule(path string) (*loadedModule, error) {
	if loaded, ok := l.cache[path]; ok {
		return loaded, nil
	}
	if l.loading[path] {
		return nil, fmt.Errorf("circular import detected: %s", path)
	}
	l.loading[path] = true
	defer delete(l.loading, path)

	program, err := l.parseFile(path)
	if err != nil {
		return nil, err
	}
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, l.stdout)
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
	machine := vm.NewWithRegistry(l.stdout, registry)
	if _, err := machine.Run(fn); err != nil {
		return nil, err
	}
	exports, err := collectExports(program, machine, fn, registry)
	if err != nil {
		return nil, err
	}
	loaded := &loadedModule{Path: path, Dir: filepath.Dir(path), Exports: exports}
	l.cache[path] = loaded
	return loaded, nil
}

func collectExports(program *ast.Program, machine *vm.VM, fn *bytecode.Function, registry *bvmruntime.Registry) (map[string]exportedSymbol, error) {
	exports := make(map[string]exportedSymbol)
	builder := newSpecBuilder(program, machine, fn, registry)
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.LetStmt:
			resolved, ok := machine.ResolveGlobal(fn, node.Name.Lexeme)
			if !ok {
				return nil, fmt.Errorf("module export %s is undefined at runtime", node.Name.Lexeme)
			}
			spec := bvmruntime.InferSpec(node.Name.Lexeme, resolved)
			if node.Type != nil && spec.TypeName == bvmruntime.TypeAny {
				spec.TypeName = builder.normalizeTypeRef(node.Type)
			}
			exports[node.Name.Lexeme] = exportedSymbol{Value: resolved, Spec: spec, Visibility: node.Visibility}
		case *ast.DestructureLetStmt:
			for _, target := range node.Targets {
				resolved, ok := machine.ResolveGlobal(fn, target.Lexeme)
				if !ok {
					return nil, fmt.Errorf("module export %s is undefined at runtime", target.Lexeme)
				}
				exports[target.Lexeme] = exportedSymbol{Value: resolved, Spec: bvmruntime.InferSpec(target.Lexeme, resolved), Visibility: node.Visibility}
			}
		case *ast.FunctionStmt:
			resolved, ok := machine.ResolveGlobal(fn, node.Name.Lexeme)
			if !ok {
				return nil, fmt.Errorf("module export %s is undefined at runtime", node.Name.Lexeme)
			}
			spec := bvmruntime.InferSpec(node.Name.Lexeme, resolved)
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
			exports[node.Name.Lexeme] = exportedSymbol{Value: resolved, Spec: spec, Visibility: node.Visibility}
		case *ast.ClassStmt:
			resolved, ok := machine.ResolveGlobal(fn, node.Name.Lexeme)
			if !ok {
				return nil, fmt.Errorf("module export %s is undefined at runtime", node.Name.Lexeme)
			}
			spec, err := builder.classDeclSpec(node, resolved)
			if err != nil {
				return nil, err
			}
			exports[node.Name.Lexeme] = exportedSymbol{Value: resolved, Spec: spec, Visibility: node.Visibility}
		case *ast.InterfaceStmt:
			spec, err := builder.interfaceDeclSpec(node)
			if err != nil {
				return nil, err
			}
			exports[node.Name.Lexeme] = exportedSymbol{Value: value.NilValue(), Spec: spec, Visibility: node.Visibility}
		case *ast.TypeAliasStmt:
			continue
		}
	}
	return exports, nil
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

func (l *Loader) bindImport(registry *bvmruntime.Registry, importStmt *ast.ImportStmt, loaded *loadedModule, importerPath string) error {
	allowed := visibleExports(loaded, importerPath)
	if len(importStmt.Names) > 0 {
		for _, name := range importStmt.Names {
			exported, ok := allowed[name.Lexeme]
			if !ok {
				return fmt.Errorf("symbol %s is not accessible from %s", name.Lexeme, strings.Join(pathTokens(importStmt.Path), "."))
			}
			registry.DefineWithSpec(name.Lexeme, exported.Value, exported.Spec)
		}
		return nil
	}
	moduleValue, moduleSpec := buildNamespaceModule(pathTokens(importStmt.Path), allowed)
	return mergeNamespace(registry, moduleValue, moduleSpec)
}

func visibleExports(loaded *loadedModule, importerPath string) map[string]exportedSymbol {
	allowed := make(map[string]exportedSymbol)
	importerDir := filepath.Dir(importerPath)
	for name, exported := range loaded.Exports {
		switch exported.Visibility {
		case ast.VisibilityPublic:
			allowed[name] = exported
		case ast.VisibilityProtected:
			if sameModuleDir(importerDir, loaded.Dir) {
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

func sameModuleDir(left string, right string) bool {
	if isStdlibModulePath(left) || isStdlibModulePath(right) {
		leftClean := strings.ReplaceAll(trimStdlibModulePath(left), "\\", "/")
		rightClean := strings.ReplaceAll(trimStdlibModulePath(right), "\\", "/")
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

func buildNamespaceModule(parts []string, exports map[string]exportedSymbol) (*value.Module, *bvmruntime.Spec) {
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
		copySpec.Callable = &bvmruntime.CallableSpec{
			Params:    params,
			Return:    substituteSpecTypeName(copySpec.Callable.Return, mapping),
			Variadic:  copySpec.Callable.Variadic,
		}
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
			spec.Members[method.Name.Lexeme] = memberSpec
			continue
		}
		spec.InstanceMembers[method.Name.Lexeme] = memberSpec
	}
	return spec, nil
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
