package apiref

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestNormalizeLineDocumentation(t *testing.T) {
	group, fset := parseDocumentationGroup(t, `// Decode decodes content.
//
// @param data {Binary} Source content.`)
	body := normalizeDocumentation(group)

	if got, want := body.text, "Decode decodes content.\n\n@param data {Binary} Source content."; got != want {
		t.Fatalf("normalized body = %q, want %q", got, want)
	}

	assertDocumentationPosition(t, fset, body.position(1), 3, 4)
	assertDocumentationPosition(t, fset, body.position(3), 5, 4)

	if got, want := body.annotationPosition("@param"), body.position(3); got != want {
		t.Fatalf("parameter position = %d, want %d", got, want)
	}
}

func TestNormalizeBlockDocumentation(t *testing.T) {
	group, fset := parseDocumentationGroup(t, `/*
 * Decode decodes content.
 *
 * @param data {Binary} Source content.
 */`)
	body := normalizeDocumentation(group)

	if got, want := body.text, "\nDecode decodes content.\n\n@param data {Binary} Source content.\n"; got != want {
		t.Fatalf("normalized body = %q, want %q", got, want)
	}

	assertDocumentationPosition(t, fset, body.position(2), 4, 4)
	assertDocumentationPosition(t, fset, body.position(4), 6, 4)

	if got, want := body.annotationPosition("@param"), body.position(4); got != want {
		t.Fatalf("parameter position = %d, want %d", got, want)
	}
}

func TestDocumentationPositionRejectsInvalidLine(t *testing.T) {
	body := documentationBody{positions: []token.Pos{10}}

	for _, line := range []int{0, 2} {
		if position := body.position(line); position != token.NoPos {
			t.Fatalf("position(%d) = %d, want NoPos", line, position)
		}
	}
}

func parseDocumentationGroup(t *testing.T, comment string) (*ast.CommentGroup, *token.FileSet) {
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
			return function.Doc, fset
		}
	}

	t.Fatal("fixture function declaration not found")

	return nil, nil
}

func assertDocumentationPosition(t *testing.T, fset *token.FileSet, position token.Pos, line, column int) {
	t.Helper()

	resolved := fset.Position(position)
	if resolved.Filename != "decode.go" || resolved.Line != line || resolved.Column != column {
		t.Fatalf("position = %s, want decode.go:%d:%d", resolved, line, column)
	}
}
