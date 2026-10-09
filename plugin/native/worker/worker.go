package worker

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	protocol  = 1
	maxReason = 4096

	endedGrace = 2 * time.Second
)

type Worker struct {
	turn chan struct{}
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	gone error

	held      io.Closer
	waited    bool
	dimension int
	maxTokens int
}

func Start(path, model string, threads, maxTokens int) (*Worker, error) {
	cmd := exec.Command(path, model, strconv.Itoa(threads), strconv.Itoa(maxTokens))
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	held, err := watch(cmd)
	if err != nil {
		return nil, err
	}

	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		held.Close()
		return nil, fmt.Errorf("the worker did not start: %w", err)
	}
	started(cmd)
	w := &Worker{turn: make(chan struct{}, 1), cmd: cmd, in: in, out: bufio.NewReaderSize(stdout, 1<<16), maxTokens: maxTokens, held: held}
	var head [12]byte
	if _, err := io.ReadFull(w.out, head[:4]); err != nil {

		return nil, w.fail(fmt.Errorf("the worker ended before its greeting (%s): %w", w.ended(), err))
	}
	if string(head[:4]) != "MNGW" {

		return nil, w.fail(w.reason(binary.LittleEndian.Uint32(head[:4])))
	}
	if _, err := io.ReadFull(w.out, head[4:]); err != nil {
		return nil, w.fail(fmt.Errorf("the worker's greeting was cut short: %w", err))
	}
	if v := binary.LittleEndian.Uint32(head[4:8]); v != protocol {
		return nil, w.fail(fmt.Errorf("the worker speaks protocol %d, this carrier %d", v, protocol))
	}
	w.dimension = int(binary.LittleEndian.Uint32(head[8:12]))
	if w.dimension < 1 || w.dimension > 65536 {
		return nil, w.fail(fmt.Errorf("the worker names a dimension of %d", w.dimension))
	}
	return w, nil
}

func (w *Worker) Dimension() int { return w.dimension }

func (w *Worker) Vector(ctx context.Context, ids []int32) ([]float32, error) {
	if len(ids) < 1 || len(ids) > w.maxTokens {
		return nil, fmt.Errorf("a request of %d tokens; the worker takes 1 to %d", len(ids), w.maxTokens)
	}
	select {
	case w.turn <- struct{}{}:
		defer func() { <-w.turn }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if w.gone != nil {
		return nil, w.gone
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	killed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(killed)
		w.cmd.Process.Kill()
	})
	vec, err := w.exchange(ids)
	if !stop() {
		<-killed
		return nil, w.fail(fmt.Errorf("the request's time ended while the worker was answering, and the worker was ended with it: %w", ctx.Err()))
	}
	return vec, err
}

func (w *Worker) exchange(ids []int32) ([]float32, error) {
	req := make([]byte, 4+4*len(ids))
	binary.LittleEndian.PutUint32(req, uint32(len(ids)))
	for i, id := range ids {
		binary.LittleEndian.PutUint32(req[4+4*i:], uint32(id))
	}
	if _, err := w.in.Write(req); err != nil {
		return nil, w.fail(fmt.Errorf("the worker took no request: %w", err))
	}
	var status [4]byte
	if _, err := io.ReadFull(w.out, status[:]); err != nil {
		return nil, w.fail(fmt.Errorf("the worker ended without a reply: %w", err))
	}
	if n := binary.LittleEndian.Uint32(status[:]); n != 0 {
		return nil, w.fail(w.reason(n))
	}
	raw := make([]byte, 4*w.dimension)
	if _, err := io.ReadFull(w.out, raw); err != nil {
		return nil, w.fail(fmt.Errorf("the worker's reply was cut short: %w", err))
	}
	vec := make([]float32, w.dimension)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
		if f := float64(vec[i]); math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, w.fail(errors.New("the worker's vector holds a value that is not a finite number"))
		}
	}
	return vec, nil
}

func (w *Worker) Usable() bool {
	w.turn <- struct{}{}
	defer func() { <-w.turn }()
	return w.gone == nil
}

func (w *Worker) Close() error {
	w.turn <- struct{}{}
	defer func() { <-w.turn }()
	if w.gone != nil {
		return nil
	}
	w.gone = errors.New("the worker was closed")
	w.in.Close()
	err := w.cmd.Wait()
	w.held.Close()
	return err
}

func (w *Worker) reason(n uint32) error {
	if n == 0 || n > maxReason {
		return fmt.Errorf("the worker stopped with a frame that is neither a greeting nor a reason (%d)", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(w.out, b); err != nil {
		return fmt.Errorf("the worker stopped and its reason was cut short: %w", err)
	}
	return fmt.Errorf("the worker stopped: %s", b)
}

func (w *Worker) ended() string {
	done := make(chan struct{})
	go func() { w.cmd.Wait(); close(done) }()
	state := "ended here: it was still running"
	select {
	case <-done:
		state = w.cmd.ProcessState.String()
	case <-time.After(endedGrace):
		w.cmd.Process.Kill()
		<-done
	}
	w.waited = true
	return state
}

func (w *Worker) fail(why error) error {
	w.gone = why
	w.in.Close()
	if !w.waited {
		if w.cmd.Process != nil {
			w.cmd.Process.Kill()
		}
		w.cmd.Wait()
	}
	w.held.Close()
	return why
}
