package core

import (
	"bufio"
	"context"
	"errors"
)

const maxLineBytes = 16 << 20

// The original offline file remains untouched. This bounds even malformed
// files containing a multi-gigabyte line without a newline.
func boundedLine(reader *bufio.Reader) (string, error, bool) {
	return boundedLineContext(context.Background(), reader)
}

func boundedLineContext(ctx context.Context, reader *bufio.Reader) (string, error, bool) {
	var line []byte
	truncated := false
	for {
		if err := ctx.Err(); err != nil {
			return "", err, truncated
		}
		part, err := reader.ReadSlice('\n')
		remaining := maxLineBytes - len(line)
		if len(part) > remaining {
			part = part[:remaining]
			truncated = true
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return string(line), err, truncated
	}
}
