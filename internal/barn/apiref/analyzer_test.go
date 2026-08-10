package apiref

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

func TestAnalyzerStandardModule(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"example.com/fixture/lib"
	"errors"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

var enabled bool

func New() (module.Module, error) {
	if enabled {
		return nil, errors.New("disabled")
	}
	return sdk.NewModule("fixture", func(bootstrap module.Bootstrap) error {
		if enabled {
			_ = lib.Register(bootstrap.Host().Library())
		}
		return lib.Register(bootstrap.Host().Library().Namespace("TEST").Namespace("API"))
	}), nil
}
`,
		"module/lib/lib.go": `package lib

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

func Register(ns runtime.Namespace) error {
	definitions := append(baseDefinitions(),
		sdk.Func("OVERLOAD", Zero),
		sdk.Func("BOUND", sdk.Bind1(Bound)),
		sdk.Func("FACTORY", factory(true)),
	)
	return sdk.RegisterFunctions(ns, definitions...)
}

func baseDefinitions() []sdk.FunctionDef {
	return []sdk.FunctionDef{
		sdk.Func("DOCUMENTED", Documented),
		sdk.Func("OVERLOAD", Two),
	}
}

// Documented is extracted from the registered declaration.
//
// The prose remains separate from Ferret metadata.
//
// Deprecated: use Parse instead.
//
// @param input {String|Binary} Source content.
// @return {Object} Parsed content.
// @throws {ParseError} Source content is malformed.
// @throws {ParseError} Source content cannot be normalized.
// @deprecated Use Parse instead.
func Documented(_ context.Context, input runtime.Value) (runtime.Value, error) { return input, nil }
func Zero(context.Context) (runtime.Value, error) { return nil, nil }
func Two(_ context.Context, left, _ runtime.Value) (runtime.Value, error) { return left, nil }
func Bound(_ context.Context, value runtime.Value) (runtime.Value, error) { return value, nil }
func factory(bool) runtime.Function1 {
	return func(_ context.Context, generated runtime.Value) (runtime.Value, error) { return generated, nil }
}

// NeverRegistered must not appear in the artifact.
// @param {String} value
func NeverRegistered(context.Context) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.2.3")
	if err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
	if got, want := namespaceNames(reference), []string{"", "TEST::API"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace names = %#v, want %#v", got, want)
	}
	for _, namespace := range reference.Namespaces {
		if got, want := functionNames(namespace.Functions), []string{"BOUND", "DOCUMENTED", "FACTORY", "OVERLOAD"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%q function names = %#v, want %#v", namespace.Name, got, want)
		}
		signature := namespace.Functions[1].Signatures[0]
		if got := signature.Description; got != "Documented is extracted from the registered declaration.\n\nThe prose remains separate from Ferret metadata." {
			t.Fatalf("description = %q", got)
		}
		if strings.Contains(signature.Description, "@param") || strings.Contains(signature.Description, "Deprecated:") {
			t.Fatalf("description contains structured metadata: %q", signature.Description)
		}
		if got, want := signature.Parameters, []registryartifact.APIParameter{{Name: "input", Type: "String|Binary", Description: "Source content."}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("documented parameters = %#v, want %#v", got, want)
		}
		if signature.Return == nil || signature.Return.Type != "Object" || signature.Return.Description != "Parsed content." {
			t.Fatalf("return = %#v", signature.Return)
		}
		if got, want := signature.Throws, []registryartifact.APIThrownError{
			{Error: "ParseError", Description: "Source content is malformed."},
			{Error: "ParseError", Description: "Source content cannot be normalized."},
		}; !reflect.DeepEqual(got, want) {
			t.Fatalf("throws = %#v, want %#v", got, want)
		}
		if signature.Deprecated != "Use Parse instead." {
			t.Fatalf("deprecated = %q", signature.Deprecated)
		}
		if got, want := namespace.Functions[3].Signatures[1].Parameters, []registryartifact.APIParameter{{Name: "left"}, {Name: "arg2"}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("overload parameters = %#v, want %#v", got, want)
		}
	}
}

