package genctrl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		dir, "user", "v1", "example.com/api/user/v1",
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
		dir, "user", "v1", "example.com/api/user/v1",
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
