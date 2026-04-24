// Copyright GoFrame gf Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package genctrl

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/gogf/gf/v2/container/gset"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogf/gf/v2/text/gstr"

	"github.com/gogf/gf/cmd/gf/v2/internal/consts"
	"github.com/gogf/gf/cmd/gf/v2/internal/utility/mlog"
)

type controllerGenerator struct{}

func newControllerGenerator() *controllerGenerator {
	return &controllerGenerator{}
}

func (c *controllerGenerator) Generate(dstModuleFolderPath string, apiModuleApiItems []apiItem, interfacePrefixIByVersion map[string]bool, merge bool) (err error) {
	var (
		doneApiItemSet = gset.NewStrSet()
	)
	for _, item := range apiModuleApiItems {
		if doneApiItemSet.Contains(item.String()) {
			continue
		}
		// retrieve all api items of the same module.
		var (
			subItems   = c.getSubItemsByModuleAndVersion(apiModuleApiItems, item.Module, item.Version)
			importPath = gstr.Replace(gfile.Dir(item.Import), "\\", "/", -1)
		)
		if err = c.doGenerateCtrlNewByModuleAndVersion(
			dstModuleFolderPath, item.Module, item.Version, importPath, interfacePrefixIByVersion[item.Version],
		); err != nil {
			return
		}

		// use -merge
		if merge {
			err = c.doGenerateCtrlMergeItem(dstModuleFolderPath, subItems, doneApiItemSet)
			continue
		}

		for _, subItem := range subItems {
			err = c.doGenerateCtrlItem(dstModuleFolderPath, subItem)
			if err != nil {
				return
			}
			doneApiItemSet.Add(subItem.String())
		}
	}
	return
}

func (c *controllerGenerator) getSubItemsByModuleAndVersion(items []apiItem, module, version string) (subItems []apiItem) {
	for _, item := range items {
		if item.Module == module && item.Version == version {
			subItems = append(subItems, item)
		}
	}
	return
}

func (c *controllerGenerator) doGenerateCtrlNewByModuleAndVersion(
	dstModuleFolderPath, module, version, importPath string, prefixI bool,
) (err error) {
	var (
		moduleFilePath    = filepath.FromSlash(gfile.Join(dstModuleFolderPath, module+".go"))
		moduleFilePathNew = filepath.FromSlash(gfile.Join(dstModuleFolderPath, module+"_new.go"))
		ctrlName          = fmt.Sprintf(`Controller%s`, gstr.UcFirst(version))
		interfaceName     = formatCtrlInterfaceName(module, version, prefixI)
		newFuncName       = fmt.Sprintf(`New%s`, gstr.UcFirst(version))
		alreadyCreated    bool
	)
	if !gfile.Exists(moduleFilePath) {
		content := gstr.ReplaceByMap(consts.TemplateGenCtrlControllerEmpty, g.MapStrStr{
			"{Module}": module,
		})
		if err = gfile.PutContents(moduleFilePath, gstr.TrimLeft(content)); err != nil {
			return err
		}
		mlog.Printf(`generated: %s`, gfile.RealPath(moduleFilePath))
	}
	filePaths, err := gfile.ScanDir(dstModuleFolderPath, "*.go", false)
	if err != nil {
		return err
	}
	for _, filePath := range filePaths {
		if functionExists(filePath, newFuncName) {
			alreadyCreated = true
			break
		}
	}
	if !alreadyCreated {
		if !gfile.Exists(moduleFilePathNew) {
			content := gstr.ReplaceByMap(consts.TemplateGenCtrlControllerNewEmpty, g.MapStrStr{
				"{Module}":     module,
				"{ImportPath}": fmt.Sprintf(`"%s"`, importPath),
			})
			if err = gfile.PutContents(moduleFilePathNew, gstr.TrimLeft(content)); err != nil {
				return err
			}
			mlog.Printf(`generated: %s`, gfile.RealPath(moduleFilePathNew))
		}
		content := gstr.ReplaceByMap(consts.TemplateGenCtrlControllerNewFunc, g.MapStrStr{
			"{CtrlName}":      ctrlName,
			"{NewFuncName}":   newFuncName,
			"{InterfaceName}": interfaceName,
		})
		err = gfile.PutContentsAppend(moduleFilePathNew, content)
		if err != nil {
			return err
		}
	}
	return
}

