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
const maxFirmwareBytes = 256 << 10
const bytesPerGiB = 1 << 30

// Image records the provenance and logical size of a saved VM disk.
type Image struct {
	Format         int       `json:"format"`
	Name           string    `json:"name"`
	Profile        string    `json:"profile"`
	Driver         string    `json:"driver"`
	Backend        string    `json:"backend,omitempty"`
	DiskFormat     string    `json:"disk_format,omitempty"`
	Arch           string    `json:"arch"`
	CPUs           int       `json:"cpus"`
	MemoryGiB      int       `json:"memory_gib"`
	DiskGiB        int       `json:"disk_gib"`
	DiskBytes      int64     `json:"disk_bytes"`
	CreatedAt      time.Time `json:"created_at"`
	BoxVersion     string    `json:"box_version"`
	Firmware       []byte    `json:"efi_variables,omitempty"`
	FirmwareSHA256 string    `json:"efi_sha256,omitempty"`
	SHA256         string    `json:"sha256"`
}

func (image Image) validate() error {
	digest, err := hex.DecodeString(image.SHA256)
	if image.Format != imageFormat || ValidateName(image.Name) != nil ||
		ValidateName(image.Profile) != nil || image.Driver == "" || image.Arch == "" ||
		image.CPUs <= 0 || image.MemoryGiB <= 0 || image.DiskGiB <= 0 ||
		image.DiskBytes <= 0 || (image.DiskFormat != "qcow2" && !image.fitsDisk(image.DiskBytes)) ||
		image.CreatedAt.IsZero() || err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("invalid image metadata")
	}
	if len(image.Firmware) > maxFirmwareBytes || image.FirmwareSHA256 != firmwareDigest(image.Firmware) {
		return fmt.Errorf("invalid image EFI variables or SHA-256 digest")
	}
	switch {
	case image.Backend == "" && image.DiskFormat == "": // Legacy VZ images.
	case image.Backend == "vz" && image.DiskFormat == "raw" && len(image.Firmware) > 0:
	case image.Backend == "qemu" && image.DiskFormat == "qcow2" && len(image.Firmware) == 0:
	default:
		return fmt.Errorf("invalid image backend, disk format or EFI variables")
	}
	return nil
}

func (image Image) EffectiveBackend() string {
	if image.Backend == "" {
		return "vz"
	}
	return image.Backend
}

func (image Image) fitsDisk(size int64) bool {
	return size > 0 && (size-1)/bytesPerGiB+1 <= int64(image.DiskGiB)
}

func (image Image) validateDisk(path string) error {
	if image.DiskFormat != "qcow2" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		var magic [4]byte
		if _, err := io.ReadFull(file, magic[:]); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if string(magic[:]) == "QFI\xfb" {
			return fmt.Errorf("qcow2 image disk is labeled as raw")
		}
		return nil
	}
	size, err := diskcopy.StandaloneQCOW2(path)
	if err != nil {
		return err
	}
	if !image.fitsDisk(size) {
		return fmt.Errorf("qcow2 virtual size exceeds the recorded image disk size")
	}
	return nil
}

func firmwareDigest(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
func (s *Store) SaveImage(ctx context.Context, box Box, name, version, backend string, copyDisk func(string) ([]byte, error)) error {
	image := Image{
		Format: imageFormat, Name: name, Profile: box.Profile, Driver: box.Driver, Arch: runtime.GOARCH,
		CPUs: box.CPUs, MemoryGiB: box.MemoryGiB, DiskGiB: box.DiskGiB, CreatedAt: time.Now().UTC(), BoxVersion: version,
	}
	image.Backend = backend
	switch backend {
	case "vz":
		image.DiskFormat = "raw"
	case "qemu":
		image.DiskFormat = "qcow2"
	}
	return s.stageImage(&image, os.Rename, func(path string) error {
		firmware, err := copyDisk(path)
		if err != nil {
			return err
		}
		image.Firmware = firmware
		image.FirmwareSHA256 = firmwareDigest(firmware)
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

// CopyImage stages an image from a trusted local store using its recorded disk digest.
func (s *Store) CopyImage(ctx context.Context, source *Store, name string, onFallback func()) error {
	image := Image{Name: name}
	publish := func(staged, destination string) error {
		return s.WithLock(func() error { return os.Rename(staged, destination) })
	}
	return s.stageImage(&image, publish, func(destination string) error {
		return source.WithLock(func() error {
			var disk string
			var err error
			image, disk, err = source.LoadImage(name)
			if err != nil {
				return err
			}
			info, err := os.Lstat(disk)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || info.Size() != image.DiskBytes {
				return fmt.Errorf("image %q disk is not a regular file of the recorded size", name)
			}
			return diskcopy.FileWithFallback(ctx, disk, destination, onFallback)
		})
	})
}

func (s *Store) stageImage(image *Image, publish func(string, string) error, stageDisk func(string) error) error {
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
	if err := s.RequireFreeSpace(); err != nil {
		return err
	}
	staged, err := os.MkdirTemp(s.imagesDir(), ".image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	disk := filepath.Join(staged, "disk")
	if err := stageDisk(disk); err != nil {
		return err
	}
	if err := image.validate(); err != nil {
		return err
	}
	if err := image.validateDisk(disk); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(staged, "image.json"), image); err != nil {
		return err
	}
	return publish(staged, destination)
}

// ExportImage writes gzip-compressed metadata followed by raw disk bytes.
func (s *Store) ExportImage(ctx context.Context, name, destination string) error {
	var image Image
	var disk *os.File
	// An open file keeps the immutable disk available if its image is removed
	// during compression. Release the state lock once the file is open.
	if err := s.WithLock(func() error {
		metadata, path, err := s.LoadImage(name)
		if err != nil {
			return err
		}
		image = metadata
		disk, err = os.Open(path)
		return err
	}); err != nil {
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

func (s *Store) ImportImage(ctx context.Context, path, name string) (string, error) {
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
	reader := bufio.NewReaderSize(compressed, 512<<10)
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
	if name != "" {
		image.Name = name
	}
	err = s.stageImage(&image, os.Rename, func(path string) error {
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
