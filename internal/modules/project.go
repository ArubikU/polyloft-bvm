package modules

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArubikU/polyloft-bvm/internal/ast"
	"github.com/ArubikU/polyloft-bvm/internal/bytecode"
	"github.com/ArubikU/polyloft-bvm/internal/classfile"
	"github.com/ArubikU/polyloft-bvm/internal/compiler"
	"github.com/ArubikU/polyloft-bvm/internal/diagnostic"
	bvmruntime "github.com/ArubikU/polyloft-bvm/internal/runtime"
	"github.com/ArubikU/polyloft-bvm/internal/sema"
	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type ProjectConfig struct {
	RootDir      string
	EntryPoint   string
	Dependencies map[string]bool
}

type serializedModuleMetadata struct {
	Exports map[string]serializedExport `json:"exports,omitempty"`
	Imports []string                    `json:"imports,omitempty"`
}

type serializedExport struct {
	Visibility ast.Visibility `json:"visibility"`
}

func ResolveSourceEntry(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() || strings.EqualFold(filepath.Base(absPath), "polyloft.toml") {
		project, err := ResolveProjectConfig(absPath)
		if err != nil {
			return "", err
		}
		return filepath.Join(project.RootDir, filepath.FromSlash(project.EntryPoint)), nil
	}
	return absPath, nil
}

func ResolveProjectConfig(path string) (*ProjectConfig, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	configPath := absPath
	if info.IsDir() {
		configPath = filepath.Join(absPath, "polyloft.toml")
	}
	if !strings.EqualFold(filepath.Base(configPath), "polyloft.toml") {
		return nil, fmt.Errorf("project path must be a directory or polyloft.toml: %s", path)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	return resolveProjectConfigData(filepath.Dir(configPath), configPath, data)
}

func resolveProjectConfigData(rootDir string, configPath string, data []byte) (*ProjectConfig, error) {
	entryPoint := "main.pf"
	dependencies := make(map[string]bool)
	inDependency := false
	currentDependencyName := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "[[dependencies.pf]]" {
			inDependency = true
			currentDependencyName = ""
			continue
		}
		if !strings.HasPrefix(trimmed, "entry_point") {
			if !inDependency {
				continue
			}
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"`)
			switch key {
			case "name":
				currentDependencyName = value
				if _, ok := dependencies[currentDependencyName]; !ok {
					dependencies[currentDependencyName] = false
				}
			case "include":
				if currentDependencyName != "" {
					dependencies[currentDependencyName] = strings.EqualFold(value, "true")
				}
			}
			continue
		}
		inDependency = false
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		entryPoint = strings.TrimSpace(parts[1])
		entryPoint = strings.Trim(entryPoint, `"`)
		break
	}
	if entryPoint == "" {
		return nil, fmt.Errorf("project config %s has empty entry_point", configPath)
	}
	return &ProjectConfig{RootDir: rootDir, EntryPoint: filepath.ToSlash(entryPoint), Dependencies: dependencies}, nil
}

func CompileSource(path string, stdout io.Writer) (*bytecode.Function, *bvmruntime.Registry, error) {
	entryPath, err := ResolveSourceEntry(path)
	if err != nil {
		return nil, nil, err
	}
	program, registry, err := Prepare(entryPath, stdout)
	if err != nil {
		return nil, nil, err
	}
	source := readDiagnosticSource(entryPath)
	if err := sema.Check(program, registry); err != nil {
		return nil, nil, diagnostic.Wrap(err, diagnostic.KindCheck, entryPath, source)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		return nil, nil, diagnostic.Wrap(err, diagnostic.KindCompile, entryPath, source)
	}
	return fn, registry, nil
}

func CheckSource(path string, stdout io.Writer) error {
	entryPath, err := ResolveSourceEntry(path)
	if err != nil {
		return err
	}
	program, registry, err := Prepare(entryPath, stdout)
	if err != nil {
		return err
	}
	source := readDiagnosticSource(entryPath)
	if err := sema.Check(program, registry); err != nil {
		return diagnostic.Wrap(err, diagnostic.KindCheck, entryPath, source)
	}
	return nil
}