func (c *controllerGenerator) doGenerateCtrlItem(dstModuleFolderPath string, item apiItem) (err error) {
	var (
		methodNameSnake = gstr.CaseSnake(item.MethodName)
		ctrlName        = fmt.Sprintf(`Controller%s`, gstr.UcFirst(item.Version))
		methodFilePath  = filepath.FromSlash(gfile.Join(dstModuleFolderPath, fmt.Sprintf(
			`%s_%s_%s.go`, item.Module, item.Version, methodNameSnake,
		)))
	)
	var content string

	if gfile.Exists(methodFilePath) {
		content = gstr.ReplaceByMap(consts.TemplateGenCtrlControllerMethodFuncMerge, g.MapStrStr{
			"{Module}":        item.Module,
			"{CtrlName}":      ctrlName,
			"{Version}":       item.Version,
			"{MethodName}":    item.MethodName,
			"{MethodComment}": item.GetComment(),
		})
		// Use AST-based checking for more accurate method detection
		if methodExists(methodFilePath, ctrlName, item.MethodName) {
			return
		}
		if err = gfile.PutContentsAppend(methodFilePath, gstr.TrimLeft(content)); err != nil {
			return err
		}
	} else {
		content = gstr.ReplaceByMap(consts.TemplateGenCtrlControllerMethodFunc, g.MapStrStr{
			"{Module}":        item.Module,
			"{ImportPath}":    item.Import,
			"{CtrlName}":      ctrlName,
			"{Version}":       item.Version,
			"{MethodName}":    item.MethodName,
			"{MethodComment}": item.GetComment(),
		})
		if err = gfile.PutContents(methodFilePath, gstr.TrimLeft(content)); err != nil {
			return err
		}
	}
	mlog.Printf(`generated: %s`, gfile.RealPath(methodFilePath))
	return
}

// use -merge
func (c *controllerGenerator) doGenerateCtrlMergeItem(dstModuleFolderPath string, apiItems []apiItem, doneApiSet *gset.StrSet) (err error) {
	type controllerFileItem struct {
		module     string
		version    string
		importPath string
		apis       []apiItem
	}
	// It is possible that there are multiple files under one module
	ctrlFileItemMap := make(map[string]*controllerFileItem)

	for _, api := range apiItems {
		ctrlFileItem, found := ctrlFileItemMap[api.FileName]
		if !found {
			ctrlFileItem = &controllerFileItem{
				module:     api.Module,
				version:    api.Version,
				importPath: api.Import,
				apis:       make([]apiItem, 0),
			}
			ctrlFileItemMap[api.FileName] = ctrlFileItem
		}
		ctrlFileItem.apis = append(ctrlFileItem.apis, api)
		doneApiSet.Add(api.String())
	}

	for ctrlFileName, ctrlFileItem := range ctrlFileItemMap {
		ctrlFilePath := gfile.Join(dstModuleFolderPath, fmt.Sprintf(
			`%s_%s_%s.go`, ctrlFileItem.module, ctrlFileItem.version, ctrlFileName,
		))
		ctrlName := fmt.Sprintf(`Controller%s`, gstr.UcFirst(ctrlFileItem.version))

		if !gfile.Exists(ctrlFilePath) {
			ctrlFileHeader := gstr.TrimLeft(gstr.ReplaceByMap(consts.TemplateGenCtrlControllerHeader, g.MapStrStr{
				"{Module}":     ctrlFileItem.module,
				"{ImportPath}": ctrlFileItem.importPath,
			}))
			ctrlFileContent := ctrlFileHeader
			for _, api := range ctrlFileItem.apis {
				ctrlFileContent += c.generateMergeCtrlMethod(ctrlName, api)
			}
			err = gfile.PutContents(ctrlFilePath, ctrlFileContent)
			if err != nil {
				return err
			}
		} else {
			var updated bool
			updated, err = c.insertMissingMergeCtrlMethods(ctrlFilePath, ctrlName, ctrlFileItem.apis)
			if err != nil {
				return err
			}
			if !updated {
				continue
			}
		}
		mlog.Printf(`generated: %s`, gfile.RealPath(ctrlFilePath))
	}
	return
}

