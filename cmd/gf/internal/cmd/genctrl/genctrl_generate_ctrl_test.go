package genctrl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/container/gset"
)

func TestDoGenerateCtrlNewByModuleAndVersionSkipsExistingNewFunc(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	moduleFilePath := filepath.Join(dir, "user.go")
	original := strings.TrimLeft(`
package user

func NewV1(
) any {
	return nil
}
`, "\n")

	if err := os.WriteFile(moduleFilePath, []byte(original), 0644); err != nil {
		t.Fatalf("write module file: %v", err)
	}

	err := newControllerGenerator().doGenerateCtrlNewByModuleAndVersion(
		dir, "user", "v1", "example.com/api/user/v1", true,
	)
	if err != nil {
		t.Fatalf("generate controller new func: %v", err)
	}

	content, err := os.ReadFile(moduleFilePath)
	if err != nil {
		t.Fatalf("read module file: %v", err)
	}
	if string(content) != original {
		t.Fatalf("existing module file changed unexpectedly:\n%s", string(content))
	}

	if _, err = os.Stat(filepath.Join(dir, "user_new.go")); !os.IsNotExist(err) {
		t.Fatalf("user_new.go should not be created when NewV1 already exists")
	}
}

func TestDoGenerateCtrlNewByModuleAndVersionCreatesNewFuncWhenMissing(t *testing.T) {
	t.Helper()

	dir := t.TempDir()

	err := newControllerGenerator().doGenerateCtrlNewByModuleAndVersion(
		dir, "user", "v1", "example.com/api/user/v1", true,
	)
	if err != nil {
		t.Fatalf("generate controller new func: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "user_new.go"))
	if err != nil {
		t.Fatalf("read generated module new file: %v", err)
	}
	if !strings.Contains(string(content), "func NewV1()") {
		t.Fatalf("generated file does not contain NewV1:\n%s", string(content))
	}
}

func TestDoGenerateCtrlNewByModuleAndVersionRespectsPrefixI(t *testing.T) {
	t.Helper()

	dir := t.TempDir()

	err := newControllerGenerator().doGenerateCtrlNewByModuleAndVersion(
		dir, "user", "v1", "example.com/api/user/v1", false,
	)
	if err != nil {
		t.Fatalf("generate controller new func: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "user_new.go"))
	if err != nil {
		t.Fatalf("read generated module new file: %v", err)
	}
	if !strings.Contains(string(content), "func NewV1() user.UserV1") {
		t.Fatalf("generated file does not contain non-prefixed interface return type:\n%s", string(content))
	}
}

func TestDoGenerateCtrlNewByModuleAndVersionKeepsLegacyPrefixedReturnType(t *testing.T) {
	t.Helper()

	apiDir := t.TempDir()
	legacyInterface := strings.TrimLeft(`
package user

type IUserV1 interface {
	GetProfile()
}
`, "\n")
	if err := os.WriteFile(filepath.Join(apiDir, "user.go"), []byte(legacyInterface), 0644); err != nil {
		t.Fatalf("write legacy interface file: %v", err)
	}

	items := []apiItem{
		{
			Import:     "example.com/api/user/v1",
			Module:     "user",
			Version:    "v1",
			MethodName: "GetProfile",
		},
	}
	prefixByVersion, err := (CGenCtrl{}).resolveInterfacePrefixIByVersion(apiDir, items, false, true)
	if err != nil {
		t.Fatalf("resolve interface naming: %v", err)
	}

	ctrlDir := t.TempDir()
	err = newControllerGenerator().doGenerateCtrlNewByModuleAndVersion(
		ctrlDir, "user", "v1", "example.com/api/user/v1", prefixByVersion["v1"],
	)
	if err != nil {
		t.Fatalf("generate controller new func: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(ctrlDir, "user_new.go"))
	if err != nil {
		t.Fatalf("read generated module new file: %v", err)
	}
	if !strings.Contains(string(content), "func NewV1() user.IUserV1") {
		t.Fatalf("generated file does not keep legacy I-prefixed interface return type:\n%s", string(content))
	}
}

func TestDoGenerateCtrlMergeItemPreservesApiOrderWhenInsertingMissingMethods(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	ctrlFilePath := filepath.Join(dir, "user_v1_profile.go")
	original := strings.TrimLeft(`
package user

import (
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"

	"example.com/api/user/v1"
)

// GetProfile keeps existing implementation.
func (c *ControllerV1) GetProfile(ctx context.Context, req *v1.GetProfileReq) (res *v1.GetProfileRes, err error) {
	return nil, gerror.NewCode(gcode.CodeNotImplemented)
}
`, "\n")
	if err := os.WriteFile(ctrlFilePath, []byte(original), 0644); err != nil {
		t.Fatalf("write merged controller file: %v", err)
	}

	apiItems := []apiItem{
		{Import: "example.com/api/user/v1", FileName: "profile", Module: "user", Version: "v1", MethodName: "GetList"},
		{Import: "example.com/api/user/v1", FileName: "profile", Module: "user", Version: "v1", MethodName: "Create"},
		{Import: "example.com/api/user/v1", FileName: "profile", Module: "user", Version: "v1", MethodName: "GetProfile", Comment: "keeps existing implementation."},
	}

	err := newControllerGenerator().doGenerateCtrlMergeItem(dir, apiItems, gset.NewStrSet())
	if err != nil {
		t.Fatalf("generate merged controller methods: %v", err)
	}

	content, err := os.ReadFile(ctrlFilePath)
	if err != nil {
		t.Fatalf("read merged controller file: %v", err)
	}
	text := string(content)

	getListIndex := strings.Index(text, "func (c *ControllerV1) GetList")
	createIndex := strings.Index(text, "func (c *ControllerV1) Create")
	getProfileIndex := strings.Index(text, "func (c *ControllerV1) GetProfile")
	if !(getListIndex >= 0 && createIndex >= 0 && getProfileIndex >= 0) {
		t.Fatalf("expected merged file to contain all controller methods:\n%s", text)
	}
	if !(getListIndex < createIndex && createIndex < getProfileIndex) {
		t.Fatalf("expected generated methods to follow api definition order:\n%s", text)
	}
	if strings.Count(text, "func (c *ControllerV1) GetProfile") != 1 {
		t.Fatalf("expected existing method to be kept without duplication:\n%s", text)
	}
	if !strings.Contains(text, "// GetProfile keeps existing implementation.") {
		t.Fatalf("expected existing method implementation to be preserved:\n%s", text)
	}
}

func TestDoGenerateCtrlMergeItemSkipsWhenAllMethodsExist(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	ctrlFilePath := filepath.Join(dir, "user_v1_profile.go")
	original := strings.TrimLeft(`
package user

import (
	"context"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"

	"example.com/api/user/v1"
)

func (c *ControllerV1) GetList(ctx context.Context, req *v1.GetListReq) (res *v1.GetListRes, err error) {
	return nil, gerror.NewCode(gcode.CodeNotImplemented)
}

// GetProfile keeps existing implementation.
func (c *ControllerV1) GetProfile(ctx context.Context, req *v1.GetProfileReq) (res *v1.GetProfileRes, err error) {
	return nil, gerror.NewCode(gcode.CodeNotImplemented)
}
`, "\n")
	if err := os.WriteFile(ctrlFilePath, []byte(original), 0644); err != nil {
		t.Fatalf("write merged controller file: %v", err)
	}

	apiItems := []apiItem{
		{Import: "example.com/api/user/v1", FileName: "profile", Module: "user", Version: "v1", MethodName: "GetList"},
		{Import: "example.com/api/user/v1", FileName: "profile", Module: "user", Version: "v1", MethodName: "GetProfile", Comment: "keeps existing implementation."},
	}

	err := newControllerGenerator().doGenerateCtrlMergeItem(dir, apiItems, gset.NewStrSet())
	if err != nil {
		t.Fatalf("generate merged controller methods: %v", err)
	}

	content, err := os.ReadFile(ctrlFilePath)
	if err != nil {
		t.Fatalf("read merged controller file: %v", err)
	}
	if string(content) != original {
		t.Fatalf("expected merged controller file to remain unchanged when all methods exist:\n%s", string(content))
	}
}
