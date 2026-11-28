package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sftpserver "github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func filesTestPool(t *testing.T, serve func(ssh.Channel)) *Pool {
	t.Helper()
	key, _ := keyForTest(t)
	clientConfig, err := pinnedClientConfig([]string{strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key.PublicKey())))})
	if err != nil {
		t.Fatal(err)
	}
	clientConfig.User = "test"
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(key)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	pool := newPool(func(ctx context.Context, _ VMRef) (*ssh.Client, error) {
		return dialSSH(ctx, listener.Addr().String(), clientConfig)
	})
	t.Cleanup(pool.Close)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					channel, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					go func() {
						defer channel.Close()
						for request := range requests {
							allowed := request.Type == "subsystem" && string(request.Payload) == "\x00\x00\x00\x04sftp"
							_ = request.Reply(allowed, nil)
							if allowed {
								serve(channel)
								return
							}
						}
					}()
				}
			}()
		}
	}()
	return pool
}

func TestFilePaths(t *testing.T) {
	for _, valid := range []string{"/", "/tmp/example.go", "/with spaces/file", "/a\\b"} {
		if err := checkFilePath(valid); err != nil {
			t.Errorf("%q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "/tmp/../file", "/tmp//a", "/tmp/", "/tmp/\x00a", "/" + strings.Repeat("a", maxFilePath)} {
		if err := checkFilePath(invalid); err == nil {
			t.Errorf("accepted invalid path %q", invalid)
		}
	}
}

func TestFilesSFTP(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pool := filesTestPool(t, func(channel ssh.Channel) {
		server, err := sftpserver.NewServer(channel, sftpserver.WithServerWorkingDirectory(root))
		if err != nil {
			t.Error(err)
			return
		}
		defer server.Close()
		_ = server.Serve()
	})
	source := filepath.Join(t.TempDir(), "upload.txt")
	destination := filepath.Join(t.TempDir(), "download.txt")
	files := NewFiles(pool, func() (string, error) { return source, nil },
		func(string) (string, error) { return destination, nil })
	t.Cleanup(func() { _ = files.ServiceShutdown() })
	ref := VMRef{Kind: "local", Name: "test"}
	one, err := files.Open(t.Context(), ref)
	if err != nil || one.Home != root {
		t.Fatalf("open = %+v, %v", one, err)
	}
	two, err := files.Open(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if files.views[one.ID].client != files.views[two.ID].client {
		t.Fatal("Files views used different SFTP clients")
	}
	files.Close(one.ID)

	target := path.Join(root, "test.txt")
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(name, []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
		// Set the mode explicitly so the runner's umask cannot change the fixture.
		if err := os.Chmod(name, 0640); err != nil {
			t.Fatal(err)
		}
	}
	write(target, "initial")
	before, err := files.Read(two.ID, target)
	if err != nil || before.Text != "initial" {
		t.Fatalf("read = %+v, %v", before, err)
	}
	saved, err := files.Save(two.ID, target, before.Stamp, "replacement")
	if err != nil || saved.Conflict || saved.Stamp.Size != 11 {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("replacement mode: %v, %v", info, err)
	}
	write(target, "other size contents")
	now := time.Unix(saved.Stamp.Modified, 0)
	if err := os.Chtimes(target, now, now); err != nil {
		t.Fatal(err)
	}
	assertConflict := func(label string, stamp FileStamp) {
		t.Helper()
		result, err := files.Save(two.ID, target, stamp, "overwrite")
		if err != nil || !result.Conflict {
			t.Errorf("%s: save = %+v, %v", label, result, err)
		}
	}
	assertConflict("changed size only", saved.Stamp)
	current, err := files.Read(two.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(time.Minute)
	if err := os.Chtimes(target, later, later); err != nil {
		t.Fatal(err)
	}
	assertConflict("changed mtime only", current.Stamp)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	assertConflict("deleted", current.Stamp)

	write(source, "uploaded bytes")
	if err := files.Upload(t.Context(), two.ID, root); err != nil {
		t.Fatal(err)
	}
	uploaded := path.Join(root, "upload.txt")
	if err := files.Upload(t.Context(), two.ID, root); err == nil {
		t.Fatal("upload overwrote an existing file")
	}
	if err := files.Download(t.Context(), two.ID, uploaded); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "uploaded bytes" {
		t.Fatalf("download = %q, %v", data, err)
	}

	write(path.Join(root, ".hidden"), "hidden")
	for _, bad := range []string{"mixed\x00bytes", strings.Repeat("a", maxEditorBytes+1)} {
		badPath := path.Join(root, ".not-editable")
		write(badPath, bad)
		if _, err := files.Read(two.ID, badPath); !errors.Is(err, errEditorFormat) {
			t.Fatalf("invalid editor file was accepted: %v", err)
		}
	}
	visible, err := files.List(t.Context(), two.ID, root, false)
	if err != nil || visible.More || len(visible.Entries) != 1 {
		t.Fatalf("visible listing = %+v, %v", visible, err)
	}
	for index := range maxDirectoryEntries {
		write(path.Join(root, fmt.Sprintf("item-%04d", index)), "")
	}
	listing, err := files.List(t.Context(), two.ID, root, true)
	if err != nil || !listing.More || len(listing.Entries) != maxDirectoryEntries {
		t.Fatalf("bounded listing: count=%d, more=%v, err=%v", len(listing.Entries), listing.More, err)
	}

	symlink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, symlink); err != nil {
		t.Fatal(err)
	}
	linkTarget := path.Join(symlink, "upload.txt")
	linked, err := files.Read(two.ID, linkTarget)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.Save(two.ID, linkTarget, linked.Stamp, "blocked"); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("write through a symlink: %v", err)
	}
}

type failRename struct{ sftpserver.FileCmder }

func (handler failRename) Filecmd(request *sftpserver.Request) error {
	if request.Method == "Rename" || request.Method == "PosixRename" {
		return fs.ErrPermission
	}
	return handler.FileCmder.Filecmd(request)
}

func TestSaveCleansUpAfterRenameFailure(t *testing.T) {
	pool := filesTestPool(t, func(channel ssh.Channel) {
		handlers := sftpserver.InMemHandler()
		handlers.FileCmd = failRename{handlers.FileCmd}
		server := sftpserver.NewRequestServer(channel, handlers)
		defer server.Close()
		_ = server.Serve()
	})
	files := NewFiles(pool, nil, nil)
	t.Cleanup(func() { _ = files.ServiceShutdown() })
	view, err := files.Open(t.Context(), VMRef{Kind: "remote", Alias: "test"})
	if err != nil {
		t.Fatal(err)
	}
	client := files.views[view.ID].client
	target := "/original"
	file, err := client.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	editor, err := files.Read(view.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := files.Save(view.ID, target, editor.Stamp, "replacement"); err == nil || !strings.Contains(err.Error(), "could not atomically replace") {
		t.Fatalf("expected rename permission error, got %v", err)
	}
	listing, err := files.List(t.Context(), view.ID, "/", true)
	if err != nil || len(listing.Entries) != 1 || listing.Entries[0].Name != "original" {
		t.Fatalf("temporary file remained: %+v, %v", listing, err)
	}
	after, err := files.Read(view.ID, target)
	if err != nil || after != editor {
		t.Fatalf("original changed: %+v, %v", after, err)
	}
}

type stopAfterWrite struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *stopAfterWrite) Write(data []byte) (int, error) {
	n, err := w.Buffer.Write(data)
	w.cancel()
	return n, err
}

func TestFileTransferCancellation(t *testing.T) {
	view := &fileView{ctx: t.Context()}
	ctx, finish, err := view.beginTransfer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	dst := &stopAfterWrite{cancel: view.cancelTransfer}
	src := strings.NewReader(strings.Repeat("x", 1<<20))
	if err := view.copyTransfer(ctx, dst, src, int64(src.Len())); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled transfer returned %v", err)
	}
	if dst.Len() == 0 || src.Len() == 0 || view.transfer.Bytes != int64(dst.Len()) {
		t.Fatalf("cancelled transfer progress = %+v, copied %d", view.transfer, dst.Len())
	}
}
