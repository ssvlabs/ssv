package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"path/filepath"
	"regexp"
	"slices"
)

type Element struct {
	Path string
	Name string
	Code string
}

type Parser struct {
	BasePath          string
	Paths             []string
	RemoveIdentifiers []string

	fset     *token.FileSet
	elements map[string]Element
}

func NewParser(basePath string, paths []string, removeIdentifiers []string) *Parser {
	return &Parser{
		BasePath:          basePath,
		Paths:             paths,
		RemoveIdentifiers: removeIdentifiers,
		elements:          make(map[string]Element),
		fset:              token.NewFileSet(),
	}
}

func (p *Parser) Parse() (map[string]Element, error) {
	for _, path := range p.Paths {
		pkgs, err := parser.ParseDir(p.fset, filepath.Join(p.BasePath, path), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}

		for _, pkg := range pkgs {
			for _, file := range pkg.Files {
				ast.Inspect(file, p.applyTransformations)
				ast.Inspect(file, p.addElement)
			}
		}
	}

	return p.elements, nil
}

// regexpRemains removes the remains of removed function calls.
var regexpRemains = regexp.MustCompile(`\t+([a-zA-Z_][a-zA-Z0-9_]* :?= )?\(\)\n`)

func (p *Parser) addElement(n ast.Node) bool {
	// Get the name.
	var name string
	switch x := n.(type) {
	case *ast.FuncDecl:
		if x.Recv != nil && len(x.Recv.List) > 0 {
			// Methods.
			switch receiverType := x.Recv.List[0].Type.(type) {
			case *ast.StarExpr: // Pointer receivers.
				if receiverIdent, ok := receiverType.X.(*ast.Ident); ok {
					name = receiverIdent.Name + "." + x.Name.Name
				}
			case *ast.Ident: // Value receivers.
				name = receiverType.Name + "." + x.Name.Name
			}
		} else {
			// Functions.
			name = x.Name.Name
		}
	case *ast.TypeSpec:
		name = x.Name.Name
	}
	if name == "" {
		return true
	}

	// Get the code.
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, p.fset, n); err != nil {
		log.Fatal(err)
	}
	code := regexpRemains.ReplaceAllString(buf.String(), "")

	// Get the file.
	filePath, err := filepath.Rel(p.BasePath, p.fset.File(n.Pos()).Name())
	if err != nil {
		log.Fatal(err)
	}

	// Save the element.
	p.elements[name] = Element{
		Path: filePath,
		Name: name,
		Code: code,
	}

	return true
}

func (p *Parser) applyTransformations(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		p.transformCallExpr(x)
	case *ast.FuncDecl:
		p.transformFuncDecl(x)
	case *ast.TypeSpec:
		p.transformTypeSpec(x)
	}
	return true
}

func (p *Parser) transformCallExpr(callExpr *ast.CallExpr) {
	// Remove function calls and arguments for specified identifiers.
	selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
	if ok {
		ident, ok := selectorExpr.X.(*ast.Ident)
		if ok && slices.Contains(p.RemoveIdentifiers, ident.Name) {
			emptyIdent := &ast.Ident{}
			callExpr.Fun = emptyIdent
			callExpr.Args = []ast.Expr{}
		}
	}

	// Remove arguments for specified identifiers.
	newArgs := []ast.Expr{}
	for _, arg := range callExpr.Args {
		if ident, ok := arg.(*ast.Ident); !ok || !slices.Contains(p.RemoveIdentifiers, ident.Name) {
			newArgs = append(newArgs, arg)
		}
	}
	callExpr.Args = newArgs
}

func (p *Parser) transformFuncDecl(funcDecl *ast.FuncDecl) {
	// Remove parameters from function definitions for specified identifiers.
	if funcDecl.Type.Params != nil {
		newList := []*ast.Field{}
		for _, field := range funcDecl.Type.Params.List {
			if len(field.Names) == 0 {
				continue
			}
			if len(field.Names) > 0 && !slices.Contains(p.RemoveIdentifiers, field.Names[0].Name) {
				newList = append(newList, field)
			}
		}
		funcDecl.Type.Params.List = newList
	}
}

func (p *Parser) transformTypeSpec(typeSpec *ast.TypeSpec) {
	// Remove parameters from function signatures in interface declarations.
	if interfaceType, ok := typeSpec.Type.(*ast.InterfaceType); ok {
		for _, field := range interfaceType.Methods.List {
			if funcType, ok := field.Type.(*ast.FuncType); ok {
				newList := []*ast.Field{}
				for _, param := range funcType.Params.List {
					if len(param.Names) > 0 && !slices.Contains(p.RemoveIdentifiers, param.Names[0].Name) {
						newList = append(newList, param)
					}
				}
				funcType.Params.List = newList
			}
		}
	}
}