func TestAnalyzerRegisterMethodAndFunctionBuilder(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

type mod struct{}
func New() module.Module { return &mod{} }
func (*mod) Register(bootstrap module.Bootstrap) error {
	ns := bootstrap.Host().Library().Namespace("AI").Namespace("LLM")
	ns.Function().A2().Add("MODEL", Model).Add("SESSION", Session)
	ns.Function().Var().Add("GENERATE", Generate)
	return nil
}
func Model(context.Context, runtime.Value, runtime.Value) (runtime.Value, error) { return nil, nil }
func Session(_ context.Context, model, options runtime.Value) (runtime.Value, error) { return model, nil }
// Generate produces model output.
// @param prompt {String} Model prompt.
// @param options {Object?} Generation options.
// @return {String} Generated text.
func Generate(_ context.Context, args ...runtime.Value) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "2.0.0")
	if err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
	if got, want := namespaceNames(reference), []string{"AI::LLM"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("namespace names = %#v, want %#v", got, want)
	}
	if got, want := functionNames(reference.Namespaces[0].Functions), []string{"GENERATE", "MODEL", "SESSION"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %#v, want %#v", got, want)
	}
	if !reference.Namespaces[0].Functions[0].Signatures[0].Variadic {
		t.Fatal("GENERATE signature is not variadic")
	}
	if got, want := reference.Namespaces[0].Functions[0].Signatures[0].Parameters, []registryartifact.APIParameter{
		{Name: "prompt", Type: "String", Description: "Model prompt."},
		{Name: "options", Type: "Object?", Description: "Generation options."},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("GENERATE parameters = %#v, want %#v", got, want)
	}
	if got, want := reference.Namespaces[0].Functions[1].Signatures[0].Parameters, []registryartifact.APIParameter{{Name: "arg1"}, {Name: "arg2"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MODEL parameters = %#v, want %#v", got, want)
	}
}

func TestAnalyzerRejectsMalformedRegisteredDocumentation(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

func New() module.Module {
	return sdk.NewModule("fixture", func(bootstrap module.Bootstrap) error {
		return sdk.RegisterFunctions(bootstrap.Host().Library(), sdk.Func("DECODE", Decode))
	})
}

// Decode decodes content.
// @param {String} data
func Decode(_ context.Context, args ...runtime.Value) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}

	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorInvalidDocumentation {
		t.Fatalf("error = %v, want invalid-documentation AnalysisError", err)
	}

	if analysisError.Position.Filename != "module/module.go" || analysisError.Position.Line != 17 {
		t.Fatalf("position = %s, want module/module.go:17", analysisError.Position)
	}

	for _, value := range []string{"Decode", `@param {String} data`, "expected"} {
		if !strings.Contains(analysisError.Error(), value) {
			t.Fatalf("diagnostic %q does not contain %q", analysisError, value)
		}
	}
}

func TestAnalyzerRejectsFixedArityDocumentationMismatch(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

func New() module.Module {
	return sdk.NewModule("fixture", func(bootstrap module.Bootstrap) error {
		return sdk.RegisterFunctions(bootstrap.Host().Library(), sdk.Func("PAIR", Pair))
	})
}

// Pair returns two values.
// @param left {Any} Left value.
func Pair(_ context.Context, left, right runtime.Value) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}

	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorInvalidDocumentation || !strings.Contains(err.Error(), "documented parameter count 1 does not match fixed Ferret arity 2") {
		t.Fatalf("error = %v, want fixed-arity documentation mismatch", err)
	}
}

