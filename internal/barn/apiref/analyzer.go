package apiref

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/MontFerret/specs/pkg/api"
	"golang.org/x/tools/go/packages"
)

const (
	sdkPackagePath     = "github.com/MontFerret/ferret/v2/pkg/sdk"
	modulePackagePath  = "github.com/MontFerret/ferret/v2/pkg/module"
	runtimePackagePath = "github.com/MontFerret/ferret/v2/pkg/runtime"
)

// Analyzer loads and analyzes one pinned module source tree without executing it.
type Analyzer struct{}

// Analyze derives a complete API Reference from the Go module rooted at directory.
func (Analyzer) Analyze(ctx context.Context, repositoryRoot, directory, packagePath, moduleID, version string) (_ *api.Reference, resultErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			resultErr = &AnalysisError{
				Kind:     ErrorInternal,
				ModuleID: moduleID,
				Version:  version,
				Err:      fmt.Errorf("unexpected analyzer panic: %v", recovered),
			}
		}
	}()

	source, err := loadSource(ctx, repositoryRoot, directory, packagePath, moduleID, version)
	if err != nil {
		return nil, err
	}

	constructor := source.root.Types.Scope().Lookup("New")
	constructorFunc, ok := constructor.(*types.Func)
	if !ok {
		return nil, source.errorAt(ErrorDeclarationNotFound, token.NoPos, "root package does not declare a New constructor")
	}

	constructorNode, exists := source.functions[constructorFunc]
	if !exists || constructorNode.decl.Body == nil {
		return nil, source.errorAt(ErrorDeclarationNotFound, constructorFunc.Pos(), "New constructor source is unavailable")
	}

	builder := &resultBuilder{namespaces: make(map[string]map[string]map[string]signatureRecord)}
	wlkr := &walker{
		source: source,
		result: builder,
		stack:  make(map[*types.Func]bool),
	}

	if err := wlkr.walkConstructor(constructorNode); err != nil {
		return nil, err
	}

	reference := builder.reference(moduleID, version)
	if err := api.Validate(reference); err != nil {
		return nil, source.errorAt(ErrorInternal, constructorFunc.Pos(), fmt.Sprintf("derived API Reference is invalid: %v", err))
	}

	return reference, nil
}

func loadSource(ctx context.Context, repositoryRoot, directory, packagePath, moduleID, version string) (*loadedSource, error) {
	absoluteRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return nil, &AnalysisError{Kind: ErrorInternal, ModuleID: moduleID, Version: version, Err: fmt.Errorf("resolve repository root: %w", err)}
	}

	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return nil, &AnalysisError{Kind: ErrorInternal, ModuleID: moduleID, Version: version, Err: fmt.Errorf("resolve module directory: %w", err)}
	}

	if !pathWithin(absoluteRoot, absoluteDirectory) {
		return nil, &AnalysisError{Kind: ErrorInvalidPackage, ModuleID: moduleID, Version: version, Err: fmt.Errorf("module directory escapes the materialized repository")}
	}

	cacheRoot := filepath.Join(filepath.Dir(absoluteRoot), "analysis-cache")

	for _, dir := range []string{
		filepath.Join(cacheRoot, "build"),
		filepath.Join(cacheRoot, "home"),
		filepath.Join(cacheRoot, "modules"),
		filepath.Join(cacheRoot, "tmp"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, &AnalysisError{Kind: ErrorInternal, ModuleID: moduleID, Version: version, Err: fmt.Errorf("create analysis cache: %w", err)}
		}
	}

	config := &packages.Config{
		Context: ctx,
		Dir:     absoluteDirectory,
		Mode:    packages.LoadSyntax | packages.NeedModule,
		Tests:   false,
		Env:     analysisEnvironment(os.Environ(), cacheRoot),
	}

	loaded, loadErr := packages.Load(config, "./...")
	if loadErr != nil {
		return nil, &AnalysisError{Kind: ErrorInvalidPackage, ModuleID: moduleID, Version: version, Err: fmt.Errorf("load Go packages: %w", loadErr)}
	}

	if len(loaded) == 0 {
		return nil, &AnalysisError{Kind: ErrorInvalidPackage, ModuleID: moduleID, Version: version, Err: fmt.Errorf("go module contains no packages")}
	}

	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			packageError := pkg.Errors[0]

			for _, candidate := range pkg.Errors[1:] {
				if packageError.Pos == "" && candidate.Pos != "" {
					packageError = candidate
				}
			}

			return nil, &AnalysisError{
				Kind:     ErrorInvalidPackage,
				ModuleID: moduleID,
				Version:  version,
				Position: normalizePosition(parsePackagePosition(packageError.Pos), absoluteRoot),
				Err:      fmt.Errorf("load package %s: %s", pkg.PkgPath, packageError.Msg),
			}
		}
	}

	source := &loadedSource{
		fset:           loaded[0].Fset,
		packages:       loaded,
		functions:      make(map[*types.Func]functionNode),
		values:         make(map[types.Object]valueNode),
		repositoryRoot: absoluteRoot,
		moduleID:       moduleID,
		version:        version,
	}

	for _, pkg := range loaded {
		if pkg.PkgPath == packagePath {
			source.root = pkg
		}

		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				if general, ok := declaration.(*ast.GenDecl); ok {
					for _, specification := range general.Specs {
						value, ok := specification.(*ast.ValueSpec)
						if !ok {
							continue
						}

						for index, name := range value.Names {
							if index >= len(value.Values) {
								continue
							}

							if object := pkg.TypesInfo.Defs[name]; object != nil {
								source.values[object] = valueNode{pkg: pkg, expr: value.Values[index]}
							}
						}
					}
				}

				function, ok := declaration.(*ast.FuncDecl)
				if !ok {
					continue
				}

				object, _ := pkg.TypesInfo.Defs[function.Name].(*types.Func)
				if object != nil {
					source.functions[object] = functionNode{pkg: pkg, decl: function}
				}
			}
		}
	}

	if source.root == nil {
		return nil, &AnalysisError{Kind: ErrorInvalidPackage, ModuleID: moduleID, Version: version, Err: fmt.Errorf("module root package %q was not loaded", packagePath)}
	}

	if source.root.Module == nil || source.root.Module.Path == "" {
		return nil, &AnalysisError{Kind: ErrorInvalidPackage, ModuleID: moduleID, Version: version, Err: fmt.Errorf("module root package has no Go module metadata")}
	}

	source.modulePath = source.root.Module.Path

	return source, nil
}

