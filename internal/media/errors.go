package media

import "errors"

// The pipeline's error vocabulary in one place. Every sentinel is tested
// with errors.Is; the import command maps them to report codes and the
// upload handler to file violations (see internal/media/AGENTS.md).
var (
	// ErrInputTooLarge reports a source over [MaxInputBytes]; a 413
	// {file, tooLarge}.
	ErrInputTooLarge = errors.New("media: input exceeds size limit")

	// ErrDimensionsTooBig reports a header width or height over
	// maxPixelDimension; a 413 {file, tooLarge}.
	ErrDimensionsTooBig = errors.New("media: header dimensions exceed limit")

	// ErrPixelsTooBig reports a header total over maxTotalPixels; a 413
	// {file, tooLarge}.
	ErrPixelsTooBig = errors.New("media: header total pixels exceed limit")

	// ErrDecodeHeader reports an unreadable image header; a 422
	// {file, invalidFormat}.
	ErrDecodeHeader = errors.New("media: cannot read image header")

	// ErrDecode reports an undecodable image body; a 422
	// {file, invalidFormat}.
	ErrDecode = errors.New("media: cannot decode image")

	// ErrEncode reports a failed re-encode of a decoded image: the upload
	// boundary answers the generic masked 500, and the import report carries
	// the encodeFailed code.
	ErrEncode = errors.New("media: cannot encode processed image")

	// ErrWrite reports a failed write of the processed image: the upload
	// boundary answers the generic masked 500, and the import report carries
	// the writeFailed code.
	ErrWrite = errors.New("media: cannot write processed image")
)
