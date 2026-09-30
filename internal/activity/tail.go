package activity

import (
	"os"
)

// TailFile returns the last maxBytes of path as a string (best-effort).
func TailFile(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 32 << 10
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := st.Size()
	start := size - int64(maxBytes)
	if start < 0 {
		start = 0
	}
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return "", err
		}
	}
	buf := make([]byte, size-start)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	data := buf[:n]
	if start > 0 {
		// Drop a possible partial first line.
		for i, b := range data {
			if b == '\n' {
				data = data[i+1:]
				break
			}
		}
	}
	return string(data), nil
}

// ReadSince returns bytes written to path after offset, capped at maxBytes
// (best-effort). If the file shrank below offset (rotation), it tails from
// the start of the new file.
func ReadSince(path string, offset int64, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 32 << 10
	}
	if offset < 0 {
		offset = 0
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := st.Size()
	if size == 0 {
		return "", nil
	}
	if offset > size {
		// Log rotated or truncated — show whatever is there now.
		offset = 0
	}
	start := offset
	if size-start > int64(maxBytes) {
		start = size - int64(maxBytes)
	}
	if start > 0 {
		if _, err := f.Seek(start, 0); err != nil {
			return "", err
		}
	}
	buf := make([]byte, size-start)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return "", err
	}
	data := buf[:n]
	if start > offset {
		// Drop a possible partial first line when we capped from the end.
		for i, b := range data {
			if b == '\n' {
				data = data[i+1:]
				break
			}
		}
	}
	return string(data), nil
}

// FileSize returns the log file size, or 0 if missing.
func FileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}
