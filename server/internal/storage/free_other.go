//go:build !unix

package storage

func freeBytes(string) (uint64, bool) { return 0, false }
