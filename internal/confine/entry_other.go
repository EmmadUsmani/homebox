//go:build !linux

package confine

func dispatch([]string) (bool, error) { return false, nil }
