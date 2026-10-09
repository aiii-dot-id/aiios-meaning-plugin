#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

#include "onnxruntime_c_api.h"

#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <werapi.h>
#include <csignal>
#include <cwchar>
#include <fcntl.h>
#include <io.h>
#else
#include <cerrno>
#include <sys/resource.h>
#include <thread>
#include <unistd.h>
#ifdef __linux__
#include <sys/prctl.h>
#endif
#endif

namespace {

const uint32_t kProtocol = 1;
const int kMaxThreads = 4;
const uint32_t kMaxReason = 4096;

const OrtApi * api = nullptr;

bool read_all(void * p, size_t n) { return n == 0 || fread(p, 1, n, stdin) == n; }
bool write_all(const void * p, size_t n) { return n == 0 || fwrite(p, 1, n, stdout) == n; }

[[noreturn]] void stop(const std::string & why) {
    std::string msg = why.empty() ? "stopped" : why;
    if (msg.size() > kMaxReason) msg.resize(kMaxReason);
    const uint32_t n = static_cast<uint32_t>(msg.size());
    write_all(&n, 4);
    write_all(msg.data(), msg.size());
    fflush(stdout);
    fprintf(stderr, "meaning-worker: %s\n", msg.c_str());
    exit(1);
}

void ok(OrtStatus * st, const char * doing) {
    if (st == nullptr) return;
    std::string why = std::string(doing) + ": " + api->GetErrorMessage(st);
    api->ReleaseStatus(st);
    stop(why);
}

bool has_name(OrtSession * session, OrtAllocator * alloc, bool input, const char * want, size_t * count) {
    ok(input ? api->SessionGetInputCount(session, count) : api->SessionGetOutputCount(session, count), "reading the model's names");
    bool found = false;
    for (size_t i = 0; i < *count; i++) {
        char * name = nullptr;
        ok(input ? api->SessionGetInputName(session, i, alloc, &name) : api->SessionGetOutputName(session, i, alloc, &name), "reading a name");
        if (strcmp(name, want) == 0) found = true;
        ok(api->AllocatorFree(alloc, name), "freeing a name");
    }
    return found;
}

#ifdef _WIN32
void end_now(int) { TerminateProcess(GetCurrentProcess(), 3); }
LONG WINAPI end_on_fault(EXCEPTION_POINTERS *) {
    TerminateProcess(GetCurrentProcess(), 3);
    return EXCEPTION_CONTINUE_SEARCH;
}

void exclude(const void * p, size_t n) {
    if (p != nullptr && n > 0) WerRegisterExcludedMemoryBlock(p, static_cast<DWORD>(n));
}
#endif

}

#ifdef _WIN32
static long number(const wchar_t * s) { return wcstol(s, nullptr, 10); }
#else
static long number(const char * s) { return strtol(s, nullptr, 10); }
#endif

