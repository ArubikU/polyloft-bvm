package modules

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/classfile"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
)

const manifestSchemaVersion = 2

type manifestTypeRef struct {
	Display string            `json:"display"`
	Kind    string            `json:"kind"`
	Name    string            `json:"name,omitempty"`
	Module  string            `json:"module,omitempty"`
	Symbol  string            `json:"symbol,omitempty"`
	Args    []manifestTypeRef `json:"args,omitempty"`
	Options []manifestTypeRef `json:"options,omitempty"`
}

type manifestTypeOrigin struct {
	Module string
	Symbol string
}

type manifestTypeCatalog struct {
	origins map[string][]manifestTypeOrigin
}

type bundleTypesManifest struct {
	SchemaVersion int                            `json:"schema_version"`
	Kind          string                         `json:"kind"`
	EntryPoint    string                         `json:"entry_point"`
	Modules       map[string]moduleTypesManifest `json:"modules,omitempty"`
}

type stdlibTypesManifest struct {
	SchemaVersion int                            `json:"schema_version"`
	Kind          string                         `json:"kind"`
	Modules       map[string]moduleTypesManifest `json:"modules,omitempty"`
}

type symbolCollectionManifest struct {
	SchemaVersion int                            `json:"schema_version"`
	Kind          string                         `json:"kind"`
	Symbols       map[string]symbolTypesManifest `json:"symbols,omitempty"`
}

type moduleTypesManifest struct {
	Source      string                         `json:"source,omitempty"`
	ImportPath  string                         `json:"import_path,omitempty"`
	BundleEntry string                         `json:"bundle_entry,omitempty"`
	Imports     []string                       `json:"imports,omitempty"`
	Exports     map[string]symbolTypesManifest `json:"exports,omitempty"`
	Functions   map[string]symbolTypesManifest `json:"functions,omitempty"`
	TextInserts map[string]textInsertManifest  `json:"text_inserts,omitempty"`
}

type textInsertManifest struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Text       string `json:"text"`
	Visibility string `json:"visibility,omitempty"`
}

type symbolTypesManifest struct {
	Kind                  string                         `json:"kind,omitempty"`
	Name                  string                         `json:"name,omitempty"`
	Type                  string                         `json:"type,omitempty"`
	TypeRef               *manifestTypeRef               `json:"type_ref,omitempty"`
	TypeParams            []string                       `json:"type_params,omitempty"`
	Callable              *callableTypesManifest         `json:"callable,omitempty"`
	Constructors          []callableTypesManifest        `json:"constructors,omitempty"`
	ConstructorVisibility string                         `json:"constructor_visibility,omitempty"`
	Members               map[string]symbolTypesManifest `json:"members,omitempty"`
	InstanceMembers       map[string]symbolTypesManifest `json:"instance_members,omitempty"`
	Flags                 []string                       `json:"flags,omitempty"`
	Permits               []string                       `json:"permits,omitempty"`
	Visibility            string                         `json:"visibility,omitempty"`
}

type callableTypesManifest struct {
	Params     []string                `json:"params,omitempty"`
	ParamTypes []manifestTypeRef       `json:"param_types,omitempty"`
	Return     string                  `json:"return,omitempty"`
	ReturnType *manifestTypeRef        `json:"return_type,omitempty"`
	Variadic   bool                    `json:"variadic,omitempty"`
	Overloaded bool                    `json:"overloaded,omitempty"`
	Overloads  []callableTypesManifest `json:"overloads,omitempty"`
}

func encodeBundleTypesManifest(projectRoot string, entryPoint string, projectModules []*LoadedModule) ([]byte, error) {
	catalog, err := newProjectManifestCatalog(projectRoot, projectModules)
	if err != nil {
		return nil, err
	}
	manifest := bundleTypesManifest{
		SchemaVersion: manifestSchemaVersion,
		Kind:          "project",
		EntryPoint:    filepath.ToSlash(entryPoint),
		Modules:       make(map[string]moduleTypesManifest),
	}
	for _, loaded := range projectModules {
		if loaded == nil {
			continue
		}
		if IsStdlibModulePath(loaded.Path) {
			continue
		}
		key, module := manifestModuleFromLoaded(projectRoot, loaded, catalog)
		manifest.Modules[key] = module
	}
	return json.MarshalIndent(manifest, "", "  ")
}