func analysisEnvironment(environment []string, cacheRoot string) []string {
	blocked := map[string]struct{}{
		"ALL_PROXY": {}, "CGO_ENABLED": {}, "GO111MODULE": {}, "GOAMD64": {}, "GOARCH": {}, "GOAUTH": {},
		"GOCACHE": {}, "GODEBUG": {}, "GOENV": {}, "GOEXPERIMENT": {}, "GOFLAGS": {},
		"GOFIPS140": {}, "GOINSECURE": {}, "GOMODCACHE": {}, "GONOPROXY": {}, "GONOSUMDB": {},
		"GOPATH": {}, "GOPRIVATE": {}, "GOPROXY": {}, "GOSUMDB": {}, "GOTOOLCHAIN": {},
		"GOTMPDIR": {}, "GOVCS": {}, "GOWORK": {}, "HOME": {}, "HTTP_PROXY": {},
		"HTTPS_PROXY": {}, "NO_PROXY": {}, "TMPDIR": {},
	}

	cleaned := make([]string, 0, len(environment)+13)

	for _, value := range environment {
		name, _, _ := strings.Cut(value, "=")
		if _, exists := blocked[strings.ToUpper(name)]; !exists {
			cleaned = append(cleaned, value)
		}
	}

	return append(cleaned,
		"CGO_ENABLED=0",
		"GOARCH=amd64",
		"GOAUTH=off",
		"GOCACHE="+filepath.Join(cacheRoot, "build"),
		"GOENV=off",
		"GOFLAGS=-mod=readonly -buildvcs=false",
		"GOAMD64=v1",
		"GOMODCACHE="+filepath.Join(cacheRoot, "modules"),
		"GONOPROXY=",
		"GONOSUMDB=",
		"GOPATH="+filepath.Join(cacheRoot, "gopath"),
		"GOPRIVATE=",
		"GOPROXY=https://proxy.golang.org",
		"GOSUMDB=sum.golang.org",
		"GOTOOLCHAIN=local",
		"GOTMPDIR="+filepath.Join(cacheRoot, "tmp"),
		"GOVCS=*:off",
		"GOOS=linux",
		"GOWORK=off",
		"HOME="+filepath.Join(cacheRoot, "home"),
		"TMPDIR="+filepath.Join(cacheRoot, "tmp"),
	)
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)

	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func parsePackagePosition(value string) token.Position {
	lastColon := strings.LastIndexByte(value, ':')
	if lastColon < 0 {
		return token.Position{}
	}

	column, columnErr := strconv.Atoi(value[lastColon+1:])
	remaining := value[:lastColon]
	lineColon := strings.LastIndexByte(remaining, ':')

	if lineColon < 0 {
		return token.Position{}
	}

	line, lineErr := strconv.Atoi(remaining[lineColon+1:])
	if lineErr != nil || columnErr != nil || line < 1 || column < 1 {
		return token.Position{}
	}

	return token.Position{Filename: remaining[:lineColon], Line: line, Column: column}
}

