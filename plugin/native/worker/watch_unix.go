//go:build !windows

package worker

import (
	"io"
	"os"
	"os/exec"
	"strconv"
)

func watch(cmd *exec.Cmd) (io.Closer, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, r)
	cmd.Args = append(cmd.Args, strconv.Itoa(2+len(cmd.ExtraFiles)))
	return w, nil
}

func started(cmd *exec.Cmd) {
	for _, f := range cmd.ExtraFiles {
		f.Close()
	}
}
