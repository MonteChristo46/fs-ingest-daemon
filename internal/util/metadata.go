package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtractMetadata returns a map of tags based on the directory structure
// relative to the root watch directory.
// Example: root=/data, path=/data/cam1/2023/img.jpg
// Result: path_parts=["cam1", "2023"]
func ExtractMetadata(root, path string) map[string]string {
	meta := make(map[string]string)
	
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return meta
	}

	dir := filepath.Dir(rel)
	if dir == "." {
		return meta
	}

	parts := strings.Split(dir, string(os.PathSeparator))
	
	for i, part := range parts {
		if part == "." || part == "" {
			continue
		}
		meta[fmt.Sprintf("dir_%d", i)] = part
	}
	
	return meta
}