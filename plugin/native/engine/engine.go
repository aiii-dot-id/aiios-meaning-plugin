package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/aiii-dot-id/aiios-meaning-plugin/plugin/native/tokenizer"
	"github.com/aiii-dot-id/aiios-meaning-plugin/plugin/native/worker"
)

const (
	Form = 1

	Window = 2048

	MaxCharacters = 8000

	cuePrefix = "query: "
)

var ErrRequest = errors.New("the request cannot be answered as asked")

var ErrSource = errors.New("the worker cannot be started")

type Kind string

const (
	Cue    Kind = "cue"
	Memory Kind = "memory"
)

type Config struct {
	Tokenizer string
	Worker    string
	Model     string
	Threads   int

	Quiet time.Duration
}

type Engine struct {
	cfg Config
	tok *tokenizer.Tokenizer

	turn   chan struct{}
	w      *worker.Worker
	used   time.Time
	closed bool

	mu    sync.Mutex
	timer *time.Timer
}

func New(cfg Config) (*Engine, error) {
	if cfg.Threads < 1 || cfg.Threads > 4 {
		return nil, fmt.Errorf("threads must be 1 to 4, not %d", cfg.Threads)
	}
	if cfg.Worker == "" || cfg.Model == "" || cfg.Tokenizer == "" {
		return nil, errors.New("the tokenizer file, the worker program and the model file must all be named")
	}
	if cfg.Quiet < 0 {
		return nil, errors.New("the quiet period cannot be negative")
	}
	tok, err := tokenizer.Load(cfg.Tokenizer)
	if err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg, tok: tok, turn: make(chan struct{}, 1)}, nil
}

func (e *Engine) Embed(ctx context.Context, kind Kind, text string) ([]float32, error) {
	input := text
	switch kind {
	case Cue:
		input = cuePrefix + text
	case Memory:
	default:
		return nil, fmt.Errorf("%w: a text is a cue or a memory, and the kind named is neither", ErrRequest)
	}

	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: there is no text", ErrRequest)
	}
	if n := utf8.RuneCountInString(text); n > MaxCharacters {
		return nil, fmt.Errorf("%w: the text is %d characters; at most %d are taken", ErrRequest, n, MaxCharacters)
	}
	ids, err := e.tok.Encode(input, Window)
	if err != nil {
		return nil, err
	}

	select {
	case e.turn <- struct{}{}:
		defer func() { <-e.turn }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e.closed {
		return nil, errors.New("the engine is closed")
	}
	if e.w == nil {
		w, err := worker.Start(e.cfg.Worker, e.cfg.Model, e.cfg.Threads, Window)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSource, err)
		}
		e.w = w
	}
	raw, err := e.w.Vector(ctx, ids)
	e.used = time.Now()
	if err != nil {

		if e.w.Usable() {
			e.arm()
		} else {
			e.w.Close()
			e.w = nil
		}
		return nil, err
	}
	e.arm()
	return unit(raw)
}

func unit(v []float32) ([]float32, error) {
	sum := 0.0
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	norm := math.Sqrt(sum)
	if !(norm > 0) || math.IsInf(norm, 0) {
		return nil, errors.New("the model's vector has no length to divide by")
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out, nil
}

func (e *Engine) arm() {
	if e.cfg.Quiet == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.timer == nil {
		e.timer = time.AfterFunc(e.cfg.Quiet, e.quiet)
		return
	}
	e.timer.Reset(e.cfg.Quiet)
}

func (e *Engine) quiet() {
	select {
	case e.turn <- struct{}{}:
		defer func() { <-e.turn }()
	default:
		return
	}
	if e.w == nil || e.closed {
		return
	}
	if rest := e.cfg.Quiet - time.Since(e.used); rest > 0 {
		e.mu.Lock()
		if e.timer != nil {
			e.timer.Reset(rest)
		}
		e.mu.Unlock()
		return
	}
	e.w.Close()
	e.w = nil
}

func (e *Engine) Close() error {
	e.turn <- struct{}{}
	defer func() { <-e.turn }()
	e.closed = true
	e.mu.Lock()
	if e.timer != nil {
		e.timer.Stop()
	}
	e.mu.Unlock()
	if e.w == nil {
		return nil
	}
	err := e.w.Close()
	e.w = nil
	return err
}

func (e *Engine) running() bool {
	e.turn <- struct{}{}
	defer func() { <-e.turn }()
	return e.w != nil
}
