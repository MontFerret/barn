package apiref

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	registryartifact "github.com/MontFerret/specs/pkg/registry/artifact"
)

func TestParseDocumentation(t *testing.T) {
	for _, test := range []struct {
		name        string
		comment     string
		description string
		parameters  []registryartifact.APIParameter
		returnValue *registryartifact.APIReturn
		throws      []registryartifact.APIThrownError
		deprecated  string
	}{
		{
			name:        "description only",
			comment:     "// Decode decodes content.",
			description: "Decode decodes content.",
		},
		{
			name:        "one parameter",
			comment:     "// Decode decodes content.\n//\n// @param data {String} Source content.",
			description: "Decode decodes content.",
			parameters:  []registryartifact.APIParameter{{Name: "data", Type: "String", Description: "Source content."}},
		},
		{
			name:    "multiple parameters preserve order",
			comment: "// Decode decodes content.\n// @param data {String|Binary} Source content.\n// @param options {Object?} Decode options.",
			parameters: []registryartifact.APIParameter{
				{Name: "data", Type: "String|Binary", Description: "Source content."},
				{Name: "options", Type: "Object?", Description: "Decode options."},
			},
			description: "Decode decodes content.",
		},
		{
			name:       "generic type",
			comment:    "// @param names {Array<String>} Field names.",
			parameters: []registryartifact.APIParameter{{Name: "names", Type: "Array<String>", Description: "Field names."}},
		},
		{
			name:       "variadic type",
			comment:    "// @param values {Any...} Values to concatenate.",
			parameters: []registryartifact.APIParameter{{Name: "values", Type: "Any...", Description: "Values to concatenate."}},
		},
		{
			name:       "opaque type spacing",
			comment:    "// @param data { String | Binary } Source content.",
			parameters: []registryartifact.APIParameter{{Name: "data", Type: " String | Binary ", Description: "Source content."}},
		},
		{
			name:        "return value",
			comment:     "// @return {Object} Normalized document.",
			returnValue: &registryartifact.APIReturn{Type: "Object", Description: "Normalized document."},
		},
		{
			name:    "multiple throws preserve order",
			comment: "// @throws {ParseError} Input is malformed.\n// @throws {LimitError} Input is too large.",
			throws: []registryartifact.APIThrownError{
				{Error: "ParseError", Description: "Input is malformed."},
				{Error: "LimitError", Description: "Input is too large."},
			},
		},
		{
			name:       "deprecated",
			comment:    "// @deprecated Use Parse instead.",
			deprecated: "Use Parse instead.",
		},
		{
			name: "prose paragraphs",
			comment: "// Decode decodes content.\n//\n// The result uses Ferret-native values.\n//\n// @param data {Binary} Source content.\n" +
				"// @return {Object} Normalized document.",
			description: "Decode decodes content.\n\nThe result uses Ferret-native values.",
			parameters:  []registryartifact.APIParameter{{Name: "data", Type: "Binary", Description: "Source content."}},
			returnValue: &registryartifact.APIReturn{Type: "Object", Description: "Normalized document."},
		},
		{
			name:        "unknown annotation remains prose",
			comment:     "// Decode decodes content.\n// @example Decode value.",
			description: "Decode decodes content.\n@example Decode value.",
		},
		{
			name:        "indented supported annotation remains prose",
			comment:     "// Decode decodes content.\n//   @param data {String} Source content.",
			description: "Decode decodes content.\n  @param data {String} Source content.",
		},
		{
			name: "structured deprecation removes standard paragraph",
			comment: "// Decode decodes content.\n//\n// Deprecated: use Parse instead.\n//\n" +
				"// More details remain.\n// @deprecated Use Parse instead.",
			description: "Decode decodes content.\n\nMore details remain.",
			deprecated:  "Use Parse instead.",
		},
		{
			name:        "standard deprecation remains without annotation",
			comment:     "// Decode decodes content.\n//\n// Deprecated: use Parse instead.",
			description: "Decode decodes content.\n\nDeprecated: use Parse instead.",
		},
		{
			name: "block comment",
			comment: `/*
 * Decode decodes content.
 *
 * @param data {Binary} Source content.
 * @return {Object} Normalized document.
 */`,
			description: "Decode decodes content.",
			parameters:  []registryartifact.APIParameter{{Name: "data", Type: "Binary", Description: "Source content."}},
			returnValue: &registryartifact.APIReturn{Type: "Object", Description: "Normalized document."},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			declaration, _ := parseDocumentationDeclaration(t, test.comment)
			metadata, err := parseDocumentation(declaration)
			if err != nil {
				t.Fatalf("parse documentation: %v", err)
			}

			if metadata.description != test.description {
				t.Fatalf("description = %q, want %q", metadata.description, test.description)
			}

			if !reflect.DeepEqual(metadata.parameters, test.parameters) {
				t.Fatalf("parameters = %#v, want %#v", metadata.parameters, test.parameters)
			}

			if !reflect.DeepEqual(metadata.returnValue, test.returnValue) {
				t.Fatalf("return = %#v, want %#v", metadata.returnValue, test.returnValue)
			}

			if !reflect.DeepEqual(metadata.throws, test.throws) {
				t.Fatalf("throws = %#v, want %#v", metadata.throws, test.throws)
			}

			if metadata.deprecated != test.deprecated {
				t.Fatalf("deprecated = %q, want %q", metadata.deprecated, test.deprecated)
			}
		})
	}
}

