package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

type TransferProgress struct {
	Active bool  `json:"active"`
	Bytes  int64 `json:"bytes"`
	Total  int64 `json:"total"`
}

func (f *Files) TransferStatus(id string) (TransferProgress, error) {
	view, err := f.view(id, "/")
	if err != nil {
		return TransferProgress{}, err
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	return view.transfer, nil
}

func (f *Files) CancelTransfer(id string) {
	view, err := f.view(id, "/")
	if err != nil {
		return
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.cancelTransfer != nil {
		view.cancelTransfer()
	}
}

func (view *fileView) beginTransfer(ctx context.Context) (context.Context, func(), error) {
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.transfer.Active {
		return nil, nil, errors.New("A transfer is already in progress")
	}
	if err := view.ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(view.ctx, cancel)
	view.cancelTransfer = cancel
	view.transfer = TransferProgress{Active: true}
	view.transfers.Add(1)
	return ctx, func() {
		stop()
		cancel()
		view.mu.Lock()
		view.transfer.Active = false
		view.cancelTransfer = nil
		view.mu.Unlock()
		view.transfers.Done()
	}, nil
}

func (view *fileView) copyTransfer(ctx context.Context, dst io.Writer, src io.Reader, size int64) error {
	view.mu.Lock()
	view.transfer.Total = size
	view.mu.Unlock()
	buffer := make([]byte, 32<<10)
	for ctx.Err() == nil {
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, err := dst.Write(buffer[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
			view.mu.Lock()
			view.transfer.Bytes += int64(written)
			view.mu.Unlock()
		}
		if errors.Is(readErr, io.EOF) {
			return ctx.Err()
		}
		if readErr != nil {
			return readErr
		}
	}
	return ctx.Err()
}

func (f *Files) Upload(ctx context.Context, id, directory string) error {
	view, err := f.view(id, directory)
	if err != nil {
		return err
	}
	source, err := f.pickUpload()
	if err != nil || source == "" {
		return err
	}
	ctx, finish, err := view.beginTransfer(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := os.Open(source)
	if err != nil {
		return err
	}
	defer local.Close()
	info, err := local.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Select a regular file to upload")
	}
	target := path.Join(directory, filepath.Base(source))
	if err := checkFilePath(target); err != nil {
		return err
	}
	remote, temporary, err := stageFile(view.client, target)
	if err != nil {
		return err
	}
	defer view.client.Remove(temporary)
	defer remote.Close()
	if err := view.copyTransfer(ctx, remote, local, info.Size()); err != nil {
		return err
	}
	if err := remote.Close(); err != nil {
		return err
	}
	if err := writableDirectory(view.client, directory); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := view.client.Link(temporary, target); err != nil {
		return fmt.Errorf("could not publish upload without overwriting an existing file: %w", err)
	}
	return nil
}

func (f *Files) Download(ctx context.Context, id, target string) error {
	view, err := f.view(id, target)
	if err != nil {
		return err
	}
	info, err := regularFile(view.client, target)
	if err != nil {
		return err
	}
	destination, err := f.pickDownload(path.Base(target))
	if err != nil || destination == "" {
		return err
	}
	ctx, finish, err := view.beginTransfer(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := view.client.Open(target)
	if err != nil {
		return err
	}
	defer remote.Close()
	local, err := os.CreateTemp(filepath.Dir(destination), ".homebox-*")
	if err != nil {
		return err
	}
	defer os.Remove(local.Name())
	defer local.Close()
	if err := view.copyTransfer(ctx, local, remote, info.Size()); err != nil {
		return err
	}
	if err := local.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(local.Name(), destination)
}
