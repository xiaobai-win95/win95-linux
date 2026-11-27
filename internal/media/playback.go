package media

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var mediaExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".flac": true, ".ogg": true, ".oga": true,
	".m4a": true, ".aac": true, ".opus": true, ".wma": true,
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".webm": true,
	".wmv": true, ".flv": true, ".m4v": true, ".mpg": true, ".mpeg": true,
	".ogv": true, ".ts": true, ".m2ts": true, ".vob": true, ".3gp": true,
}

func IsMediaFile(path string) bool {
	return mediaExtensions[strings.ToLower(filepath.Ext(path))]
}

func ValidateMediaFile(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("media path must be absolute")
	}
	if !IsMediaFile(path) {
		return errors.New("unsupported media file extension")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("cannot open media file: %w", err)
	}
	if !st.Mode().IsRegular() {
		return errors.New("media path must refer to a regular file")
	}
	if st.Size() == 0 {
		return errors.New("media file is empty")
	}
	return nil
}

func FFplayArgs(path string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-autoexit", "-nostats",
		"-x", "960", "-y", "720", "-left", "32", "-top", "24",
		"-window_title", "Win95 Media Player", "-i", path,
	}
}
