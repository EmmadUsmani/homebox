package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	sftp "github.com/pkg/sftp/v2"
	"golang.org/x/crypto/ssh"
)

const (
	maxFilePath         = 4096
	maxDirectoryEntries = 5000
	maxEditorBytes      = 2 << 20
)

var errFileConflict = errors.New("This file changed in the VM since you opened it")
var errEditorFormat = errors.New("File is too large or binary. Use Download to open it locally.")

type Files struct {
	pool         *Pool
	pickUpload   func() (string, error)
	pickDownload func(string) (string, error)
	mu           sync.Mutex
	closed       bool
	clients      map[*ssh.Client]*fileClient
	views        map[string]*fileView
}

type fileClient struct {
	client *sftp.Client
	users  int
}

type fileView struct {
	client         *sftp.Client
	ctx            context.Context
	close          func()
	mu             sync.Mutex
	transfer       TransferProgress
	cancelTransfer context.CancelFunc
	transfers      sync.WaitGroup
}

func NewFiles(pool *Pool, pickUpload func() (string, error), pickDownload func(string) (string, error)) *Files {
	return &Files{pool: pool, pickUpload: pickUpload, pickDownload: pickDownload,
		clients: make(map[*ssh.Client]*fileClient), views: make(map[string]*fileView)}
}

type FileView struct {
	ID   string `json:"id"`
	Home string `json:"home"`
}

func (f *Files) Open(ctx context.Context, ref VMRef) (FileView, error) {
	ctx, cancelSetup := context.WithTimeout(ctx, setupTimeout)
	defer cancelSetup()
	transport, release, err := f.pool.Acquire(ctx, ref)
	if err != nil {
		return FileView{}, err
	}
	f.mu.Lock()
	if f.closed || ctx.Err() != nil {
		f.mu.Unlock()
		release()
		return FileView{}, errors.New("Opening Files was cancelled")
	}
	entry := f.clients[transport]
	if entry == nil {
		// Match the terminal's setup deadline for a guest that stops replying.
		stop := context.AfterFunc(ctx, func() { _ = transport.Close() })
		client, err := sftp.NewClient(ctx, transport)
		if !stop() && err == nil {
			_ = client.Close()
			err = ctx.Err()
		}
		if err != nil {
			f.mu.Unlock()
			release()
			return FileView{}, fmt.Errorf("start SFTP: %w", err)
		}
		entry = &fileClient{client: client}
		f.clients[transport] = entry
	}
	entry.users++
	f.mu.Unlock()

	lifetime, cancel := context.WithCancel(context.Background())
	view := &fileView{client: entry.client, ctx: lifetime}
	view.close = func() {
		view.mu.Lock()
		cancel()
		view.mu.Unlock()
		view.transfers.Wait()
		f.mu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(f.clients, transport)
			_ = entry.client.Close()
		}
		f.mu.Unlock()
		release()
	}
	stop := context.AfterFunc(ctx, func() { _ = transport.Close() })
	home, err := entry.client.RealPath(".")
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = checkFilePath(home)
	}
	if err != nil {
		view.close()
		return FileView{}, fmt.Errorf("locate VM home: %w", err)
	}
	id := rand.Text()
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		view.close()
		return FileView{}, errors.New("Files service is closed")
	}
	f.views[id] = view
	f.mu.Unlock()
	return FileView{ID: id, Home: home}, nil
}

func (f *Files) Close(id string) {
	f.mu.Lock()
	view := f.views[id]
	delete(f.views, id)
	f.mu.Unlock()
	if view != nil {
		view.close()
	}
}

func (f *Files) ServiceShutdown() error {
	f.mu.Lock()
	f.closed = true
	ids := make([]string, 0, len(f.views))
	for id := range f.views {
		ids = append(ids, id)
	}
	f.mu.Unlock()
	for _, id := range ids {
		f.Close(id)
	}
	return nil
}

func (f *Files) view(id string, target string) (*fileView, error) {
	if err := checkFilePath(target); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	view := f.views[id]
	if view == nil {
		return nil, errors.New("Files connection is closed. Reopen the Files tab.")
	}
	return view, nil
}

func checkFilePath(target string) error {
	if len(target) > maxFilePath || !utf8.ValidString(target) || strings.ContainsRune(target, 0) ||
		!path.IsAbs(target) || path.Clean(target) != target {
		return errors.New("Invalid VM path. Use an absolute path of at most 4096 bytes.")
	}
	return nil
}

type FileStamp struct {
	Size     int64 `json:"size"`
	Modified int64 `json:"modified"`
}

func fileStamp(info fs.FileInfo) FileStamp {
	return FileStamp{Size: info.Size(), Modified: info.ModTime().Unix()}
}

type FileEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	FileStamp
}

type FileListing struct {
	Entries []FileEntry `json:"entries"`
	More    bool        `json:"more"`
}