#ifdef _WIN32
int wmain(int argc, wchar_t ** argv) {
    _setmode(_fileno(stdin), _O_BINARY);
    _setmode(_fileno(stdout), _O_BINARY);
#else
int main(int argc, char ** argv) {
#endif
    {
        const uint16_t probe = 1;
        if (*reinterpret_cast<const uint8_t *>(&probe) != 1) stop("this worker is built for little-endian machines only");
    }
#ifndef _WIN32
    {
        const struct rlimit none = {0, 0};
        setrlimit(RLIMIT_CORE, &none);
#ifdef __linux__
        prctl(PR_SET_DUMPABLE, 0);
#endif
    }
#else
    {
        SetErrorMode(GetErrorMode() | SEM_FAILCRITICALERRORS | SEM_NOGPFAULTERRORBOX);
        _set_abort_behavior(0, _WRITE_ABORT_MSG | _CALL_REPORTFAULT);
        signal(SIGABRT, end_now);
        SetUnhandledExceptionFilter(end_on_fault);
        WerSetFlags(WER_FAULT_REPORTING_FLAG_NOHEAP);
    }
#endif
#ifdef _WIN32
    if (argc != 4) stop("usage: meaning-worker <model.onnx> <threads> <max tokens>");
#else
    if (argc != 4 && argc != 5) stop("usage: meaning-worker <model.onnx> <threads> <max tokens> [<carrier descriptor>]");
#endif
    const long threads = number(argv[2]);
    const long max_tokens = number(argv[3]);
    if (threads < 1 || threads > kMaxThreads) stop("threads must be 1 to 4: the plugin's budget is four");
    if (max_tokens < 3 || max_tokens > 65536) stop("max tokens must be 3 to 65536");
#ifndef _WIN32
    if (argc == 5) {
        const int carrier = static_cast<int>(number(argv[4]));
        if (carrier < 3) stop("the carrier descriptor is 3 or more");
        std::thread([carrier] {
            char byte;
            for (;;) {
                const ssize_t n = read(carrier, &byte, 1);
                if (n == 0 || (n < 0 && errno != EINTR)) _Exit(1);
            }
        }).detach();
    }
#endif

    api = OrtGetApiBase()->GetApi(ORT_API_VERSION);
    if (api == nullptr) stop("the ONNX Runtime library is older than the one this worker was built against");

    OrtEnv * env = nullptr;
    ok(api->CreateEnv(ORT_LOGGING_LEVEL_ERROR, "meaning", &env), "starting the runtime");
    ok(api->DisableTelemetryEvents(env), "turning the runtime's telemetry off");
    OrtSessionOptions * options = nullptr;
    ok(api->CreateSessionOptions(&options), "making session options");
    ok(api->SetIntraOpNumThreads(options, static_cast<int>(threads)), "setting threads");
    ok(api->SetInterOpNumThreads(options, 1), "setting threads");
    ok(api->SetSessionGraphOptimizationLevel(options, ORT_ENABLE_ALL), "setting optimisation");

    OrtSession * session = nullptr;
    ok(api->CreateSession(env, argv[1], options, &session), "loading the model");

    OrtAllocator * alloc = nullptr;
    ok(api->GetAllocatorWithDefaultOptions(&alloc), "getting an allocator");
    size_t inputs = 0, outputs = 0;
    const bool has_ids = has_name(session, alloc, true, "input_ids", &inputs);
    const bool has_mask = has_name(session, alloc, true, "attention_mask", &inputs);
    if (!has_ids || !has_mask || inputs != 2) stop("the model does not take exactly input_ids and attention_mask");
    if (!has_name(session, alloc, false, "token_embeddings", &outputs)) stop("the model gives no output named token_embeddings");

    OrtMemoryInfo * memory = nullptr;
    ok(api->CreateCpuMemoryInfo(OrtArenaAllocator, OrtMemTypeDefault, &memory), "describing memory");
    const char * in_names[] = {"input_ids", "attention_mask"};
    const char * out_names[] = {"token_embeddings"};

    std::vector<int32_t> ids32;
    std::vector<int64_t> ids64;
    std::vector<int64_t> mask;
    ids32.reserve(static_cast<size_t>(max_tokens));
    ids64.reserve(static_cast<size_t>(max_tokens));
    mask.reserve(static_cast<size_t>(max_tokens));

    auto run = [&](std::vector<int64_t> & ids, std::vector<float> * vec) {
        mask.assign(ids.size(), 1);
        const int64_t shape[2] = {1, static_cast<int64_t>(ids.size())};
        OrtValue * in[2] = {nullptr, nullptr};
        ok(api->CreateTensorWithDataAsOrtValue(memory, ids.data(), ids.size() * sizeof(int64_t), shape, 2, ONNX_TENSOR_ELEMENT_DATA_TYPE_INT64, &in[0]), "making the ids tensor");
        ok(api->CreateTensorWithDataAsOrtValue(memory, mask.data(), mask.size() * sizeof(int64_t), shape, 2, ONNX_TENSOR_ELEMENT_DATA_TYPE_INT64, &in[1]), "making the mask tensor");
        OrtValue * out = nullptr;
        ok(api->Run(session, nullptr, in_names, in, 2, out_names, 1, &out), "running the model");
        OrtTensorTypeAndShapeInfo * info = nullptr;
        ok(api->GetTensorTypeAndShape(out, &info), "reading the output's shape");
        size_t rank = 0;
        ok(api->GetDimensionsCount(info, &rank), "reading the output's shape");
        ONNXTensorElementDataType type = ONNX_TENSOR_ELEMENT_DATA_TYPE_UNDEFINED;
        ok(api->GetTensorElementType(info, &type), "reading the output's type");
        std::vector<int64_t> dims(rank);
        ok(api->GetDimensions(info, dims.data(), rank), "reading the output's shape");
        api->ReleaseTensorTypeAndShapeInfo(info);
        if (type != ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT || rank != 3 || dims[0] != 1 || dims[1] != shape[1] || dims[2] < 1 || dims[2] > 65536)
            stop("the model's output is not one row of float vectors for each token");
        float * data = nullptr;
        ok(api->GetTensorMutableData(out, reinterpret_cast<void **>(&data)), "reading the output");
        vec->assign(data, data + dims[2]);
        api->ReleaseValue(out);
        api->ReleaseValue(in[0]);
        api->ReleaseValue(in[1]);
    };

    std::vector<float> vec;
    {
        std::vector<int64_t> warm = {0, 2};
        run(warm, &vec);
    }
    const uint32_t dimension = static_cast<uint32_t>(vec.size());
#ifdef _WIN32
    exclude(ids32.data(), ids32.capacity() * sizeof(int32_t));
    exclude(ids64.data(), ids64.capacity() * sizeof(int64_t));
    exclude(mask.data(), mask.capacity() * sizeof(int64_t));
    exclude(vec.data(), vec.capacity() * sizeof(float));
#endif
    if (!write_all("MNGW", 4) || !write_all(&kProtocol, 4) || !write_all(&dimension, 4) || fflush(stdout) != 0) return 1;

    for (;;) {
        uint32_t n = 0;
        if (!read_all(&n, 4)) return 0;
        if (n == 0) return 0;
        if (n > static_cast<uint32_t>(max_tokens)) stop("a request of more tokens than this worker was started for");
        ids32.resize(n);
        if (!read_all(ids32.data(), n * sizeof(int32_t))) stop("a request ended before its tokens did");
        ids64.assign(ids32.begin(), ids32.end());
        run(ids64, &vec);
        if (vec.size() != dimension) stop("the model's dimension changed between two runs");
        const uint32_t zero = 0;
        if (!write_all(&zero, 4) || !write_all(vec.data(), vec.size() * sizeof(float)) || fflush(stdout) != 0) return 1;
    }
}