func TestAnalyzerPropagatesStaticRegistrationStateThroughHelpersAndBranches(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

var enabled bool

func New() module.Module { return sdk.NewModule("fixture", root) }
func root(bootstrap module.Bootstrap) error {
	return register(bootstrap, sdk.Func(" FIRST ", First), sdk.Func("SECOND", Second))
}
func register(bootstrap module.Bootstrap, incoming ...sdk.FunctionDef) error {
	definitions := []sdk.FunctionDef{sdk.Func("BASE", Base)}
	if enabled {
		definitions = append(definitions, incoming...)
	}
	return sdk.RegisterFunctions(bootstrap.Host().Library(), definitions...)
}
func Base(context.Context) (runtime.Value, error) { return nil, nil }
func First(context.Context) (runtime.Value, error) { return nil, nil }
func Second(context.Context) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
	if got, want := functionNames(reference.Namespaces[0].Functions), []string{"BASE", "FIRST", "SECOND"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %#v, want %#v", got, want)
	}
}

func TestAnalyzerUsesOnlyTheReturnedModuleRoot(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

type mod struct{}
func New() module.Module { return &mod{} }
func (*mod) Register(bootstrap module.Bootstrap) error {
	return sdk.RegisterFunctions(bootstrap.Host().Library(), sdk.Func("ACTUAL", Actual))
}
func unused() module.Module {
	return sdk.NewModule("unused", func(bootstrap module.Bootstrap) error {
		return sdk.RegisterFunctions(bootstrap.Host().Library(), sdk.Func("WRONG", Wrong))
	})
}
func Actual(context.Context) (runtime.Value, error) { return nil, nil }
func Wrong(context.Context) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
	if got, want := functionNames(reference.Namespaces[0].Functions), []string{"ACTUAL"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %#v, want %#v", got, want)
	}
}

func TestAnalyzerAllowsModuleWithoutFunctions(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

func New() module.Module {
	return sdk.NewModule("fixture", func(module.Bootstrap) error { return nil })
}
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
	if len(reference.Namespaces) != 0 {
		t.Fatalf("namespaces = %#v, want none", reference.Namespaces)
	}
}

func TestAnalyzerDoesNotTreatOrdinaryVariadicValuesAsRegistrationState(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

type option struct{ enabled bool }
func New() module.Module {
	return sdk.NewModule("fixture", func(bootstrap module.Bootstrap) error {
		return register(bootstrap.Host().Library(), option{enabled: true})
	})
}
func register(namespace runtime.Namespace, options ...option) error {
	if len(options) > 0 { _ = options[0] }
	return sdk.RegisterFunctions(namespace, sdk.Func("RUN", Run))
}
func Run(context.Context) (runtime.Value, error) { return nil, nil }
`,
	})

	if _, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0"); err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
}

func TestAnalyzerRejectsUnsupportedRegistrationWithoutPartialArtifact(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

func New() module.Module {
	return sdk.NewModule("fixture", func(bootstrap module.Bootstrap) error {
		name := runtimeName()
		return sdk.RegisterFunctions(bootstrap.Host().Library(), sdk.Func(name, Valid))
	})
}
func runtimeName() string { return "DYNAMIC" }
func Valid(context.Context) (runtime.Value, error) { return nil, nil }
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}
	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorUnsupportedRegistration {
		t.Fatalf("error = %v, want unsupported-registration AnalysisError", err)
	}
}

func TestAnalyzerRejectsMissingModuleRoot(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": "package fixture\nfunc New() int { return 1 }\n",
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}
	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorDeclarationNotFound {
		t.Fatalf("error = %v, want declaration-not-found AnalysisError", err)
	}
}

func TestAnalyzerRejectsInvalidGoPackage(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": "package fixture\nfunc New( {\n",
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}
	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorInvalidPackage {
		t.Fatalf("error = %v, want invalid-package AnalysisError", err)
	}
	if !analysisError.Position.IsValid() || analysisError.Position.Filename != "module/module.go" {
		t.Fatalf("position = %#v, want repository-relative source position", analysisError.Position)
	}
}

func TestAnalyzerRejectsUnresolvedInterfaceRegistrationHelper(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)
type registrar interface { Register(runtime.Namespace) error }
var selected registrar
func New() module.Module {
	return sdk.NewModule("fixture", func(bootstrap module.Bootstrap) error {
		return selected.Register(bootstrap.Host().Library())
	})
}
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}
	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorUnresolvedTarget {
		t.Fatalf("error = %v, want unresolved-target AnalysisError", err)
	}
}

func TestAnalyzerRejectsUnsupportedDynamicRegistrationFamilies(t *testing.T) {
	for _, test := range []struct {
		name     string
		source   string
		wantKind ErrorKind
	}{
		{
			name:     "loop registrations",
			wantKind: ErrorUnsupportedRegistration,
			source: `package fixture
import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)
func New() module.Module { return sdk.NewModule("fixture", func(b module.Bootstrap) error {
	for _, name := range []string{"ONE"} { _ = sdk.RegisterFunctions(b.Host().Library(), sdk.Func(name, Fn)) }
	return nil
}) }
func Fn(context.Context) (runtime.Value, error) { return nil, nil }
`,
		},
		{
			name:     "ambiguous factories",
			wantKind: ErrorUnsupportedRegistration,
			source: `package fixture