func normalizePosition(position token.Position, repositoryRoot string) token.Position {
	if !position.IsValid() || position.Filename == "" {
		return position
	}

	filename, err := filepath.Abs(position.Filename)
	if err != nil || !pathWithin(repositoryRoot, filename) {
		return position
	}

	relative, err := filepath.Rel(repositoryRoot, filename)
	if err == nil {
		position.Filename = filepath.ToSlash(relative)
	}

	return position
}

func (source *loadedSource) errorAt(kind ErrorKind, position token.Pos, message string) error {
	return &AnalysisError{
		Kind:     kind,
		ModuleID: source.moduleID,
		Version:  source.version,
		Position: normalizePosition(source.fset.Position(position), source.repositoryRoot),
		Err:      fmt.Errorf("%s", message),
	}
}

func (builder *resultBuilder) add(namespace, name string, signature signatureRecord) error {
	functions, exists := builder.namespaces[namespace]
	if !exists {
		functions = make(map[string]map[string]signatureRecord)
		builder.namespaces[namespace] = functions
	}

	signatures, exists := functions[name]
	if !exists {
		signatures = make(map[string]signatureRecord)
		functions[name] = signatures
	}

	key := strconv.Itoa(len(signature.parameters))
	if signature.variadic {
		key = "variadic"
	}

	if existing, exists := signatures[key]; exists {
		if reflect.DeepEqual(existing, signature) {
			return nil
		}

		return fmt.Errorf("function %s::%s has ambiguous %s registrations", namespace, name, key)
	}

	signatures[key] = signature

	return nil
}

func (builder *resultBuilder) reference(moduleID, version string) *api.Reference {
	namespaceNames := make([]string, 0, len(builder.namespaces))

	for name := range builder.namespaces {
		namespaceNames = append(namespaceNames, name)
	}

	sort.Strings(namespaceNames)

	reference := &api.Reference{
		SchemaVersion: api.SchemaVersion,
		ID:            moduleID,
		Version:       version,
		Namespaces:    make([]api.Namespace, 0, len(namespaceNames)),
	}

	for _, namespaceName := range namespaceNames {
		functionMap := builder.namespaces[namespaceName]
		functionNames := make([]string, 0, len(functionMap))

		for name := range functionMap {
			functionNames = append(functionNames, name)
		}

		sort.Strings(functionNames)

		namespace := api.Namespace{
			Name:      namespaceName,
			Functions: make([]api.Function, 0, len(functionNames)),
		}

		for _, functionName := range functionNames {
			signatureMap := functionMap[functionName]
			signatures := make([]signatureRecord, 0, len(signatureMap))

			for _, signature := range signatureMap {
				signatures = append(signatures, signature)
			}

			sort.Slice(signatures, func(i, j int) bool {
				if signatures[i].variadic != signatures[j].variadic {
					return !signatures[i].variadic
				}
				return len(signatures[i].parameters) < len(signatures[j].parameters)
			})

			function := api.Function{
				Name:       functionName,
				Signatures: make([]api.Signature, 0, len(signatures)),
			}

			for _, signature := range signatures {
				parameters := append([]api.Parameter{}, signature.parameters...)
				throws := append([]api.Throw{}, signature.throws...)
				var returnValue *api.Return

				if signature.returnValue != nil {
					cloned := *signature.returnValue
					returnValue = &cloned
				}

				function.Signatures = append(function.Signatures, api.Signature{
					Parameters:  parameters,
					Variadic:    signature.variadic,
					Description: signature.description,
					Return:      returnValue,
					Throws:      throws,
					Deprecated:  signature.deprecated,
				})
			}

			namespace.Functions = append(namespace.Functions, function)
		}

		reference.Namespaces = append(reference.Namespaces, namespace)
	}

	return reference
}