func CompileInlineSource(logicalPath string, source string, stdout io.Writer) (*bytecode.Function, *bvmruntime.Registry, error) {
	inlinePath, err := normalizeInlinePath(logicalPath)
	if err != nil {
		return nil, nil, err
	}
	loader, err := newLoader(stdout)
	if err != nil {
		return nil, nil, err
	}
	program, registry, err := loader.PrepareSource(inlinePath, source)
	if err != nil {
		return nil, nil, err
	}
	if err := sema.Check(program, registry); err != nil {
		return nil, nil, diagnostic.Wrap(err, diagnostic.KindCheck, inlinePath, source)
	}
	fn, err := compiler.CompileWithRegistry(program, registry)
	if err != nil {
		return nil, nil, diagnostic.Wrap(err, diagnostic.KindCompile, inlinePath, source)
	}
	return fn, registry, nil
}

func CheckInlineSource(logicalPath string, source string, stdout io.Writer) error {
	inlinePath, err := normalizeInlinePath(logicalPath)
	if err != nil {
		return err
	}
	loader, err := newLoader(stdout)
	if err != nil {
		return err
	}
	program, registry, err := loader.PrepareSource(inlinePath, source)
	if err != nil {
		return err
	}
	if err := sema.Check(program, registry); err != nil {
		return diagnostic.Wrap(err, diagnostic.KindCheck, inlinePath, source)
	}
	return nil
}

func readDiagnosticSource(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func normalizeInlinePath(logicalPath string) (string, error) {
	if strings.TrimSpace(logicalPath) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, "__inline__.pf"), nil
	}
	if filepath.IsAbs(logicalPath) {
		return filepath.Clean(logicalPath), nil
	}
	return filepath.Abs(logicalPath)
}

func CompileModule(path string, stdout io.Writer) (*LoadedModule, error) {
	entryPath, err := ResolveSourceEntry(path)
	if err != nil {
		return nil, err
	}
	loader, err := newLoader(stdout)
	if err != nil {
		return nil, err
	}
	return loader.loadModule(entryPath)
}

func WriteCompiledModule(path string, stdout io.Writer, w io.Writer) error {
	loaded, err := CompileModule(path, stdout)
	if err != nil {
		return err
	}
	metadata, err := encodeSerializedMetadata(loaded.Exports, loaded.Imports)
	if err != nil {
		return err
	}
	return classfile.WriteModule(w, loaded.Function, metadata)
}

func LoadCompiledModule(path string) (*bytecode.Function, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fn, _, err := classfile.ReadModule(f)
	if err != nil {
		return nil, err
	}
	return fn, nil
}

func LoadBundle(path string, stdout io.Writer) (*bytecode.Function, *bvmruntime.Registry, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	reader, err := zip.OpenReader(absPath)
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	var project *ProjectConfig
	for _, file := range reader.File {
		if file.Name != classfile.BundleProjectConfigPath {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, nil, err
		}
		parsed, err := resolveProjectConfigData("", classfile.BundleProjectConfigPath, data)
		if err != nil {
			return nil, nil, err
		}
		project = parsed
		break
	}
	if project == nil {
		for _, file := range reader.File {
			if file.Name != classfile.BundleManifestPath {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				return nil, nil, err
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, nil, err
			}
			manifest, err := classfile.UnmarshalBundleManifest(data)
			if err != nil {
				return nil, nil, err
			}
			project = &ProjectConfig{RootDir: "", EntryPoint: manifest.EntryPoint, Dependencies: map[string]bool{}}
			break
		}
	}
	if project == nil {
		return nil, nil, fmt.Errorf("bundle %s is missing %s", path, classfile.BundleProjectConfigPath)
	}
	loader, err := newLoader(stdout)
	if err != nil {
		return nil, nil, err
	}
	loader.Archive = reader.File
	return loader.PrepareFromBundle(project.EntryPoint)
}

