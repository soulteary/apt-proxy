package vfs

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	goTestFile = "go1.3.src.tar.gz"
)

type errNoTestFile string

func (e errNoTestFile) Error() string {
	return fmt.Sprintf("%s test file not found, use testdata/download-data.sh to fetch it", filepath.Base(string(e)))
}

func openOptionalTestFile(t testing.TB, name string) *os.File {
	filename := filepath.Join("testdata", name)
	f, err := os.Open(filename)
	if err != nil {
		t.Skip(errNoTestFile(filename))
	}
	return f
}

func testVFS(t *testing.T, fs VFS) {
	if err := WriteFile(fs, "a", []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(fs, "a")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A" {
		t.Errorf("expecting file a to contain \"A\" got %q instead", string(data))
	}
	if err := WriteFile(fs, "b", []byte("B"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.OpenFile("b", os.O_CREATE|os.O_TRUNC|os.O_EXCL|os.O_WRONLY, 0755); err == nil || !IsExist(err) {
		t.Errorf("error should be ErrExist, it's %v", err)
	}
	fb, err := fs.OpenFile("b", os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatalf("error opening b: %s", err)
	}
	if _, err := fb.Write([]byte("BB")); err != nil {
		t.Errorf("error writing to b: %s", err)
	}
	if _, err := fb.Seek(0, os.SEEK_SET); err != nil {
		t.Errorf("error seeking b: %s", err)
	}
	if _, err := fb.Read(make([]byte, 2)); err == nil {
		t.Error("allowed reading WRONLY file b")
	}
	if err := fb.Close(); err != nil {
		t.Errorf("error closing b: %s", err)
	}
	files, err := fs.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expecting 2 files, got %d", len(files))
	}
	if n := files[0].Name(); n != "a" {
		t.Errorf("expecting first file named \"a\", got %q", n)
	}
	if n := files[1].Name(); n != "b" {
		t.Errorf("expecting first file named \"b\", got %q", n)
	}
	for ii, v := range files {
		es := int64(ii + 1)
		if s := v.Size(); es != s {
			t.Errorf("expecting file %s to have size %d, has %d", v.Name(), es, s)
		}
	}
	if err := MkdirAll(fs, "a/b/c/d", 0); err == nil {
		t.Error("should not allow dir over file")
	}
	if err := MkdirAll(fs, "c/d", 0755); err != nil {
		t.Fatal(err)
	}
	// Idempotent
	if err := MkdirAll(fs, "c/d", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir("c", 0755); err == nil || !IsExist(err) {
		t.Errorf("err should be ErrExist, it's %v", err)
	}
	// Should fail to remove, c is not empty
	if err := fs.Remove("c"); err == nil {
		t.Fatalf("removed non-empty directory")
	}
	var walked []os.FileInfo
	var walkedNames []string
	err = Walk(fs, "c", func(fs VFS, path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		walked = append(walked, info)
		walkedNames = append(walkedNames, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if exp := []string{"c", "c/d"}; !reflect.DeepEqual(exp, walkedNames) {
		t.Error("expecting walked names %v, got %v", exp, walkedNames)
	}
	for _, v := range walked {
		if !v.IsDir() {
			t.Errorf("%s should be a dir", v.Name())
		}
	}
	if err := RemoveAll(fs, "c"); err != nil {
		t.Fatal(err)
	}
	err = Walk(fs, "c", func(fs VFS, path string, info os.FileInfo, err error) error {
		return err
	})
	if err == nil || !IsNotExist(err) {
		t.Errorf("error should be ErrNotExist, it's %v", err)
	}
}

func TestMapFS(t *testing.T) {
	fs, err := Map(nil)
	if err != nil {
		t.Fatal(err)
	}
	testVFS(t, fs)
}

func TestPopulatedMap(t *testing.T) {
	files := map[string]*File{
		"a/1": &File{},
		"a/2": &File{},
	}
	fs, err := Map(files)
	if err != nil {
		t.Fatal(err)
	}
	infos, err := fs.ReadDir("a")
	if err != nil {
		t.Fatal(err)
	}
	if c := len(infos); c != 2 {
		t.Fatalf("expecting 2 files in a, got %d", c)
	}
	if infos[0].Name() != "1" || infos[1].Name() != "2" {
		t.Errorf("expecting names 1, 2 got %q, %q", infos[0].Name(), infos[1].Name())
	}
}

func TestBadPopulatedMap(t *testing.T) {
	// 1 can't be file and directory
	files := map[string]*File{
		"a/1":   &File{},
		"a/1/2": &File{},
	}
	_, err := Map(files)
	if err == nil {
		t.Fatal("Map should not work with a path as both file and directory")
	}
}

func TestTmpFS(t *testing.T) {
	fs, err := TmpFS("vfs-test")
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	testVFS(t, fs)
}

const (
	go13FileCount = 4157
	// +1 because of the root, the real count is 407
	go13DirCount = 407 + 1
)

func countFileSystem(fs VFS) (int, int, error) {
	files, dirs := 0, 0
	err := Walk(fs, "/", func(fs VFS, _ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			dirs++
		} else {
			files++
		}
		return nil
	})
	return files, dirs, err
}

func testGoFileCount(t *testing.T, fs VFS) {
	files, dirs, err := countFileSystem(fs)
	if err != nil {
		t.Fatal(err)
	}
	if files != go13FileCount {
		t.Errorf("expecting %d files in go1.3, got %d instead", go13FileCount, files)
	}
	if dirs != go13DirCount {
		t.Errorf("expecting %d directories in go1.3, got %d instead", go13DirCount, dirs)
	}
}

func TestGo13Files(t *testing.T) {
	f := openOptionalTestFile(t, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		t.Fatal(err)
	}
	testGoFileCount(t, fs)
}

func TestMounter(t *testing.T) {
	m := &Mounter{}
	f := openOptionalTestFile(t, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		t.Fatal(err)
	}
	m.Mount(fs, "/")
	testGoFileCount(t, m)
}

func TestClone(t *testing.T) {
	fs, err := Open(filepath.Join("testdata", "fs.zip"))
	if err != nil {
		t.Fatal(err)
	}
	infos1, err := fs.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	mem1 := Memory()
	if err := Clone(mem1, fs); err != nil {
		t.Fatal(err)
	}
	infos2, err := mem1.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos2) != len(infos1) {
		t.Fatalf("cloned fs has %d entries in / rather than %d", len(infos2), len(infos1))
	}
	mem2 := Memory()
	if err := Clone(mem2, mem1); err != nil {
		t.Fatal(err)
	}
	infos3, err := mem2.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos3) != len(infos2) {
		t.Fatalf("cloned fs has %d entries in / rather than %d", len(infos3), len(infos2))
	}
}

func measureVFSMemorySize(t testing.TB, fs VFS) int {
	mem, ok := fs.(*memoryFileSystem)
	if !ok {
		t.Fatalf("%T is not a memory filesystem", fs)
	}
	var total int
	var f func(d *Dir)
	f = func(d *Dir) {
		for _, v := range d.Entries {
			total += int(v.Size())
			if sd, ok := v.(*Dir); ok {
				f(sd)
			}
		}
	}
	f(mem.root)
	return total
}

func hashVFS(t testing.TB, fs VFS) string {
	sha := sha1.New()
	err := Walk(fs, "/", func(fs VFS, p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		f, err := fs.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(sha, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sha.Sum(nil))
}

func TestCompress(t *testing.T) {
	f := openOptionalTestFile(t, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		t.Fatal(err)
	}
	size1 := measureVFSMemorySize(t, fs)
	hash1 := hashVFS(t, fs)
	if err := Compress(fs); err != nil {
		t.Fatalf("can't compress fs: %s", err)
	}
	testGoFileCount(t, fs)
	size2 := measureVFSMemorySize(t, fs)
	hash2 := hashVFS(t, fs)
	if size2 >= size1 {
		t.Fatalf("compressed fs takes more memory %d than bare fs %d", size2, size1)
	}
	if hash1 != hash2 {
		t.Fatalf("compressing fs changed hash from %s to %s", hash1, hash2)
	}
}
