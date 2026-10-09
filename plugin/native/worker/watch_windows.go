package worker

import (
	"io"
	"os/exec"
)

func watch(*exec.Cmd) (io.Closer, error) { return noHold{}, nil }

func started(*exec.Cmd) {}

type noHold struct{}

func (noHold) Close() error { return nil }
