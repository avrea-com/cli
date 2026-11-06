package commands

import (
	"fmt"
	"io"
	"os"
)

// spoolDownload finishes the network read before a destination is opened.
// Python buffers downloads before writing; a private spool preserves that
// failure behavior without retaining the entire response in memory.
func spoolDownload(stream io.ReadCloser, limit int64) (*os.File, int64, error) {
	defer func() { _ = stream.Close() }()
	file, err := os.CreateTemp("", "avr-download-*")
	if err != nil {
		return nil, 0, err
	}
	count, err := io.Copy(file, io.LimitReader(stream, limit+1))
	if err == nil && count > limit {
		err = fmt.Errorf("download exceeds decompressed size limit of %d MiB", limit>>20)
	}
	if err == nil {
		_, err = file.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, count, err
	}
	return file, count, nil
}
