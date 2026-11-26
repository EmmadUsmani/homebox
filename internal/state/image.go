package state

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/EmmadUsmani/homebox/internal/diskcopy"
)

const imageFormat = 1
const bytesPerGiB = 1 << 30

// Image records the provenance and logical size of a saved VM disk.
type Image struct {
	Format     int       `json:"format"`
	Name       string    `json:"name"`
	Profile    string    `json:"profile"`
	Driver     string    `json:"driver"`
	Arch       string    `json:"arch"`
	CPUs       int       `json:"cpus"`
	MemoryGiB  int       `json:"memory_gib"`
	DiskGiB    int       `json:"disk_gib"`
	DiskBytes  int64     `json:"disk_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	BoxVersion string    `json:"box_version"`
	SHA256     string    `json:"sha256"`
}

func (image Image) validate() error {
	digest, err := hex.DecodeString(image.SHA256)
	if image.Format != imageFormat || ValidateName(image.Name) != nil ||
		ValidateName(image.Profile) != nil || image.Driver == "" || image.Arch == "" ||
		image.CPUs <= 0 || image.MemoryGiB <= 0 || image.DiskGiB <= 0 ||
		image.DiskBytes <= 0 || (image.DiskBytes-1)/bytesPerGiB+1 > int64(image.DiskGiB) ||
		image.CreatedAt.IsZero() || err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("invalid image metadata")
	}
	return nil
}

func (s *Store) imagesDir() string { return filepath.Join(s.Root, "images") }

// LoadImage opens metadata for a stored image and returns its disk path.
func (s *Store) LoadImage(name string) (Image, string, error) {
	var image Image
	if err := ValidateName(name); err != nil {
		return image, "", err
	}
	dir := filepath.Join(s.imagesDir(), name)
	err := readJSON(filepath.Join(dir, "image.json"), &image)
	if err != nil {
		return image, "", err
	}
	if image.Name != name {
		return image, "", fmt.Errorf("image metadata does not match directory %q", name)
	}
	return image, filepath.Join(dir, "disk"), image.validate()
}

func (s *Store) ListImages() ([]Image, error) {
	result := []Image{}
	entries, err := os.ReadDir(s.imagesDir())
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || ValidateName(entry.Name()) != nil {
			continue
		}
		image, _, err := s.LoadImage(entry.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, image)
	}
	return result, nil
}

func (s *Store) RemoveImage(name string) error {
	if _, _, err := s.LoadImage(name); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.imagesDir(), name))
}

// SaveImage copies a stopped disk into an unpublished directory before hashing it.
func (s *Store) SaveImage(ctx context.Context, box Box, name, version string, copyDisk func(string) error) error {
	image := Image{
		Format: imageFormat, Name: name, Profile: box.Profile, Driver: box.Driver, Arch: runtime.GOARCH,
		CPUs: box.CPUs, MemoryGiB: box.MemoryGiB, DiskGiB: box.DiskGiB, CreatedAt: time.Now().UTC(), BoxVersion: version,
	}
	return s.stageImage(&image, func(path string) error {
		if err := copyDisk(path); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		if err := file.Chmod(0600); err != nil {
			return err
		}
		hash := sha256.New()
		image.DiskBytes, err = io.Copy(hash, diskcopy.ContextReader{Context: ctx, Reader: file})
		image.SHA256 = hex.EncodeToString(hash.Sum(nil))
		return err
	})
}

func (s *Store) stageImage(image *Image, stageDisk func(string) error) error {
	if err := ValidateName(image.Name); err != nil {
		return err
	}
	destination := filepath.Join(s.imagesDir(), image.Name)
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("image %q already exists", image.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(s.imagesDir(), 0700); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(s.imagesDir(), ".image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err := stageDisk(filepath.Join(staged, "disk")); err != nil {
		return err
	}
	if err := image.validate(); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(staged, "image.json"), image); err != nil {
		return err
	}
	return os.Rename(staged, destination)
}

// ExportImage writes gzip-compressed metadata followed by raw disk bytes.
func (s *Store) ExportImage(ctx context.Context, name, destination string) error {
	image, path, err := s.LoadImage(name)
	if err != nil {
		return err
	}
	disk, err := os.Open(path)
	if err != nil {
		return err
	}
	defer disk.Close()
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("export file %q already exists", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".image-export-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	compressed, _ := gzip.NewWriterLevel(file, gzip.BestSpeed)
	writeErr := json.NewEncoder(compressed).Encode(image)
	if writeErr == nil {
		writeErr = image.transfer(ctx, compressed, disk)
	}
	if err := errors.Join(writeErr, compressed.Close(), file.Close()); err != nil {
		return err
	}
	return os.Link(file.Name(), destination)
}

func (s *Store) ImportImage(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer compressed.Close()
	reader := bufio.NewReaderSize(compressed, 16<<10)
	header, err := reader.ReadSlice('\n')
	if err != nil {
		return "", fmt.Errorf("read image metadata: %w", err)
	}
	var image Image
	if err := json.Unmarshal(header, &image); err != nil {
		return "", err
	}
	if err := image.validate(); err != nil {
		return "", err
	}
	err = s.stageImage(&image, func(path string) error {
		disk, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		output := bufio.NewWriterSize(diskcopy.SparseWriter{File: disk}, 1<<20)
		return errors.Join(image.transfer(ctx, output, reader), output.Flush(), disk.Close())
	})
	return image.Name, err
}

func (image Image) transfer(ctx context.Context, destination io.Writer, input io.Reader) error {
	hash := sha256.New()
	reader := diskcopy.ContextReader{Context: ctx, Reader: input}
	n, err := io.CopyBuffer(io.MultiWriter(destination, hash), io.LimitReader(reader, image.DiskBytes), make([]byte, 1<<20))
	if err != nil {
		return err
	}
	if n != image.DiskBytes || hex.EncodeToString(hash.Sum(nil)) != image.SHA256 {
		return fmt.Errorf("image disk size or SHA-256 digest does not match metadata")
	}
	var extra [1]byte
	if _, err := io.ReadFull(reader, extra[:]); !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid image stream ending: %v", err)
	}
	return nil
}
