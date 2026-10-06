package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Content thumbnail contract: fit within 1280×720,
// JPEG quality 85 unless the image requires transparency, then PNG.
// Avatars are always 200×200 square PNGs ("200×200 square crop",
// "PNG for avatars").
const (
	contentMaxWidth  = 1280
	contentMaxHeight = 720

	// avatarSize is the target edge length for avatars. The crop
	// scales UP small sources too — the stored avatar is always exactly
	// 200×200, never a smaller image.
	avatarSize = 200

	// maxPixelDimension is the DecodeConfig pre-check bound:
	// reject before pixel allocation when a header claims more than 8192
	// pixels on either axis.
	maxPixelDimension = 8192

	// maxTotalPixels bounds width×height before full decode: the per-axis
	// 8192 check does not bound TOTAL pixels, so a small
	// file claiming e.g. 8000×8000 (64 MP) would allocate ~256 MB during
	// decode. 2^24 (16 MP) covers real 4K-plus sources (3840×2160 = 8.3 MP).
	// The product is bounded by maxPixelDimension² = 2^26 — no overflow.
	maxTotalPixels = 1 << 24

	// MaxInputBytes bounds the decoded source (10 MB upload cap).
	// Exported so callers that must read the source before processing (the
	// import command hashes the bytes) enforce the same bound.
	MaxInputBytes = 10 << 20
)

// ProcessContentThumbnail runs the processing pipeline for a blog thumbnail:
//
//  1. bound the input (10 MB);
//  2. DecodeConfig header pre-check (reject >8192px per axis or >16 MP
//     total before allocation);
//  3. decode (JPEG/PNG/WebP — magic bytes, not declared Content-Type);
//  4. metadata is dropped by re-encoding (EXIF never survives a decode);
//  5. proportional fit to 1280×720 when larger;
//  6. encode JPEG quality 85, or PNG when any pixel is not fully opaque;
//  7. atomic write to <dataDir>/media/images/<id[:2]>/<id>.<ext>.
//
// It returns the database-facing relative path. id must be a validated
// 32-hex identifier. The output is deterministic for a given input.
func ProcessContentThumbnail(dataDir, id string, r io.Reader) (string, error) {
	raw, err := readBounded(r)
	if err != nil {
		return "", err
	}

	img, err := decodeBounded(raw)
	if err != nil {
		return "", err
	}

	if img.Bounds().Dx() > contentMaxWidth || img.Bounds().Dy() > contentMaxHeight {
		img = resizeFit(img, contentMaxWidth, contentMaxHeight)
	}

	ext := ExtJPG
	if requiresPNG(img) {
		ext = ExtPNG
	}

	var out bytes.Buffer
	if ext == ExtPNG {
		if err := png.Encode(&out, img); err != nil {
			return "", fmt.Errorf("%w: %v", ErrEncode, err)
		}
	} else {
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 85}); err != nil {
			return "", fmt.Errorf("%w: %v", ErrEncode, err)
		}
	}

	if err := writeAtomic(dataDir, id, ext, out.Bytes()); err != nil {
		return "", err
	}
	return RelativePath(id, ext), nil
}

// ProcessAvatar runs the processing pipeline for a user avatar:
//
//  1. bound the input (10 MB);
//  2. header pre-checks + decode — the shared decodeBounded steps;
//  3. metadata is dropped by re-encoding (EXIF never survives a decode);
//  4. crop to the largest CENTERED square;
//  5. scale to exactly avatarSize×avatarSize (up or down — small sources
//     are upscaled; the stored avatar is always 200×200);
//  6. encode PNG (avatars are PNG regardless of transparency —
//     a JPEG-sourced avatar becomes an opaque PNG);
//  7. atomic write to <dataDir>/media/images/<id[:2]>/<id>.png.
//
// It returns the database-facing relative path. id must be a validated
// 32-hex identifier. The output is deterministic for a given input.
func ProcessAvatar(dataDir, id string, r io.Reader) (string, error) {
	raw, err := readBounded(r)
	if err != nil {
		return "", err
	}

	img, err := decodeBounded(raw)
	if err != nil {
		return "", err
	}

	img = centerSquareCrop(img)
	img = scaleTo(img, avatarSize, avatarSize)

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return "", fmt.Errorf("%w: %v", ErrEncode, err)
	}

	if err := writeAtomic(dataDir, id, ExtPNG, out.Bytes()); err != nil {
		return "", err
	}
	return RelativePath(id, ExtPNG), nil
}

// decodeBounded runs the shared header checks and decode: the per-axis 8192
// bound, the 2^24 total-pixel bound (a decompression-bomb guard; both run
// on the HEADER before any pixel allocation), and the magic-byte decode
// (JPEG/PNG/WebP — the declared Content-Type is never trusted).
func decodeBounded(raw []byte) (image.Image, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecodeHeader, err)
	}
	if cfg.Width > maxPixelDimension || cfg.Height > maxPixelDimension {
		return nil, fmt.Errorf("%w: %dx%d", ErrDimensionsTooBig, cfg.Width, cfg.Height)
	}
	if cfg.Width*cfg.Height > maxTotalPixels {
		return nil, fmt.Errorf("%w: %dx%d", ErrPixelsTooBig, cfg.Width, cfg.Height)
	}
	img, err := decodeImage(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	return img, nil
}

