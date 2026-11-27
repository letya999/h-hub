package cellinit

import wv1 "github.com/letya999/h-hub/contracts/worker/v1"

type stream struct {
	buf       []byte
	truncated bool
}

func (s *stream) Write(p []byte) (int, error) {
	n := min(len(p), wv1.MaxOutput/2-len(s.buf))
	s.buf = append(s.buf, p[:n]...)
	s.truncated = s.truncated || n < len(p)
	return len(p), nil
}
