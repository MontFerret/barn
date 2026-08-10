package apiref

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

type (
	signatureShape struct {
		arity    int
		variadic bool
	}

	functionNode struct {
		pkg  *packages.Package
		decl *ast.FuncDecl
	}

	loadedSource struct {
		fset           *token.FileSet
		packages       []*packages.Package
		functions      map[*types.Func]functionNode
		values         map[types.Object]valueNode
		root           *packages.Package
		modulePath     string
		repositoryRoot string
		moduleID       string
		version        string
	}

	valueNode struct {
		pkg  *packages.Package
		expr ast.Expr
	}

	signatureRecord struct {
		parameters    []string
		variadic      bool
		documentation string
	}

	addEntry struct {
		name     ast.Expr
		function ast.Expr
	}

	resultBuilder struct {
		namespaces map[string]map[string]map[string]signatureRecord
	}
)