// isWebP reports whether raw carries the WebP container signature — "RIFF", a
// size field, then "WEBP" — covering the VP8, VP8L, and VP8X variants.
func isWebP(raw []byte) bool {
	return len(raw) >= 12 && string(raw[0:4]) == "RIFF" && string(raw[8:12]) == "WEBP"
}

// decodeConfig reads the header of raw, sending WebP to its own decoder.
func decodeConfig(raw []byte) (image.Config, error) {
	if isWebP(raw) {
		return webp.DecodeConfig(bytes.NewReader(raw))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	return cfg, err
}

// decodeImage decodes raw fully, sending WebP to its own decoder.
func decodeImage(raw []byte) (image.Image, error) {
	if isWebP(raw) {
		return webp.Decode(bytes.NewReader(raw))
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	return img, err
}

// centerSquareCrop returns the largest centered square of img. A square
// source is returned unchanged. The crop copies pixels into a fresh RGBA
// image (draw.Src preserves the alpha channel exactly), so transparency
// survives the avatar pipeline.
func centerSquareCrop(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return img
	}
	side := min(w, h)
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), img, image.Pt(x0, y0), draw.Src)
	return dst
}

// scaleTo scales img to exactly w×h (up or down) with CatmullRom
// interpolation — the avatar contract needs an exact size, unlike the
// thumbnail fit.
func scaleTo(img image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

// readBounded reads at most MaxInputBytes+1 bytes so an oversized source is
// detected without allocating beyond the contract.
func readBounded(r io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxInputBytes+1))
	if err != nil {
		// A source-read failure is infrastructure, not an image-format fact;
		// it stays unsentineled (the boundary answers its masked 500) and the
		// cause is flattened — the reader's error type is not our contract.
		return nil, fmt.Errorf("media: read input: %v", err)
	}
	if len(raw) > MaxInputBytes {
		return nil, ErrInputTooLarge
	}
	return raw, nil
}

// resizeFit scales img proportionally so it fits inside maxW×maxH,
// preserving aspect ratio. A source already within bounds is returned
// unchanged (callers check first). Each computed edge floors at one pixel:
// an extreme aspect ratio (for example 1×2000) would otherwise round the
// short edge to zero and hand the encoder an invalid image.
func resizeFit(img image.Image, maxW, maxH int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return img
	}
	sx := float64(maxW) / float64(w)
	sy := float64(maxH) / float64(h)
	if sx > sy {
		sx = sy
	}
	dst := image.NewRGBA(image.Rect(0, 0,
		max(1, int(float64(w)*sx+0.5)), max(1, int(float64(h)*sx+0.5))))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// requiresPNG reports whether any pixel is not fully opaque (PNG is used
// for images requiring transparency). Opacity is sampled over the decoded
// pixel data; the encoder-independent color space (jpeg → YCbCr, gray) has
// no alpha channel and returns false without a scan.
func requiresPNG(img image.Image) bool {
	switch src := img.(type) {
	case *image.YCbCr, *image.CMYK, *image.Gray, *image.Gray16:
		return false
	case *image.Paletted:
		for _, c := range src.Palette {
			if _, _, _, a := c.RGBA(); a != 0xffff {
				return true
			}
		}
		return false
	default:
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
					return true
				}
			}
		}
		return false
	}
}

// writeAtomic writes the encoded bytes to a temp file in the target
// directory and renames it into place, so a crash never leaves a
// half-written served image.
func writeAtomic(dataDir, id, ext string, data []byte) error {
	dir := imageDir(dataDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return wrapWriteError(err)
	}
	tmp, err := os.CreateTemp(dir, tempPrefix+"*")
	if err != nil {
		return wrapWriteError(err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		// Best-effort cleanup of the temp file; the write already failed.
		tmp.Close()
		os.Remove(tmpName)
		return wrapWriteError(err)
	}
	if err := tmp.Close(); err != nil {
		// Best-effort cleanup; the close already failed.
		os.Remove(tmpName)
		return wrapWriteError(err)
	}
	if err := os.Rename(tmpName, ImagePath(dataDir, id, ext)); err != nil {
		// Best-effort cleanup; the rename already failed.
		os.Remove(tmpName)
		return wrapWriteError(err)
	}
	return nil
}

// wrapWriteError keeps the ErrWrite contract while flattening the OS cause:
// the *os.PathError text carries the storage path, which never reaches a log
// record (docs/patterns/go/logging.md rule 8).
func wrapWriteError(err error) error {
	if cause := errors.Unwrap(err); cause != nil {
		return fmt.Errorf("%w: %v", ErrWrite, cause)
	}
	return ErrWrite
}
