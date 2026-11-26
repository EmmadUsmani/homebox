package state

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageArchive(t *testing.T) {
	source := &Store{Root: t.TempDir()}
	destination := &Store{Root: t.TempDir()}
	disk := []byte("test disk data")
	box := Box{Profile: "ubuntu", Driver: "lima", CPUs: 2, MemoryGiB: 4, DiskGiB: 20}
	save := func() error {
		return source.SaveImage(t.Context(), box, "test-image", "dev", func(path string) error {
			return os.WriteFile(path, disk, 0600)
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
	name, err := destination.ImportImage(t.Context(), archive)
	if err != nil || name != "test-image" {
		t.Fatalf("import = %q, %v", name, err)
	}
	original, _, err := source.LoadImage(name)
	if err != nil {
		t.Fatal(err)
	}
	imported, path, err := destination.LoadImage(name)
	if err != nil || imported != original {
		t.Fatalf("metadata changed: %+v, %v", imported, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, disk) {
		t.Fatalf("disk changed: %q, %v", data, err)
	}
	if err := destination.RemoveImage(name); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		data []byte
	}{
		{"wrong digest", bytes.Repeat([]byte("x"), len(disk))},
		{"truncated", disk[:len(disk)-1]},
		{"trailing bytes", append(bytes.Clone(disk), 'x')},
	} {
		t.Run(test.name, func(t *testing.T) {
			var invalid bytes.Buffer
			writer := gzip.NewWriter(&invalid)
			if err := json.NewEncoder(writer).Encode(original); err != nil {
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
			if _, err := destination.ImportImage(t.Context(), file); err == nil || !strings.Contains(err.Error(), "image") {
				t.Fatalf("bad archive import = %v", err)
			}
			images, err := destination.ListImages()
			if err != nil || len(images) != 0 {
				t.Fatalf("bad archive left an image: %v, %v", images, err)
			}
		})
	}
}
