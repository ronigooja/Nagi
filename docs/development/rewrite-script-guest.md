# Rewrite script guest

This document describes the independently tested JavaScript executor in
`internal/rewritescript`, its resource boundary, and reproduction of the embedded
WebAssembly artifact. It does not establish live request-rewrite integration or
platform acceptance. The request-rewrite integration owns message validation,
request authorization, persistence, activation, and failure policy.

## Execution contract

`Prepare(ctx)` warms the immutable WASM compilation cache. `Execute(ctx, source,
input)` invokes a synchronous global `transform(input)` in a new QuickJS/WASM
instance and returns JSON. The result must contain `action` equal to `unchanged`,
`replace`, or `block`. Callers validate all remaining message fields. Promises
are not awaited, module imports have no loader, and no asynchronous jobs run.

The process admits at most two executions, immediately returning `script_busy`
when both slots are occupied. Each execution has a one-second deadline constrained
by its parent's deadline. That deadline covers compilation (on a cold cache),
instantiation, evaluation, copying, and output validation. Prepare during service
startup to avoid charging cold compilation to the first request. Cancellation
interrupts guest execution; the runtime and its guest memory are destroyed before
the slot is released. Script global state never survives an invocation.

Source is limited to 64 KiB; input and output JSON are each limited to 12 MiB.
Integration may apply stricter source/message limits. The 12 MiB envelope permits
an 8 MiB binary message encoded as base64 plus metadata. Host copies are bounded
by those limits; this is not a process RSS cap. Guest linear memory has a hard
128 MiB maximum, its C stack reserves 1 MiB, and QuickJS applies a 120 MiB heap
accounting limit and 512 KiB JavaScript stack limit. Allocation overhead can
reach the linear-memory maximum before the QuickJS heap-accounting limit.

The guest includes QuickJS core, not `quickjs-libc`. It exports allocation and
JSON-transform functions only. Its five libc WASI imports (`clock_time_get`,
`fd_close`, `fd_seek`, `fd_write`, and `poll_oneoff`) are host stubs returning
`ENOTCAPABLE`; they perform no host operation. There is no filesystem mount,
network, process API, environment, clock, entropy, module downloader, or output
channel. `Date` and `Math.random` are removed before evaluating source. No
request/body/source/exception/stack information appears in returned errors.
Only stable `script_*` codes are returned. The caller must continue to apply
ordinary redaction rules to explicitly viewed or exported scripts and messages,
which may contain sensitive data.

## Fixed inputs and reproduction

Normal Go builds embed the checked-in
[`quickjs.wasm`](../../internal/rewritescript/guest/quickjs.wasm); no C toolchain
or network download is required. The Go dependency is
`github.com/tetratelabs/wazero v1.10.1`.

The reproducible guest build uses these fixed inputs:

| Input | Version or commit | SHA-256 |
| --- | --- | --- |
| QuickJS source archive | `quickjs-2025-09-13-2.tar.xz` | `996c6b5018fc955ad4d06426d0e9cb713685a00c825aa5c0418bd53f7df8b0b4` |
| Linux ARM64 WASI SDK archive | `wasi-sdk-27.0-arm64-linux.tar.gz` | `4cf4c553c4640e63e780442146f87d83fdff5737f988c06a6e3b2f0228e37665` |
| Nagi C runner | [`runner.c`](../../internal/rewritescript/guest/runner.c) | `6d081aa5a2d02fb04650016a3662b31fc9c38f1457df7fb8609d7c877cb751d0` |
| Embedded WASM | `quickjs.wasm` | `5932a32f2d0b0dea7e02a6861852d437ba6e11addf9d6636e52122e1285bf0ba` |

The SDK uses Clang 20.1.8, LLVM commit
`87f0227cb60147a26a1eeb4fb06e3b505e9c7261`, and wasi-libc commit
`3f7eb4c7d6ede4dde3c4bffa6ed14e8d656fe93f`. The script downloads the exact
archives, verifies every input before compiling, applies one mechanical change
(removing an unused `setjmp.h` include from `dtoa.c`), and force-includes
`malloc.h` for WASI's existing `malloc_usable_size` declaration. Removing that
unused header avoids requiring the unsupported WASM exception-handling proposal.
The source's executable logic is unchanged. Build flags, source URLs, and the
expected artifact digest are recorded in the script.

On Linux ARM64, run from the repository root:

```sh
./scripts/build-rewrite-script-guest.sh
```

For an offline build, supply previously downloaded and verified archives:

```sh
NAGI_QUICKJS_ARCHIVE=/path/to/quickjs-2025-09-13-2.tar.xz \
NAGI_WASI_SDK_ARCHIVE=/path/to/wasi-sdk-27.0-arm64-linux.tar.gz \
  ./scripts/build-rewrite-script-guest.sh
```

The script verifies the rebuilt artifact digest before replacing the checked-in
file, then removes its temporary build directory. Reproduction with other host
architectures or compiler versions is unverified and is explicitly rejected by
this pinned script. The resulting WASM is portable across supported wazero hosts.

QuickJS's MIT license is retained in
[`QUICKJS-LICENSE`](../../internal/rewritescript/guest/QUICKJS-LICENSE). The
linked wasi-libc and compiler runtime notices are retained under
[`guest/licenses`](../../internal/rewritescript/guest/licenses/). The wasi-libc
license index identifies the source components and their applicable licenses.

## Verification

```sh
go test ./internal/rewritescript -count=1
go test -race ./internal/rewritescript -count=1
```

Tests exercise a real 8 MiB binary base64 roundtrip, deterministic repeated
invocations, absence of host APIs, fixed capability-denying imports, invalid and oversized
outputs, allocation exhaustion, source/input bounds, timeout, parent cancellation,
concurrent admission, and recovery after failure. The 8 MiB throughput acceptance runs without race instrumentation: instrumented
execution can exceed the unchanged one-second production deadline, so that one
test is explicitly skipped in race builds. All isolation, cancellation, memory,
and admission tests also run under the race detector. Artifact digest and the WASM
import/memory boundaries are asserted separately. The pinned build was reproduced
in a second temporary directory with the same artifact hash on Linux ARM64.

This package's tests do not establish live interception, macOS native behavior,
production latency under load, or a hard process RSS limit.
