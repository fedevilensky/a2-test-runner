package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestCPPDependencies(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ unavailable")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "student with spaces")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(dir, "ejercicio1.cpp"): "#include \"first header.h\"\n" +
			"int main() {return answer;}\n",
		filepath.Join(dir, "first header.h"): "#include \"../shared.h\"\n",
		filepath.Join(root, "shared.h"):      "const int answer = 0;\n",
		filepath.Join(dir, "unrelated.h"):    "// unused\n",
	}
	for path, data := range files {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := cppDependencies(
		context.Background(), dir, filepath.Join(dir, "ejercicio1.cpp"), []string{"-std=c++11"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "ejercicio1.cpp"), filepath.Join(dir, "first header.h"),
		filepath.Join(root, "shared.h"),
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %v, want %v", got, want)
	}
}

func TestCPPDependenciesRejectSymlinks(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ unavailable")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.h"), []byte("// header"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.h", filepath.Join(dir, "link.h")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "ejercicio1.cpp")
	if err := os.WriteFile(source, []byte("#include \"link.h\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cppDependencies(context.Background(), dir, source, nil); err == nil {
		t.Fatal("expected symlink to disable caching")
	}
}

func TestParseMakeDependencies(t *testing.T) {
	got, err := parseMakeDependencies(
		"a2_dependencies: /tmp/a\\ b /tmp/\\#hash /tmp/$$dollar \\\n /tmp/other\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/a b", "/tmp/#hash", "/tmp/$dollar", "/tmp/other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
