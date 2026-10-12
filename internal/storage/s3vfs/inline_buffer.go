package s3vfs

import (
	"bytes"
	"io"
)

// inlineBuffer stores exactly the admitted payload bytes in independent
// chunks. bytes.Buffer capacity growth would otherwise hide retained memory
// beyond the admitted logical length. Streaming spill/upload avoids a second
// whole-object concatenation.
type inlineBuffer struct {
	chunks [][]byte
	size   int
}

func (b *inlineBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	chunk := make([]byte, len(p))
	copy(chunk, p)
	b.chunks = append(b.chunks, chunk)
	b.size += len(p)
	return len(p), nil
}
func (b *inlineBuffer) Len() int { return b.size }
func (b *inlineBuffer) Reset()   { b.chunks = nil; b.size = 0 }
func (b *inlineBuffer) Reader() io.Reader {
	readers := make([]io.Reader, len(b.chunks))
	for i, chunk := range b.chunks {
		readers[i] = bytes.NewReader(chunk)
	}
	return io.MultiReader(readers...)
}

// Bytes exists for small internal fixtures; production streams Reader instead.
func (b *inlineBuffer) Bytes() []byte { return bytes.Join(b.chunks, nil) }
