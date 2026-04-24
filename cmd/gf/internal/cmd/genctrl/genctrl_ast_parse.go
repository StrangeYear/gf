// Copyright GoFrame gf Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package genctrl

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"

	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogf/gf/v2/text/gstr"
	"golang.org/x/tools/go/packages"
)

type structInfo struct {
	structName string
	comment    string
}

// getStructsNameInSrc retrieves all struct names and comment
// that end in "Req" and have "g.Meta" in their body.
func (c CGenCtrl) getStructsNameInSrc(filePath string) (structInfos []*structInfo, err error) {
	var (
		fileContent = gfile.GetContents(filePath)
		fileSet     = token.NewFileSet()
	)

	node, err := parser.ParseFile(fileSet, "", fileContent, parser.ParseComments)
	if err != nil {
		return
	}
	typeSpecMap, err := c.getTypeSpecsInPackage(gfile.Dir(filePath), node.Name.Name)
	if err != nil {
		return nil, err
	}
	typesPkg, _ := c.getTypesPackageInDir(gfile.Dir(filePath), node.Name.Name)

	ast.Inspect(node, func(n ast.Node) bool {
		if typeSpec, ok := n.(*ast.TypeSpec); ok {
			structName := typeSpec.Name.Name
			if !gstr.HasSuffix(structName, "Req") {
				// ignore struct name that do not end in "Req"
				return true
			}
			if structType, ok := typeSpec.Type.(*ast.StructType); ok {
				var buf bytes.Buffer
				if err := printer.Fprint(&buf, fileSet, structType); err != nil {
					return false
				}
				// ignore struct name that match a request, but has no g.Meta in its body.
				if !gstr.Contains(buf.String(), `g.Meta`) {
					return true
				}
				resStructName := gstr.TrimRightStr(structName, "Req", 1) + "Res"
				if !c.isStructLikeType(typeSpecMap, resStructName, make(map[string]struct{})) &&
					!c.isStructLikeGoPackageType(typesPkg, resStructName, make(map[types.Type]struct{})) {
					err = fmt.Errorf(
						`missing response struct "%s" for request struct "%s" in file "%s"`,
						resStructName, structName, filePath,
					)
					return false
				}

				comment := typeSpec.Doc.Text()
				// remove the struct name from the comment
				if gstr.HasPrefix(comment, structName) {
					comment = gstr.TrimLeftStr(comment, structName, 1)
				}
				// remove the comment \n or space
				comment = gstr.Trim(comment)

				structInfos = append(structInfos, &structInfo{
					structName: structName,
					comment:    comment,
				})
			}
		}
		return true
	})

	return
}

func (c CGenCtrl) getTypeSpecsInPackage(dirPath, packageName string) (typeSpecMap map[string]*ast.TypeSpec, err error) {
	var (
		fileSet = token.NewFileSet()
		pkgs    map[string]*ast.Package
	)
	pkgs, err = parser.ParseDir(fileSet, dirPath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	typeSpecMap = make(map[string]*ast.TypeSpec)
	if pkg, ok := pkgs[packageName]; ok {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				typeSpec, ok := n.(*ast.TypeSpec)
				if !ok {
					return true
				}
				typeSpecMap[typeSpec.Name.Name] = typeSpec
				return true
			})
		}
	}
	return typeSpecMap, nil
}

func (c CGenCtrl) getTypesPackageInDir(dirPath, packageName string) (typesPkg *types.Package, err error) {
	var pkgs []*packages.Package
	pkgs, err = packages.Load(&packages.Config{
		Dir: dirPath,
		Mode: packages.NeedName |
			packages.NeedTypes |
			packages.NeedImports |
			packages.NeedDeps,
	}, ".")
	if err != nil {
		return nil, err
	}
	for _, pkg := range pkgs {
		if pkg != nil && pkg.Name == packageName && pkg.Types != nil {
			return pkg.Types, nil
		}
	}
	return nil, nil
}

func (c CGenCtrl) isStructLikeType(typeSpecMap map[string]*ast.TypeSpec, typeName string, visited map[string]struct{}) bool {
	if _, ok := visited[typeName]; ok {
		return false
	}
	typeSpec, ok := typeSpecMap[typeName]
	if !ok {
		return false
	}
	visited[typeName] = struct{}{}
	return c.isStructLikeExpr(typeSpecMap, typeSpec.Type, visited)
}

func (c CGenCtrl) isStructLikeExpr(typeSpecMap map[string]*ast.TypeSpec, expr ast.Expr, visited map[string]struct{}) bool {
	switch expr := expr.(type) {
	case *ast.StructType:
		return true

	case *ast.Ident:
		return c.isStructLikeType(typeSpecMap, expr.Name, visited)

	case *ast.IndexExpr:
		return c.isStructLikeExpr(typeSpecMap, expr.X, visited)

	case *ast.IndexListExpr:
		return c.isStructLikeExpr(typeSpecMap, expr.X, visited)

	case *ast.ParenExpr:
		return c.isStructLikeExpr(typeSpecMap, expr.X, visited)
	}
	return false
}

func (c CGenCtrl) isStructLikeGoPackageType(typesPkg *types.Package, typeName string, visited map[types.Type]struct{}) bool {
	if typesPkg == nil {
		return false
	}
	obj := typesPkg.Scope().Lookup(typeName)
	typeNameObj, ok := obj.(*types.TypeName)
	if !ok {
		return false
	}
	return c.isStructLikeGoType(typeNameObj.Type(), visited)
}

func (c CGenCtrl) isStructLikeGoType(goType types.Type, visited map[types.Type]struct{}) bool {
	if goType == nil {
		return false
	}
	goType = types.Unalias(goType)
	if _, ok := visited[goType]; ok {
		return false
	}
	visited[goType] = struct{}{}

	switch value := goType.(type) {
	case *types.Named:
		return c.isStructLikeGoType(value.Underlying(), visited)

	case *types.Struct:
		return true

	case *types.Pointer:
		return c.isStructLikeGoType(value.Elem(), visited)
	}
	return false
}

// getImportsInDst retrieves all import paths in the file.
func (c CGenCtrl) getImportsInDst(filePath string) (imports []string, err error) {
	var (
		fileContent = gfile.GetContents(filePath)
		fileSet     = token.NewFileSet()
	)

	node, err := parser.ParseFile(fileSet, "", fileContent, parser.ParseComments)
	if err != nil {
		return
	}

	ast.Inspect(node, func(n ast.Node) bool {
		if imp, ok := n.(*ast.ImportSpec); ok {
			imports = append(imports, imp.Path.Value)
		}
		return true
	})

	return
}
