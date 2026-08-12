//go:build !windows

package viewerservice

func NewPipeListener() (PipeListener, error) {
	return nil, ErrUnsupportedPlatform
}