import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)
func New() module.Module { return sdk.NewModule("fixture", func(b module.Bootstrap) error {
	return sdk.RegisterFunctions(b.Host().Library(), sdk.Func("ONE", factory(true)))
}) }
func factory(flag bool) runtime.Function0 {
	if flag { return func(context.Context) (runtime.Value, error) { return nil, nil } }
	return func(context.Context) (runtime.Value, error) { return nil, nil }
}
`,
		},
		{
			name:     "loop-composed definitions",
			wantKind: ErrorUnsupportedRegistration,
			source: `package fixture
import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)
func New() module.Module { return sdk.NewModule("fixture", func(b module.Bootstrap) error {
	definitions := []sdk.FunctionDef{sdk.Func("ONE", One)}
	for enabled := true; enabled; enabled = false { definitions = append(definitions, sdk.Func("TWO", Two)) }
	return sdk.RegisterFunctions(b.Host().Library(), definitions...)
}) }
func One(context.Context) (runtime.Value, error) { return nil, nil }
func Two(context.Context) (runtime.Value, error) { return nil, nil }
`,
		},
		{
			name:     "looped bootstrap helper",
			wantKind: ErrorUnsupportedRegistration,
			source: `package fixture
import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)
func New() module.Module { return sdk.NewModule("fixture", func(b module.Bootstrap) error {
	for enabled := true; enabled; enabled = false { _ = register(b) }
	return nil
}) }
func register(b module.Bootstrap) error {
	return sdk.RegisterFunctions(b.Host().Library(), sdk.Func("ONE", One))
}
func One(context.Context) (runtime.Value, error) { return nil, nil }
`,
		},
		{
			name:     "stored function builder",
			wantKind: ErrorUnsupportedRegistration,
			source: `package fixture
import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
)
type mod struct{}
func New() module.Module { return &mod{} }
func (*mod) Register(b module.Bootstrap) error {
	functions := b.Host().Library().Function().A0()
	functions.Add("ONE", One)
	return nil
}
func One(context.Context) (runtime.Value, error) { return nil, nil }
`,
		},
		{
			name:     "dynamic function value",
			wantKind: ErrorUnresolvedTarget,
			source: `package fixture
import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)
var enabled bool
func New() module.Module { return sdk.NewModule("fixture", func(b module.Bootstrap) error {
	fn := One
	if enabled { fn = Two }
	return sdk.RegisterFunctions(b.Host().Library(), sdk.Func("DYNAMIC", fn))
}) }
func One(context.Context) (runtime.Value, error) { return nil, nil }
func Two(context.Context) (runtime.Value, error) { return nil, nil }
`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{"module/module.go": test.source})
			reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
			if reference != nil {
				t.Fatalf("partial artifact returned: %#v", reference)
			}
			var analysisError *AnalysisError
			if !errors.As(err, &analysisError) || analysisError.Kind != test.wantKind {
				t.Fatalf("error = %v, want %s AnalysisError", err, test.wantKind)
			}
		})
	}
}

func TestAnalyzerRejectsMultipleReturnedModuleRoots(t *testing.T) {
	repository, moduleDirectory := writeAnalyzerFixture(t, map[string]string{
		"module/module.go": `package fixture