func TestParseDocumentationRejectsMalformedAnnotations(t *testing.T) {
	for _, test := range []struct {
		name    string
		comment string
		want    string
	}{
		{name: "empty parameter", comment: "// @param", want: "expected"},
		{name: "parameter name only", comment: "// @param data", want: "expected"},
		{name: "parameter without braces", comment: "// @param data String description", want: "opening brace"},
		{name: "type first parameter", comment: "// @param {String} data", want: "expected"},
		{name: "missing parameter description", comment: "// @param data {String}", want: "description"},
		{name: "JSDoc parameter separator", comment: "// @param data {String} - Source content.", want: "JSDoc"},
		{name: "empty return", comment: "// @return", want: "expected"},
		{name: "return without braces", comment: "// @return Object value", want: "expected"},
		{name: "missing return description", comment: "// @return {Object}", want: "expected"},
		{name: "empty throws", comment: "// @throws", want: "expected"},
		{name: "missing throws description", comment: "// @throws {ParseError}", want: "expected"},
		{name: "empty deprecated", comment: "// @deprecated", want: "expected"},
		{name: "blank type", comment: "// @param data { } Source content.", want: "must not be blank"},
		{name: "missing closing brace", comment: "// @param data {String Source content.", want: "closing brace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			declaration, fset := parseDocumentationDeclaration(t, test.comment)
			_, err := parseDocumentation(declaration)
			parseErr, ok := err.(*documentationParseError)
			if !ok {
				t.Fatalf("error = %T %v, want documentationParseError", err, err)
			}

			if !strings.Contains(parseErr.Error(), "Decode: malformed") || !strings.Contains(parseErr.Error(), test.want) || !strings.Contains(parseErr.Error(), strings.TrimPrefix(test.comment, "// ")) {
				t.Fatalf("unexpected diagnostic: %v", parseErr)
			}

			position := fset.Position(parseErr.position)
			if position.Filename != "decode.go" || position.Line != 3 {
				t.Fatalf("position = %s, want decode.go:3", position)
			}
		})
	}
}

func TestParseDocumentationRejectsDuplicateMetadata(t *testing.T) {
	for _, test := range []struct {
		name    string
		comment string
		want    string
	}{
		{
			name:    "parameter",
			comment: "// @param data {String} Source content.\n// @param data {Binary} Binary content.",
			want:    `parameter "data" is declared more than once`,
		},
		{
			name:    "return",
			comment: "// @return {String} Text.\n// @return {Binary} Binary.",
			want:    "at most one @return",
		},
		{
			name:    "deprecated",
			comment: "// @deprecated Use Parse.\n// @deprecated Use Read.",
			want:    "at most one @deprecated",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			declaration, _ := parseDocumentationDeclaration(t, test.comment)
			_, err := parseDocumentation(declaration)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func parseDocumentationDeclaration(t *testing.T, comment string) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()

	source := "package fixture\n\n" + comment + "\nfunc Decode() {}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "decode.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok {
			return function, fset
		}
	}

	t.Fatal("fixture function declaration not found")

	return nil, nil
}
