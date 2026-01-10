package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractMetadata returns the context (directory parts) and a map of tags
// based on the directory structure relative to the root watch directory.
// Example: root=/data, path=/data/cam1/2023/img.jpg
// Result: context=["cam1", "2023"], meta={"dir_0": "cam1", "dir_1": "2023"}
func ExtractMetadata(root, path string) ([]string, map[string]string) {
	meta := make(map[string]string)
	var context []string

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return context, meta
	}

	dir := filepath.Dir(rel)
	if dir == "." {
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
