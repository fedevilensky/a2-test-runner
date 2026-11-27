package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestJavaDependencies(t *testing.T) {
	if _, err := exec.LookPath("javac"); err != nil {
		t.Skip("javac unavailable")
	}
	dir := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Ejercicio1.java", "import pkg.Helper; class Ejercicio1 { int value = Helper.value(); }")
	write("pkg/Helper.java", "package pkg; public class Helper { "+
		"public static int value() { return Other.value(); } }")
	write("pkg/Other.java", "package pkg; class Other { static int value() { return 1; } }")
	write("Unrelated.java", "class Unrelated {}")
	source := filepath.Join(dir, "Ejercicio1.java")
	deps, err := javaDependencies(context.Background(), dir, source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		source, filepath.Join(dir, "pkg/Helper.java"), filepath.Join(dir, "pkg/Other.java"),
	}
	if !reflect.DeepEqual(deps, want) {
		t.Fatalf("dependencies = %v, want %v", deps, want)
	}
	write("Unrelated.java", "class Unrelated { int changed; }")
	after, err := javaDependencies(context.Background(), dir, source)
	if err != nil || !reflect.DeepEqual(after, want) {
		t.Fatalf("after unrelated change: %v, %v", after, err)
	}
	write("pkg/Other.java", "package pkg; class Other { "+
		"static int value() { return NewDependency.value(); } }")
	write("pkg/NewDependency.java", "package pkg; class NewDependency { "+
		"static int value() { return 2; } }")
	after, err = javaDependencies(context.Background(), dir, source)
	if err != nil || len(after) != 4 || after[2] != filepath.Join(dir, "pkg/NewDependency.java") {
		t.Fatalf("after dependency added: %v, %v", after, err)
	}
}

func TestJavaDependenciesRejectSymlink(t *testing.T) {
	if _, err := exec.LookPath("javac"); err != nil {
		t.Skip("javac unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "Ejercicio1.java")
	if err := os.WriteFile(source, []byte("class Ejercicio1 { Helper h; }"), 0o600); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "Helper.java")
	if err := os.WriteFile(external, []byte("class Helper {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(dir, "Helper.java")); err != nil {
		t.Fatal(err)
	}
	if _, err := javaDependencies(context.Background(), dir, source); err == nil {
		t.Fatal("symlink dependency must disable caching")
	}
}

func TestJavaDependenciesDetectsNewShadowAndClasses(t *testing.T) {
	if _, err := exec.LookPath("javac"); err != nil {
		t.Skip("javac unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "Ejercicio1.java")
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Ejercicio1.java", "class Ejercicio1 { int x = Math.abs(-1); }")
	before, err := javaDependencies(context.Background(), dir, source)
	if err != nil || len(before) != 1 {
		t.Fatalf("initial dependencies: %v, %v", before, err)
	}
	write("Math.java", "class Math { static int abs(int x) { return x; } }")
	after, err := javaDependencies(context.Background(), dir, source)
	if err != nil || len(after) != 2 || after[1] != filepath.Join(dir, "Math.java") {
		t.Fatalf("shadowed dependencies: %v, %v", after, err)
	}
	cmd := exec.Command("javac", "Math.java")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile helper: %s: %v", output, err)
	}
	if err := os.Remove(filepath.Join(dir, "Math.java")); err != nil {
		t.Fatal(err)
	}
	after, err = javaDependencies(context.Background(), dir, source)
	if err != nil || len(after) != 2 || after[1] != filepath.Join(dir, "Math.class") {
		t.Fatalf("class dependencies: %v, %v", after, err)
	}
}
