# Windows x64 ABI contract for native code

This is the interoperability contract for a direct `windows-x64` PE32+
backend. It applies at every call into Windows and at every Kryndel function
that uses the Windows calling convention. KIR carries checked types and
ordered arguments, but does not itself assign registers or stack slots. The
existing Go interpreter runs on Windows; the C AOT `exe`/`pe` formats use a
MinGW-capable compiler. `pe-direct` writes a PE32+ executable for static output
and a bounded scalar subset without external tools. It calls three
fixed-signature Win32 functions and supports Kryndel functions with up to four
integer or pointer arguments. Floating-point ABI values, aggregate arguments,
and stack-passed arguments remain unsupported. Each further direct backend
capability must implement and test the applicable requirements below.

## Arguments and results

Windows x64 has four **positional** register slots. Integer and pointer
arguments in positions 1–4 use `RCX`, `RDX`, `R8`, and `R9`; floating-point
arguments in those positions use `XMM0`–`XMM3` at the same index. A mixed call
such as `(Int, Float, Int, Float)` therefore uses `RCX`, `XMM1`, `R8`, and
`XMM3`; unused registers do not shift later arguments. Arguments from
position 5 onward occupy eight-byte-aligned stack slots after the 32-byte
shadow space. A single argument is never split between registers. Stack
arguments appear in their original order at increasing addresses, although
the calling convention describes their placement as right-to-left. Aggregate
values that cannot be passed as an allowed one-, two-, four-, or eight-byte
integer value are passed by pointer to caller-owned storage with the required
alignment. [Microsoft's x64 calling convention](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention?view=msvc-170)
specifies the exact parameter and aggregate rules.

Integer and pointer scalar results of at most 64 bits use `RAX`; scalar
floating-point results use `XMM0`. Larger or otherwise ineligible aggregates
require caller-allocated result storage, passed as a hidden first pointer
argument, which shifts the other arguments by one position. This is an ABI
boundary requirement, not a promise that the current direct backend supports
every Kryndel aggregate type. See [Microsoft's return-value rules](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention?view=msvc-170#return-values).

## Stack and register lifetime

The caller allocates **32 bytes of shadow space for every call**, including
calls with fewer than four parameters. Immediately on callee entry, `[RSP]`
holds the return address, `[RSP+8]` through `[RSP+39]` are the four shadow
slots, and `[RSP+40]` is the fifth argument. Before a `call`, `RSP` is
16-byte aligned; after the return address is pushed, the callee entry value
is 8 modulo 16. Nonleaf function bodies restore 16-byte alignment outside
their prolog and epilog. The shadow area belongs to the callee for saving
register parameters, so the caller cannot keep live values there across a
call. Storage below the current `RSP` is volatile and cannot be used as a
SysV-style red zone. [Microsoft documents shadow space and stack alignment](https://learn.microsoft.com/en-us/cpp/build/stack-usage?view=msvc-170).

The caller must assume `RAX`, `RCX`, `RDX`, `R8`–`R11`, and `XMM0`–`XMM5`
are overwritten. A callee that uses `RBX`, `RBP`, `RDI`, `RSI`, `R12`–`R15`,
or `XMM6`–`XMM15` must save and restore them. `RSP` must be restored as well.
These are the [Windows x64 volatile and nonvolatile sets](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention?view=msvc-170#callercallee-saved-registers);
SysV AMD64's six integer argument registers and preservation rules do not
apply to Windows calls.

Nonleaf functions need valid unwind information for Windows exception
dispatch and stack walking; their prologs and epilogs must match that metadata.
The current direct PE backend emits `RUNTIME_FUNCTION` entries and version 1
unwind records for the entry function and each user function. Its leaf exit
stubs do not alter nonvolatile state. The structural regression checks the
function ranges; Windows execution tests also exercise generated calls. The
test suite does not yet inject exceptions through generated frames.
[Microsoft describes the unwind requirement](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention?view=msvc-170#unwindability).

## Variadic calls

For a variadic or unprototyped Windows function, a floating-point argument
in one of the first four positions must be duplicated into the corresponding
integer register as well as `XMM0`–`XMM3`. Later arguments still use the stack
slots after shadow space. The first direct backend may reject such imports;
silently lowering them as ordinary fixed-signature calls is incorrect.
[Microsoft's varargs rules](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention?view=msvc-170#varargs)
cover this case.

## PE imports and runtime boundary

A direct backend must write an AMD64 PE32+ image with a valid entry-point RVA,
section alignment, console subsystem when standard streams are used, import
directory, import lookup/name records, import address table, and terminating
null entries. Calls into a DLL go through the loader-resolved import address
table using the Windows x64 ABI. The [Microsoft PE format reference](https://learn.microsoft.com/en-us/windows/win32/debug/pe-format)
defines these structures and their RVA rules. Importing a Win32 function
does not require embedding a C runtime or invoking a C compiler.

For the current minimal console runtime, the Win32 boundary is
`GetStdHandle(STD_OUTPUT_HANDLE)` (and `STD_ERROR_HANDLE` for diagnostics),
`WriteFile` for bytes sent to those handles, and `ExitProcess` for the exit
status. The backend checks null or invalid standard handles, retries short
writes, fails a write that makes no progress, and works with redirected stdout.
Standard error and console code page conversion are not implemented. These
functions are documented by
[GetStdHandle](https://learn.microsoft.com/en-us/windows/console/getstdhandle),
[WriteFile](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-writefile),
and [ExitProcess](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-exitprocess).
Additional imports require an explicit runtime capability and ownership rule.

## Acceptance checks

| Boundary | Required evidence |
| --- | --- |
| KIR transport | Windows target metadata, checked argument types, and all positional arguments survive `EmitKIR`/`DecodeKIR` on Windows and Linux. |
| PE structure | Parse the emitted image independently, including machine, optional header, sections, entry point, and import directory. |
| Windows execution | Launch generated `.exe` files on Windows with captured and redirected stdout/stderr; check output and exit status. |
| Calls | Current subset: zero through four integer/pointer register arguments, nested calls, stack alignment, and callee-saved registers. Future support must cover stack arguments, floats, and aggregates before accepting them. |
| Unsupported ABI cases | Reject variadic calls and aggregate passing until each has lowering and a Windows execution regression; report nonleaf exception unwinding as unsupported until unwind tables are emitted and checked. |

The `windows-latest` CI job runs the Go package tests natively, so the KIR
transport check and both static and dynamic PE launches are exercised on
Windows. Structural tests parse the image, imports, and unwind ranges with
Go's `debug/pe` reader on every host. Stack arguments, floats, aggregates,
stderr, and injected exception unwinding remain acceptance work for later
backend capabilities.