func (c *controllerGenerator) generateMergeCtrlMethod(ctrlName string, api apiItem) string {
	return gstr.TrimLeft(gstr.ReplaceByMap(consts.TemplateGenCtrlControllerMethodFuncMerge, g.MapStrStr{
		"{Module}":        api.Module,
		"{CtrlName}":      ctrlName,
		"{Version}":       api.Version,
		"{MethodName}":    api.MethodName,
		"{MethodComment}": api.GetComment(),
	}))
}

func (c *controllerGenerator) insertMissingMergeCtrlMethods(ctrlFilePath, ctrlName string, apiItems []apiItem) (updated bool, err error) {
	content := gfile.GetContents(ctrlFilePath)
	methodOffsets, err := getMethodStartOffsets(ctrlFilePath, ctrlName)
	if err != nil {
		return false, err
	}

	type insertion struct {
		offset int
		text   string
	}
	insertions := make([]insertion, 0)
	var missingMethods strings.Builder
	for _, api := range apiItems {
		if offset, ok := methodOffsets[api.MethodName]; ok {
			if missingMethods.Len() > 0 {
				insertions = append(insertions, insertion{
					offset: offset,
					text:   missingMethods.String(),
				})
				missingMethods.Reset()
			}
			continue
		}
		missingMethods.WriteString(c.generateMergeCtrlMethod(ctrlName, api))
		updated = true
	}
	if missingMethods.Len() > 0 {
		insertions = append(insertions, insertion{
			offset: len(content),
			text:   missingMethods.String(),
		})
	}
	if !updated {
		return false, nil
	}

	var builder strings.Builder
	lastOffset := 0
	for _, item := range insertions {
		builder.WriteString(content[lastOffset:item.offset])
		builder.WriteString(item.text)
		lastOffset = item.offset
	}
	builder.WriteString(content[lastOffset:])
	if err = gfile.PutContents(ctrlFilePath, builder.String()); err != nil {
		return false, err
	}
	return true, nil
}

// methodExists checks if a method with the given receiver type and name exists in the file.
// It uses AST parsing to accurately detect method definitions regardless of formatting.
// This handles various code formatting styles including multi-line method signatures.
func methodExists(filePath, ctrlName, methodName string) bool {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		// If parsing fails (e.g., file doesn't exist or invalid syntax), return false
		return false
	}
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		// Check if it's a method (has receiver)
		if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
			// Extract receiver type name
			// Handle both *T and T patterns
			recvType := ""
			switch t := funcDecl.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				if ident, ok := t.X.(*ast.Ident); ok {
					recvType = ident.Name
				}
			case *ast.Ident:
				recvType = t.Name
			}

			// Check if both receiver type and method name match
			if recvType == ctrlName && funcDecl.Name.Name == methodName {
				return true
			}
		}
	}
	return false
}

func formatCtrlInterfaceName(module, version string, prefixI bool) string {
	return fmt.Sprintf(`%s.%s`, module, formatInterfaceTypeName(module, version, prefixI))
}

func formatInterfaceTypeName(module, version string, prefixI bool) string {
	name := fmt.Sprintf(`%s%s`, gstr.CaseCamel(module), gstr.UcFirst(version))
	if prefixI {
		return "I" + name
	}
	return name
}

// functionExists checks if a plain function with the given name exists in the file.
// It uses AST parsing so formatting differences, including multi-line signatures,
// do not trigger duplicate generation.
func functionExists(filePath, funcName string) bool {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return false
	}
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv != nil {
			continue
		}
		if funcDecl.Name.Name == funcName {
			return true
		}
	}
	return false
}

func getMethodStartOffsets(filePath, ctrlName string) (map[string]int, error) {
	content := gfile.GetContents(filePath)
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	methodOffsets := make(map[string]int)
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Recv == nil || len(funcDecl.Recv.List) == 0 {
			continue
		}
		recvType := ""
		switch t := funcDecl.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				recvType = ident.Name
			}
		case *ast.Ident:
			recvType = t.Name
		}
		if recvType != ctrlName {
			continue
		}
		pos := funcDecl.Pos()
		if funcDecl.Doc != nil {
			pos = funcDecl.Doc.Pos()
		}
		methodOffsets[funcDecl.Name.Name] = fset.Position(pos).Offset
	}
	return methodOffsets, nil
}
