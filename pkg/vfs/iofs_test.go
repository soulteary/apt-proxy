package vfs

import (
	"io/fs"
	"path"
	"testing"
)

// TestAsReadOnlyFS verifies that a VFS can be used as io/fs.FS for read-only
// paths (fs.ReadFile, fs.WalkDir, etc.) without changing the VFS interface.
func TestAsReadOnlyFS(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "x", []byte("xxx"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/b", []byte("ab"), 0644); err != nil {
		t.Fatal(err)
	}

	ro := AsReadOnlyFS(mem)

	// fs.ReadFile
	data, err := fs.ReadFile(ro, "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "xxx" {
		t.Errorf("fs.ReadFile(ro, \"x\") = %q, want \"xxx\"", data)
	}
	data, err = fs.ReadFile(ro, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ab" {
		t.Errorf("fs.ReadFile(ro, \"a/b\") = %q, want \"ab\"", data)
	}
	// root as ".": ReadFile on a directory must fail
	_, err = fs.ReadFile(ro, ".")
	if err == nil {
		t.Error("fs.ReadFile(ro, \".\") should fail for directory")
	}

	// fs.WalkDir (read-only path using io/fs)
	var names []string
	err = fs.WalkDir(ro, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, path.Clean(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// expect ".", "x", "a", "a/b" (order may vary)
	haveRoot, haveX, haveA, haveAB := false, false, false, false
	for _, n := range names {
		switch n {
		case ".", "/":
			haveRoot = true
		case "x":
			haveX = true
		case "a":
			haveA = true
		case "a/b":
			haveAB = true
		}
	}
	if !haveRoot {
		t.Errorf("WalkDir did not visit root, got names %v", names)
	}
	if !haveX {
		t.Errorf("WalkDir did not visit x, got names %v", names)
	}
	if !haveA {
		t.Errorf("WalkDir did not visit a, got names %v", names)
	}
	if !haveAB {
		t.Errorf("WalkDir did not visit a/b, got names %v", names)
	}
}

// TestAsReadOnlyFS_OpenDir verifies Open(".") returns a directory that supports ReadDir.
func TestAsReadOnlyFS_OpenDir(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("f"), 0644)

	ro := AsReadOnlyFS(mem)
	f, err := ro.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	df, ok := f.(fs.ReadDirFile)
	if !ok {
		t.Fatal("Open(\".\") should return fs.ReadDirFile for directory")
	}
	entries, err := df.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("ReadDir(-1) got %d entries, want 1", len(entries))
	}
	if len(entries) > 0 && entries[0].Name() != "f" {
		t.Errorf("first entry Name() = %q, want \"f\"", entries[0].Name())
	}
}
