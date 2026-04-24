package genctrl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApiInterfaceGeneratorRespectsPrefixI(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	items := []apiItem{
		{
			Import:     "example.com/api/user/v1",
			Module:     "user",
			Version:    "v1",
			MethodName: "GetProfile",
			Comment:    "gets profile information\nwith multiline description",
		},
	}

	err := newApiInterfaceGenerator().Generate(dir, items, map[string]bool{"v1": false})
	if err != nil {
		t.Fatalf("generate api interface: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "user.go"))
	if err != nil {
		t.Fatalf("read generated interface file: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "type UserV1 interface {") {
		t.Fatalf("generated file does not contain non-prefixed interface name:\n%s", text)
	}
	if strings.Contains(text, "type IUserV1 interface {") {
		t.Fatalf("generated file still contains I-prefixed interface name:\n%s", text)
	}
	if !strings.Contains(text, "\t// GetProfile gets profile information") {
		t.Fatalf("generated file does not contain interface method comment:\n%s", text)
	}
	if !strings.Contains(text, "\t// with multiline description") {
		t.Fatalf("generated file does not contain multiline interface method comment:\n%s", text)
	}
}

func TestResolveInterfacePrefixIByVersionKeepsLegacyPrefixedInterfaces(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	content := strings.TrimLeft(`
package user

type IUserV1 interface {
	GetProfile()
}
`, "\n")
	if err := os.WriteFile(filepath.Join(dir, "user.go"), []byte(content), 0644); err != nil {
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

	prefixByVersion, err := (CGenCtrl{}).resolveInterfacePrefixIByVersion(dir, items, false, true)
	if err != nil {
		t.Fatalf("resolve interface naming: %v", err)
	}
	if !prefixByVersion["v1"] {
		t.Fatalf("expected legacy I-prefixed interface to be kept")
	}
}

func TestResolveInterfacePrefixIByVersionAllowsRenamingLegacyInterfaces(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	content := strings.TrimLeft(`
package user

type IUserV1 interface {
	GetProfile()
}
`, "\n")
	if err := os.WriteFile(filepath.Join(dir, "user.go"), []byte(content), 0644); err != nil {
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

	prefixByVersion, err := (CGenCtrl{}).resolveInterfacePrefixIByVersion(dir, items, false, false)
	if err != nil {
		t.Fatalf("resolve interface naming: %v", err)
	}
	if prefixByVersion["v1"] {
		t.Fatalf("expected legacy I-prefixed interface to be renamed when keepOldPrefixI=false")
	}
}

func TestGetStructsNameInSrcReturnsErrorWhenResponseStructMissing(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "user.go")
	content := strings.TrimLeft(`
package v1

import "github.com/gogf/gf/v2/frame/g"

type GetProfileReq struct {
	g.Meta `+"`path:\"/profile\" method:\"get\"`"+`
}
`, "\n")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("write api definition file: %v", err)
	}

	_, err := (CGenCtrl{}).getStructsNameInSrc(filePath)
	if err == nil {
		t.Fatalf("expected missing response struct error")
	}
	if !strings.Contains(err.Error(), `missing response struct "GetProfileRes"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
