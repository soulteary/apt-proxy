package vfs

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
)

func copyVFS(fs VFS, copier func(p string, info os.FileInfo, f io.Reader) error) error {
	return Walk(fs, "/", func(vfs VFS, p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		f, err := fs.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		return copier(p[1:], info, f)
	})
}

// WriteZip writes the given VFS as a zip file to the given io.Writer.
func WriteZip(w io.Writer, fs VFS) error {
	zw := zip.NewWriter(w)
	err := copyVFS(fs, func(p string, info os.FileInfo, f io.Reader) error {
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = p
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, f)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}

// WriteTar writes the given VFS as a tar file to the given io.Writer.
func WriteTar(w io.Writer, fs VFS) error {
	tw := tar.NewWriter(w)
	err := copyVFS(fs, func(p string, info os.FileInfo, f io.Reader) error {
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = p
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// WriteTarGzip writes the given VFS as a tar.gz file to the given io.Writer.
func WriteTarGzip(w io.Writer, fs VFS) error {
	gw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	if err := WriteTar(gw, fs); err != nil {
		return err
	}
	return gw.Close()
}
