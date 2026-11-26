//go:build !darwin

package diskcopy

func clone(string, string) bool { return false }
