package s3vfs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/resource"
)

func s3BudgetManager(t *testing.T, total int64) *resource.Manager {
	t.Helper()
	m, err := resource.New(resource.Config{MaxObjectBytes: 64, TotalTempBytes: total, MaxActiveDownloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestS3SpillCountsInlineAndDiskDuringTransition(t *testing.T) {
	budget := s3BudgetManager(t, 12)
	w := &s3WFile{ctx: context.Background(), inlineMax: 8, tmpDir: t.TempDir(), budget: budget}
	if _, err := w.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("9")); !errors.Is(err, resource.ErrAdmission) {
		t.Fatalf("unaccounted inline+disk overlap: %v", err)
	}
	if s := budget.Stats(); s.TemporaryBytes != 8 || s.ByKind[resource.StageS3Inline] != 8 {
		t.Fatalf("failed transition ledger=%+v", s)
	}
	if err := w.Close(); !errors.Is(err, resource.ErrAdmission) {
		t.Fatalf("failed write attempted upload: %v", err)
	}
	if budget.Stats().TemporaryBytes != 0 {
		t.Fatalf("inline reservation retained: %+v", budget.Stats())
	}
	entries, err := os.ReadDir(w.tmpDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("spill orphan=%v %v", entries, err)
	}
}
func TestS3SpillMovesChargeAndUploadFailureReleases(t *testing.T) {
	budget := s3BudgetManager(t, 24)
	fs, backend := newMemoryVFS(t, 8)
	fs.budget = budget
	wf, err := fs.OpenFile("package", os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	w := wf.(*s3WFile)
	if _, err := w.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("90")); err != nil {
		t.Fatal(err)
	}
	if s := budget.Stats(); s.TemporaryBytes != 10 || s.ByKind[resource.StageS3Inline] != 0 || s.ByKind[resource.StageS3Spill] != 10 || s.PeakTemporaryBytes != 16 {
		t.Fatalf("spill charge transfer=%+v", s)
	}
	backend.failUploads = true
	if err := w.Close(); err == nil {
		t.Fatal("injected upload failure accepted")
	}
	if s := budget.Stats(); s.TemporaryBytes != 0 {
		t.Fatalf("failed upload retained copy: %+v", s)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

type blockingBudgetS3 struct {
	base    *memoryS3
	entered chan struct{}
}

func (b *blockingBudgetS3) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPut {
		close(b.entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	return b.base.RoundTrip(r)
}
func TestS3UploadDeadlineReleasesInlineReservation(t *testing.T) {
	budget := s3BudgetManager(t, 64)
	transport := &blockingBudgetS3{base: &memoryS3{objects: map[string]string{}}, entered: make(chan struct{})}
	fs, err := New(context.Background(), Config{Endpoint: "s3.test", Region: "us-east-1", Bucket: "test-bucket", UsePathStyle: true, InlineMaxBytes: 32, TempDir: t.TempDir(), StagingBudget: budget, UploadTimeout: 25 * time.Millisecond, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := fs.OpenFile("object", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "bounded-upload"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Close() }()
	select {
	case <-transport.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("PUT did not start")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("hung PUT succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PUT ignored deadline")
	}
	if budget.Stats().TemporaryBytes != 0 {
		t.Fatalf("timed-out PUT retained reservation: %+v", budget.Stats())
	}
}

type singlePutOnlyS3 struct {
	base *memoryS3
	puts int
}

func (b *singlePutOnlyS3) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Query().Has("uploads") || r.URL.Query().Has("partNumber") {
		return nil, errors.New("multipart SDK staging is forbidden")
	}
	if r.Method == http.MethodPut {
		b.puts++
	}
	return b.base.RoundTrip(r)
}
func TestS3LargeUploadUsesBoundedSinglePut(t *testing.T) {
	budget := s3BudgetManager(t, 24<<20)
	transport := &singlePutOnlyS3{base: &memoryS3{objects: map[string]string{}}}
	fs, err := New(context.Background(), Config{Endpoint: "s3.test", Region: "us-east-1", Bucket: "test-bucket", UsePathStyle: true, InlineMaxBytes: 1 << 20, TempDir: t.TempDir(), StagingBudget: budget, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := fs.OpenFile("large", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for i := 0; i < 17; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if transport.puts != 1 || len(transport.base.objects["large"]) != 17<<20 || budget.Stats().TemporaryBytes != 0 {
		t.Fatalf("single PUT lifecycle: puts=%d size=%d stats=%+v", transport.puts, len(transport.base.objects["large"]), budget.Stats())
	}
}
func TestS3SinglePutCapRejectsBeforeAllocating(t *testing.T) {
	w := &s3WFile{written: maxBoundedObjectBytes, inlineMax: 32, tmpDir: t.TempDir()}
	if _, err := w.Write([]byte("x")); !errors.Is(err, resource.ErrObjectTooLarge) {
		t.Fatalf("single PUT overflow accepted: %v", err)
	}
	if w.inlineBuf.Len() != 0 || w.tmp != nil {
		t.Fatal("oversized S3 object allocated a new copy")
	}
	if err := w.Close(); !errors.Is(err, resource.ErrObjectTooLarge) {
		t.Fatalf("oversized S3 object attempted PUT: %v", err)
	}
}
