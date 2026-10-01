package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
)

func extractExecutable(archive []byte, archiveName, executableName string) ([]byte, error) {
	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		return extractTarGzExecutable(archive, executableName)
	case strings.HasSuffix(archiveName, ".zip"):
		return extractZipExecutable(archive, executableName)
	default:
		return nil, fmt.Errorf("unsupported archive format %q", archiveName)
	}
}

func extractTarGzExecutable(archive []byte, executableName string) ([]byte, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip archive: %w", err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var executable []byte
	found := false
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar archive: %w", err)
		}
		if err := validateArchivePath(header.Name); err != nil {
			return nil, err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxExecutableSize {
				return nil, fmt.Errorf("archive entry %q exceeds size limit", header.Name)
			}
			if header.Name != executableName {
				if _, err := io.Copy(io.Discard, reader); err != nil {
					return nil, fmt.Errorf("skip archive entry %q: %w", header.Name, err)
				}
				continue
			}
			if found {
				return nil, fmt.Errorf("archive contains duplicate executable %q", executableName)
			}
			executable, err = io.ReadAll(io.LimitReader(reader, maxExecutableSize+1))
			if err != nil {
				return nil, fmt.Errorf("read executable from archive: %w", err)
			}
			if int64(len(executable)) > maxExecutableSize {
				return nil, fmt.Errorf("executable exceeds %d-byte limit", maxExecutableSize)
			}
			found = true
		default:
			return nil, fmt.Errorf("archive contains unsupported entry type for %q", header.Name)
		}
	}
	if !found {
		return nil, fmt.Errorf("archive does not contain %q", executableName)
	}
	return executable, nil
}

func extractZipExecutable(archive []byte, executableName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open ZIP archive: %w", err)
	}
	var executable []byte
	found := false
	for _, file := range reader.File {
		if err := validateArchivePath(file.Name); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return nil, fmt.Errorf("archive contains unsupported entry type for %q", file.Name)
		}
		if file.UncompressedSize64 > uint64(maxExecutableSize) {
			return nil, fmt.Errorf("archive entry %q exceeds size limit", file.Name)
		}
		if file.Name != executableName {
			continue
		}
		if found {
			return nil, fmt.Errorf("archive contains duplicate executable %q", executableName)
		}
		body, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open executable in ZIP archive: %w", err)
		}
		executable, err = io.ReadAll(io.LimitReader(body, maxExecutableSize+1))
		_ = body.Close()
		if err != nil {
			return nil, fmt.Errorf("read executable from ZIP archive: %w", err)
		}
		if int64(len(executable)) > maxExecutableSize {
			return nil, fmt.Errorf("executable exceeds %d-byte limit", maxExecutableSize)
		}
		found = true
	}
	if !found {
		return nil, fmt.Errorf("archive does not contain %q", executableName)
	}
	return executable, nil
}

func validateArchivePath(name string) error {
	if name == "" || strings.Contains(name, `\`) || strings.Contains(name, ":") || strings.HasPrefix(name, "/") || path.IsAbs(name) || path.Clean(name) != name {
		return fmt.Errorf("archive contains unsafe path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." {
			return fmt.Errorf("archive contains unsafe path %q", name)
		}
	}
	return nil
}
