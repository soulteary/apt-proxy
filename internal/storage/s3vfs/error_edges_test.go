package s3vfs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestTranslateObjectStoreErrors(t *testing.T) {
	denied := errors.New("denied")
	for _, tc := range []struct{ err, want error }{
		{nil, nil},
		{minio.ErrorResponse{Code: "NoSuchKey"}, os.ErrNotExist},
		{minio.ErrorResponse{Code: "NoSuchBucket"}, os.ErrNotExist},
		{minio.ErrorResponse{Code: "NotFound"}, os.ErrNotExist},
		{minio.ErrorResponse{Code: "Other", StatusCode: 404}, os.ErrNotExist},
		{fmt.Errorf("wrapped: %w", io.EOF), io.EOF},
		{errors.New("key does not exist"), os.ErrNotExist},
		{denied, denied},
	} {
		got := translateError("open", "package.deb", tc.err)
		if !errors.Is(got, tc.want) {
			t.Fatalf("%v mapped to %v, want %v", tc.err, got, tc.want)
		}
		if tc.err != nil {
			var pe *os.PathError
			if !errors.As(got, &pe) || pe.Op != "open" || pe.Path != "package.deb" {
				t.Fatal("lost path context")
			}
		}
	}
	if isNotFound(nil) {
		t.Fatal("nil recognized as missing")
	}
}

func TestSpillDescriptorFailureRejectsPublication(t *testing.T) {
	for _, operation := range []string{"write", "close"} {
		t.Run(operation, func(t *testing.T) {
			fs, _ := newMemoryVFS(t, 1)
			handle, err := fs.OpenFile("broken", os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			w := handle.(*s3WFile)
			if _, err := w.Write([]byte("spill")); err != nil {
				t.Fatal(err)
			}
			if err := w.tmp.Close(); err != nil {
				t.Fatal(err)
			}
			if operation == "write" {
				if _, err := w.Write([]byte("more")); err == nil {
					t.Fatal("closed spill accepted bytes")
				}
			}
			if err := w.Close(); err == nil {
				t.Fatal("closed spill published")
			}
		})
	}
}
