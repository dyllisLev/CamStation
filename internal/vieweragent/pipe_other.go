//go:build !windows

package vieweragent

import (
	"context"
	"errors"
)

func ServeViewerPipe(context.Context, Config, func(PipeMessage) (PipeMessage, error)) error {
	return errors.New("viewer named pipe is only available on Windows")
}
