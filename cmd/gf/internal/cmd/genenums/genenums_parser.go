// Copyright GoFrame gf Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package genenums

import (
	"go/ast"
	"go/constant"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/gogf/gf/v2/util/gconv"
)

const pkgLoadMode = 0xffffff

type EnumsParser struct {
	enums            []EnumItem
	parsedPkg        map[string]struct{}
	prefixes         []string
	standardPackages map[string]struct{}
}

type EnumItem struct {
	Name    string
	Value   string
	Comment string
	Kind    constant.Kind // String/Int/Bool/Float/Complex/Unknown
	Type    string        // Pkg.ID + TypeName
}

type EnumExportItem struct {
	Value   any    `json:"value"`
	Comment string `json:"comment,omitempty"`
}

func NewEnumsParser(prefixes []string) *EnumsParser {
	return &EnumsParser{
		enums:            make([]EnumItem, 0),
		parsedPkg:        make(map[string]struct{}),
		prefixes:         prefixes,
		standardPackages: getStandardPackages(),
	}
}

func (p *EnumsParser) ParsePackages(pkgs []*packages.Package) {
	for _, pkg := range pkgs {
		p.ParsePackage(pkg)
	}
}

func (p *EnumsParser) ParsePackage(pkg *packages.Package) {
	// Ignore std packages.
	if _, ok := p.standardPackages[pkg.ID]; ok {
		return
	}
	// Ignore pared packages.
	if _, ok := p.parsedPkg[pkg.ID]; ok {
		return
	}
	p.parsedPkg[pkg.ID] = struct{}{}

	// Only parse specified prefixes.
	if len(p.prefixes) > 0 {
		var hasPrefix bool
		for _, prefix := range p.prefixes {
			if hasPrefix = gstr.HasPrefix(pkg.ID, prefix); hasPrefix {
				break
			}
		}
		if !hasPrefix {
			return
		}
	}

	p.enums = append(p.enums, p.parseEnumItems(pkg)...)
	for _, im := range pkg.Imports {
		p.ParsePackage(im)
	}
}

func (p *EnumsParser) Export() string {
	var typeEnumMap = make(map[string][]any)
	for _, enum := range p.enums {
		if typeEnumMap[enum.Type] == nil {
			typeEnumMap[enum.Type] = make([]any, 0)
		}
		var value any
		switch enum.Kind {
		case constant.Int:
			value = gconv.Int64(enum.Value)
		case constant.String:
			value = enum.Value
		case constant.Float:
			value = gconv.Float64(enum.Value)
		case constant.Bool:
			value = gconv.Bool(enum.Value)
		default:
			value = enum.Value
		}
		typeEnumMap[enum.Type] = append(typeEnumMap[enum.Type], EnumExportItem{
			Value:   value,
			Comment: enum.Comment,
		})
	}
	return gjson.MustEncodeString(typeEnumMap)
}

func (p *EnumsParser) parseEnumItems(pkg *packages.Package) []EnumItem {
	var enums []EnumItem
	if pkg == nil || pkg.TypesInfo == nil {
		return enums
	}
	files := append([]*ast.File(nil), pkg.Syntax...)
	sort.SliceStable(files, func(i, j int) bool {
		if pkg.Fset == nil {
			return files[i].Pos() < files[j].Pos()
		}
		iPos := pkg.Fset.Position(files[i].Pos())
		jPos := pkg.Fset.Position(files[j].Pos())
		if iPos.Filename == jPos.Filename {
			return files[i].Pos() < files[j].Pos()
		}
		return iPos.Filename < jPos.Filename
	})
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			valueSpec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			comment := p.valueSpecComment(valueSpec)
			for _, name := range valueSpec.Names {
				enum, ok := p.enumItemFromIdent(pkg, name, comment)
				if !ok {
					continue
				}
				enums = append(enums, enum)
			}
			return true
		})
	}
	return enums
}

func (p *EnumsParser) enumItemFromIdent(pkg *packages.Package, name *ast.Ident, comment string) (EnumItem, bool) {
	obj := pkg.TypesInfo.Defs[name]
	con, ok := obj.(*types.Const)
	if !ok || !con.Exported() {
		return EnumItem{}, false
	}
	enumType := con.Type().String()
	if !gstr.Contains(enumType, "/") {
		return EnumItem{}, false
	}
	enumValue := con.Val().ExactString()
	enumKind := con.Val().Kind()
	if enumKind == constant.String {
		enumValue = constant.StringVal(con.Val())
	}
	return EnumItem{
		Name:    con.Name(),
		Value:   enumValue,
		Type:    enumType,
		Kind:    enumKind,
		Comment: p.normalizeEnumComment(con.Name(), comment),
	}, true
}

func (p *EnumsParser) valueSpecComment(valueSpec *ast.ValueSpec) string {
	comment := ""
	if valueSpec.Comment != nil {
		comment = valueSpec.Comment.Text()
	}
	if comment == "" && valueSpec.Doc != nil {
		comment = valueSpec.Doc.Text()
	}
	return gstr.Trim(comment)
}

func (p *EnumsParser) normalizeEnumComment(enumName, comment string) string {
	comment = gstr.Trim(comment)
	if comment == "" {
		return ""
	}
	if gstr.HasPrefix(comment, enumName) {
		comment = gstr.Trim(gstr.TrimLeftStr(comment, enumName, 1))
	}
	return comment
}

func getStandardPackages() map[string]struct{} {
	standardPackages := make(map[string]struct{})
	stdPackages, err := packages.Load(nil, "std")
	if err != nil {
		panic(err)
	}
	for _, p := range stdPackages {
		standardPackages[p.ID] = struct{}{}
	}
	return standardPackages
}
