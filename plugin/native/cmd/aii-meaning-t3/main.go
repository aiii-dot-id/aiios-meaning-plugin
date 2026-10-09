package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"

	"github.com/aiii-dot-id/aiios-meaning-plugin/plugin/native/engine"
)

const (
	pluginID  = "id.aiii.meaning"
	readyMark = "AII_MEANING_READY"

	runtimeEnv = "AII_RUNTIME_ROOT"
	modelsEnv  = "AII_MODELS_DIR"

	workerName    = "meaning-worker"
	modelName     = "snowflake-arctic-embed-m-v2.0-q8.onnx"
	tokenizerName = "snowflake-arctic-embed-m-v2.0-tokenizer.json"

	opEmbed = "embed"
	argKind = "kind"
	argText = "text"

	reasonArgument = "OPERATION_ARGUMENT_INVALID"
	reasonEngine   = "MEANING_ENGINE_FAILED"

	maxThreads = 4

	quiet = time.Minute

	readyText = "ready"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "aii-meaning-t3:", err)
		os.Exit(1)
	}
}

func run() error {
	p := aiiosdk.New(pluginID)
	var eng *engine.Engine

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	p.Handle(opEmbed, func(c aiiosdk.Call) (any, error) {
		kind, haveKind := c.Args().String(argKind)
		text, haveText := c.Args().String(argText)
		if !haveKind || !haveText {
			return nil, aiiosdk.Fail(reasonArgument, "embed takes a kind and a text, both strings")
		}
		v, err := eng.Embed(ctx, engine.Kind(kind), text)
		if errors.Is(err, engine.ErrRequest) {
			return nil, aiiosdk.Fail(reasonArgument, err.Error())
		}
		if errors.Is(err, engine.ErrSource) {

			fmt.Fprintln(os.Stderr, "aii-meaning-t3:", err)
			os.Exit(1)
		}
		if err != nil {

			fmt.Fprintln(os.Stderr, "aii-meaning-t3: embed:", err)
			return nil, aiiosdk.Fail(reasonEngine, "")
		}
		out := make([]float64, len(v))
		for i, x := range v {
			out[i] = float64(x)
		}
		return map[string]any{"vector": out}, nil
	})

	if os.Getenv(aiiosdk.DescribeEnv) == "1" {
		return p.Serve("")
	}

	cfg, err := config()
	if err != nil {
		return err
	}
	if eng, err = engine.New(cfg); err != nil {
		return err
	}
	defer eng.Close()

	debug.FreeOSMemory()

	began := time.Now()
	if _, err := eng.Embed(ctx, engine.Cue, readyText); err != nil {
		return fmt.Errorf("the model did not answer at start: %w", err)
	}
	ready := aiiosdk.ReadyReport{ModelsLoaded: 1, Accelerator: "cpu", ProbeMS: max(1, int(time.Since(began).Milliseconds()))}
	return p.ServeReady(readyMark, ready)
}

func config() (engine.Config, error) {
	root, models := os.Getenv(runtimeEnv), os.Getenv(modelsEnv)
	if !filepath.IsAbs(root) || !filepath.IsAbs(models) {
		return engine.Config{}, fmt.Errorf("the host names %s and %s as absolute paths, and at least one is missing", runtimeEnv, modelsEnv)
	}
	worker := workerName
	if runtime.GOOS == "windows" {
		worker += ".exe"
	}
	return engine.Config{
		Tokenizer: filepath.Join(models, tokenizerName),
		Worker:    filepath.Join(root, worker),
		Model:     filepath.Join(models, modelName),
		Threads:   min(maxThreads, runtime.NumCPU()),
		Quiet:     quiet,
	}, nil
}
