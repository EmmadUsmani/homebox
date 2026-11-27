package state

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImageArchive(t *testing.T) {
	source := &Store{Root: t.TempDir()}
	destination := &Store{Root: t.TempDir()}
	disk := []byte("test disk data")
	box := Box{Profile: "ubuntu", Driver: "lima", CPUs: 2, MemoryGiB: 4, DiskGiB: 20}
	save := func() error {
		return source.SaveImage(t.Context(), box, "test-image", "dev", func(path string) ([]byte, error) {
			return []byte("EFI fixture"), os.WriteFile(path, disk, 0600)
		})
	}
	if err := save(); err != nil {
		t.Fatal(err)
	}
	if err := save(); err == nil {
		t.Fatal("saved over an existing image")
	}
	archive := filepath.Join(t.TempDir(), "test.hbimg.gz")
	if err := source.ExportImage(t.Context(), "test-image", archive); err != nil {
		t.Fatal(err)
	}
	if err := source.ExportImage(t.Context(), "test-image", archive); err == nil {
		t.Fatal("export overwrote an existing archive")
	}
	name, err := destination.ImportImage(t.Context(), archive, "")
	if err != nil || name != "test-image" {
		t.Fatalf("import = %q, %v", name, err)
	}
	original, _, err := source.LoadImage(name)
	if err != nil {
		t.Fatal(err)
	}
	imported, path, err := destination.LoadImage(name)
	if err != nil || !reflect.DeepEqual(imported, original) {
		t.Fatalf("metadata changed: %+v, %v", imported, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, disk) {
		t.Fatalf("disk changed: %q, %v", data, err)
	}
	if err := destination.RemoveImage(name); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []string{"../outside", "UPPERCASE"} {
		if _, err := destination.ImportImage(t.Context(), archive, invalid); err == nil {
			t.Fatalf("import accepted invalid name %q", invalid)
		}
	}
	const alias = "renamed-image"
	name, err = destination.ImportImage(t.Context(), archive, alias)
	if err != nil || name != alias {
		t.Fatalf("renamed import = %q, %v", name, err)
	}
	renamed, _, err := destination.LoadImage(alias)
	original.Name = alias
	if err != nil || !reflect.DeepEqual(renamed, original) {
		t.Fatalf("renamed metadata changed: %+v, %v", renamed, err)
	}
	if _, err := destination.ImportImage(t.Context(), archive, alias); err == nil {
		t.Fatal("import overwrote an existing image")
	}
	if err := destination.RemoveImage(alias); err != nil {
		t.Fatal(err)
	}
	original.Name = "test-image"

	for _, test := range []struct {
		name        string
		data        []byte
		badFirmware bool
	}{
		{name: "wrong digest", data: bytes.Repeat([]byte("x"), len(disk))},
		{name: "truncated", data: disk[:len(disk)-1]},
		{name: "trailing bytes", data: append(bytes.Clone(disk), 'x')},
		{name: "wrong firmware digest", data: disk, badFirmware: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var invalid bytes.Buffer
			writer := gzip.NewWriter(&invalid)
			metadata := original
			if test.badFirmware {
				metadata.Firmware = []byte("changed EFI fixture")
			}
			if err := json.NewEncoder(writer).Encode(metadata); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write(test.data); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "invalid.gz")
			if err := os.WriteFile(file, invalid.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := destination.ImportImage(t.Context(), file, ""); err == nil || !strings.Contains(err.Error(), "image") {
				t.Fatalf("bad archive import = %v", err)
			}
			images, err := destination.ListImages()
			if err != nil || len(images) != 0 {
				t.Fatalf("bad archive left an image: %v, %v", images, err)
			}
		})
	}
}

func TestCopyImage(t *testing.T) {
	source := &Store{Root: t.TempDir()}
	destination := &Store{Root: t.TempDir()}
	const name = "test-image"
	box := Box{Profile: "ubuntu", Driver: "lima", CPUs: 2, MemoryGiB: 4, DiskGiB: 20}
	if err := source.SaveImage(t.Context(), box, name, "dev", func(path string) ([]byte, error) {
		return []byte("EFI fixture"), os.WriteFile(path, []byte("original"), 0600)
	}); err != nil {
		t.Fatal(err)
	}
	original, sourceDisk, err := source.LoadImage(name)
	if err != nil {
		t.Fatal(err)
	}
	// A same-size change demonstrates that copying trusts the recorded digest.
	if err := os.WriteFile(sourceDisk, []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := destination.CopyImage(t.Context(), source, name, nil); err != nil {
		t.Fatal(err)
	}
	copied, copiedDisk, err := destination.LoadImage(name)
	if err != nil || !reflect.DeepEqual(copied, original) {
		t.Fatalf("copied metadata = %+v, %v", copied, err)
	}
	data, err := os.ReadFile(copiedDisk)
	if err != nil || string(data) != "modified" {
		t.Fatalf("copied disk = %q, %v", data, err)
	}
	if err := os.WriteFile(copiedDisk, []byte("VM write"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(sourceDisk)
	if err != nil || string(data) != "modified" {
		t.Fatalf("source disk after destination write = %q, %v", data, err)
	}
	if err := destination.CopyImage(t.Context(), source, name, nil); err == nil {
		t.Fatal("copy overwrote an existing image")
	}
	if err := destination.RemoveImage(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(sourceDisk, 1); err != nil {
		t.Fatal(err)
	}
	if err := destination.CopyImage(t.Context(), source, name, nil); err == nil {
		t.Fatal("copy accepted a disk of the wrong size")
	}
	entries, err := os.ReadDir(destination.imagesDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed copy left entries: %v, %v", entries, err)
	}
}
