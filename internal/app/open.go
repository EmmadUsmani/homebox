package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

// Open selects a published guest port and opens or returns its loopback URL.
func (s *Service) Open(ctx context.Context, name, port string, print bool) (string, error) {
	guestPort, err := parsePort(port)
	if err != nil {
		return "", err
	}
	box, err := s.load(name)
	if err != nil {
		return "", err
	}
	for _, forward := range box.Forwards {
		if forward.GuestPort == guestPort {
			url := publishedURL(forward.HostPort)
			if print || (runtime.GOOS != "darwin" && runtime.GOOS != "linux") {
				return url, nil
			}
			if s.OpenURL == nil {
				return "", errors.New("URL opener is unavailable")
			}
			return "", s.OpenURL(ctx, url)
		}
	}
	return "", fmt.Errorf("box %q has no published forward for guest port %d", name, guestPort)
}

func publishedURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d/", port) }