import (
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/sdk"
)

var enabled bool
func New() module.Module {
	if enabled {
		return sdk.NewModule("one", func(module.Bootstrap) error { return nil })
	}
	return sdk.NewModule("two", func(module.Bootstrap) error { return nil })
}
`,
	})

	reference, err := (Analyzer{}).Analyze(context.Background(), repository, moduleDirectory, "example.com/fixture", "owner/fixture", "1.0.0")
	if reference != nil {
		t.Fatalf("partial artifact returned: %#v", reference)
	}
	var analysisError *AnalysisError
	if !errors.As(err, &analysisError) || analysisError.Kind != ErrorUnsupportedRegistration {
		t.Fatalf("error = %v, want unsupported-registration AnalysisError", err)
	}
}

func namespaceNames(reference *registryartifact.APIReference) []string {
	result := make([]string, 0, len(reference.Namespaces))
	for _, namespace := range reference.Namespaces {
		result = append(result, namespace.Name)
	}
	return result
}

func functionNames(functions []registryartifact.APIFunction) []string {
	result := make([]string, 0, len(functions))
	for _, function := range functions {
		result = append(result, function.Name)
	}
	return result
}

func writeAnalyzerFixture(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	repository := t.TempDir()
	files["module/go.mod"] = `module example.com/fixture

go 1.25.0

require github.com/MontFerret/ferret/v2 v2.0.0

replace github.com/MontFerret/ferret/v2 => ../ferret
`
	files["ferret/go.mod"] = "module github.com/MontFerret/ferret/v2\n\ngo 1.25.0\n"
	files["ferret/pkg/runtime/runtime.go"] = `package runtime

import "context"

type Value = any
type Function = func(context.Context, ...Value) (Value, error)
type Function0 = func(context.Context) (Value, error)
type Function1 = func(context.Context, Value) (Value, error)
type Function2 = func(context.Context, Value, Value) (Value, error)
type Function3 = func(context.Context, Value, Value, Value) (Value, error)
type Function4 = func(context.Context, Value, Value, Value, Value) (Value, error)

type FunctionCollection[T any] interface { Add(string, T) FunctionCollection[T] }
type FunctionDefs interface {
	Var() FunctionCollection[Function]
	A0() FunctionCollection[Function0]
	A1() FunctionCollection[Function1]
	A2() FunctionCollection[Function2]
	A3() FunctionCollection[Function3]
	A4() FunctionCollection[Function4]
}
type Namespace interface { Namespace(string) Namespace; Function() FunctionDefs }
type Library interface { Namespace; Build() error }
`
	files["ferret/pkg/module/module.go"] = `package module

import "github.com/MontFerret/ferret/v2/pkg/runtime"

type Module interface { Register(Bootstrap) error }
type HostContext interface { Library() runtime.Library }
type Bootstrap interface { Host() HostContext }
`
	files["ferret/pkg/sdk/sdk.go"] = `package sdk

import (
	"context"
	"github.com/MontFerret/ferret/v2/pkg/module"
	"github.com/MontFerret/ferret/v2/pkg/runtime"
)

type FunctionDef struct{}
func NewModule(string, func(module.Bootstrap) error) module.Module { return nil }
func Func[T any](string, T) FunctionDef { return FunctionDef{} }
func RegisterFunctions(runtime.Namespace, ...FunctionDef) error { return nil }
func Bind1[A any](func(context.Context, A) (runtime.Value, error)) runtime.Function1 { return nil }
`

	for name, contents := range files {
		filename := filepath.Join(repository, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture file: %v", err)
		}
	}

	return repository, filepath.Join(repository, "module")
}
