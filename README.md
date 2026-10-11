# AII OS meaning plugin

`id.aiii.meaning` is an optional plugin for AII OS that turns a memory or a search into a vector on the machine
the identity runs on, so that recall finds what is near in meaning as well as what shares words. Only AII OS asks
it. It has no network, no tools, no settings and no storage, and keeps nothing it is handed.

## Install and use

Install it through the AII OS plugin catalog (AII OS 0.1.15 or later; this release is the one for 0.1.16). AII OS
chooses the build for its system, downloads the runtime companion from this project's release and the model and
its tokenizer file from their publisher, and holds each file to its hash at download and at every start. Before
it uses the plugin it asks the package's signed checks, and it uses the plugin only if every answer agrees with
the publisher's.

Systems: Linux x86-64 and arm64 (glibc 2.34 or later), macOS on Apple silicon (macOS 26.5 or later), Windows
x86-64. It computes on the processor.

In the dashboard, the plugin's card under Settings → Plugins shows its state and its checks, and the Memory card
shows which source reads the memories. The plugin is a background helper: a resident never calls it.

## The model

| | |
|---|---|
| Model | `Snowflake/snowflake-arctic-embed-m-v2.0` on huggingface.co, Apache-2.0 |
| Revision | `95c2741480856aa9666782eb4afe11959938017f` |
| `onnx/model_int8.onnx` | SHA-256 `03d923bb1850ebdccb068e2f3abd8aa43fe81c50d07d037ef103fe3d0fb78e3b` |
| `tokenizer.json` | SHA-256 `f1cc44ad7faaeec47241864835473fd5403f2da94673f3f764a77ebcb0a803ec` |

Neither file is in this repository or in the release. AII OS fetches both from the publisher and refuses either if
its hash differs. A memory is read to its first 2,048 tokens.

## What it finds, and what it does not

- **A memory is one vector of its first 2,048 tokens.** By meaning alone, among 5,150 memories, a search about what
  a long memory is about finds it about seven times in ten, a search for one detail inside it about four times in
  ten, and a detail past the part that is read less often still. Recall also finds a memory by its words. A
  Japanese, Korean or Chinese memory is read to about its first 2,900 to 3,900 characters.
- **A short paraphrase of a short belief** (four to eight words that share none with it) is found about half the
  time; one of fourteen to twenty-four words, more than eight times in ten.
- **On AII OS 0.1.15** a search made while the plugin is reading new memories can wait behind them and be answered
  by words alone. AII OS 0.1.16 serves the search first.
- **Processors.** Tested on x86-64 with and without AVX-512, on Arm (a Cortex-A76 and Apple silicon), not on Arm
  processors with SVE.
- **Two choices the operator makes on AII OS.** By default it uses four processor threads and keeps the model
  loaded for a minute after the last request. An AII OS that offers the choice hands it to the plugin as
  `AII_THREADS` and `AII_KEEP_LOADED_SECONDS`; AII OS 0.1.15 and 0.1.16 do not, and the defaults hold. It uses
  four threads at most.

## Where the code lies

| Part | Where | What it does |
|---|---|---|
| The carrier (Go, on the AII OS Plugin SDK) | `plugin/native/cmd/aii-meaning-t3` | answers AII OS's request for a vector, one at a time |
| The engine | `plugin/native/engine` | text to tokens, tokens to the worker, the vector divided by its length |
| The worker's client | `plugin/native/worker` | starts the worker, exchanges one request and one reply with it, ends it with the carrier |
| The tokenizer | `plugin/native/tokenizer` | the model's tokenizer in Go; its Unicode tables are made by `internal/ucd/gen.py` from the Unicode 17.0 data beside it |
| The worker (C++, ONNX Runtime 1.24.2) | `runtime/native` | loads the model, takes token ids, returns the vector; the only native code |
| The package's definition | `plugin/native/cmd/aii-meaning-t3/plugin.json` | variants, model, memory asked of the host; with `embeddings.json` (the signed statement and probes) and `validation.json` (the checks) |

## Build

    cmake -S runtime/native -B .build/worker -DORT_INCLUDE=<onnxruntime headers> -DORT_LIBRARY=<onnxruntime library> \
        [-DCMAKE_OSX_DEPLOYMENT_TARGET=26.5]    # required on macOS
    cmake --build .build/worker
    scripts/build-carrier.sh                    # the carrier for the four systems

The Plugin SDK is its public release v0.1.16, required in `plugin/native/go.mod` and vendored in
`plugin/native/vendor`, so nothing is fetched. To package: lay out each system's runtime (the worker, the ONNX
Runtime library, on Windows the C++ runtime libraries, and their licences), pack it with
`aiisdk runtime-pack -dir <dir> -o meaning-runtime-<variant>.tar.gz -platform <linux|macos|windows>`, write
`plugin.json`'s runtimes with `scripts/runtimes.py`, and run `aiisdk package` in `plugin/native/cmd/aii-meaning-t3`.
`aiisdk` is the Plugin SDK's own tool.

The tests and the reference set the engine is held to are kept in the private repository: this source builds and
packages the plugin, and does not rerun its suite.

## Licence

Apache License 2.0 (`LICENSE`). The Unicode data in `plugin/native/tokenizer/internal/ucd` is under the Unicode
licence beside it. The model is its publisher's, under its own terms (Apache-2.0). ONNX Runtime travels in the
runtime companions with its licence and notices, and on Windows the Visual C++ runtime libraries with theirs.
