package imageutil

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

// CompressImage decodes an image, resizes it so its maximum dimension does not exceed maxDim,
// and encodes it back to its original format (JPEG or PNG) into a byte slice.
func CompressImage(path string, maxDim int, quality int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(path))
	var img image.Image

	switch ext {
	case ".jpg", ".jpeg":
		img, err = jpeg.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("failed to decode JPEG: %w", err)
		}
	case ".png":
		img, err = png.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("failed to decode PNG: %w", err)
		}
	default:
		// Fallback to standard decode if extension is misleading
		img, _, err = image.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("failed to decode image: %w", err)
		}
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// If the image is smaller than maxDim in both dimensions, use original size.
	// We might still compress it to save space even if we don't resize it.
	newWidth, newHeight := width, height
	if width > maxDim || height > maxDim {
		if width > height {
			newWidth = maxDim
			newHeight = int((float64(height) / float64(width)) * float64(maxDim))
		} else {
			newHeight = maxDim
			newWidth = int((float64(width) / float64(height)) * float64(maxDim))
		}
	}

	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	// CatmullRom is a high quality resizing algorithm
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf bytes.Buffer
	switch ext {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("failed to encode JPEG: %w", err)
		}
	case ".png":
		// PNG is lossless, so it doesn't take a quality parameter.
		err = png.Encode(&buf, dst)
		if err != nil {
			return nil, fmt.Errorf("failed to encode PNG: %w", err)
		}
	default:
		// Default to JPEG encoding if original type is unknown
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality})
		if err != nil {
			return nil, fmt.Errorf("failed to encode JPEG fallback: %w", err)
		}
	}

	return buf.Bytes(), nil
}