func BuildProjectBundle(path string, stdout io.Writer, w io.Writer) error {
	project, err := ResolveProjectConfig(path)
	if err != nil {
		return err
	}
	loader, err := newLoader(stdout)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(w)
	projectConfigBytes, err := os.ReadFile(filepath.Join(project.RootDir, classfile.BundleProjectConfigPath))
	if err != nil {
		return err
	}
	projectConfigFile, err := writer.Create(classfile.BundleProjectConfigPath)
	if err != nil {
		return err
	}
	if _, err := projectConfigFile.Write(projectConfigBytes); err != nil {
		return err
	}
	moduleFiles, err := collectProjectModules(project.RootDir)
	if err != nil {
		return err
	}
	loadedProjectModules := make([]*LoadedModule, 0, len(moduleFiles))
	for _, modulePath := range moduleFiles {
		loaded, err := loader.loadModule(modulePath)
		if err != nil {
			return err
		}
		loadedProjectModules = append(loadedProjectModules, loaded)
		metadata, err := encodeSerializedMetadata(loaded.Exports, loaded.Imports)
		if err != nil {
			return err
		}
		entryName, err := bundleEntryName(project.RootDir, modulePath)
		if err != nil {
			return err
		}
		moduleFile, err := writer.Create(entryName)
		if err != nil {
			return err
		}
		if err := classfile.WriteModule(moduleFile, loaded.Function, metadata); err != nil {
			return err
		}
	}
	if err := writeBundleTypesManifest(writer, project.RootDir, project.EntryPoint, loadedProjectModules); err != nil {
		return err
	}
	return writer.Close()
}

func newLoader(stdout io.Writer) (*Loader, error) {
	workspaceDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &Loader{
		Stdout:       stdout,
		WorkspaceDir: workspaceDir,
		Cache:        make(map[string]*LoadedModule),
		Loading:      make(map[string]bool),
	}, nil
}

func collectProjectModules(rootDir string) ([]string, error) {
	modules := make([]string, 0)
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".pf" {
			return nil
		}
		modules = append(modules, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(modules)
	return modules, nil
}

func bundleEntryName(projectRoot string, modulePath string) (string, error) {
	if IsStdlibModulePath(modulePath) {
		return strings.TrimSuffix(filepath.ToSlash(TrimStdlibModulePath(modulePath)), ".pf") + ".pfbc", nil
	}
	relPath, err := filepath.Rel(projectRoot, modulePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(filepath.ToSlash(relPath), ".pf") + ".pfbc", nil
}

func collectBundledStdlibModules(loader *Loader, rootModules []string) ([]string, error) {
	stdlibModules := make([]string, 0)
	seen := make(map[string]bool)
	var visit func(string) error
	visit = func(modulePath string) error {
		loaded, err := loader.loadModule(modulePath)
		if err != nil {
			return err
		}
		for _, imported := range loaded.Imports {
			if !IsStdlibModulePath(imported) || seen[imported] {
				continue
			}
			seen[imported] = true
			stdlibModules = append(stdlibModules, imported)
			if err := visit(imported); err != nil {
				return err
			}
		}
		return nil
	}
	for _, modulePath := range rootModules {
		if err := visit(modulePath); err != nil {
			return nil, err
		}
	}
	sort.Strings(stdlibModules)
	return stdlibModules, nil
}

func encodeSerializedMetadata(exports map[string]ExportMetadata, imports []string) ([]byte, error) {
	metadata := serializedModuleMetadata{Imports: imports}
	if len(exports) > 0 {
		metadata.Exports = make(map[string]serializedExport, len(exports))
	}
	for name, exported := range exports {
		metadata.Exports[name] = serializedExport{Visibility: exported.Visibility}
	}
	return json.Marshal(metadata)
}

func decodeSerializedMetadata(data []byte) (map[string]ExportMetadata, []string, error) {
	if len(data) == 0 {
		return map[string]ExportMetadata{}, nil, nil
	}
	var metadata serializedModuleMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, nil, err
	}
	exports := make(map[string]ExportMetadata, len(metadata.Exports))
	for name, exported := range metadata.Exports {
		exports[name] = ExportMetadata{Value: value.NilValue(), Spec: bvmruntime.Spec{Name: name}, Visibility: exported.Visibility}
	}
	return exports, metadata.Imports, nil
}

func decodeSerializedExports(data []byte) (map[string]ExportMetadata, error) {
	exports, _, err := decodeSerializedMetadata(data)
	return exports, err
}

func decodeSerializedImports(data []byte) []string {
	_, imports, err := decodeSerializedMetadata(data)
	if err != nil {
		return nil
	}
	return imports
}
