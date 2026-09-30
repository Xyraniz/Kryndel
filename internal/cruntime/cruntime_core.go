package cruntime

// Shared C value model, arena, execution budgets, metadata, and invocation state.
const cRuntimePrelude = `
#ifndef _WIN32
#define _POSIX_C_SOURCE 200809L
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
#include <errno.h>
#include <setjmp.h>
#include <limits.h>
#include <time.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <dirent.h>
#include <unistd.h>
#include <fcntl.h>

/* ---- portability shims ------------------------------------------------- */
#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#include <direct.h>
#include <io.h>
#include <process.h>
#include <windows.h>
typedef SOCKET KSocketFD;
#define K_INVALID_SOCKET INVALID_SOCKET
#define k_mkdir(p) _mkdir(p)
#define k_rmdir(p) _rmdir(p)
static void k_sleep_ms(long long m) { Sleep((DWORD)m); }
#else
#include <netdb.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/wait.h>
#include <dlfcn.h>
typedef int KSocketFD;
#define K_INVALID_SOCKET (-1)
#define k_mkdir(p) mkdir((p), 0755)
#define k_rmdir(p) rmdir(p)
static void k_sleep_ms(long long m) { struct timespec ts; ts.tv_sec=m/1000; ts.tv_nsec=(m%1000)*1000000LL; nanosleep(&ts,NULL); }
#endif

/* ---- error handling ---------------------------------------------------- */
static jmp_buf k_jmp;
static char k_errbuf[512];
static long long k_mem = 0;
static long long k_max_mem = 268435456LL;
static uint64_t k_instruction_count = 0;
static uint64_t k_max_instructions = 5000000ULL;
#ifdef _WIN32
static __declspec(thread) int k_call_depth = 0;
#else
static _Thread_local int k_call_depth = 0;
#endif
static int k_max_call_depth = 1024;
static long long k_out = 0;
static long long k_max_out = 16777216LL;
static long long k_max_json = 67108864LL;
static long long k_max_wall_ms = 0;
static unsigned long long k_runtime_deadline_ms = 0;
static long long k_max_tcp_receive = 0;
static long long k_max_array_elements = 1000000LL;
static void k_sqlite_cleanup(void);
static unsigned long long k_tcp_now_ms(void);
static unsigned long long k_tcp_deadline_after(unsigned long long now, unsigned long long duration);

static void kfail(const char *msg) {
    snprintf(k_errbuf, sizeof(k_errbuf), "%s", msg);
    longjmp(k_jmp, 1);
}

static inline void k_check_wall_time(void) {
    if (k_max_wall_ms < 0 || (k_runtime_deadline_ms && k_tcp_now_ms() >= k_runtime_deadline_ms))
        kfail("wall-clock execution limit exceeded");
}

static inline void k_step(void) {
    if (k_instruction_count >= k_max_instructions) kfail("instruction limit exceeded");
    k_instruction_count++;
    if (k_max_wall_ms < 0 || (k_runtime_deadline_ms && (k_instruction_count & 255ULL) == 0))
        k_check_wall_time();
}

static void *kalloc(size_t n) {
    k_mem += (long long)n;
    if (k_mem > k_max_mem) kfail("memory budget exceeded");
    void *p = malloc(n ? n : 1);
    if (!p) kfail("out of memory");
    return p;
}

/* ---- value model ------------------------------------------------------- */
typedef struct KValue KValue;
typedef struct KTcpSocket KTcpSocket;
typedef struct KSQLiteHandle KSQLiteHandle;
typedef struct { char *data; size_t len; } KStr;
typedef struct { KValue *items; size_t len; } KArr;
typedef struct { KValue *keys; KValue *vals; size_t len; } KMap;
typedef struct { int type_id; KValue *fields; } KStruct;
typedef struct { int type_id; int variant; } KEnum;

struct KValue {
    int tag;
    union {
        long long i;
        unsigned long long u64;
        double f;
        int b;
        KStr s;
        KArr a;
        KMap m;
        KStruct st;
        KEnum en;
        struct { int present; KValue *inner; } opt;
        struct { int ok; KValue *inner; } res;
        struct { KValue *cell; } sh;
        struct { struct KChan *ch; } ac;
        struct { struct KHandle *h; } hd;
        struct { KTcpSocket *socket; } tcp;
        struct { KSQLiteHandle *database; } sqlite;
    } u;
    struct KValue *json_root;
};

#ifdef _WIN32
static __declspec(thread) KValue (*k_tail_target)(void) = 0;
static __declspec(thread) int k_tail_pending = 0;
#else
static _Thread_local KValue (*k_tail_target)(void) = 0;
static _Thread_local int k_tail_pending = 0;
#endif

enum { K_NIL=0, K_INT, K_FLOAT, K_BOOL, K_STRING, K_BYTES, K_ARRAY,
       K_STRUCT, K_ENUM, K_OPTION, K_RESULT, K_MAP, K_SET, K_JSON,
       K_SHARED, K_ACTOR, K_THREAD, K_TASKGROUP, K_CHANNEL,
       K_UINT, K_JSON_NUMBER, K_TCP, K_SQLITE };

static KValue kv_nil(void) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_NIL; return v; }
static KValue kv_int(long long x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_INT; v.u.i=x; return v; }
static KValue kv_uint64(unsigned long long x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_UINT; v.u.u64=x; return v; }
static KValue kv_float(double x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_FLOAT; v.u.f=x; return v; }
static KValue kv_bool(int x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_BOOL; v.u.b=x?1:0; return v; }
static KValue kv_strn(const char *s, size_t n) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_STRING;
    v.u.s.data = (char*)kalloc(n+1); memcpy(v.u.s.data,s,n); v.u.s.data[n]=0; v.u.s.len=n; return v;
}
static KValue kv_str_take(char *s, size_t n) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_STRING; v.u.s.data=s; v.u.s.len=n; return v;
}
static KValue kv_cstr(const char *s) { return kv_strn(s, strlen(s)); }
static KValue kv_json_text(int tag, const char *s, size_t n) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=tag;
    v.u.s.data=(char*)kalloc(n+1); memcpy(v.u.s.data,s,n); v.u.s.data[n]=0; v.u.s.len=n; return v;
}
static KValue kv_jsonn(const char *s, size_t n) { return kv_json_text(K_JSON,s,n); }
static KValue kv_json_ownednode(KValue node, char *s, size_t n) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_JSON; v.u.s.data=s; v.u.s.len=n;
    v.json_root=(KValue*)kalloc(sizeof(KValue)); *v.json_root=node;
    return v;
}
static KValue kv_json_numbern(const char *s, size_t n) { return kv_json_text(K_JSON_NUMBER,s,n); }
static KValue kv_bytesn(const char *s, size_t n) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_BYTES;
    v.u.s.data = (char*)kalloc(n+1); memcpy(v.u.s.data,s,n); v.u.s.data[n]=0; v.u.s.len=n; return v;
}
static KValue kv_arr(KValue *items, size_t n) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_ARRAY; v.u.a.items=items; v.u.a.len=n; return v; }
static KValue kv_set(KValue *items, size_t n) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_SET; v.u.a.items=items; v.u.a.len=n; return v; }
static KValue kv_map(KValue *keys, KValue *vals, size_t n) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_MAP; v.u.m.keys=keys; v.u.m.vals=vals; v.u.m.len=n; return v; }
static KValue kv_struct(int tid, KValue *fields) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_STRUCT; v.u.st.type_id=tid; v.u.st.fields=fields; return v; }
static KValue kv_enum(int tid, int variant) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_ENUM; v.u.en.type_id=tid; v.u.en.variant=variant; return v; }
static KValue kv_opt(int present, KValue inner) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_OPTION; v.u.opt.present=present?1:0;
    if (present) { v.u.opt.inner=(KValue*)kalloc(sizeof(KValue)); *v.u.opt.inner=inner; }
    return v;
}
static KValue kv_res(int ok, KValue inner) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_RESULT; v.u.res.ok=ok?1:0;
    v.u.res.inner=(KValue*)kalloc(sizeof(KValue)); *v.u.res.inner=inner; return v;
}

/* ---- type metadata ----------------------------------------------------- */
typedef struct { const char *name; int nfields; const char **fields; } KStructDesc;
typedef struct { const char *name; int nvariants; const char **variants; } KEnumDesc;
static KStructDesc *k_structs = 0;
static KEnumDesc *k_enums = 0;

/* ---- call frame and defer stack ---------------------------------------- */
#define K_MAX_ARGS 64
static KValue k_args[K_MAX_ARGS];
static int k_argc;
#define K_MAX_DEFERS 512
static void (*k_defers[K_MAX_DEFERS])(void);
static int k_ndefers = 0;
`
