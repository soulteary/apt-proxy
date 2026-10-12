package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soulteary/apt-proxy/internal/repository"
)

// RunRepository executes an explicit repository snapshot command.
func RunRepository(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("repository command required: prefetch, verify, repair, export, import, serve")
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(output, "Usage: apt-proxy repository {prefetch|verify|repair|export|import|serve} --dir DIRECTORY [options]\nUse a subcommand with --help for its flags. APT verifies repository signatures.")
		return err
	}
	command := args[0]
	flags := flag.NewFlagSet("repository "+command, flag.ContinueOnError)
	flags.SetOutput(output)
	dir := flags.String("dir", "", "snapshot store directory (required)")
	var limits repository.Limits
	flags.Int64Var(&limits.MaxFileBytes, "max-file-bytes", 0, "maximum compressed/object bytes (default 2 GiB)")
	flags.Int64Var(&limits.MaxTotalBytes, "max-total-bytes", 0, "maximum unique snapshot bytes (default 128 GiB)")
	flags.Int64Var(&limits.MaxIndexBytes, "max-index-bytes", 0, "maximum decompressed index bytes (default 128 MiB)")
	flags.Int64Var(&limits.MaxManifestBytes, "max-manifest-bytes", 0, "maximum manifest bytes (default 16 MiB)")
	flags.IntVar(&limits.MaxObjects, "max-objects", 0, "maximum manifest paths, including aliases (default 1000000)")
	var repoURL, suite, components, architectures, filename, repairPaths, listen string
	var sources bool
	switch command {
	case "prefetch":
		flags.StringVar(&repoURL, "url", "", "HTTP(S) repository URL, without credentials or query")
		flags.StringVar(&suite, "suite", "", "distribution suite, e.g. bookworm")
		flags.StringVar(&components, "components", "main", "comma-separated components")
		flags.StringVar(&architectures, "architectures", "amd64", "comma-separated binary architectures")
		flags.BoolVar(&sources, "sources", false, "include Sources and every referenced source file")
	case "export", "import":
		flags.StringVar(&filename, "file", "", "snapshot tar archive (required)")
	case "repair":
		flags.StringVar(&repairPaths, "paths", "", "optional comma-separated repository paths to repair")
	case "serve":
		flags.StringVar(&listen, "listen", "127.0.0.1:3143", "offline HTTP listen address")
	case "verify":
	default:
		return fmt.Errorf("unknown repository command: %s", command)
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" || len(flags.Args()) != 0 {
		return errors.New("--dir is required; positional arguments are not supported")
	}
	store, err := repository.Open(*dir, limits, nil)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	switch command {
	case "prefetch":
		m, err := store.Prefetch(ctx, repository.Selection{URL: repoURL, Suite: suite, Components: splitValues(components), Architectures: splitValues(architectures), Sources: sources})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Published complete snapshot: %d paths, suite %s\n", len(m.Objects), m.Selection.Suite)
		return err
	case "verify":
		damaged, err := store.Verify(ctx)
		if err != nil {
			return fmt.Errorf("verification failed (%s): %w", strings.Join(damaged, ", "), err)
		}
		_, err = fmt.Fprintln(output, "Complete snapshot verified")
		return err
	case "repair":
		if err := store.Repair(ctx, splitValues(repairPaths)); err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "Repair complete; entire snapshot verified")
		return err
	case "export":
		if filename == "" {
			return errors.New("--file is required")
		}
		return exportRepository(ctx, store, filename)
	case "import":
		if filename == "" {
			return errors.New("--file is required")
		}
		info, err := os.Lstat(filename)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("archive must be a regular file")
		}
		// The operator explicitly chooses this local archive with --file; it is
		// not derived from repository metadata. Lstat and SameFile reject
		// non-regular archives and replacements before consuming the archive.
		f, err := os.Open(filename) // #nosec G304 -- Explicit CLI archive path with regular-file and identity checks.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		actual, err := f.Stat()
		if err != nil || !os.SameFile(info, actual) {
			return errors.New("archive changed while opening")
		}
		m, err := store.Import(ctx, f)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Imported complete snapshot: %d paths\n", len(m.Objects))
		return err
	case "serve":
		return serveRepository(ctx, store, listen, output)
	}
	return nil
}

func splitValues(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func exportRepository(ctx context.Context, store *repository.Store, filename string) error {
	f, err := os.CreateTemp(filepath.Dir(filename), ".repository-export-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if err := store.Export(ctx, f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

func serveRepository(ctx context.Context, store *repository.Store, address string, output io.Writer) error {
	handler, err := store.Handler(ctx)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	if _, err := fmt.Fprintf(output, "Offline repository listening on http://%s\n", listener.Addr()); err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
