package app

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestOpen(t *testing.T) {
	service, _, _, _ := fixture(t)
	ctx := t.Context()
	if err := service.New(ctx, NewOptions{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	for port, want := range map[string]string{"8000": "http://127.0.0.1:10100/", "8001": "http://127.0.0.1:10101/"} {
		url, err := service.Open(ctx, "main", port, true)
		if err != nil || url != want {
			t.Fatalf("Open(%s) = %q, %v", port, url, err)
		}
	}
	for port, want := range map[string]string{"65536": "invalid TCP port", "9000": "no published forward"} {
		if _, err := service.Open(ctx, "main", port, true); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Open(%s) error = %v, want %q", port, err, want)
		}
	}

	openErr := errors.New("test opener failure")
	var opened string
	service.OpenURL = func(_ context.Context, url string) error {
		opened = url
		return openErr
	}
	url, err := service.Open(ctx, "main", "8000", false)
	const want = "http://127.0.0.1:10100/"
	if runtime.GOOS == "darwin" {
		if url != "" || opened != want || !errors.Is(err, openErr) {
			t.Fatalf("macOS Open = %q, opened %q, err %v", url, opened, err)
		}
	} else if url != want || opened != "" || err != nil {
		t.Fatalf("Open = %q, opened %q, err %v", url, opened, err)
	}
}
