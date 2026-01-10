package util

// Package util provides utility functions used across the daemon.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractMetadata returns the context (directory parts) and a map of tags
// based on the directory structure relative to the root watch directory.
//
// Arguments:
//
//	root: The base directory being watched (e.g., "/data")
//	path: The full path to the file (e.g., "/data/cam1/2023/img.jpg")
//
// Returns:
//
//	context: A slice of directory names (e.g., ["cam1", "2023"])
//	meta: A map where keys are "dir_N" and values are the directory names.
//	      (e.g., {"dir_0": "cam1", "dir_1": "2023"})
func ExtractMetadata(root, path string) ([]string, map[string]string) {
	meta := make(map[string]string)
	var context []string

	// Get path relative to the root watch directory
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return context, meta
	}

	dir := filepath.Dir(rel)
	if dir == "." {
		// File is in the root watch directory, no context
		return context, meta
	}

	parts := strings.Split(dir, string(os.PathSeparator))

	for i, part := range parts {
		if part == "." || part == "" {
			continue
		}
		context = append(context, part)
		meta[fmt.Sprintf("dir_%d", i)] = part
	}

	return context, meta
}
