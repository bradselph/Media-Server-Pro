package thumbnails

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
)

// GetPlaceholderPath returns path to static placeholder images
func (m *Module) GetPlaceholderPath(placeholderType string) (string, error) {
	var filename string
	switch placeholderType {
	case "audio_placeholder":
		filename = "audio_placeholder.jpg"
	case "censored":
		filename = "censored_placeholder.jpg"
	default:
		filename = "placeholder.jpg"
	}

	placeholderPath := filepath.Join(m.thumbnailDir, filename)

	// Check if exists
	if _, err := os.Stat(placeholderPath); err == nil {
		return placeholderPath, nil
	}

	// Generate if missing
	if err := m.generateStaticPlaceholder(placeholderPath, placeholderType); err != nil {
		return "", err
	}

	return placeholderPath, nil
}

// generateStaticPlaceholder creates static placeholder images
func (m *Module) generateStaticPlaceholder(outputPath, placeholderType string) error {
	cfg := m.config.Get()
	img := image.NewRGBA(image.Rect(0, 0, cfg.Thumbnails.Width, cfg.Thumbnails.Height))

	var bgColor color.RGBA
	if placeholderType == "censored" {
		bgColor = color.RGBA{R: 80, G: 20, B: 20, A: 255} // Dark red
	} else {
		bgColor = color.RGBA{R: 40, G: 40, B: 50, A: 255} // Dark gray
	}

	// Fill image
	for y := 0; y < cfg.Thumbnails.Height; y++ {
		for x := 0; x < cfg.Thumbnails.Width; x++ {
			img.Set(x, y, bgColor)
		}
	}

	return m.writePlaceholderImage(outputPath, img)
}

// writePlaceholderImage writes an RGBA image to path as JPEG (for static placeholders).
func (m *Module) writePlaceholderImage(outputPath string, img image.Image) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			m.log.Warn("Failed to close thumbnail file: %v", closeErr)
		}
	}()

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 80}); err != nil {
		if removeErr := os.Remove(outputPath); removeErr != nil {
			m.log.Error("Failed to remove corrupted placeholder %s: %v (corrupted file will persist)", outputPath, removeErr)
			return fmt.Errorf("failed to encode thumbnail: %w; also failed to remove partial file: %v", err, removeErr)
		}
		return fmt.Errorf("failed to encode thumbnail: %w", err)
	}
	return nil
}