func WriteStdlibTypesManifest(w io.Writer) error {
	loadedModules, err := loadStdlibLoadedModules()
	if err != nil {
		return err
	}
	catalog := newBaseManifestCatalog()
	catalog.addLoadedModules("", loadedModules)
	stdlibModules := make(map[string]moduleTypesManifest, len(loadedModules))
	for _, loaded := range loadedModules {
		key, module := manifestModuleFromLoaded("", loaded, catalog)
		module.BundleEntry = ""
		stdlibModules[key] = module
	}
	data, err := json.MarshalIndent(stdlibTypesManifest{
		SchemaVersion: manifestSchemaVersion,
		Kind:          "stdlib",
		Modules:       stdlibModules,
	}, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func WritePrimitiveTypesManifest(w io.Writer) error {
	catalog := newBaseManifestCatalog()
	data, err := json.MarshalIndent(symbolCollectionManifest{
		SchemaVersion: manifestSchemaVersion,
		Kind:          "primitives",
		Symbols:       buildPrimitiveTypesManifest(catalog),
	}, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func WriteRuntimeTypesManifest(w io.Writer) error {
	catalog := newBaseManifestCatalog()
	data, err := json.MarshalIndent(symbolCollectionManifest{
		SchemaVersion: manifestSchemaVersion,
		Kind:          "runtime",
		Symbols:       buildRuntimeTypesManifest(catalog),
	}, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func manifestModuleFromLoaded(projectRoot string, loaded *LoadedModule, catalog manifestTypeCatalog) (string, moduleTypesManifest) {
	key := manifestModuleKey(projectRoot, loaded.Path)
	bundleEntry := ""
	if !IsStdlibModulePath(loaded.Path) {
		entry, err := bundleEntryName(projectRoot, loaded.Path)
		if err == nil {
			bundleEntry = entry
		}
	}
	module := moduleTypesManifest{
		Source:      manifestModuleSource(projectRoot, loaded.Path),
		ImportPath:  manifestImportPath(projectRoot, loaded.Path),
		BundleEntry: bundleEntry,
		Imports:     manifestImportList(projectRoot, loaded.Imports),
		Exports:     make(map[string]symbolTypesManifest),
		Functions:   make(map[string]symbolTypesManifest),
		TextInserts: make(map[string]textInsertManifest),
	}
	for name, exported := range loaded.Exports {
		if exported.TextInsert != "" {
			module.TextInserts[name] = textInsertManifest{
				Kind:       exported.TextInsertKind,
				Name:       name,
				Text:       exported.TextInsert,
				Visibility: string(exported.Visibility),
			}
		}
		if exported.IsNative {
			continue
		}
		symbol := runtimeSpecToManifest(exported.Spec, catalog)
		symbol.Visibility = string(exported.Visibility)
		if symbol.Name == "" {
			symbol.Name = name
		}
		module.Exports[name] = symbol
		if symbol.Callable != nil {
			module.Functions[name] = symbol
		}
	}
	if len(module.Functions) == 0 {
		module.Functions = nil
	}
	if len(module.TextInserts) == 0 {
		module.TextInserts = nil
	}
	if len(module.Imports) == 0 {
		module.Imports = nil
	}
	return key, module
}

func manifestModuleKey(projectRoot string, modulePath string) string {
	if IsStdlibModulePath(modulePath) {
		return stdlibImportPathFromModulePath(modulePath)
	}
	return manifestModuleSource(projectRoot, modulePath)
}

func manifestModuleSource(projectRoot string, modulePath string) string {
	if IsStdlibModulePath(modulePath) {
		return filepath.ToSlash(TrimStdlibModulePath(modulePath))
	}
	relPath, err := filepath.Rel(projectRoot, modulePath)
	if err != nil {
		return filepath.ToSlash(modulePath)
	}
	return filepath.ToSlash(relPath)
}

func manifestImportPath(projectRoot string, modulePath string) string {
	if IsStdlibModulePath(modulePath) {
		return stdlibImportPathFromModulePath(modulePath)
	}
	return strings.TrimSuffix(manifestModuleSource(projectRoot, modulePath), ".pf")
}

func manifestImportList(projectRoot string, imports []string) []string {
	if len(imports) == 0 {
		return nil
	}
	result := make([]string, 0, len(imports))
	for _, imported := range imports {
		result = append(result, manifestImportPath(projectRoot, imported))
	}
	sort.Strings(result)
	return result
}

func stdlibImportPathFromModulePath(modulePath string) string {
	cleanPath := filepath.ToSlash(TrimStdlibModulePath(modulePath))
	return stdlibImportPathFromRelative(cleanPath)
}

func stdlibImportPathFromRelative(relPath string) string {
	clean := strings.TrimSuffix(filepath.ToSlash(relPath), ".pf")
	clean = strings.TrimPrefix(clean, "stdlib/")
	if strings.HasSuffix(clean, "/index") {
		clean = strings.TrimSuffix(clean, "/index")
	} else {
		parts := strings.Split(clean, "/")
		if len(parts) > 1 && parts[len(parts)-1] == parts[len(parts)-2] {
			clean = strings.Join(parts[:len(parts)-1], "/")
		}
	}
	return strings.ReplaceAll(clean, "/", ".")
}

func loadStdlibLoadedModules() ([]*LoadedModule, error) {
	loader, err := newLoader(io.Discard)
	if err != nil {
		return nil, err
	}
	modulePaths, err := collectStdlibModulePaths()
	if err != nil {
		return nil, err
	}
	result := make([]*LoadedModule, 0, len(modulePaths))
	for _, modulePath := range modulePaths {
		loaded, err := loader.loadModule(modulePath)
		if err != nil {
			return nil, err
		}
		result = append(result, loaded)
	}
	return result, nil
}

func collectStdlibModulePaths() ([]string, error) {
	moduleSet := make(map[string]bool)
	err := fs.WalkDir(stdlibFS, "stdlib", func(current string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(current) != ".pf" {
			return nil
		}
		moduleSet[stdlibModulePrefix+current] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	modules := make([]string, 0, len(moduleSet))
	for modulePath := range moduleSet {
		modules = append(modules, modulePath)
	}
	sort.Strings(modules)
	return modules, nil
}

func buildRuntimeTypesManifest(catalog manifestTypeCatalog) map[string]symbolTypesManifest {
	registry := bvmruntime.NewRegistry()
	bvmruntime.InstallCoreGlobals(registry, io.Discard)
	names := make([]string, 0, len(registry.Specs()))
	for name := range registry.Specs() {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make(map[string]symbolTypesManifest, len(names))
	for _, name := range names {
		result[name] = runtimeSpecToManifest(registry.Specs()[name], catalog)
	}
	return result
}

func buildPrimitiveTypesManifest(catalog manifestTypeCatalog) map[string]symbolTypesManifest {
	item := sema.TypeVariable("T", nil)
	key := sema.TypeVariable("K", nil)
	valueType := sema.TypeVariable("V", nil)
	primitives := map[string]sema.Type{
		bvmruntime.TypeAny:    sema.Primitive(bvmruntime.TypeAny),
		bvmruntime.TypeInt:    sema.Primitive(bvmruntime.TypeInt),
		bvmruntime.TypeFloat:  sema.Primitive(bvmruntime.TypeFloat),
		bvmruntime.TypeNumber: sema.Primitive(bvmruntime.TypeNumber),
		bvmruntime.TypeBool:   sema.Primitive(bvmruntime.TypeBool),
		bvmruntime.TypeChar:   sema.Primitive(bvmruntime.TypeChar),
		bvmruntime.TypeString: sema.Primitive(bvmruntime.TypeString),
		bvmruntime.TypeNil:    sema.Primitive(bvmruntime.TypeNil),
		bvmruntime.TypeVoid:   sema.Primitive(bvmruntime.TypeVoid),
		bvmruntime.TypeRange:  sema.Primitive(bvmruntime.TypeRange),
		bvmruntime.TypeTuple:  sema.TupleOf([]sema.Type{item}),
		bvmruntime.TypeArray:  sema.ArrayOf(item),
		bvmruntime.TypeMap:    sema.MapOf(key, valueType),
	}
	names := make([]string, 0, len(primitives))
	for name := range primitives {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make(map[string]symbolTypesManifest, len(names))
	for _, name := range names {
		result[name] = semaTypeToManifest(primitives[name], catalog)
	}
	return result
}

func runtimeSpecToManifest(spec bvmruntime.Spec, catalog manifestTypeCatalog) symbolTypesManifest {
	result := symbolTypesManifest{
		Name:                  spec.Name,
		Type:                  spec.TypeName,
		TypeRef:               catalog.resolve(spec.TypeName),
		TypeParams:            append([]string(nil), spec.TypeParams...),
		ConstructorVisibility: spec.ConstructorVisibility,
		Permits:               append([]string(nil), spec.Permits...),
	}
	switch {
	case spec.Module != nil:
		result.Kind = "module"
	case spec.TypeName == bvmruntime.TypeFunction && spec.Callable != nil:
		result.Kind = "function"
	case spec.IsInterface:
		result.Kind = "interface"
	case spec.ConstructorVisibility != "" || len(spec.ConstructorOverloads) > 0 || len(spec.InstanceMembers) > 0:
		result.Kind = "class"
	case spec.Callable != nil:
		result.Kind = "function"
	default:
		result.Kind = "value"
	}
	if spec.Callable != nil {
		result.Callable = callableSpecToManifest(spec.Callable, catalog)
	}
	if len(spec.ConstructorOverloads) > 0 {
		result.Constructors = make([]callableTypesManifest, 0, len(spec.ConstructorOverloads))
		for _, callable := range spec.ConstructorOverloads {
			if callable == nil {
				continue
			}
			result.Constructors = append(result.Constructors, *callableSpecToManifest(callable, catalog))
		}
	}
	if len(spec.Members) > 0 {
		result.Members = make(map[string]symbolTypesManifest, len(spec.Members))
		for name, member := range spec.Members {
			result.Members[name] = shallowRuntimeSpecToManifest(member, catalog)
		}
	}
	if len(spec.InstanceMembers) > 0 {
		result.InstanceMembers = make(map[string]symbolTypesManifest, len(spec.InstanceMembers))
		for name, member := range spec.InstanceMembers {
			result.InstanceMembers[name] = shallowRuntimeSpecToManifest(member, catalog)
		}
	}
	flags := make([]string, 0, 4)
	if spec.IsAbstract {
		flags = append(flags, "abstract")
	}
	if spec.IsSealed {
		flags = append(flags, "sealed")
	}
	if spec.IsInterface {
		flags = append(flags, "interface")
	}
	if spec.IsRecord {
		flags = append(flags, "record")
	}
	if len(flags) > 0 {
		result.Flags = flags
	}
	return result
}

func shallowRuntimeSpecToManifest(spec bvmruntime.Spec, catalog manifestTypeCatalog) symbolTypesManifest {
	result := symbolTypesManifest{
		Name:                  spec.Name,
		Type:                  spec.TypeName,
		TypeRef:               catalog.resolve(spec.TypeName),
		TypeParams:            append([]string(nil), spec.TypeParams...),
		ConstructorVisibility: spec.ConstructorVisibility,
		Permits:               append([]string(nil), spec.Permits...),
	}
	switch {
	case spec.Module != nil:
		result.Kind = "module"
	case spec.TypeName == bvmruntime.TypeFunction && spec.Callable != nil:
		result.Kind = "function"
	case spec.IsInterface:
		result.Kind = "interface"
	case spec.ConstructorVisibility != "" || len(spec.ConstructorOverloads) > 0 || len(spec.InstanceMembers) > 0:
		result.Kind = "class"
	case spec.Callable != nil:
		result.Kind = "function"
	default:
		result.Kind = "value"
	}
	if spec.Callable != nil {
		result.Callable = callableSpecToManifest(spec.Callable, catalog)
	}
	if len(spec.ConstructorOverloads) > 0 {
		result.Constructors = make([]callableTypesManifest, 0, len(spec.ConstructorOverloads))
		for _, callable := range spec.ConstructorOverloads {
			if callable == nil {
				continue
			}
			result.Constructors = append(result.Constructors, *callableSpecToManifest(callable, catalog))
		}
	}
	flags := make([]string, 0, 4)
	if spec.IsAbstract {
		flags = append(flags, "abstract")
	}
	if spec.IsSealed {
		flags = append(flags, "sealed")
	}
	if spec.IsInterface {
		flags = append(flags, "interface")
	}
	if spec.IsRecord {
		flags = append(flags, "record")
	}
	if len(flags) > 0 {
		result.Flags = flags
	}
	return result
}

func callableSpecToManifest(spec *bvmruntime.CallableSpec, catalog manifestTypeCatalog) *callableTypesManifest {
	if spec == nil {
		return nil
	}
	paramTypes := make([]manifestTypeRef, 0, len(spec.Params))
	for _, param := range spec.Params {
		resolved := catalog.resolve(param)
		if resolved == nil {
			resolved = &manifestTypeRef{Display: param, Kind: "unknown", Name: param}
		}
		paramTypes = append(paramTypes, *resolved)
	}
	manifest := &callableTypesManifest{
		Params:     append([]string(nil), spec.Params...),
		ParamTypes: paramTypes,
		Return:     spec.Return,
		ReturnType: catalog.resolve(spec.Return),
		Variadic:   spec.Variadic,
		Overloaded: spec.Overloaded,
	}
	if len(spec.Overloads) > 0 {
		manifest.Overloads = make([]callableTypesManifest, 0, len(spec.Overloads))
		for _, overload := range spec.Overloads {
			if overload == nil {
				continue
			}
			converted := callableSpecToManifest(overload, catalog)
			if converted == nil {
				continue
			}
			manifest.Overloads = append(manifest.Overloads, *converted)
		}
	}
	return manifest
}

func semaTypeToManifest(t sema.Type, catalog manifestTypeCatalog) symbolTypesManifest {
	display := sema.DisplayName(t)
	result := symbolTypesManifest{
		Name:       t.Name,
		Type:       display,
		TypeRef:    catalog.resolve(display),
		TypeParams: semaTypeParams(t),
	}
	if t.Callable != nil {
		result.Kind = "function"
		result.Callable = &callableTypesManifest{
			Params:     semaParamNames(t.Callable.Params),
			ParamTypes: semaParamRefs(t.Callable.Params, catalog),
			Return:     sema.DisplayName(t.Callable.Return),
			ReturnType: catalog.resolve(sema.DisplayName(t.Callable.Return)),
			Variadic:   t.Callable.Variadic,
			Overloaded: t.Callable.Overloaded,
		}
		if len(t.CallOverloads) > 0 {
			result.Callable.Overloads = make([]callableTypesManifest, 0, len(t.CallOverloads))
			for _, overload := range t.CallOverloads {
				if overload == nil {
					continue
				}
				result.Callable.Overloads = append(result.Callable.Overloads, callableTypesManifest{
					Params:     semaParamNames(overload.Params),
					ParamTypes: semaParamRefs(overload.Params, catalog),
					Return:     sema.DisplayName(overload.Return),
					ReturnType: catalog.resolve(sema.DisplayName(overload.Return)),
					Variadic:   overload.Variadic,
					Overloaded: overload.Overloaded,
				})
			}
		}
	} else {
		result.Kind = "primitive"
	}
	if len(t.Members) > 0 {
		result.Members = make(map[string]symbolTypesManifest, len(t.Members))
		for name, member := range t.Members {
			result.Members[name] = shallowSemaTypeToManifest(member, catalog)
		}
	}
	flags := make([]string, 0, 4)
	if t.IsAbstract {
		flags = append(flags, "abstract")
	}
	if t.IsSealed {
		flags = append(flags, "sealed")
	}
	if t.IsInterface {
		flags = append(flags, "interface")
	}
	if t.IsRecord {
		flags = append(flags, "record")
	}
	if len(flags) > 0 {
		result.Flags = flags
	}
	return result
}

func shallowSemaTypeToManifest(t sema.Type, catalog manifestTypeCatalog) symbolTypesManifest {
	display := sema.DisplayName(t)
	result := symbolTypesManifest{
		Name:       t.Name,
		Type:       display,
		TypeRef:    catalog.resolve(display),
		TypeParams: semaTypeParams(t),
	}
	if t.Callable != nil {
		result.Kind = "function"
		result.Callable = &callableTypesManifest{
			Params:     semaParamNames(t.Callable.Params),
			ParamTypes: semaParamRefs(t.Callable.Params, catalog),
			Return:     sema.DisplayName(t.Callable.Return),
			ReturnType: catalog.resolve(sema.DisplayName(t.Callable.Return)),
			Variadic:   t.Callable.Variadic,
			Overloaded: t.Callable.Overloaded,
		}
		if len(t.CallOverloads) > 0 {
			result.Callable.Overloads = make([]callableTypesManifest, 0, len(t.CallOverloads))
			for _, overload := range t.CallOverloads {
				if overload == nil {
					continue
				}
				result.Callable.Overloads = append(result.Callable.Overloads, callableTypesManifest{
					Params:     semaParamNames(overload.Params),
					ParamTypes: semaParamRefs(overload.Params, catalog),
					Return:     sema.DisplayName(overload.Return),
					ReturnType: catalog.resolve(sema.DisplayName(overload.Return)),
					Variadic:   overload.Variadic,
					Overloaded: overload.Overloaded,
				})
			}
		}
	} else {
		result.Kind = "primitive"
	}
	flags := make([]string, 0, 4)
	if t.IsAbstract {
		flags = append(flags, "abstract")
	}
	if t.IsSealed {
		flags = append(flags, "sealed")
	}
	if t.IsInterface {
		flags = append(flags, "interface")
	}
	if t.IsRecord {
		flags = append(flags, "record")
	}
	if len(flags) > 0 {
		result.Flags = flags
	}
	return result
}

func semaParamRefs(params []sema.Type, catalog manifestTypeCatalog) []manifestTypeRef {
	if len(params) == 0 {
		return nil
	}
	result := make([]manifestTypeRef, 0, len(params))
	for _, param := range params {
		display := sema.DisplayName(param)
		resolved := catalog.resolve(display)
		if resolved == nil {
			resolved = &manifestTypeRef{Display: display, Kind: "unknown", Name: display}
		}
		result = append(result, *resolved)
	}
	return result
}

func newBaseManifestCatalog() manifestTypeCatalog {
	catalog := manifestTypeCatalog{origins: make(map[string][]manifestTypeOrigin)}
	for _, primitive := range []string{
		bvmruntime.TypeAny,
		bvmruntime.TypeInt,
		bvmruntime.TypeFloat,
		bvmruntime.TypeNumber,
		bvmruntime.TypeBool,
		bvmruntime.TypeChar,
		bvmruntime.TypeString,
		bvmruntime.TypeNil,
		bvmruntime.TypeVoid,
		bvmruntime.TypeRange,
		bvmruntime.TypeTuple,
		bvmruntime.TypeArray,
		bvmruntime.TypeMap,
	} {
		catalog.add(primitive, "primitives", primitive)
	}
	for _, runtimeType := range []string{bvmruntime.TypeFunction, bvmruntime.TypeModule} {
		catalog.add(runtimeType, "runtime", runtimeType)
	}
	return catalog
}

func newProjectManifestCatalog(projectRoot string, projectModules []*LoadedModule) (manifestTypeCatalog, error) {
	catalog := newBaseManifestCatalog()
	stdlibModules, err := loadStdlibLoadedModules()
	if err != nil {
		return manifestTypeCatalog{}, err
	}
	catalog.addLoadedModules("", stdlibModules)
	catalog.addLoadedModules(projectRoot, projectModules)
	return catalog, nil
}

func (c *manifestTypeCatalog) addLoadedModules(projectRoot string, modules []*LoadedModule) {
	for _, loaded := range modules {
		if loaded == nil {
			continue
		}
		moduleImportPath := manifestImportPath(projectRoot, loaded.Path)
		for exportName := range loaded.Exports {
			c.add(exportName, moduleImportPath, exportName)
		}
		baseName := path.Base(moduleImportPath)
		if baseName != "" && baseName != moduleImportPath {
			c.add(baseName, moduleImportPath, baseName)
		}
	}
}

func (c *manifestTypeCatalog) add(name string, module string, symbol string) {
	if name == "" || module == "" || symbol == "" {
		return
	}
	origins := c.origins[name]
	for _, existing := range origins {
		if existing.Module == module && existing.Symbol == symbol {
			return
		}
	}
	c.origins[name] = append(origins, manifestTypeOrigin{Module: module, Symbol: symbol})
}

func (c manifestTypeCatalog) resolve(display string) *manifestTypeRef {
	display = strings.TrimSpace(display)
	if display == "" {
		return nil
	}
	parts := splitTopLevel(display, '|')
	if len(parts) > 1 {
		resolved := &manifestTypeRef{Display: display, Kind: "union", Options: make([]manifestTypeRef, 0, len(parts))}
		for _, part := range parts {
			child := c.resolve(part)
			if child == nil {
				child = &manifestTypeRef{Display: strings.TrimSpace(part), Kind: "unknown", Name: strings.TrimSpace(part)}
			}
			resolved.Options = append(resolved.Options, *child)
		}
		return resolved
	}
	base, argString, hasArgs := splitGeneric(display)
	resolved := &manifestTypeRef{Display: display, Name: base}
	if strings.HasPrefix(base, "?") {
		resolved.Kind = "wildcard"
	} else if origins := c.origins[base]; len(origins) == 1 {
		resolved.Kind = "symbol"
		resolved.Module = origins[0].Module
		resolved.Symbol = origins[0].Symbol
	} else if len(origins) == 0 {
		resolved.Kind = "unknown"
	} else {
		resolved.Kind = "symbol"
	}
	if hasArgs {
		args := splitTopLevel(argString, ',')
		resolved.Args = make([]manifestTypeRef, 0, len(args))
		for _, arg := range args {
			child := c.resolve(arg)
			if child == nil {
				trimmed := strings.TrimSpace(arg)
				child = &manifestTypeRef{Display: trimmed, Kind: "unknown", Name: trimmed}
			}
			resolved.Args = append(resolved.Args, *child)
		}
	}
	if resolved.Kind == "unknown" {
		if _, ok := c.origins[base]; ok {
			resolved.Kind = "symbol"
		}
	}
	return resolved
}

func splitGeneric(display string) (string, string, bool) {
	trimmed := strings.TrimSpace(display)
	depth := 0
	start := -1
	for i, ch := range trimmed {
		switch ch {
		case '<':
			if depth == 0 {
				start = i
			}
			depth++
		case '>':
			depth--
			if depth == 0 && start >= 0 && i == len(trimmed)-1 {
				return strings.TrimSpace(trimmed[:start]), trimmed[start+1 : i], true
			}
		}
	}
	return trimmed, "", false
}

func splitTopLevel(input string, separator rune) []string {
	parts := make([]string, 0, 2)
	depth := 0
	start := 0
	for i, ch := range input {
		switch ch {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if ch == separator && depth == 0 {
				parts = append(parts, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(input[start:]))
	filtered := parts[:0]
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func init() {
	if manifestSchemaVersion < 2 {
		panic(fmt.Sprintf("invalid manifest schema version %d", manifestSchemaVersion))
	}
}

func semaTypeParams(t sema.Type) []string {
	if len(t.Args) == 0 {
		return nil
	}
	params := make([]string, 0, len(t.Args))
	for _, arg := range t.Args {
		params = append(params, sema.DisplayName(arg))
	}
	return params
}

func semaParamNames(params []sema.Type) []string {
	if len(params) == 0 {
		return nil
	}
	result := make([]string, 0, len(params))
	for _, param := range params {
		result = append(result, sema.DisplayName(param))
	}
	return result
}

func writeBundleTypesManifest(w *zip.Writer, projectRoot string, entryPoint string, projectModules []*LoadedModule) error {
	data, err := encodeBundleTypesManifest(projectRoot, entryPoint, projectModules)
	if err != nil {
		return err
	}
	typesFile, err := w.Create(classfile.BundleTypesPath)
	if err != nil {
		return err
	}
	_, err = typesFile.Write(data)
	return err
}