func (f *Files) List(ctx context.Context, id, target string, hidden bool) (FileListing, error) {
	view, err := f.view(id, target)
	if err != nil {
		return FileListing{}, err
	}
	dir, err := view.client.OpenDir(target)
	if err != nil {
		return FileListing{}, err
	}
	defer dir.Close()
	items, err := dir.ReadDirContext(ctx, maxDirectoryEntries+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return FileListing{}, err
	}
	listing := FileListing{Entries: make([]FileEntry, 0, min(len(items), maxDirectoryEntries)), More: len(items) > maxDirectoryEntries}
	for _, item := range items[:min(len(items), maxDirectoryEntries)] {
		name := item.Name()
		if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') ||
			(!hidden && strings.HasPrefix(name, ".")) || checkFilePath(path.Join(target, name)) != nil {
			continue
		}
		kind := "other"
		switch {
		case item.IsDir():
			kind = "folder"
		case item.Type().IsRegular():
			kind = "file"
		case item.Type()&fs.ModeSymlink != 0:
			kind = "link"
		}
		info, err := item.Info()
		if err != nil {
			return FileListing{}, err
		}
		listing.Entries = append(listing.Entries, FileEntry{Name: name, Kind: kind, FileStamp: fileStamp(info)})
	}
	slices.SortFunc(listing.Entries, func(a, b FileEntry) int {
		if (a.Kind == "folder") != (b.Kind == "folder") {
			if a.Kind == "folder" {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return listing, nil
}

type EditorFile struct {
	Text  string    `json:"text"`
	Stamp FileStamp `json:"stamp"`
}

func (f *Files) Read(id, target string) (EditorFile, error) {
	view, err := f.view(id, target)
	if err != nil {
		return EditorFile{}, err
	}
	info, err := regularFile(view.client, target)
	if err != nil {
		return EditorFile{}, err
	}
	if info.Size() > maxEditorBytes {
		return EditorFile{}, errEditorFormat
	}
	file, err := view.client.Open(target)
	if err != nil {
		return EditorFile{}, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxEditorBytes+1))
	if err != nil {
		return EditorFile{}, err
	}
	if len(content) > maxEditorBytes || !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return EditorFile{}, errEditorFormat
	}
	stamp := fileStamp(info)
	if _, err := unchangedFile(view.client, target, stamp); err != nil {
		return EditorFile{}, err
	}
	return EditorFile{Text: string(content), Stamp: stamp}, nil
}

type SaveResult struct {
	Stamp    FileStamp `json:"stamp"`
	Conflict bool      `json:"conflict"`
}

func (f *Files) Save(id, target string, stamp FileStamp, content string) (SaveResult, error) {
	view, err := f.view(id, target)
	if err != nil {
		return SaveResult{}, err
	}
	if len(content) > maxEditorBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return SaveResult{}, errEditorFormat
	}
	info, err := saveFile(view.client, target, stamp, content)
	if errors.Is(err, errFileConflict) {
		return SaveResult{Conflict: true}, nil
	}
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Stamp: info}, nil
}

func regularFile(client *sftp.Client, target string) (fs.FileInfo, error) {
	info, err := client.LStat(target)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("Select a regular file. Symbolic links and special files cannot be opened.")
	}
	return info, checkDirectoryPath(client, path.Dir(target))
}

func unchangedFile(client *sftp.Client, target string, stamp FileStamp) (fs.FileInfo, error) {
	info, err := client.LStat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errFileConflict
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || fileStamp(info) != stamp {
		return nil, errFileConflict
	}
	return info, nil
}

func checkDirectoryPath(client *sftp.Client, directory string) error {
	for current := directory; current != "/"; current = path.Dir(current) {
		info, err := client.LStat(current)
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return errors.New("Paths through symbolic links are blocked. Open the real directory and try again.")
		}
		if !info.IsDir() {
			return errors.New("VM path does not point to a directory")
		}
	}
	return nil
}

func stageFile(client *sftp.Client, target string) (*sftp.File, string, error) {
	directory := path.Dir(target)
	if err := checkDirectoryPath(client, directory); err != nil {
		return nil, "", err
	}
	temporary := path.Join(directory, ".homebox-"+rand.Text())
	if err := checkFilePath(temporary); err != nil {
		return nil, "", err
	}
	file, err := client.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	return file, temporary, err
}

func saveFile(client *sftp.Client, target string, stamp FileStamp, content string) (FileStamp, error) {
	if _, err := unchangedFile(client, target, stamp); err != nil {
		return FileStamp{}, err
	}
	file, temporary, err := stageFile(client, target)
	if err != nil {
		return FileStamp{}, err
	}
	defer client.Remove(temporary)
	defer file.Close()
	if _, err := io.WriteString(file, content); err != nil {
		return FileStamp{}, err
	}
	if err := checkDirectoryPath(client, path.Dir(target)); err != nil {
		return FileStamp{}, err
	}
	current, err := unchangedFile(client, target, stamp)
	if err != nil {
		return FileStamp{}, err
	}
	if err := file.Chmod(current.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)); err != nil {
		return FileStamp{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return FileStamp{}, err
	}
	if err := file.Close(); err != nil {
		return FileStamp{}, err
	}
	if _, err := unchangedFile(client, target, stamp); err != nil {
		return FileStamp{}, err
	}
	if err := client.Rename(temporary, target); err != nil {
		return FileStamp{}, fmt.Errorf("could not atomically replace the original: %w", err)
	}
	return fileStamp(info), nil
}
