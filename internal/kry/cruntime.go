package kry

// cRuntimePrelude is the embedded C runtime that every native Kryndel program
// links against. It implements the same value model, checked arithmetic,
// deterministic display, and pure builtins as the Go interpreter so that
// native output matches interpreted output byte for byte.
//
// Values are immutable and arena-allocated; because Kryndel collections are
// immutable, sharing value pointers is safe and no deep copy is required.
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
static long long k_out = 0;
static long long k_max_out = 16777216LL;
static long long k_max_json = 67108864LL;
static long long k_max_wall_ms = 0;
static long long k_max_tcp_receive = 0;
static long long k_max_array_elements = 1000000LL;
static void k_sqlite_cleanup(void);

static void kfail(const char *msg) {
    snprintf(k_errbuf, sizeof(k_errbuf), "%s", msg);
    longjmp(k_jmp, 1);
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

// cRuntimeDisplay holds the display/equality/arithmetic half of the runtime.
const cRuntimeDisplay = `
/* ---- string builder ---------------------------------------------------- */
typedef struct { char *buf; size_t len; size_t cap; } KBuf;
static void kb_init(KBuf *b) { b->cap=64; b->len=0; b->buf=(char*)kalloc(b->cap); b->buf[0]=0; }
static void kb_grow(KBuf *b) {
    if (b->cap>SIZE_MAX/2 || b->cap>(size_t)LLONG_MAX) kfail("memory budget exceeded");
    size_t next=b->cap*2;
    long long old=(long long)b->cap;
    if (k_mem<old || next>(size_t)LLONG_MAX || k_mem-old>k_max_mem-(long long)next) kfail("memory budget exceeded");
    char *n=(char*)realloc(b->buf,next);
    if (!n) kfail("out of memory");
    k_mem=k_mem-old+(long long)next;
    b->buf=n; b->cap=next;
}
static void kb_putc(KBuf *b, char c) { if (b->len>=b->cap-1) kb_grow(b); b->buf[b->len++]=c; b->buf[b->len]=0; }
static void kb_putn(KBuf *b, const char *s, size_t n) { for (size_t i=0;i<n;i++) kb_putc(b,s[i]); }
static void kb_puts(KBuf *b, const char *s) { while (*s) kb_putc(b,*s++); }

/* ---- Go-compatible float formatting ------------------------------------ */
static void k_fmt_float(double v, char *out, size_t outsz) {
    if (isnan(v)) { snprintf(out,outsz,"NaN"); return; }
    if (isinf(v)) { snprintf(out,outsz, v<0?"-Inf":"+Inf"); return; }
    if (v==0) { snprintf(out,outsz,"0"); return; }
    char tmp[64]; int prec;
    for (prec=1; prec<=17; prec++) {
        snprintf(tmp,sizeof(tmp),"%.*g",prec,v);
        if (strtod(tmp,NULL)==v) break;
    }
    if (prec>17) prec=17;
    char ebuf[64];
    snprintf(ebuf,sizeof(ebuf),"%.*e",prec-1,v);
    const char *p=ebuf; int neg=0;
    if (*p=='-') { neg=1; p++; }
    char digits[32]; int nd=0;
    while (*p && *p!='e' && *p!='E') { if (*p>='0'&&*p<='9') digits[nd++]=*p; p++; }
    digits[nd]=0;
    int exp10=0;
    if (*p=='e'||*p=='E') exp10=atoi(p+1);
    while (nd>1 && digits[nd-1]=='0') { nd--; digits[nd]=0; }
    char *o=out;
    if (neg) *o++='-';
    if (exp10 < -4 || exp10 >= 6) {
        *o++=digits[0];
        if (nd>1) { *o++='.'; for (int i=1;i<nd;i++) *o++=digits[i]; }
        *o++='e';
        int e=exp10; *o++ = e<0?'-':'+'; if (e<0) e=-e;
        char eb[8]; int en=0;
        if (e==0) eb[en++]='0';
        while (e>0) { eb[en++]='0'+(e%10); e/=10; }
        if (en<2) eb[en++]='0';
        while (en>0) *o++=eb[--en];
        *o=0;
    } else {
        int dp=exp10+1;
        if (dp<=0) { *o++='0'; *o++='.'; for (int i=0;i<-dp;i++) *o++='0'; for (int i=0;i<nd;i++) *o++=digits[i]; }
        else if (dp>=nd) { for (int i=0;i<nd;i++) *o++=digits[i]; for (int i=nd;i<dp;i++) *o++='0'; }
        else { for (int i=0;i<dp;i++) *o++=digits[i]; *o++='.'; for (int i=dp;i<nd;i++) *o++=digits[i]; }
        *o=0;
    }
}

/* ---- display ----------------------------------------------------------- */
static void k_disp(KBuf *b, KValue v);

static void k_disp(KBuf *b, KValue v) {
    char num[64];
    switch (v.tag) {
    case K_NIL: kb_puts(b,"nil"); break;
    case K_INT: snprintf(num,sizeof(num),"%lld",v.u.i); kb_puts(b,num); break;
    case K_UINT: snprintf(num,sizeof(num),"%llu",v.u.u64); kb_puts(b,num); break;
    case K_FLOAT: k_fmt_float(v.u.f,num,sizeof(num)); kb_puts(b,num); break;
    case K_BOOL: kb_puts(b, v.u.b?"true":"false"); break;
    case K_STRING: case K_JSON: kb_putn(b,v.u.s.data,v.u.s.len); break;
    case K_JSON_NUMBER: kb_putn(b,v.u.s.data,v.u.s.len); break;
    case K_BYTES: snprintf(num,sizeof(num),"<Bytes:%zu>",v.u.s.len); kb_puts(b,num); break;
    case K_ARRAY:
        kb_putc(b,'[');
        for (size_t i=0;i<v.u.a.len;i++) { if (i) kb_puts(b,", "); k_disp(b,v.u.a.items[i]); }
        kb_putc(b,']'); break;
    case K_SET:
        kb_puts(b,"|{");
        for (size_t i=0;i<v.u.a.len;i++) { if (i) kb_puts(b,", "); k_disp(b,v.u.a.items[i]); }
        kb_puts(b,"}|"); break;
    case K_MAP:
        kb_putc(b,'{');
        for (size_t i=0;i<v.u.m.len;i++) { if (i) kb_puts(b,", "); k_disp(b,v.u.m.keys[i]); kb_puts(b,": "); k_disp(b,v.u.m.vals[i]); }
        kb_putc(b,'}'); break;
    case K_STRUCT: {
        KStructDesc *d = &k_structs[v.u.st.type_id];
        kb_puts(b,d->name); kb_putc(b,'{');
        for (int i=0;i<d->nfields;i++) { if (i) kb_puts(b,", "); kb_puts(b,d->fields[i]); kb_puts(b,": "); k_disp(b,v.u.st.fields[i]); }
        kb_putc(b,'}'); break;
    }
    case K_ENUM: {
        KEnumDesc *d = &k_enums[v.u.en.type_id];
        kb_puts(b,d->name); kb_puts(b,"::"); kb_puts(b,d->variants[v.u.en.variant]); break;
    }
    case K_OPTION:
        if (!v.u.opt.present) kb_puts(b,"none");
        else { kb_puts(b,"some("); k_disp(b,*v.u.opt.inner); kb_putc(b,')'); }
        break;
    case K_RESULT:
        if (v.u.res.ok) { kb_puts(b,"ok("); k_disp(b,*v.u.res.inner); kb_putc(b,')'); }
        else { kb_puts(b,"err("); k_disp(b,*v.u.res.inner); kb_putc(b,')'); }
        break;
    case K_SHARED: kb_puts(b,"<Shared>"); break;
    case K_ACTOR: kb_puts(b,"<Actor>"); break;
    case K_THREAD: kb_puts(b,"<Thread>"); break;
    case K_TASKGROUP: kb_puts(b,"<TaskGroup>"); break;
    case K_CHANNEL: kb_puts(b,"<Channel>"); break;
    case K_TCP: kb_puts(b,"<TcpSocket>"); break;
    default: kb_puts(b,"<invalid>"); break;
    }
}

static KValue k_display(KValue v) {
    KBuf b; kb_init(&b); k_disp(&b,v);
    return kv_str_take(b.buf,b.len);
}

/* ---- equality ---------------------------------------------------------- */
static int k_equal(KValue a, KValue b) {
    if (a.tag != b.tag) return 0;
    switch (a.tag) {
    case K_NIL: return 1;
    case K_INT: return a.u.i==b.u.i;
    case K_UINT: return a.u.u64==b.u.u64;
    case K_FLOAT: return a.u.f==b.u.f;
    case K_BOOL: return a.u.b==b.u.b;
    case K_STRING: case K_JSON: case K_JSON_NUMBER: case K_BYTES:
        return a.u.s.len==b.u.s.len && memcmp(a.u.s.data,b.u.s.data,a.u.s.len)==0;
    case K_ARRAY: case K_SET:
        if (a.u.a.len!=b.u.a.len) return 0;
        for (size_t i=0;i<a.u.a.len;i++) if (!k_equal(a.u.a.items[i],b.u.a.items[i])) return 0;
        return 1;
    case K_MAP:
        if (a.u.m.len!=b.u.m.len) return 0;
        for (size_t i=0;i<a.u.m.len;i++)
            if (!k_equal(a.u.m.keys[i],b.u.m.keys[i]) || !k_equal(a.u.m.vals[i],b.u.m.vals[i])) return 0;
        return 1;
    case K_STRUCT:
        if (a.u.st.type_id!=b.u.st.type_id) return 0;
        { KStructDesc *d=&k_structs[a.u.st.type_id];
          for (int i=0;i<d->nfields;i++) if (!k_equal(a.u.st.fields[i],b.u.st.fields[i])) return 0; }
        return 1;
    case K_ENUM: return a.u.en.type_id==b.u.en.type_id && a.u.en.variant==b.u.en.variant;
    case K_OPTION:
        if (a.u.opt.present!=b.u.opt.present) return 0;
        return !a.u.opt.present || k_equal(*a.u.opt.inner,*b.u.opt.inner);
    case K_RESULT:
        return a.u.res.ok==b.u.res.ok && k_equal(*a.u.res.inner,*b.u.res.inner);
    case K_SHARED: return a.u.sh.cell==b.u.sh.cell;
    case K_TCP: return a.u.tcp.socket==b.u.tcp.socket;
    case K_ACTOR: case K_CHANNEL: return a.u.ac.ch==b.u.ac.ch;
    case K_THREAD: case K_TASKGROUP: return a.u.hd.h==b.u.hd.h;
    }
    return 0;
}

/* ---- checked arithmetic ------------------------------------------------ */
static long long k_add_i(long long a, long long b) {
    if ((b>0 && a>LLONG_MAX-b) || (b<0 && a<LLONG_MIN-b)) kfail("checked integer arithmetic overflow");
    return a+b;
}
static long long k_sub_i(long long a, long long b) {
    if ((b<0 && a>LLONG_MAX+b) || (b>0 && a<LLONG_MIN+b)) kfail("checked integer arithmetic overflow");
    return a-b;
}
static long long k_mul_i(long long a, long long b) {
    long long z;
    if (__builtin_mul_overflow(a,b,&z)) kfail("checked integer arithmetic overflow");
    return z;
}
static long long k_div_i(long long a, long long b) {
    if (b==0) kfail("division by zero");
    if (a==LLONG_MIN&&b==-1) kfail("checked integer arithmetic overflow");
    return a/b;
}
static long long k_rem_i(long long a, long long b) {
    if (b==0) kfail("remainder by zero");
    if (a==LLONG_MIN&&b==-1) kfail("checked integer arithmetic overflow");
    return a%b;
}

static KValue k_add(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_add_i(a.u.i,b.u.i));
    if (a.tag==K_UINT && b.tag==K_UINT) return kv_uint64(a.u.u64+b.u.u64);
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { double z=a.u.f+b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    if (a.tag==K_STRING && b.tag==K_STRING) {
        char *p=(char*)kalloc(a.u.s.len+b.u.s.len+1);
        memcpy(p,a.u.s.data,a.u.s.len); memcpy(p+a.u.s.len,b.u.s.data,b.u.s.len); p[a.u.s.len+b.u.s.len]=0;
        return kv_strn(p,a.u.s.len+b.u.s.len);
    }
    if (a.tag==K_BYTES && b.tag==K_BYTES) {
        char *p=(char*)kalloc(a.u.s.len+b.u.s.len+1);
        memcpy(p,a.u.s.data,a.u.s.len); memcpy(p+a.u.s.len,b.u.s.data,b.u.s.len); p[a.u.s.len+b.u.s.len]=0;
        return kv_bytesn(p,a.u.s.len+b.u.s.len);
    }
    if (a.tag==K_ARRAY && b.tag==K_ARRAY) {
        size_t n=a.u.a.len+b.u.a.len;
        KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
        memcpy(p,a.u.a.items,sizeof(KValue)*a.u.a.len);
        memcpy(p+a.u.a.len,b.u.a.items,sizeof(KValue)*b.u.a.len);
        return kv_arr(p,n);
    }
    kfail("operator operands have incompatible types");
    return kv_nil();
}
static KValue k_sub(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_sub_i(a.u.i,b.u.i));
    if (a.tag==K_UINT && b.tag==K_UINT) return kv_uint64(a.u.u64-b.u.u64);
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { double z=a.u.f-b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_mul(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_mul_i(a.u.i,b.u.i));
    if (a.tag==K_UINT && b.tag==K_UINT) return kv_uint64(a.u.u64*b.u.u64);
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { double z=a.u.f*b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_div(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_div_i(a.u.i,b.u.i));
    if (a.tag==K_UINT && b.tag==K_UINT) { if (b.u.u64==0) kfail("division by zero"); return kv_uint64(a.u.u64/b.u.u64); }
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { if (b.u.f==0) kfail("floating division by zero"); double z=a.u.f/b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_rem(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_rem_i(a.u.i,b.u.i));
    if (a.tag==K_UINT && b.tag==K_UINT) { if (b.u.u64==0) kfail("remainder by zero"); return kv_uint64(a.u.u64%b.u.u64); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_neg(KValue a) {
    if (a.tag==K_INT) { if (a.u.i==LLONG_MIN) kfail("negation overflow"); return kv_int(-a.u.i); }
    if (a.tag==K_FLOAT) { double z=-a.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("unary sign expects numeric value"); return kv_nil();
}
static KValue k_not(KValue a) { if (a.tag!=K_BOOL) kfail("'!' expects Bool"); return kv_bool(!a.u.b); }
static KValue k_lt(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i<b.u.i);
    if (a.tag==K_UINT&&b.tag==K_UINT) return kv_bool(a.u.u64<b.u.u64);
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f<b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_le(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i<=b.u.i);
    if (a.tag==K_UINT&&b.tag==K_UINT) return kv_bool(a.u.u64<=b.u.u64);
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f<=b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_gt(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i>b.u.i);
    if (a.tag==K_UINT&&b.tag==K_UINT) return kv_bool(a.u.u64>b.u.u64);
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f>b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_ge(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i>=b.u.i);
    if (a.tag==K_UINT&&b.tag==K_UINT) return kv_bool(a.u.u64>=b.u.u64);
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f>=b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_eq(KValue a, KValue b) { return kv_bool(k_equal(a,b)); }
static KValue k_neq(KValue a, KValue b) { return kv_bool(!k_equal(a,b)); }
static KValue k_and(KValue a, KValue b) { if(a.tag!=K_BOOL||b.tag!=K_BOOL) kfail("logical operators require Bool"); return kv_bool(a.u.b&&b.u.b); }
static KValue k_or(KValue a, KValue b) { if(a.tag!=K_BOOL||b.tag!=K_BOOL) kfail("logical operators require Bool"); return kv_bool(a.u.b||b.u.b); }

/* ---- output ------------------------------------------------------------ */
static void k_print(KValue v, int newline) {
    KValue s = k_display(v);
    if (s.u.s.len>(size_t)LLONG_MAX) kfail("output limit exceeded");
    long long bytes=(long long)s.u.s.len+(newline?1:0);
    if (bytes>k_max_out || k_out>k_max_out-bytes) kfail("output limit exceeded");
    if (fwrite(s.u.s.data,1,s.u.s.len,stdout)!=s.u.s.len) kfail("stream failure");
    if (newline && fputc('\n',stdout)==EOF) kfail("stream failure");
    if (fflush(stdout)!=0) kfail("stream failure");
    k_out += bytes;
}
`

// cRuntimeBuiltins holds the pure builtin implementations.
const cRuntimeBuiltins = `
/* ---- UTF-8 helpers ----------------------------------------------------- */
static size_t k_utf8_count(const char *s, size_t len) {
    size_t c=0;
    for (size_t i=0;i<len;) {
        unsigned char ch=(unsigned char)s[i];
        if (ch<0x80) i+=1; else if ((ch>>5)==0x6) i+=2; else if ((ch>>4)==0xe) i+=3; else if ((ch>>3)==0x1e) i+=4; else i+=1;
        c++;
    }
    return c;
}
static size_t k_utf8_off(const char *s, size_t len, size_t idx) {
    size_t c=0,i=0;
    while (i<len && c<idx) {
        unsigned char ch=(unsigned char)s[i];
        if (ch<0x80) i+=1; else if ((ch>>5)==0x6) i+=2; else if ((ch>>4)==0xe) i+=3; else if ((ch>>3)==0x1e) i+=4; else i+=1;
        c++;
    }
    return i;
}
static int k_utf8_valid(const char *s, size_t len) {
    size_t i=0;
    while (i<len) {
        unsigned char ch=(unsigned char)s[i];
        size_t n;
        if (ch<0x80) n=1; else if ((ch>>5)==0x6) n=2; else if ((ch>>4)==0xe) n=3; else if ((ch>>3)==0x1e) n=4; else return 0;
        if (i+n>len) return 0;
        for (size_t j=1;j<n;j++) if (((unsigned char)s[i+j]>>6)!=0x2) return 0;
        i+=n;
    }
    return 1;
}

/* ---- conversions ------------------------------------------------------- */
static KValue k_to_int(KValue v) {
    if (v.tag==K_INT) return v;
    if (v.tag==K_UINT) {
        if (v.u.u64>(unsigned long long)LLONG_MAX) kfail("UInt is outside Int range");
        return kv_int((long long)v.u.u64);
    }
    if (v.tag==K_BOOL) return kv_int(v.u.b?1:0);
    if (v.tag==K_FLOAT) {
        if (!isfinite(v.u.f) || v.u.f < (double)LLONG_MIN || v.u.f > (double)LLONG_MAX) kfail("float is outside Int range");
        return kv_int((long long)v.u.f);
    }
    if (v.tag==K_STRING) {
        char *tmp=(char*)kalloc(v.u.s.len+1); memcpy(tmp,v.u.s.data,v.u.s.len); tmp[v.u.s.len]=0;
        char *end=NULL; long long n=strtoll(tmp,&end,10);
        if (end==tmp || *end!=0) kfail("String must be a complete decimal String");
        return kv_int(n);
    }
    kfail("int conversion is unsupported"); return kv_nil();
}
static KValue k_to_uint(KValue v, int bits) {
    unsigned long long n;
    if (v.tag==K_UINT) n=v.u.u64;
    else if (v.tag==K_INT) {
        if (v.u.i<0) kfail("Int is outside unsigned range");
        n=(unsigned long long)v.u.i;
    } else { kfail("unsigned conversion is unsupported"); return kv_nil(); }
    if (bits<64 && n>=1ULL<<bits) kfail("value is outside unsigned range");
    return kv_uint64(n);
}
static KValue k_to_float(KValue v) {
    if (v.tag==K_FLOAT) { if(!isfinite(v.u.f)) kfail("Float must be finite"); return v; }
    if (v.tag==K_INT) { double z=(double)v.u.i; if(!isfinite(z)) kfail("conversion produced non-finite Float"); return kv_float(z); }
    if (v.tag==K_STRING) {
        char *tmp=(char*)kalloc(v.u.s.len+1); memcpy(tmp,v.u.s.data,v.u.s.len); tmp[v.u.s.len]=0;
        char *end=NULL; double z=strtod(tmp,&end);
        if (end==tmp || *end!=0 || !isfinite(z)) kfail("invalid complete Float string");
        return kv_float(z);
    }
    kfail("float conversion is unsupported"); return kv_nil();
}
static int k_to_bool(KValue v) {
    switch (v.tag) {
    case K_NIL: return 0;
    case K_BOOL: return v.u.b;
    case K_INT: return v.u.i!=0;
    case K_UINT: return v.u.u64!=0;
    case K_FLOAT: return v.u.f!=0 && !isnan(v.u.f);
    case K_STRING: case K_BYTES: return v.u.s.len>0;
    case K_ARRAY: case K_SET: return v.u.a.len>0;
    case K_MAP: return v.u.m.len>0;
    case K_OPTION: return v.u.opt.present;
    case K_RESULT: return v.u.res.ok;
    }
    return 1;
}

/* ---- pure builtins ----------------------------------------------------- */
static KValue k_len(KValue v) {
    switch (v.tag) {
    case K_STRING: return kv_int((long long)k_utf8_count(v.u.s.data,v.u.s.len));
    case K_BYTES: return kv_int((long long)v.u.s.len);
    case K_ARRAY: case K_SET: return kv_int((long long)v.u.a.len);
    case K_MAP: return kv_int((long long)v.u.m.len);
    }
    kfail("len expects String, Array, or Bytes"); return kv_nil();
}
static KValue k_bytes(KValue v) {
    if (v.tag!=K_ARRAY) kfail("bytes expects Array[Int]");
    char *p=(char*)kalloc(v.u.a.len+1);
    for (size_t i=0;i<v.u.a.len;i++) {
        KValue x=v.u.a.items[i];
        if (x.tag!=K_INT || x.u.i<0 || x.u.i>255) kfail("bytes element must be in range 0..255");
        p[i]=(char)x.u.i;
    }
    return kv_bytesn(p,v.u.a.len);
}
static KValue k_string_to_bytes(KValue v) { return kv_bytesn(v.u.s.data,v.u.s.len); }
static KValue k_bytes_to_string(KValue v) {
    if (!k_utf8_valid(v.u.s.data,v.u.s.len)) kfail("bytes are not valid UTF-8");
    return kv_strn(v.u.s.data,v.u.s.len);
}
static KValue k_array_push(KValue a, KValue x) {
    if (a.tag!=K_ARRAY) kfail("array_push expects Array[T]");
    size_t n=a.u.a.len+1;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*n);
    memcpy(p,a.u.a.items,sizeof(KValue)*a.u.a.len); p[a.u.a.len]=x;
    return kv_arr(p,n);
}
static KValue k_array_pop(KValue a) {
    if (a.tag!=K_ARRAY) kfail("array_pop expects Array[T]");
    if (a.u.a.len==0) return kv_opt(0,kv_nil());
    return kv_opt(1,a.u.a.items[a.u.a.len-1]);
}
static KValue k_array_get(KValue a, KValue i) {
    if (a.tag!=K_ARRAY) kfail("array_get expects Array[T]");
    if (i.u.i<0 || i.u.i>=(long long)a.u.a.len) return kv_opt(0,kv_nil());
    return kv_opt(1,a.u.a.items[i.u.i]);
}
static KValue k_array_concat(KValue a, KValue b) {
    if (a.tag!=K_ARRAY||b.tag!=K_ARRAY) kfail("array_concat expects Array[T]");
    size_t n=a.u.a.len+b.u.a.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    memcpy(p,a.u.a.items,sizeof(KValue)*a.u.a.len);
    memcpy(p+a.u.a.len,b.u.a.items,sizeof(KValue)*b.u.a.len);
    return kv_arr(p,n);
}
static KValue k_array_contains(KValue a, KValue x) {
    if (a.tag!=K_ARRAY) kfail("array_contains expects Array[T]");
    for (size_t i=0;i<a.u.a.len;i++) if (k_equal(a.u.a.items[i],x)) return kv_bool(1);
    return kv_bool(0);
}
static KValue k_array_slice(KValue a, KValue s, KValue n) {
    if (a.tag!=K_ARRAY) kfail("array_slice expects Array[T]");
    long long start=s.u.i, cnt=n.u.i;
    if (start<0 || cnt<0 || start>(long long)a.u.a.len || cnt>(long long)a.u.a.len-start) kfail("array slice range is out of bounds");
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(cnt?cnt:1));
    memcpy(p,a.u.a.items+start,sizeof(KValue)*cnt);
    return kv_arr(p,(size_t)cnt);
}
static KValue k_array_reverse(KValue a) {
    if (a.tag!=K_ARRAY) kfail("array_reverse expects Array[T]");
    size_t n=a.u.a.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) p[i]=a.u.a.items[n-1-i];
    return kv_arr(p,n);
}
static KValue k_array_join(KValue a, KValue sep) {
    if (a.tag!=K_ARRAY) kfail("array_join expects Array[String]");
    KBuf b; kb_init(&b);
    for (size_t i=0;i<a.u.a.len;i++) {
        if (i) kb_putn(&b,sep.u.s.data,sep.u.s.len);
        kb_putn(&b,a.u.a.items[i].u.s.data,a.u.a.items[i].u.s.len);
    }
    return kv_strn(b.buf,b.len);
}
static KValue k_abs(KValue v) {
    if (v.tag==K_INT) { if (v.u.i==LLONG_MIN) kfail("minimum Int cannot be represented by abs"); return kv_int(v.u.i<0?-v.u.i:v.u.i); }
    if (v.tag==K_FLOAT) { double z=fabs(v.u.f); if(!isfinite(z)) kfail("absolute value must be finite"); return kv_float(z); }
    kfail("abs expects Int or Float"); return kv_nil();
}
static KValue k_sqrt(KValue v) {
    double z = v.tag==K_INT?(double)v.u.i:v.u.f;
    if (z<0 || !isfinite(z)) kfail("sqrt domain error");
    double q=sqrt(z); if(!isfinite(q)) kfail("sqrt result must be finite");
    return kv_float(q);
}
static KValue k_min(KValue a, KValue b) {
    if (a.tag==K_INT) return kv_int(a.u.i<b.u.i?a.u.i:b.u.i);
    return kv_float(a.u.f<b.u.f?a.u.f:b.u.f);
}
static KValue k_max(KValue a, KValue b) {
    if (a.tag==K_INT) return kv_int(a.u.i>b.u.i?a.u.i:b.u.i);
    return kv_float(a.u.f>b.u.f?a.u.f:b.u.f);
}
static KValue k_floor(KValue v) { double z=floor(v.u.f); if(z<(double)LLONG_MIN||z>(double)LLONG_MAX) kfail("rounded value is outside Int range"); return kv_int((long long)z); }
static KValue k_ceil(KValue v) { double z=ceil(v.u.f); if(z<(double)LLONG_MIN||z>(double)LLONG_MAX) kfail("rounded value is outside Int range"); return kv_int((long long)z); }
static KValue k_round(KValue v) { double z=round(v.u.f); if(z<(double)LLONG_MIN||z>(double)LLONG_MAX) kfail("rounded value is outside Int range"); return kv_int((long long)z); }
static KValue k_pow(KValue a, KValue b) { double z=pow(a.u.f,b.u.f); if(!isfinite(z)) kfail("math result must be finite"); return kv_float(z); }
static KValue k_log(KValue v) { if(v.u.f<=0) kfail("log domain error"); double z=log(v.u.f); if(!isfinite(z)) kfail("math result must be finite"); return kv_float(z); }
static KValue k_sin(KValue v) { double z=sin(v.u.f); if(!isfinite(z)) kfail("math result must be finite"); return kv_float(z); }
static KValue k_cos(KValue v) { double z=cos(v.u.f); if(!isfinite(z)) kfail("math result must be finite"); return kv_float(z); }
static KValue k_is_nan(KValue v) { return kv_bool(isnan(v.u.f)); }
static KValue k_is_finite(KValue v) { return kv_bool(isfinite(v.u.f)); }
static KValue k_is_some(KValue v) { return kv_bool(v.tag==K_OPTION && v.u.opt.present); }
static KValue k_is_none(KValue v) { return kv_bool(v.tag==K_OPTION && !v.u.opt.present); }
static KValue k_is_ok(KValue v) { return kv_bool(v.tag==K_RESULT && v.u.res.ok); }
static KValue k_is_err(KValue v) { return kv_bool(v.tag==K_RESULT && !v.u.res.ok); }
static KValue k_unwrap_or(KValue v, KValue d) { if (v.tag==K_OPTION && v.u.opt.present) return *v.u.opt.inner; return d; }
static KValue k_some(KValue v) { return kv_opt(1,v); }
static KValue k_none(void) { return kv_opt(0,kv_nil()); }
static KValue k_ok(KValue v) { return kv_res(1,v); }
static KValue k_err(KValue v) { return kv_res(0,v); }
static KValue k_substring(KValue v, KValue s, KValue n) {
    size_t total=k_utf8_count(v.u.s.data,v.u.s.len);
    long long start=s.u.i, cnt=n.u.i;
    if (start<0 || cnt<0 || start>(long long)total || cnt>(long long)total-start) return kv_res(0,kv_cstr("substring range is out of bounds"));
    size_t b0=k_utf8_off(v.u.s.data,v.u.s.len,(size_t)start);
    size_t b1=k_utf8_off(v.u.s.data,v.u.s.len,(size_t)(start+cnt));
    return kv_res(1,kv_strn(v.u.s.data+b0,b1-b0));
}
static KValue k_contains(KValue v, KValue n) {
    if (n.u.s.len==0) return kv_bool(1);
    if (n.u.s.len>v.u.s.len) return kv_bool(0);
    for (size_t i=0;i+n.u.s.len<=v.u.s.len;i++) if (memcmp(v.u.s.data+i,n.u.s.data,n.u.s.len)==0) return kv_bool(1);
    return kv_bool(0);
}
static KValue k_starts_with(KValue v, KValue p) {
    if (p.u.s.len>v.u.s.len) return kv_bool(0);
    return kv_bool(memcmp(v.u.s.data,p.u.s.data,p.u.s.len)==0);
}
static KValue k_ends_with(KValue v, KValue p) {
    if (p.u.s.len>v.u.s.len) return kv_bool(0);
    return kv_bool(memcmp(v.u.s.data+v.u.s.len-p.u.s.len,p.u.s.data,p.u.s.len)==0);
}
static int k_is_space(unsigned char c) { return c==' '||c=='\t'||c=='\n'||c=='\r'||c=='\v'||c=='\f'; }
static KValue k_trim(KValue v) {
    size_t a=0,b=v.u.s.len;
    while (a<b && k_is_space((unsigned char)v.u.s.data[a])) a++;
    while (b>a && k_is_space((unsigned char)v.u.s.data[b-1])) b--;
    return kv_strn(v.u.s.data+a,b-a);
}
static KValue k_split(KValue v, KValue sep) {
    if (sep.u.s.len==0) {
        size_t n=k_utf8_count(v.u.s.data,v.u.s.len);
        KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
        size_t c=0,i=0;
        while (i<v.u.s.len) {
            unsigned char ch=(unsigned char)v.u.s.data[i]; size_t w;
            if (ch<0x80) w=1; else if ((ch>>5)==0x6) w=2; else if ((ch>>4)==0xe) w=3; else if ((ch>>3)==0x1e) w=4; else w=1;
            p[c++]=kv_strn(v.u.s.data+i,w); i+=w;
        }
        return kv_arr(p,c);
    }
    size_t cap=8,cnt=0; KValue *p=(KValue*)kalloc(sizeof(KValue)*cap);
    size_t start=0,i=0;
    while (i+sep.u.s.len<=v.u.s.len) {
        if (memcmp(v.u.s.data+i,sep.u.s.data,sep.u.s.len)==0) {
            if (cnt==cap) { cap*=2; KValue *q=(KValue*)kalloc(sizeof(KValue)*cap); memcpy(q,p,sizeof(KValue)*cnt); p=q; }
            p[cnt++]=kv_strn(v.u.s.data+start,i-start); i+=sep.u.s.len; start=i;
        } else i++;
    }
    if (cnt==cap) { cap*=2; KValue *q=(KValue*)kalloc(sizeof(KValue)*cap); memcpy(q,p,sizeof(KValue)*cnt); p=q; }
    p[cnt++]=kv_strn(v.u.s.data+start,v.u.s.len-start);
    return kv_arr(p,cnt);
}
static KValue k_replace(KValue v, KValue from, KValue to) {
    if (from.u.s.len==0) return v;
    KBuf b; kb_init(&b);
    size_t i=0;
    while (i<v.u.s.len) {
        if (i+from.u.s.len<=v.u.s.len && memcmp(v.u.s.data+i,from.u.s.data,from.u.s.len)==0) {
            kb_putn(&b,to.u.s.data,to.u.s.len); i+=from.u.s.len;
        } else { kb_putc(&b,v.u.s.data[i]); i++; }
    }
    return kv_strn(b.buf,b.len);
}
static KValue k_codepoints(KValue v) {
    size_t n=k_utf8_count(v.u.s.data,v.u.s.len);
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    size_t c=0,i=0;
    while (i<v.u.s.len) {
        unsigned char ch=(unsigned char)v.u.s.data[i]; long long cp; size_t w;
        if (ch<0x80) { cp=ch; w=1; }
        else if ((ch>>5)==0x6) { cp=((ch&0x1f)<<6)|((unsigned char)v.u.s.data[i+1]&0x3f); w=2; }
        else if ((ch>>4)==0xe) { cp=((ch&0x0f)<<12)|(((unsigned char)v.u.s.data[i+1]&0x3f)<<6)|((unsigned char)v.u.s.data[i+2]&0x3f); w=3; }
        else { cp=((ch&0x07)<<18)|(((unsigned char)v.u.s.data[i+1]&0x3f)<<12)|(((unsigned char)v.u.s.data[i+2]&0x3f)<<6)|((unsigned char)v.u.s.data[i+3]&0x3f); w=4; }
        p[c++]=kv_int(cp); i+=w;
    }
    return kv_arr(p,c);
}
static KValue k_byte_at(KValue v, KValue idx) {
    long long i=idx.u.i;
    if (i<0 || i>=(long long)v.u.s.len) return kv_res(0,kv_cstr("index out of range"));
    return kv_res(1,kv_int((unsigned char)v.u.s.data[i]));
}
static const char k_hexd[]="0123456789abcdef";
static KValue k_hex_encode(KValue v) {
    char *p=(char*)kalloc(v.u.s.len*2+1);
    for (size_t i=0;i<v.u.s.len;i++) { p[i*2]=k_hexd[(unsigned char)v.u.s.data[i]>>4]; p[i*2+1]=k_hexd[(unsigned char)v.u.s.data[i]&0xf]; }
    p[v.u.s.len*2]=0;
    return kv_strn(p,v.u.s.len*2);
}
static int k_hexval(char c) { if(c>='0'&&c<='9')return c-'0'; if(c>='a'&&c<='f')return c-'a'+10; if(c>='A'&&c<='F')return c-'A'+10; return -1; }
static KValue k_hex_decode(KValue v) {
    if (v.u.s.len%2!=0) return kv_res(0,kv_cstr("invalid hex"));
    size_t n=v.u.s.len/2; char *p=(char*)kalloc(n+1);
    for (size_t i=0;i<n;i++) {
        int hi=k_hexval(v.u.s.data[i*2]), lo=k_hexval(v.u.s.data[i*2+1]);
        if (hi<0||lo<0) return kv_res(0,kv_cstr("invalid hex"));
        p[i]=(char)((hi<<4)|lo);
    }
    return kv_res(1,kv_bytesn(p,n));
}
static const char k_b64[]="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
static KValue k_base64_encode(KValue v) {
    size_t n=v.u.s.len; size_t olen=((n+2)/3)*4;
    char *p=(char*)kalloc(olen+1);
    size_t j=0;
    for (size_t i=0;i<n;i+=3) {
        unsigned int b0=(unsigned char)v.u.s.data[i];
        unsigned int b1=i+1<n?(unsigned char)v.u.s.data[i+1]:0;
        unsigned int b2=i+2<n?(unsigned char)v.u.s.data[i+2]:0;
        unsigned int t=(b0<<16)|(b1<<8)|b2;
        p[j++]=k_b64[(t>>18)&0x3f]; p[j++]=k_b64[(t>>12)&0x3f];
        p[j++]=i+1<n?k_b64[(t>>6)&0x3f]:'='; p[j++]=i+2<n?k_b64[t&0x3f]:'=';
    }
    p[j]=0;
    return kv_strn(p,j);
}
static KValue k_base64_decode(KValue v) {
    size_t n=v.u.s.len;
    if (n%4!=0) return kv_res(0,kv_cstr("invalid base64"));
    size_t olen=(n/4)*3; char *p=(char*)kalloc(olen+1); size_t j=0;
    for (size_t i=0;i<n;i+=4) {
        int q[4];
        for (int k=0;k<4;k++) {
            char c=v.u.s.data[i+k];
            if (c=='=') { q[k]=-2; continue; }
            const char *f=strchr(k_b64,c);
            if (!f) return kv_res(0,kv_cstr("invalid base64"));
            q[k]=(int)(f-k_b64);
        }
        unsigned int t=((unsigned)q[0]<<18)|((unsigned)q[1]<<12)|((q[2]<0?0:(unsigned)q[2])<<6)|(q[3]<0?0:(unsigned)q[3]);
        p[j++]=(char)((t>>16)&0xff);
        if (q[2]>=0) p[j++]=(char)((t>>8)&0xff);
        if (q[3]>=0) p[j++]=(char)(t&0xff);
    }
    return kv_res(1,kv_bytesn(p,j));
}
static KValue k_map_get(KValue m, KValue key) {
    if (m.tag!=K_MAP) kfail("map_get expects Map[K,V]");
    for (size_t i=0;i<m.u.m.len;i++) if (k_equal(m.u.m.keys[i],key)) return kv_opt(1,m.u.m.vals[i]);
    return kv_opt(0,kv_nil());
}
static KValue k_map_insert(KValue m, KValue key, KValue val) {
    if (m.tag!=K_MAP) kfail("map_insert expects Map[K,V]");
    size_t n=m.u.m.len;
    KValue *keys=(KValue*)kalloc(sizeof(KValue)*(n+1));
    KValue *vals=(KValue*)kalloc(sizeof(KValue)*(n+1));
    int found=0;
    for (size_t i=0;i<n;i++) { keys[i]=m.u.m.keys[i]; vals[i]=m.u.m.vals[i]; if (k_equal(keys[i],key)) { vals[i]=val; found=1; } }
    if (!found) { keys[n]=key; vals[n]=val; n++; }
    return kv_map(keys,vals,n);
}
static KValue k_map_keys(KValue m) {
    if (m.tag!=K_MAP) kfail("map_keys expects Map[K,V]");
    size_t n=m.u.m.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) p[i]=m.u.m.keys[i];
    return kv_arr(p,n);
}
static KValue k_set_contains(KValue s, KValue x) {
    if (s.tag!=K_SET) kfail("set_contains expects Set[T]");
    for (size_t i=0;i<s.u.a.len;i++) if (k_equal(s.u.a.items[i],x)) return kv_bool(1);
    return kv_bool(0);
}
static KValue k_set_insert(KValue s, KValue x) {
    if (s.tag!=K_SET) kfail("set_insert expects Set[T]");
    for (size_t i=0;i<s.u.a.len;i++) if (k_equal(s.u.a.items[i],x)) return s;
    size_t n=s.u.a.len+1;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*n);
    memcpy(p,s.u.a.items,sizeof(KValue)*s.u.a.len); p[s.u.a.len]=x;
    return kv_set(p,n);
}
static KValue k_set_len(KValue s) { if(s.tag!=K_SET) kfail("set_len expects Set[T]"); return kv_int((long long)s.u.a.len); }
static KValue k_assert(KValue c) { if (c.tag!=K_BOOL) kfail("assert expects Bool"); if (!c.u.b) kfail("assertion failed"); return kv_nil(); }
static KValue k_assert_eq(KValue a, KValue b) { if (!k_equal(a,b)) kfail("assertion failed: values are not equal"); return kv_nil(); }
static KValue k_str(KValue v) { return k_display(v); }
static KValue k_bool(KValue v) { return kv_bool(k_to_bool(v)); }
`

// cRuntimeExtra holds indexing, field access, iteration, and the small set of
// host builtins (filesystem and environment) that the native backend supports.
const cRuntimeExtra = `
/* ---- indexing ---------------------------------------------------------- */
static KValue k_index(KValue base, KValue ix) {
    if (base.tag==K_MAP) {
        for (size_t i=0;i<base.u.m.len;i++) if (k_equal(base.u.m.keys[i],ix)) return base.u.m.vals[i];
        kfail("map key not found");
    }
    if (ix.tag!=K_INT || ix.u.i<0) kfail("index must be a non-negative Int");
    long long i=ix.u.i;
    if (base.tag==K_ARRAY) {
        if (i>=(long long)base.u.a.len) kfail("array index out of range");
        return base.u.a.items[i];
    }
    if (base.tag==K_STRING) {
        size_t total=k_utf8_count(base.u.s.data,base.u.s.len);
        if (i>=(long long)total) kfail("string index out of range");
        size_t b0=k_utf8_off(base.u.s.data,base.u.s.len,(size_t)i);
        size_t b1=k_utf8_off(base.u.s.data,base.u.s.len,(size_t)i+1);
        return kv_strn(base.u.s.data+b0,b1-b0);
    }
    if (base.tag==K_BYTES) {
        if (i>=(long long)base.u.s.len) kfail("byte index out of range");
        return kv_int((unsigned char)base.u.s.data[i]);
    }
    kfail("indexing expects String, Bytes, Array, or Map");
    return kv_nil();
}

/* ---- field access ------------------------------------------------------ */
static KValue k_field(KValue base, int idx) {
    if (base.tag!=K_STRUCT) kfail("field access expects a struct");
    return base.u.st.fields[idx];
}

/* ---- iteration --------------------------------------------------------- */
static KValue k_iter_items(KValue v) {
    if (v.tag==K_ARRAY || v.tag==K_SET) return v;
    if (v.tag==K_STRING) {
        size_t n=k_utf8_count(v.u.s.data,v.u.s.len);
        KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
        size_t c=0,i=0;
        while (i<v.u.s.len) {
            unsigned char ch=(unsigned char)v.u.s.data[i]; size_t w;
            if (ch<0x80) w=1; else if ((ch>>5)==0x6) w=2; else if ((ch>>4)==0xe) w=3; else if ((ch>>3)==0x1e) w=4; else w=1;
            p[c++]=kv_strn(v.u.s.data+i,w); i+=w;
        }
        return kv_arr(p,c);
    }
    if (v.tag==K_BYTES) {
        size_t n=v.u.s.len;
        KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
        for (size_t i=0;i<n;i++) p[i]=kv_int((unsigned char)v.u.s.data[i]);
        return kv_arr(p,n);
    }
    kfail("for expects Array, Set, String, or Bytes");
    return kv_nil();
}

/* ---- host builtins: filesystem and environment ------------------------- */
static KValue k_fs_create_dir_all(KValue path);
static KValue k_fs_parent_dir(KValue path);
static int k_fs_path_has_nul(KValue path) {
    return memchr(path.u.s.data,0,path.u.s.len)!=NULL;
}
static KValue k_fs_path_nul_result(void) {
    return kv_res(0,kv_cstr("path contains NUL"));
}
static KValue k_fs_path_error(const char *op, KValue path) {
    int code=errno;
    const char *reason=strerror(code);
    size_t op_len=strlen(op), reason_len=strlen(reason);
    size_t cap=op_len+path.u.s.len+reason_len+4;
    char *message=(char*)kalloc(cap);
    snprintf(message,cap,"%s %.*s: %s",op,(int)path.u.s.len,path.u.s.data,reason);
    size_t reason_offset=op_len+path.u.s.len+3;
    if (message[reason_offset]>='A' && message[reason_offset]<='Z') message[reason_offset]=(char)(message[reason_offset]-'A'+'a');
    return kv_cstr(message);
}
static KValue k_fs_write_data(KValue path, const char *data, size_t length);
static KValue k_fs_read_text(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (!f) return kv_res(0,k_fs_path_error("open",path));
    fseek(f,0,SEEK_END); long n=ftell(f); fseek(f,0,SEEK_SET);
    if (n<0) { fclose(f); return kv_res(0,kv_cstr("cannot read file")); }
    char *buf=(char*)kalloc((size_t)n+1);
    size_t got=fread(buf,1,(size_t)n,f); fclose(f);
    buf[got]=0;
    if (!k_utf8_valid(buf,got)) return kv_res(0,kv_cstr("file is not valid UTF-8"));
    return kv_res(1,kv_strn(buf,got));
}
static KValue k_fs_write_text(KValue path, KValue text) {
    if (!k_utf8_valid(text.u.s.data,text.u.s.len)) return kv_res(0,kv_cstr("invalid UTF-8"));
    return k_fs_write_data(path,text.u.s.data,text.u.s.len);
}
static KValue k_fs_read_bytes(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (!f) return kv_res(0,k_fs_path_error("open",path));
    fseek(f,0,SEEK_END); long n=ftell(f); fseek(f,0,SEEK_SET);
    if (n<0) { fclose(f); return kv_res(0,kv_cstr("cannot read file")); }
    char *buf=(char*)kalloc((size_t)n+1);
    size_t got=fread(buf,1,(size_t)n,f); fclose(f);
    return kv_res(1,kv_bytesn(buf,got));
}
static KValue k_fs_write_bytes(KValue path, KValue data) {
    return k_fs_write_data(path,data.u.s.data,data.u.s.len);
}
static KValue k_fs_exists(KValue path) {
    if (k_fs_path_has_nul(path)) kfail("path contains NUL");
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
#ifdef _WIN32
    return kv_bool(GetFileAttributesA(p)!=INVALID_FILE_ATTRIBUTES);
#else
    struct stat st;
    return kv_bool(lstat(p,&st)==0);
#endif
}
static KValue k_env_get(KValue name) {
    char *p=(char*)kalloc(name.u.s.len+1); memcpy(p,name.u.s.data,name.u.s.len); p[name.u.s.len]=0;
    const char *v=getenv(p);

    if (!v) return kv_opt(0,kv_nil());
    return kv_opt(1,kv_cstr(v));
}

/* ---- SHA-256 ----------------------------------------------------------- */
typedef struct { uint32_t h[8]; uint64_t len; unsigned char buf[64]; size_t n; } KSha256;
static const uint32_t k_sha_k[64] = {
 0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
 0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
 0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
 0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
 0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
 0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
 0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
 0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};
#define K_ROTR(x,n) (((x)>>(n))|((x)<<(32-(n))))
static void k_sha_block(KSha256 *s, const unsigned char *p) {
    uint32_t w[64];
    for (int i=0;i<16;i++) w[i]=((uint32_t)p[i*4]<<24)|((uint32_t)p[i*4+1]<<16)|((uint32_t)p[i*4+2]<<8)|((uint32_t)p[i*4+3]);
    for (int i=16;i<64;i++) {
        uint32_t s0=K_ROTR(w[i-15],7)^K_ROTR(w[i-15],18)^(w[i-15]>>3);
        uint32_t s1=K_ROTR(w[i-2],17)^K_ROTR(w[i-2],19)^(w[i-2]>>10);
        w[i]=w[i-16]+s0+w[i-7]+s1;
    }
    uint32_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4],f=s->h[5],g=s->h[6],h=s->h[7];
    for (int i=0;i<64;i++) {
        uint32_t S1=K_ROTR(e,6)^K_ROTR(e,11)^K_ROTR(e,25);
        uint32_t ch=(e&f)^((~e)&g);
        uint32_t t1=h+S1+ch+k_sha_k[i]+w[i];
        uint32_t S0=K_ROTR(a,2)^K_ROTR(a,13)^K_ROTR(a,22);
        uint32_t maj=(a&b)^(a&c)^(b&c);
        uint32_t t2=S0+maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e; s->h[5]+=f; s->h[6]+=g; s->h[7]+=h;
}
static void k_sha_init(KSha256 *s) {
    s->h[0]=0x6a09e667; s->h[1]=0xbb67ae85; s->h[2]=0x3c6ef372; s->h[3]=0xa54ff53a;
    s->h[4]=0x510e527f; s->h[5]=0x9b05688c; s->h[6]=0x1f83d9ab; s->h[7]=0x5be0cd19;
    s->len=0; s->n=0;
}
static void k_sha_update(KSha256 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 64 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 64) { k_sha_block(s, s->buf); s->n = 0; }
    }
}
static void k_sha_final(KSha256 *s, unsigned char out[32]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_sha_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 56) k_sha_update(s, &z, 1);
    unsigned char lb[8];
    for (int i=0;i<8;i++) lb[i]=(unsigned char)(bits >> (56 - i*8));
    k_sha_update(s, lb, 8);
    for (int i=0;i<8;i++) {
        out[i*4]=(unsigned char)(s->h[i]>>24); out[i*4+1]=(unsigned char)(s->h[i]>>16);
        out[i*4+2]=(unsigned char)(s->h[i]>>8); out[i*4+3]=(unsigned char)(s->h[i]);
    }
}
static KValue k_crypto_sha256(KValue data) {
    KSha256 s; k_sha_init(&s);
    k_sha_update(&s, (const unsigned char*)data.u.s.data, data.u.s.len);
    unsigned char out[32]; k_sha_final(&s, out);
    return kv_bytesn((const char*)out, 32);
}
static KValue k_crypto_hmac_sha256(KValue key, KValue msg) {
    unsigned char k[64]; memset(k,0,64);
    if (key.u.s.len > 64) { KSha256 s; k_sha_init(&s); k_sha_update(&s,(const unsigned char*)key.u.s.data,key.u.s.len); k_sha_final(&s,k); }
    else memcpy(k, key.u.s.data, key.u.s.len);
    unsigned char ipad[64], opad[64];
    for (int i=0;i<64;i++) { ipad[i]=k[i]^0x36; opad[i]=k[i]^0x5c; }
    KSha256 s; k_sha_init(&s);
    k_sha_update(&s, ipad, 64);
    k_sha_update(&s, (const unsigned char*)msg.u.s.data, msg.u.s.len);
    unsigned char inner[32]; k_sha_final(&s, inner);
    k_sha_init(&s);
    k_sha_update(&s, opad, 64);
    k_sha_update(&s, inner, 32);
    unsigned char out[32]; k_sha_final(&s, out);
    return kv_bytesn((const char*)out, 32);
}
static KValue k_crypto_random_bytes(KValue nv) {
    long long n = nv.u.i;
    if (n <= 0 || n > 1024) return kv_res(0, kv_cstr("random bytes length must be between 1 and 1024"));
    unsigned char *buf = (unsigned char*)kalloc((size_t)n);
    FILE *f = fopen("/dev/urandom", "rb");
    if (f) { size_t got = fread(buf, 1, (size_t)n, f); fclose(f); if (got != (size_t)n) return kv_res(0, kv_cstr("cannot read random bytes")); }
    else { for (long long i=0;i<n;i++) buf[i]=(unsigned char)(rand() & 0xff); }
    return kv_res(1, kv_bytesn((const char*)buf, (size_t)n));
}

/* ---- JSON -------------------------------------------------------------- */
static void k_json_escape(KBuf *b, const char *s, size_t n) {
    kb_putc(b, '"');
    for (size_t i=0;i<n;i++) {
        unsigned char c = (unsigned char)s[i];
        if (c==0xE2 && i+2<n && (unsigned char)s[i+1]==0x80 && ((unsigned char)s[i+2]==0xA8 || (unsigned char)s[i+2]==0xA9)) {
            kb_puts(b, (unsigned char)s[i+2]==0xA8 ? "\\u2028" : "\\u2029");
            i+=2;
            continue;
        }
        switch (c) {
            case '"': kb_puts(b,"\\\""); break;
            case '\\': kb_puts(b,"\\\\"); break;
            case '\b': kb_puts(b,"\\b"); break;
            case '\f': kb_puts(b,"\\f"); break;
            case '\n': kb_puts(b,"\\n"); break;
            case '\r': kb_puts(b,"\\r"); break;
            case '\t': kb_puts(b,"\\t"); break;
            case '<': kb_puts(b,"\\u003c"); break;
            case '>': kb_puts(b,"\\u003e"); break;
            case '&': kb_puts(b,"\\u0026"); break;
            default:
                if (c < 0x20) { char t[8]; snprintf(t,sizeof(t),"\\u%04x",c); kb_puts(b,t); }
                else kb_putc(b, (char)c);
        }
    }
    kb_putc(b, '"');
}
static void k_json_float(KBuf *b, double v) {
    if (isnan(v) || isinf(v)) { kb_puts(b,"null"); return; }
    if (v==0) { kb_putc(b,'0'); return; }
    char tmp[64]; int prec;
    for (prec=1; prec<=17; prec++) { snprintf(tmp,sizeof(tmp),"%.*g",prec,v); if (strtod(tmp,NULL)==v) break; }
    if (prec>17) prec=17;
    char ebuf[64]; snprintf(ebuf,sizeof(ebuf),"%.*e",prec-1,v);
    const char *p=ebuf; int neg=0; if (*p=='-'){neg=1;p++;}
    char digits[32]; int nd=0;
    while (*p && *p!='e' && *p!='E') { if (*p>='0'&&*p<='9') digits[nd++]=*p; p++; }
    digits[nd]=0;
    int exp10=0; if (*p=='e'||*p=='E') exp10=atoi(p+1);
    while (nd>1 && digits[nd-1]=='0') { nd--; digits[nd]=0; }
    double av = v<0?-v:v;
    if (neg) kb_putc(b,'-');
    if (av < 1e-6 || av >= 1e21) {
        kb_putc(b,digits[0]);
        if (nd>1) { kb_putc(b,'.'); for (int i=1;i<nd;i++) kb_putc(b,digits[i]); }
        kb_putc(b,'e');
        int e=exp10; kb_putc(b, e<0?'-':'+'); if (e<0) e=-e;
        char eb[8]; int en=0;
        if (e==0) eb[en++]='0';
        while (e>0) { eb[en++]='0'+(e%10); e/=10; }
        if (en<2) eb[en++]='0';
        /* Go trims a single leading zero on negative exponents (e-09 -> e-9). */
        if (exp10<0 && en==2 && eb[1]=='0') en=1;
        while (en>0) kb_putc(b, eb[--en]);
    } else {
        int dp=exp10+1;
        if (dp<=0) { kb_putc(b,'0'); kb_putc(b,'.'); for (int i=0;i<-dp;i++) kb_putc(b,'0'); for (int i=0;i<nd;i++) kb_putc(b,digits[i]); }
        else if (dp>=nd) { for (int i=0;i<nd;i++) kb_putc(b,digits[i]); for (int i=nd;i<dp;i++) kb_putc(b,'0'); }
        else { for (int i=0;i<dp;i++) kb_putc(b,digits[i]); kb_putc(b,'.'); for (int i=dp;i<nd;i++) kb_putc(b,digits[i]); }
    }
}
typedef struct { KValue key; size_t index; } KJsonKeyIndex;
static int k_json_key_index_compare(const void *left, const void *right) {
    const KJsonKeyIndex *a=(const KJsonKeyIndex*)left, *b=(const KJsonKeyIndex*)right;
    size_t n=a->key.u.s.len<b->key.u.s.len?a->key.u.s.len:b->key.u.s.len;
    int order=memcmp(a->key.u.s.data,b->key.u.s.data,n);
    if (order) return order;
    return a->key.u.s.len<b->key.u.s.len?-1:(a->key.u.s.len>b->key.u.s.len?1:0);
}
static void k_json_write(KBuf *b, KValue v) {
    switch (v.tag) {
        case K_NIL: kb_puts(b,"null"); break;
        case K_BOOL: kb_puts(b, v.u.b?"true":"false"); break;
        case K_INT: { char t[32]; snprintf(t,sizeof(t),"%lld",v.u.i); kb_puts(b,t); break; }
        case K_UINT: { char t[32]; snprintf(t,sizeof(t),"%llu",v.u.u64); kb_puts(b,t); break; }
        case K_FLOAT: k_json_float(b, v.u.f); break;
        case K_STRING: k_json_escape(b, v.u.s.data, v.u.s.len); break;
        case K_JSON: case K_JSON_NUMBER: kb_putn(b,v.u.s.data,v.u.s.len); break;
        case K_BYTES: k_json_escape(b, v.u.s.data, v.u.s.len); break;
        case K_ARRAY: case K_SET: {
            kb_putc(b,'[');
            for (size_t i=0;i<v.u.a.len;i++) { if (i) kb_putc(b,','); k_json_write(b, v.u.a.items[i]); }
            kb_putc(b,']'); break;
        }
        case K_MAP: {
            /* Go's json.Marshal sorts object keys by their original UTF-8 bytes. */
            size_t n=v.u.m.len;
            KJsonKeyIndex *idx=(KJsonKeyIndex*)kalloc(sizeof(KJsonKeyIndex)*(n?n:1));
            for (size_t i=0;i<n;i++) { idx[i].key=v.u.m.keys[i]; idx[i].index=i; }
            qsort(idx,n,sizeof(KJsonKeyIndex),k_json_key_index_compare);
            kb_putc(b,'{');
            for (size_t i=0;i<n;i++) {
                if (i) kb_putc(b,',');
                k_json_write(b, v.u.m.keys[idx[i].index]);
                kb_putc(b,':');
                k_json_write(b, v.u.m.vals[idx[i].index]);
            }
            kb_putc(b,'}'); break;
        }
        case K_OPTION: if (v.u.opt.present) k_json_write(b,*v.u.opt.inner); else kb_puts(b,"null"); break;
        case K_RESULT: k_json_write(b,*v.u.res.inner); break;
        default: kb_puts(b,"null");
    }
}
static KValue k_json_stringify(KValue v) {
    if (v.tag==K_JSON) { v.tag=K_STRING; v.json_root=NULL; return v; }
    KBuf b; kb_init(&b); k_json_write(&b, v);
    return kv_str_take(b.buf, b.len);
}

typedef struct { const char *p; size_t n; size_t i; int depth; } KJson;
typedef struct { KValue key; KValue value; size_t order; } KJsonObjectEntry;
static int k_json_object_entry_compare(const void *left, const void *right) {
    const KJsonObjectEntry *a=(const KJsonObjectEntry*)left, *b=(const KJsonObjectEntry*)right;
    size_t n=a->key.u.s.len<b->key.u.s.len?a->key.u.s.len:b->key.u.s.len;
    int order=memcmp(a->key.u.s.data,b->key.u.s.data,n);
    if (order) return order;
    if (a->key.u.s.len!=b->key.u.s.len) return a->key.u.s.len<b->key.u.s.len?-1:1;
    return a->order<b->order?-1:(a->order>b->order?1:0);
}
static void k_json_ws(KJson *j) { while (j->i<j->n) { char c=j->p[j->i]; if (c==' '||c=='\t'||c=='\n'||c=='\r') j->i++; else break; } }
static KValue k_json_value(KJson *j);
static unsigned k_json_hex4(KJson *j) {
    if (j->i+4>j->n) kfail("invalid JSON string escape");
    unsigned value=0;
    for (int i=0;i<4;i++) {
        unsigned char c=(unsigned char)j->p[j->i++];
        unsigned digit;
        if (c>='0'&&c<='9') digit=c-'0';
        else if (c>='a'&&c<='f') digit=c-'a'+10;
        else if (c>='A'&&c<='F') digit=c-'A'+10;
        else kfail("invalid JSON string escape");
        value=(value<<4)|digit;
    }
    return value;
}
static void k_json_put_utf8(KBuf *b, unsigned cp) {
    if (cp<0x80) kb_putc(b,(char)cp);
    else if (cp<0x800) { kb_putc(b,(char)(0xC0|(cp>>6))); kb_putc(b,(char)(0x80|(cp&0x3F))); }
    else if (cp<0x10000) { kb_putc(b,(char)(0xE0|(cp>>12))); kb_putc(b,(char)(0x80|((cp>>6)&0x3F))); kb_putc(b,(char)(0x80|(cp&0x3F))); }
    else { kb_putc(b,(char)(0xF0|(cp>>18))); kb_putc(b,(char)(0x80|((cp>>12)&0x3F))); kb_putc(b,(char)(0x80|((cp>>6)&0x3F))); kb_putc(b,(char)(0x80|(cp&0x3F))); }
}
static size_t k_json_utf8_sequence(const unsigned char *s, size_t n) {
    if (!n) return 0;
    unsigned char a=s[0];
    if (a>=0xC2 && a<=0xDF) return n>=2 && (s[1]&0xC0)==0x80 ? 2 : 0;
    if (a==0xE0) return n>=3 && s[1]>=0xA0 && s[1]<=0xBF && (s[2]&0xC0)==0x80 ? 3 : 0;
    if ((a>=0xE1 && a<=0xEC) || (a>=0xEE && a<=0xEF)) return n>=3 && (s[1]&0xC0)==0x80 && (s[2]&0xC0)==0x80 ? 3 : 0;
    if (a==0xED) return n>=3 && s[1]>=0x80 && s[1]<=0x9F && (s[2]&0xC0)==0x80 ? 3 : 0;
    if (a==0xF0) return n>=4 && s[1]>=0x90 && s[1]<=0xBF && (s[2]&0xC0)==0x80 && (s[3]&0xC0)==0x80 ? 4 : 0;
    if (a>=0xF1 && a<=0xF3) return n>=4 && (s[1]&0xC0)==0x80 && (s[2]&0xC0)==0x80 && (s[3]&0xC0)==0x80 ? 4 : 0;
    if (a==0xF4) return n>=4 && s[1]>=0x80 && s[1]<=0x8F && (s[2]&0xC0)==0x80 && (s[3]&0xC0)==0x80 ? 4 : 0;
    return 0;
}
static KValue k_json_string(KJson *j) {
    j->i++; /* opening quote */
    KBuf b; kb_init(&b);
    while (j->i<j->n) {
        char c=j->p[j->i++];
        if (c=='"') return kv_str_take(b.buf,b.len);
        if (c=='\\') {
            if (j->i>=j->n) break;
            char e=j->p[j->i++];
            switch (e) {
                case '"': kb_putc(&b,'"'); break;
                case '\\': kb_putc(&b,'\\'); break;
                case '/': kb_putc(&b,'/'); break;
                case 'b': kb_putc(&b,'\b'); break;
                case 'f': kb_putc(&b,'\f'); break;
                case 'n': kb_putc(&b,'\n'); break;
                case 'r': kb_putc(&b,'\r'); break;
                case 't': kb_putc(&b,'\t'); break;
                case 'u': {
                    unsigned cp=k_json_hex4(j);
                    if (cp>=0xD800 && cp<=0xDBFF) {
                        if (j->i+2<=j->n && j->p[j->i]=='\\' && j->p[j->i+1]=='u') {
                            size_t second=j->i;
                            j->i+=2;
                            unsigned lo=k_json_hex4(j);
                            if (lo>=0xDC00 && lo<=0xDFFF) cp=0x10000+((cp-0xD800)<<10)+(lo-0xDC00);
                            else { j->i=second; cp=0xFFFD; }
                        } else cp=0xFFFD;
                    } else if (cp>=0xDC00 && cp<=0xDFFF) cp=0xFFFD;
                    k_json_put_utf8(&b,cp);
                    break;
                }
                default: kfail("invalid JSON string escape");
            }
        } else {
            unsigned char byte=(unsigned char)c;
            if (byte<0x20) kfail("invalid control character in JSON string");
            if (byte<0x80) kb_putc(&b,c);
            else {
                size_t start=j->i-1;
                size_t sequence=k_json_utf8_sequence((const unsigned char*)j->p+start,j->n-start);
                if (!sequence) k_json_put_utf8(&b,0xFFFD);
                else { kb_putn(&b,j->p+start,sequence); j->i=start+sequence; }
            }
        }
    }
    kfail("unterminated JSON string");
    return kv_nil();
}
static KValue k_json_number(KJson *j) {
    size_t start=j->i; int isf=0;
    if (j->i<j->n && j->p[j->i]=='-') j->i++;
    if (j->i>=j->n || j->p[j->i]<'0' || j->p[j->i]>'9') kfail("invalid JSON number");
    if (j->p[j->i]=='0') j->i++;
    else while (j->i<j->n && j->p[j->i]>='0' && j->p[j->i]<='9') j->i++;
    if (j->i<j->n && j->p[j->i]=='.') {
        isf=1; j->i++;
        if (j->i>=j->n || j->p[j->i]<'0' || j->p[j->i]>'9') kfail("invalid JSON number");
        while (j->i<j->n && j->p[j->i]>='0' && j->p[j->i]<='9') j->i++;
    }
    if (j->i<j->n && (j->p[j->i]=='e' || j->p[j->i]=='E')) {
        isf=1; j->i++;
        if (j->i<j->n && (j->p[j->i]=='+' || j->p[j->i]=='-')) j->i++;
        if (j->i>=j->n || j->p[j->i]<'0' || j->p[j->i]>'9') kfail("invalid JSON number");
        while (j->i<j->n && j->p[j->i]>='0' && j->p[j->i]<='9') j->i++;
    }
    size_t len=j->i-start;
    char *text=(char*)kalloc(len+1); memcpy(text,j->p+start,len); text[len]=0;
    char *end=NULL;
    errno=0;
    if (!isf) {
        long long value=strtoll(text,&end,10);
        if (errno!=ERANGE && end==text+len) return kv_int(value);
        return kv_json_numbern(text,len);
    }
    double value=strtod(text,&end);
    if (errno!=ERANGE && end==text+len && isfinite(value)) return kv_float(value);
    return kv_json_numbern(text,len);
}
#define K_MAX_JSON_NESTING_DEPTH 256
static int k_json_exceeds_nesting_depth(const char *text, size_t len) {
    size_t depth=0;
    int in_string=0, escaped=0;
    for (size_t i=0;i<len;i++) {
        unsigned char c=(unsigned char)text[i];
        if (in_string) {
            if (escaped) { escaped=0; continue; }
            if (c=='\\') escaped=1;
            else if (c=='"') in_string=0;
            continue;
        }
        if (c=='"') in_string=1;
        else if (c=='[' || c=='{') {
            if (++depth>K_MAX_JSON_NESTING_DEPTH) return 1;
        } else if ((c==']' || c=='}') && depth) depth--;
    }
    return 0;
}
static KValue k_json_value(KJson *j) {
    k_json_ws(j);
    if (j->i>=j->n) kfail("unexpected end of JSON");
    char c=j->p[j->i];
    if (c=='{') {
        if (j->depth>=K_MAX_JSON_NESTING_DEPTH) kfail("JSON nesting exceeds configured limit");
        j->depth++;
        j->i++; k_json_ws(j);
        KJsonObjectEntry *entries=(KJsonObjectEntry*)kalloc(sizeof(KJsonObjectEntry)*8);
        size_t cap=8,n=0;
        if (j->i<j->n && j->p[j->i]=='}') {
            j->i++; j->depth--;
            KValue *keys=(KValue*)kalloc(sizeof(KValue));
            KValue *vals=(KValue*)kalloc(sizeof(KValue));
            return kv_map(keys,vals,0);
        }
        for (;;) {
            k_json_ws(j);
            if (j->i>=j->n || j->p[j->i]!='"') kfail("invalid JSON object key");
            KValue key=k_json_string(j);
            k_json_ws(j);
            if (j->i>=j->n || j->p[j->i]!=':') kfail("invalid JSON object");
            j->i++;
            KValue val=k_json_value(j);
            if (n==cap) {
                if (cap>SIZE_MAX/2/sizeof(KJsonObjectEntry)) kfail("memory budget exceeded");
                size_t nc=cap*2;
                KJsonObjectEntry *next=(KJsonObjectEntry*)kalloc(sizeof(KJsonObjectEntry)*nc);
                memcpy(next,entries,sizeof(KJsonObjectEntry)*n);
                entries=next; cap=nc;
            }
            entries[n]=(KJsonObjectEntry){key,val,n}; n++;
            k_json_ws(j);
            if (j->i<j->n && j->p[j->i]==',') { j->i++; continue; }
            if (j->i<j->n && j->p[j->i]=='}') { j->i++; break; }
            kfail("invalid JSON object");
        }
        j->depth--;
        qsort(entries,n,sizeof(KJsonObjectEntry),k_json_object_entry_compare);
        KValue *keys=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
        KValue *vals=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
        size_t unique=0;
        for (size_t i=0;i<n;i++) {
            if (unique && k_equal(keys[unique-1],entries[i].key)) vals[unique-1]=entries[i].value;
            else { keys[unique]=entries[i].key; vals[unique]=entries[i].value; unique++; }
        }
        return kv_map(keys,vals,unique);
    }
    if (c=='[') {
        if (j->depth>=K_MAX_JSON_NESTING_DEPTH) kfail("JSON nesting exceeds configured limit");
        j->depth++;
        j->i++; k_json_ws(j);
        KValue *items=(KValue*)kalloc(sizeof(KValue)*8); size_t cap=8,n=0;
        if (j->i<j->n && j->p[j->i]==']') { j->i++; j->depth--; return kv_arr(items,0); }
        for (;;) {
            KValue val=k_json_value(j);
            if (n==cap) { size_t nc=cap*2; KValue *ni=(KValue*)kalloc(sizeof(KValue)*nc); memcpy(ni,items,sizeof(KValue)*n); items=ni; cap=nc; }
            items[n++]=val;
            k_json_ws(j);
            if (j->i<j->n && j->p[j->i]==',') { j->i++; continue; }
            if (j->i<j->n && j->p[j->i]==']') { j->i++; break; }
            kfail("invalid JSON array");
        }
        j->depth--; return kv_arr(items,n);
    }
    if (c=='"') return k_json_string(j);
    if (c=='t') { if (j->i+4<=j->n && !memcmp(j->p+j->i,"true",4)) { j->i+=4; return kv_bool(1); } kfail("invalid JSON literal"); }
    if (c=='f') { if (j->i+5<=j->n && !memcmp(j->p+j->i,"false",5)) { j->i+=5; return kv_bool(0); } kfail("invalid JSON literal"); }
    if (c=='n') { if (j->i+4<=j->n && !memcmp(j->p+j->i,"null",4)) { j->i+=4; return kv_nil(); } kfail("invalid JSON literal"); }
    return k_json_number(j);
}
static KValue k_json_parse(KValue text) {
    if (k_max_json<0 || text.u.s.len>(size_t)k_max_json) return kv_res(0,kv_cstr("JSON input exceeds configured limit"));
    if (k_json_exceeds_nesting_depth(text.u.s.data,text.u.s.len))
        return kv_res(0,kv_cstr("JSON nesting exceeds configured limit"));
    KJson j; j.p=text.u.s.data; j.n=text.u.s.len; j.i=0; j.depth=0;
    KValue v;
    /* Parse under a nested error guard so malformed input yields err(...). */
    jmp_buf saved; memcpy(&saved,&k_jmp,sizeof(jmp_buf));
    if (setjmp(k_jmp)) { memcpy(&k_jmp,&saved,sizeof(jmp_buf)); return kv_res(0,kv_cstr("invalid JSON")); }
    v = k_json_value(&j);
    k_json_ws(&j);
    if (j.i != j.n) { memcpy(&k_jmp,&saved,sizeof(jmp_buf)); return kv_res(0,kv_cstr("invalid JSON")); }
    KBuf b; kb_init(&b); k_json_write(&b,v);
    memcpy(&k_jmp,&saved,sizeof(jmp_buf));
    return kv_res(1,kv_json_ownednode(v,b.buf,b.len));
}
static KValue k_json_unwrap(KValue value) {
    if (value.tag!=K_JSON) kfail("invalid Json value: value is not JSON");
    if (value.json_root) return *value.json_root;
    KJson j; j.p=value.u.s.data; j.n=value.u.s.len; j.i=0; j.depth=0;
    jmp_buf saved; memcpy(&saved,&k_jmp,sizeof(jmp_buf));
    if (setjmp(k_jmp)) { memcpy(&k_jmp,&saved,sizeof(jmp_buf)); kfail("invalid Json value: invalid JSON"); }
    KValue node=k_json_value(&j);
    k_json_ws(&j);
    if (j.i!=j.n) { memcpy(&k_jmp,&saved,sizeof(jmp_buf)); kfail("invalid Json value: trailing data"); }
    memcpy(&k_jmp,&saved,sizeof(jmp_buf));
    return node;
}
static KValue k_json_wrap(KValue node) {
    KBuf b; kb_init(&b); k_json_write(&b,node);
    return kv_json_ownednode(node,b.buf,b.len);
}
static KValue k_json_kind(KValue value) {
    KValue node=k_json_unwrap(value);
    switch (node.tag) {
        case K_NIL: return kv_cstr("null");
        case K_BOOL: return kv_cstr("bool");
        case K_INT: case K_UINT: case K_FLOAT: case K_JSON_NUMBER: return kv_cstr("number");
        case K_STRING: return kv_cstr("string");
        case K_ARRAY: return kv_cstr("array");
        case K_MAP: return kv_cstr("object");
        default: kfail("invalid Json value: unsupported node"); return kv_nil();
    }
}
static KValue k_json_object_get(KValue value, KValue key) {
    KValue node=k_json_unwrap(value);
    if (node.tag!=K_MAP) return kv_res(0,kv_cstr("JSON value is not an object"));
    if (key.tag==K_STRING) {
        for (size_t i=node.u.m.len;i>0;i--) {
            KValue candidate=node.u.m.keys[i-1];
            if (candidate.tag==K_STRING && candidate.u.s.len==key.u.s.len && !memcmp(candidate.u.s.data,key.u.s.data,key.u.s.len))
                return kv_res(1,k_json_wrap(node.u.m.vals[i-1]));
        }
    }
    return kv_res(0,kv_cstr("JSON object key not found"));
}
static KValue k_json_array_len(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag!=K_ARRAY) return kv_res(0,kv_cstr("JSON value is not an array"));
    return kv_res(1,kv_int((long long)node.u.a.len));
}
static KValue k_json_array_get(KValue value, KValue index) {
    KValue node=k_json_unwrap(value);
    if (node.tag!=K_ARRAY) return kv_res(0,kv_cstr("JSON value is not an array"));
    if (index.tag!=K_INT || index.u.i<0 || (unsigned long long)index.u.i>=node.u.a.len)
        return kv_res(0,kv_cstr("JSON array index out of range"));
    return kv_res(1,k_json_wrap(node.u.a.items[(size_t)index.u.i]));
}
static KValue k_json_string_value(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag!=K_STRING) return kv_res(0,kv_cstr("JSON value is not a string"));
    return kv_res(1,kv_strn(node.u.s.data,node.u.s.len));
}
static KValue k_json_int(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag==K_INT) return kv_res(1,node);
    if (node.tag!=K_JSON_NUMBER) return kv_res(0,kv_cstr("JSON value is not a number"));
    char *end=NULL; errno=0;
    long long parsed=strtoll(node.u.s.data,&end,10);
    if (errno==ERANGE || end!=node.u.s.data+node.u.s.len)
        return kv_res(0,kv_cstr("JSON number is not a signed Int"));
    return kv_res(1,kv_int(parsed));
}
static KValue k_json_uint(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag==K_INT) {
        if (node.u.i<0) return kv_res(0,kv_cstr("JSON number is not a UInt64"));
        return kv_res(1,kv_uint64((unsigned long long)node.u.i));
    }
    if (node.tag!=K_JSON_NUMBER) return kv_res(0,kv_cstr("JSON value is not a number"));
    if (node.u.s.len==0 || node.u.s.data[0]=='-') return kv_res(0,kv_cstr("JSON number is not a UInt64"));
    char *end=NULL; errno=0;
    unsigned long long parsed=strtoull(node.u.s.data,&end,10);
    if (errno==ERANGE || end!=node.u.s.data+node.u.s.len)
        return kv_res(0,kv_cstr("JSON number is not a UInt64"));
    return kv_res(1,kv_uint64(parsed));
}
static KValue k_json_to_float(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag==K_FLOAT) return kv_res(1,node);
    if (node.tag!=K_JSON_NUMBER) return kv_res(0,kv_cstr("JSON value is not a number"));
    char *end=NULL; errno=0;
    double parsed=strtod(node.u.s.data,&end);
    if ((errno==ERANGE && parsed!=0.0) || end!=node.u.s.data+node.u.s.len || !isfinite(parsed))
        return kv_res(0,kv_cstr("JSON number is not a finite Float"));
    return kv_res(1,kv_float(parsed));
}
static KValue k_json_bool(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag!=K_BOOL) return kv_res(0,kv_cstr("JSON value is not a Bool"));
    return kv_res(1,node);
}
static KValue k_json_is_null(KValue value) { return kv_bool(k_json_unwrap(value).tag==K_NIL); }

/* ---- extended filesystem ----------------------------------------------- */
static char *k_cpath(KValue path) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0; return p;
}
#ifdef _WIN32
static void k_fs_set_errno(DWORD error) {
    switch (error) {
    case ERROR_FILE_NOT_FOUND:
    case ERROR_PATH_NOT_FOUND: errno=ENOENT; break;
    case ERROR_ACCESS_DENIED:
    case ERROR_SHARING_VIOLATION:
    case ERROR_LOCK_VIOLATION: errno=EACCES; break;
    case ERROR_ALREADY_EXISTS:
    case ERROR_FILE_EXISTS: errno=EEXIST; break;
    case ERROR_INVALID_NAME:
    case ERROR_INVALID_PARAMETER: errno=EINVAL; break;
    case ERROR_DIR_NOT_EMPTY: errno=ENOTEMPTY; break;
    case ERROR_DIRECTORY: errno=ENOTDIR; break;
    default: errno=EIO; break;
    }
}
static int k_fs_lstat(const char *path, struct stat *st, int *is_link, int *is_dir) {
    HANDLE handle=CreateFileA(path,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,NULL,OPEN_EXISTING,FILE_FLAG_BACKUP_SEMANTICS|FILE_FLAG_OPEN_REPARSE_POINT,NULL);
    if (handle==INVALID_HANDLE_VALUE) { k_fs_set_errno(GetLastError()); return -1; }
    BY_HANDLE_FILE_INFORMATION info;
    if (!GetFileInformationByHandle(handle,&info)) {
        DWORD error=GetLastError(); CloseHandle(handle); k_fs_set_errno(error); return -1;
    }
    CloseHandle(handle);
    *is_link=(info.dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT)!=0;
    *is_dir=(info.dwFileAttributes&FILE_ATTRIBUTE_DIRECTORY)!=0;
    if (*is_link) { memset(st,0,sizeof(*st)); return 0; }
    if (stat(path,st)!=0) return -1;
    *is_dir=S_ISDIR(st->st_mode);
    return 0;
}
static int k_fs_remove_entry(const char *path, int is_dir) {
    if (is_dir) {
        if (RemoveDirectoryA(path)) return 0;
        k_fs_set_errno(GetLastError()); return -1;
    }
    if (DeleteFileA(path)) return 0;
    k_fs_set_errno(GetLastError()); return -1;
}
#else
static int k_fs_lstat(const char *path, struct stat *st, int *is_link, int *is_dir) {
    if (lstat(path,st)!=0) return -1;
    *is_link=S_ISLNK(st->st_mode);
    *is_dir=S_ISDIR(st->st_mode);
    return 0;
}
static int k_fs_remove_entry(const char *path, int is_dir) {
    (void)is_dir;
    return remove(path);
}
#endif
static char *k_fs_temp_path(const char *destination) {
    size_t parent_len=0;
    for (size_t i=0;destination[i];i++) {
#ifdef _WIN32
        if (destination[i]=='/' || destination[i]=='\\') parent_len=i+1;
#else
        if (destination[i]=='/') parent_len=i+1;
#endif
    }
    size_t prefix_len=parent_len ? 0 : 2;
    const char *name=".kryndel-copy-XXXXXX";
    size_t name_len=strlen(name);
    char *path=(char*)kalloc(parent_len+prefix_len+name_len+1);
    if (parent_len) memcpy(path,destination,parent_len);
    else {
        path[0]='.';
#ifdef _WIN32
        path[1]='\\';
#else
        path[1]='/';
#endif
    }
    memcpy(path+parent_len+prefix_len,name,name_len+1);
    return path;
}
static int k_fs_open_temp(char *path, size_t path_size) {
#ifdef _WIN32
    if (_mktemp_s(path,path_size)!=0) {
        if (!errno) errno=EIO;
        return -1;
    }
    return _open(path,_O_CREAT|_O_EXCL|_O_BINARY|_O_WRONLY,_S_IREAD|_S_IWRITE);
#else
    (void)path_size;
    return mkstemp(path);
#endif
}
static int k_fs_replace_file(const char *source, const char *destination) {
#ifdef _WIN32
    if (MoveFileExA(source,destination,MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)) return 0;
    k_fs_set_errno(GetLastError());
    return -1;
#else
    return rename(source,destination);
#endif
}
static int k_fs_close_fd(int fd) {
#ifdef _WIN32
    return _close(fd);
#else
    return close(fd);
#endif
}
static KValue k_fs_begin_write(KValue path, char **destination, char **temporary, FILE **output) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    *destination=k_cpath(path);
    KValue parent=k_fs_parent_dir(path);
    if (!parent.u.res.ok) return parent;
    *temporary=k_fs_temp_path(*destination);
    int fd=k_fs_open_temp(*temporary,strlen(*temporary)+1);
    if (fd<0) return kv_res(0,k_fs_path_error("create",path));
#ifdef _WIN32
    *output=_fdopen(fd,"wb");
#else
    *output=fdopen(fd,"wb");
#endif
    if (!*output) {
        int error=errno ? errno : EIO;
        k_fs_close_fd(fd); remove(*temporary); errno=error;
        return kv_res(0,k_fs_path_error("open",path));
    }
    return kv_res(1,kv_nil());
}
static KValue k_fs_finish_write(KValue path, char *destination, char *temporary, FILE *output, int write_error) {
    if (!write_error && fflush(output)!=0) write_error=errno ? errno : EIO;
#ifdef _WIN32
    if (!write_error && _commit(_fileno(output))!=0) write_error=errno ? errno : EIO;
#else
    if (!write_error && fsync(fileno(output))!=0) write_error=errno ? errno : EIO;
#endif
    if (fclose(output)!=0 && !write_error) write_error=errno ? errno : EIO;
    if (write_error) {
        remove(temporary); errno=write_error;
        return kv_res(0,k_fs_path_error("write",path));
    }
    struct stat st;
    int is_link=0, is_dir=0;
    if (k_fs_lstat(destination,&st,&is_link,&is_dir)==0 && is_link) {
        remove(temporary);
        return kv_res(0,kv_cstr("path denied by sandbox"));
    }
    if (k_fs_replace_file(temporary,destination)!=0) {
        int error=errno;
        remove(temporary); errno=error;
        return kv_res(0,k_fs_path_error("rename",path));
    }
    return kv_res(1,kv_nil());
}
static KValue k_fs_write_data(KValue path, const char *data, size_t length) {
    char *destination=NULL, *temporary=NULL;
    FILE *output=NULL;
    KValue result=k_fs_begin_write(path,&destination,&temporary,&output);
    if (!result.u.res.ok) return result;
    int write_error=0;
    if (fwrite(data,1,length,output)!=length) write_error=errno ? errno : EIO;
    return k_fs_finish_write(path,destination,temporary,output,write_error);
}
static int k_fs_dir_entry_compare(const void *left, const void *right) {
    const KValue *a=(const KValue*)left, *b=(const KValue*)right;
    return strcmp(a->u.s.data,b->u.s.data);
}
static KValue k_fs_read_dir(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    DIR *d=opendir(p);
    if (!d) return kv_res(0, kv_cstr("cannot read directory"));
    KValue *items=(KValue*)kalloc(sizeof(KValue)*8); size_t cap=8,n=0;
    struct dirent *e;
    while ((e=readdir(d))) {
        if (!strcmp(e->d_name,".")||!strcmp(e->d_name,"..")) continue;
        if (n==cap) { size_t nc=cap*2; KValue *ni=(KValue*)kalloc(sizeof(KValue)*nc); memcpy(ni,items,sizeof(KValue)*n); items=ni; cap=nc; }
        items[n++]=kv_cstr(e->d_name);
    }
    closedir(d);
    qsort(items,n,sizeof(*items),k_fs_dir_entry_compare);
    return kv_res(1, kv_arr(items,n));
}
static KValue k_fs_create_dir(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    if (k_mkdir(p)!=0) return kv_res(0, kv_cstr("cannot create directory"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_create_dir_all(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    char *start=p+1;
#ifdef _WIN32
    if (p[0] && p[1]==':' && (p[2]=='/' || p[2]=='\\')) start=p+3;
#endif
    for (char *q=start; *q; q++) {
#ifdef _WIN32
        if (*q=='/' || *q=='\\') {
#else
        if (*q=='/') {
#endif
            char sep=*q; *q=0; if (*p) k_mkdir(p); *q=sep;
        }
    }
    if (k_mkdir(p)!=0) {
        int mkdir_errno=errno; struct stat st;
        if (stat(p,&st)==0) {
            if (!S_ISDIR(st.st_mode)) { errno=ENOTDIR; return kv_res(0,kv_cstr("cannot create directory")); }
        } else { errno=mkdir_errno; return kv_res(0,kv_cstr("cannot create directory")); }
    }
    return kv_res(1, kv_nil());
}
static KValue k_fs_parent_dir(KValue path) {
    char *p=k_cpath(path), *last=NULL;
    for (char *q=p; *q; q++) {
#ifdef _WIN32
        if (*q=='/' || *q=='\\') last=q;
#else
        if (*q=='/') last=q;
#endif
    }
    if (!last) return kv_res(1,kv_nil());
    if (last==p) last++;
#ifdef _WIN32
    else if (last==p+2 && p[1]==':') last++;
#endif
    size_t parent_len=(size_t)(last-p);
    KValue parent=kv_strn(p,parent_len);
    KValue created=k_fs_create_dir_all(parent);
    if (!created.u.res.ok) return kv_res(0,k_fs_path_error("mkdir",parent));
    return created;
}
static KValue k_fs_remove_file(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    if (remove(p)!=0) return kv_res(0, kv_cstr("cannot remove file"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_remove_dir_all(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    struct stat st;
    int is_link=0, is_dir=0;
    if (k_fs_lstat(p,&st,&is_link,&is_dir)!=0) {
        if (errno==ENOENT) return kv_res(1,kv_nil());
        return kv_res(0,k_fs_path_error("unlinkat",path));
    }
    if (is_link || !is_dir) {
        if (k_fs_remove_entry(p,is_dir)==0 || errno==ENOENT) return kv_res(1,kv_nil());
        return kv_res(0,k_fs_path_error("unlinkat",path));
    }
    DIR *d=opendir(p);
    if (!d) {
        if (errno==ENOENT) return kv_res(1,kv_nil());
        return kv_res(0,k_fs_path_error("open",path));
    }
    struct dirent *e;
    KValue result=kv_res(1,kv_nil());
    int read_error=0;
    for (;;) {
        errno=0;
        e=readdir(d);
        if (!e) { read_error=errno; break; }
        if (!strcmp(e->d_name,".")||!strcmp(e->d_name,"..")) continue;
        size_t pl=strlen(p), nl=strlen(e->d_name);
        char *child=(char*)kalloc(pl+nl+2); memcpy(child,p,pl); child[pl]='/'; memcpy(child+pl+1,e->d_name,nl+1);
        result=k_fs_remove_dir_all(kv_cstr(child));
        if (!result.u.res.ok) break;
    }
    int close_error=closedir(d)==0 ? 0 : errno;
    if (!result.u.res.ok) return result;
    if (read_error) { errno=read_error; return kv_res(0,k_fs_path_error("readdirent",path)); }
    if (close_error) { errno=close_error; return kv_res(0,k_fs_path_error("closedir",path)); }
    if (k_rmdir(p)!=0 && errno!=ENOENT) return kv_res(0,k_fs_path_error("unlinkat",path));
    return kv_res(1, kv_nil());
}
static KValue k_fs_copy_file(KValue src, KValue dst) {
    if (k_fs_path_has_nul(src)) return k_fs_path_nul_result();
    char *s=k_cpath(src), *d=k_cpath(dst);
    FILE *in=fopen(s,"rb");
    if (!in) return kv_res(0,k_fs_path_error("open",src));
    char *destination=NULL, *temporary=NULL;
    FILE *out=NULL;
    KValue result=k_fs_begin_write(dst,&destination,&temporary,&out);
    if (!result.u.res.ok) { fclose(in); return result; }
    char buf[8192]; size_t got; int copy_error=0;
    while ((got=fread(buf,1,sizeof(buf),in))>0) {
        if (fwrite(buf,1,got,out)!=got) { copy_error=errno ? errno : EIO; break; }
    }
    if (!copy_error && ferror(in)) copy_error=errno ? errno : EIO;
    if (fclose(in)!=0 && !copy_error) copy_error=errno ? errno : EIO;
    return k_fs_finish_write(dst,d,temporary,out,copy_error);
}
static KValue k_fs_move_file(KValue src, KValue dst) {
    if (k_fs_path_has_nul(src) || k_fs_path_has_nul(dst)) return k_fs_path_nul_result();
    char *s=k_cpath(src), *d=k_cpath(dst);
    if (k_fs_replace_file(s,d)!=0) return kv_res(0, kv_cstr("cannot move file"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_is_file(KValue path) {
    if (k_fs_path_has_nul(path)) return kv_bool(0);
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_bool(0);
    return kv_bool(!S_ISDIR(st.st_mode));
}
static KValue k_fs_is_dir(KValue path) {
    if (k_fs_path_has_nul(path)) return kv_bool(0);
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_bool(0);
    return kv_bool(S_ISDIR(st.st_mode));
}
static KValue k_fs_file_size(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot stat file"));
    return kv_res(1, kv_int((long long)st.st_size));
}
static KValue k_fs_file_modified_time(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot stat file"));
    return kv_res(1, kv_int((long long)st.st_mtime));
}
static int k_path_is_separator(char c) {
#ifdef _WIN32
    return c=='/' || c=='\\';
#else
    return c=='/';
#endif
}
static char k_path_native_separator(void) {
#ifdef _WIN32
    return '\\';
#else
    return '/';
#endif
}
static int k_path_is_absolute(const char *path, size_t length) {
#ifdef _WIN32
    if (length>=3 && path[1]==':' && k_path_is_separator(path[2])) return 1;
    return length>0 && k_path_is_separator(path[0]);
#else
    return length>0 && path[0]=='/';
#endif
}
static KValue k_path_clean_bytes(const char *path, size_t length) {
    char *clean=(char*)kalloc(length+4);
    size_t read=0, written=0, volume=0, root=0;
    int absolute=0;
#ifdef _WIN32
    if (length>=2 && path[1]==':') {
        clean[written++]=path[0]; clean[written++]=':';
        volume=written; root=written; read=2;
        if (read<length && k_path_is_separator(path[read])) {
            clean[written++]=k_path_native_separator(); root=written; absolute=1;
            while (read<length && k_path_is_separator(path[read])) read++;
        }
    } else if (length>=2 && k_path_is_separator(path[0]) && k_path_is_separator(path[1])) {
        size_t server, server_end, share, share_end;
        read=2;
        while (read<length && k_path_is_separator(path[read])) read++;
        server=read;
        while (read<length && !k_path_is_separator(path[read])) read++;
        server_end=read;
        while (read<length && k_path_is_separator(path[read])) read++;
        share=read;
        while (read<length && !k_path_is_separator(path[read])) read++;
        share_end=read;
        if (server_end>server && share_end>share) {
            clean[written++]=k_path_native_separator();
            clean[written++]=k_path_native_separator();
            memcpy(clean+written,path+server,server_end-server); written+=server_end-server;
            clean[written++]=k_path_native_separator();
            memcpy(clean+written,path+share,share_end-share); written+=share_end-share;
            volume=written; root=written; absolute=1;
            while (read<length && k_path_is_separator(path[read])) read++;
        } else {
            read=0;
            clean[written++]=k_path_native_separator(); root=written; absolute=1;
            while (read<length && k_path_is_separator(path[read])) read++;
        }
    } else if (length>0 && k_path_is_separator(path[0])) {
        clean[written++]=k_path_native_separator(); root=written; absolute=1;
        while (read<length && k_path_is_separator(path[read])) read++;
    }
#else
    if (length>0 && path[0]=='/') {
        clean[written++]='/'; root=written; absolute=1;
        while (read<length && path[read]=='/') read++;
    }
#endif
    while (read<length) {
        size_t start, part_length;
        while (read<length && k_path_is_separator(path[read])) read++;
        if (read>=length) break;
        start=read;
        while (read<length && !k_path_is_separator(path[read])) read++;
        part_length=read-start;
        if (part_length==1 && path[start]=='.') continue;
        if (part_length==2 && path[start]=='.' && path[start+1]=='.') {
            if (written>root) {
                size_t part_start=written;
                while (part_start>root && !k_path_is_separator(clean[part_start-1])) part_start--;
                size_t previous_length=written-part_start;
                int previous_parent=previous_length==2 && clean[part_start]=='.' && clean[part_start+1]=='.';
                if (!previous_parent) {
                    written=part_start;
                    if (written>root && k_path_is_separator(clean[written-1])) written--;
                    continue;
                }
            }
            if (absolute) continue;
        }
        if (written>0 && !k_path_is_separator(clean[written-1]) && !(volume>0 && written==volume && !absolute))
            clean[written++]=k_path_native_separator();
        memcpy(clean+written,path+start,part_length);
        written+=part_length;
    }
#ifdef _WIN32
    if (written==volume && volume>0 && !absolute) clean[written++]='.';
#endif
    if (written==0) clean[written++]='.';
    return kv_strn(clean,written);
}
static KValue k_fs_join_path(KValue base, KValue parts) {
    KBuf b; kb_init(&b);
    kb_putn(&b, base.u.s.data, base.u.s.len);
    for (size_t i=0;i<parts.u.a.len;i++) {
        KValue p=parts.u.a.items[i];
        if (b.len>0 && !k_path_is_separator(b.buf[b.len-1])) kb_putc(&b,k_path_native_separator());
        kb_putn(&b, p.u.s.data, p.u.s.len);
    }
    if (b.len==0) return kv_strn("",0);
    return k_path_clean_bytes(b.buf,b.len);
}
static KValue k_fs_absolute_path(KValue path) {
#ifdef _WIN32
    if (!k_fs_path_has_nul(path)) {
        char *p=k_cpath(path);
        char buf[4096];
        if (_fullpath(buf,p,sizeof(buf))) return kv_res(1,kv_cstr(buf));
        return kv_res(0,kv_cstr("cannot resolve path"));
    }
#endif
    KBuf b; kb_init(&b);
    if (k_path_is_absolute(path.u.s.data,path.u.s.len)) {
        kb_putn(&b,path.u.s.data,path.u.s.len);
    } else {
        char cwd[4096];
        if (!getcwd(cwd,sizeof(cwd))) return kv_res(0,kv_cstr("cannot resolve path"));
        kb_puts(&b,cwd);
        if (b.len>0 && !k_path_is_separator(b.buf[b.len-1])) kb_putc(&b,k_path_native_separator());
        kb_putn(&b,path.u.s.data,path.u.s.len);
    }
    return kv_res(1,k_path_clean_bytes(b.buf,b.len));
}
static KValue k_fs_temp_dir(KValue unused) {
    (void)unused;
#ifdef _WIN32
    char path[4096];
    DWORD length=GetTempPathA((DWORD)sizeof(path),path);
    if (!length || length>=sizeof(path)) return kv_cstr(".");
    size_t used=(size_t)length;
    while (used>1 && k_path_is_separator(path[used-1]) && !(used==3 && path[1]==':')) used--;
    return kv_strn(path,used);
#else
    const char *t=getenv("TMPDIR"); if (!t||!*t) t="/tmp";
    return kv_cstr(t);
#endif
}
static KValue k_fs_temp_file(KValue prefix) {
	if (k_fs_path_has_nul(prefix)) return kv_res(0,kv_cstr("cannot create temp file"));
	KValue directory=k_fs_temp_dir(kv_nil());
	KBuf path; kb_init(&path);
	kb_putn(&path,directory.u.s.data,directory.u.s.len);
	if (path.len>0 && !k_path_is_separator(path.buf[path.len-1])) kb_putc(&path,k_path_native_separator());
	kb_putn(&path,prefix.u.s.data,prefix.u.s.len);
	kb_puts(&path,"-XXXXXX");
	int fd=k_fs_open_temp(path.buf,path.cap);
	if (fd<0) return kv_res(0,kv_cstr("cannot create temp file"));
	if (k_fs_close_fd(fd)!=0) {
		int error=errno ? errno : EIO;
		remove(path.buf); errno=error;
		return kv_res(0,kv_cstr("cannot create temp file"));
	}
	return kv_res(1,kv_strn(path.buf,path.len));
}

/* ---- process and timing ------------------------------------------------ */
static KValue k_sleep(KValue ms) {
    long long m = ms.u.i;
    if (m < 0) return kv_res(0, kv_cstr("sleep duration must be non-negative"));
    k_sleep_ms(m);
    return kv_res(1, kv_nil());
}
static KValue k_yield_now(void) { return kv_nil(); }
static KValue k_process_run(KValue prog, KValue args) {
    char *p = k_cpath(prog);
    size_t n = args.u.a.len;
    char **argv = (char**)kalloc(sizeof(char*)*(n+2));
    argv[0] = p;
    for (size_t i=0;i<n;i++) argv[i+1] = k_cpath(args.u.a.items[i]);
    argv[n+1] = 0;
#ifdef _WIN32
    intptr_t rc = _spawnvp(_P_WAIT, p, (const char* const*)argv);
    if (rc == -1) return kv_res(0, kv_cstr("cannot run process"));
    if (rc == 0) return kv_res(1, kv_int(0));
    char msg[64]; snprintf(msg,sizeof(msg),"process exited with code %d",(int)rc);
    return kv_res(0, kv_cstr(msg));
#else
    pid_t pid = fork();
    if (pid < 0) return kv_res(0, kv_cstr("cannot fork"));
    if (pid == 0) {
        /* The interpreter captures and discards child output; match it. */
        int devnull = open("/dev/null", O_WRONLY);
        if (devnull >= 0) { dup2(devnull, 1); dup2(devnull, 2); close(devnull); }
        execvp(p, argv); _exit(127);
    }
    int status; waitpid(pid, &status, 0);
    if (WIFEXITED(status) && WEXITSTATUS(status)==0) return kv_res(1, kv_int(0));
    int code = WIFEXITED(status) ? WEXITSTATUS(status) : -1;
    char msg[64]; snprintf(msg,sizeof(msg),"process exited with code %d",code);
    return kv_res(0, kv_cstr(msg));
#endif
}

/* ---- concurrency: shared cells, actors, threads, task groups ----------- */
/* The native backend runs single-threaded. Shared cells, actor mailboxes and
   task groups are modelled faithfully for the deterministic subset of the
   language: values are immutable, so a shared cell is a heap pointer that
   shared_read/shared_write/shared_swap mutate in place, and an actor mailbox
   is a FIFO queue. Worker threads are not executed concurrently; a thread
   handle records the worker function and its arguments so await() can run it
   on demand and return its result. */

typedef struct KChan { KValue *items; size_t len, cap; int closed; } KChan;
typedef struct KHandle {
    int kind;              /* 0 = thread, 1 = task group */
    KValue (*fn)(void);    /* worker entry (threads) */
    KValue *args; int nargs;
    int done; KValue result;
    struct KHandle **members; size_t nmembers; int cancelled;
} KHandle;

static KValue kv_shared(KValue *cell) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_SHARED; v.u.sh.cell=cell; return v; }
static KValue kv_actor(KChan *ch) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_ACTOR; v.u.ac.ch=ch; return v; }
static KValue kv_thread(KHandle *h) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_THREAD; v.u.hd.h=h; return v; }
static KValue kv_taskgroup(KHandle *h) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_TASKGROUP; v.u.hd.h=h; return v; }
static KValue kv_channel(KChan *ch) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_CHANNEL; v.u.ac.ch=ch; return v; }

static KChan *k_chan_new(size_t cap) {
    KChan *c=(KChan*)kalloc(sizeof(KChan));
    c->cap = cap ? cap : 64; c->len=0; c->closed=0;
    c->items=(KValue*)kalloc(sizeof(KValue)*c->cap);
    return c;
}
static void k_chan_push(KChan *c, KValue v) {
    if (c->closed) kfail("closed actor mailbox");
    if (c->len >= c->cap) kfail("actor mailbox is full");
    c->items[c->len++]=v;
}
static int k_chan_pop(KChan *c, KValue *out) {
    if (c->len==0) return 0;
    *out=c->items[0];
    for (size_t i=1;i<c->len;i++) c->items[i-1]=c->items[i];
    c->len--;
    return 1;
}

static KValue k_shared_new(KValue v) {
    KValue *cell=(KValue*)kalloc(sizeof(KValue)); *cell=v;
    return kv_shared(cell);
}
static KValue k_shared_read(KValue s) {
    if (s.tag!=K_SHARED) kfail("invalid shared cell");
    return *s.u.sh.cell;
}
static KValue k_shared_write(KValue s, KValue v) {
    if (s.tag!=K_SHARED) kfail("invalid shared cell");
    *s.u.sh.cell=v; return kv_nil();
}
static KValue k_shared_swap(KValue s, KValue v) {
    if (s.tag!=K_SHARED) kfail("invalid shared cell");
    KValue old=*s.u.sh.cell; *s.u.sh.cell=v; return old;
}

static KValue k_actor_channel(void) { return kv_actor(k_chan_new(64)); }
static KValue k_actor_channel_cap(KValue capv) {
    long long cap=capv.u.i;
    if (cap<1 || cap>1024) kfail("actor mailbox capacity is outside configured limits");
    return kv_actor(k_chan_new((size_t)cap));
}
static KValue k_actor_send(KValue a, KValue v) {
    if (a.tag!=K_ACTOR) kfail("invalid actor");
    k_chan_push(a.u.ac.ch, v); return kv_nil();
}
static KValue k_actor_try_receive(KValue a) {
    if (a.tag!=K_ACTOR) kfail("invalid actor");
    KValue out;
    if (k_chan_pop(a.u.ac.ch,&out)) return kv_res(1,out);
    if (a.u.ac.ch->closed) return kv_res(0, kv_cstr("closed"));
    return kv_res(0, kv_cstr("empty"));
}
static KValue k_actor_receive_timeout(KValue a, KValue msv) {
    if (msv.u.i<0) kfail("timeout duration cannot be negative");
    if (a.tag!=K_ACTOR) kfail("invalid actor");
    KValue out;
    if (k_chan_pop(a.u.ac.ch,&out)) return out;
    if (a.u.ac.ch->closed) kfail("closed actor mailbox");
    kfail("actor receive timed out");
    return kv_nil();
}
static KValue k_actor_close(KValue a) {
    if (a.tag==K_ACTOR) a.u.ac.ch->closed=1;
    return kv_nil();
}

static KHandle *k_handle_new(int kind) {
    KHandle *h=(KHandle*)kalloc(sizeof(KHandle));
    memset(h,0,sizeof(KHandle)); h->kind=kind; h->result=kv_nil();
    return h;
}
static KValue k_task_group(void) { return kv_taskgroup(k_handle_new(1)); }
static KValue k_task_spawn(KValue g, KValue (*fn)(void), KValue *args, int nargs) {
    if (g.tag!=K_TASKGROUP) kfail("invalid task group");
    if (g.u.hd.h->cancelled) kfail("task group is cancelled");
    KHandle *h=k_handle_new(0); h->fn=fn; h->args=args; h->nargs=nargs;
    KHandle *grp=g.u.hd.h;
    KHandle **m=(KHandle**)kalloc(sizeof(KHandle*)*(grp->nmembers+1));
    for (size_t i=0;i<grp->nmembers;i++) m[i]=grp->members[i];
    m[grp->nmembers]=h; grp->members=m; grp->nmembers++;
    return kv_thread(h);
}
static KValue k_task_group_cancel(KValue g) {
    if (g.tag!=K_TASKGROUP) kfail("invalid task group");
    g.u.hd.h->cancelled=1; return kv_nil();
}
static KValue k_task_group_wait(KValue g) {
    if (g.tag!=K_TASKGROUP) kfail("invalid task group");
    KHandle *grp=g.u.hd.h;
    for (size_t i=0;i<grp->nmembers;i++) {
        KHandle *h=grp->members[i];
        if (!h->done) { h->result=h->fn(); h->done=1; }
    }
    if (grp->cancelled) return kv_res(0, kv_cstr("task group cancelled"));
    return kv_res(1, kv_nil());
}
static KValue k_await(KValue t) {
    if (t.tag!=K_THREAD) kfail("invalid thread");
    KHandle *h=t.u.hd.h;
    if (!h->done) { h->result=h->fn(); h->done=1; }
    return h->result;
}
static KValue k_await_timeout(KValue t, KValue msv) {
    if (msv.u.i<0) kfail("timeout duration cannot be negative");
    return k_await(t);
}
static KValue k_thread_spawn(KValue (*fn)(void)) {
    KHandle *h=k_handle_new(0); h->fn=fn; return kv_thread(h);
}

/* ---- runtime polymorphism (dispatch slots) ----------------------------- */
#define K_MAX_POLY 128
typedef struct { KValue slot; KValue handler; long long prio; } KPolyEntry;
static KPolyEntry k_poly[K_MAX_POLY];
static int k_poly_n = 0;
static const char **k_poly_names = 0;
static KValue (**k_poly_fns)(void) = 0;
static int k_poly_nfns = 0;

static int k_poly_lookup(KValue name) {
    for (int i=0;i<k_poly_nfns;i++) {
        size_t n=strlen(k_poly_names[i]);
        if (n==name.u.s.len && memcmp(k_poly_names[i],name.u.s.data,n)==0) return i;
    }
    return -1;
}
static int k_poly_find(KValue slot, KValue handler) {
    for (int i=0;i<k_poly_n;i++)
        if (k_equal(k_poly[i].handler,handler) && k_equal(k_poly[i].slot,slot)) return i;
    return -1;
}
static KValue k_poly_register(KValue slot, KValue handler, KValue prio) {
    if (k_poly_lookup(handler) < 0) return kv_res(0, kv_cstr("handler must be a top-level fn(String) -> String"));
    if (k_poly_find(slot,handler) >= 0) return kv_res(0, kv_cstr("handler already registered in slot"));
    if (k_poly_n >= K_MAX_POLY) return kv_res(0, kv_cstr("too many dispatch handlers"));
    int pos = k_poly_n;
    for (int i=0;i<k_poly_n;i++) {
        if (k_equal(k_poly[i].slot,slot) && prio.u.i > k_poly[i].prio) { pos=i; break; }
    }
    for (int i=k_poly_n;i>pos;i--) k_poly[i]=k_poly[i-1];
    k_poly[pos].slot=slot; k_poly[pos].handler=handler; k_poly[pos].prio=prio.u.i;
    k_poly_n++;
    return kv_res(1, kv_nil());
}
static KValue k_poly_reorder(KValue slot, KValue handler, KValue before) {
    int from=-1, target=-1;
    for (int i=0;i<k_poly_n;i++) {
        if (k_equal(k_poly[i].slot,slot)) {
            if (k_equal(k_poly[i].handler,handler)) from=i;
            if (k_equal(k_poly[i].handler,before)) target=i;
        }
    }
    if (from<0 || target<0 || k_equal(handler,before))
        return kv_res(0, kv_cstr("both handlers must already be registered and distinct"));
    KPolyEntry e=k_poly[from];
    for (int i=from;i<k_poly_n-1;i++) k_poly[i]=k_poly[i+1];
    k_poly_n--;
    if (from<target) target--;
    for (int i=k_poly_n;i>target;i--) k_poly[i]=k_poly[i-1];
    k_poly[target]=e; k_poly_n++;
    return kv_res(1, kv_nil());
}
static KValue k_poly_dispatch(KValue slot, KValue input) {
    for (int i=0;i<k_poly_n;i++) {
        if (k_equal(k_poly[i].slot,slot)) {
            int idx=k_poly_lookup(k_poly[i].handler);
            if (idx<0) return kv_res(0, kv_cstr("registered handler is missing"));
            k_args[0]=input;
            k_argc=1;
            return kv_res(1, k_poly_fns[idx]());
        }
    }
    return kv_res(0, kv_cstr("dispatch slot has no registered handlers"));
}

/* ---- extended string, array, collection and math builtins -------------- */
static int k_less(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return a.u.i < b.u.i;
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) return a.u.f < b.u.f;
    if (a.tag==K_INT && b.tag==K_FLOAT) return (double)a.u.i < b.u.f;
    if (a.tag==K_FLOAT && b.tag==K_INT) return a.u.f < (double)b.u.i;
    if (a.tag==K_STRING && b.tag==K_STRING) {
        size_t n = a.u.s.len<b.u.s.len?a.u.s.len:b.u.s.len;
        int c = memcmp(a.u.s.data,b.u.s.data,n);
        if (c) return c<0;
        return a.u.s.len < b.u.s.len;
    }
    KBuf x,y; kb_init(&x); kb_init(&y); k_disp(&x,a); k_disp(&y,b);
    return strcmp(x.buf,y.buf) < 0;
}
static KValue k_string_repeat(KValue s, KValue cnt) {
    long long n = cnt.u.i;
    if (n < 0 || n > 1000000) return kv_res(0, kv_cstr("repeat count out of range"));
    if ((long long)s.u.s.len * n > k_max_out) return kv_res(0, kv_cstr("repeated string exceeds limit"));
    size_t total = s.u.s.len * (size_t)n;
    char *buf = (char*)kalloc(total+1);
    for (long long i=0;i<n;i++) memcpy(buf + (size_t)i*s.u.s.len, s.u.s.data, s.u.s.len);
    buf[total]=0;
    return kv_res(1, kv_strn(buf,total));
}
static KValue k_string_index_of(KValue s, KValue needle) {
    if (needle.u.s.len == 0) return kv_opt(1, kv_int(0));
    if (needle.u.s.len > s.u.s.len) return kv_opt(0, kv_nil());
    for (size_t i=0; i + needle.u.s.len <= s.u.s.len; i++) {
        if (!memcmp(s.u.s.data+i, needle.u.s.data, needle.u.s.len))
            return kv_opt(1, kv_int((long long)k_utf8_count(s.u.s.data, i)));
    }
    return kv_opt(0, kv_nil());
}
static KValue k_string_pad(KValue s, KValue widthv, KValue fill, int start) {
    long long width = widthv.u.i;
    if (width < 0 || width > 1000000) return kv_res(0, kv_cstr("pad width out of range"));
    if (fill.u.s.len == 0) return kv_res(0, kv_cstr("pad fill must not be empty"));
    long long cur = (long long)k_utf8_count(s.u.s.data, s.u.s.len);
    if (cur >= width) return kv_res(1, kv_strn(s.u.s.data, s.u.s.len));
    long long need = width - cur;
    KBuf b; kb_init(&b);
    if (start) {
        for (long long i=0;i<need;i++) kb_putn(&b, fill.u.s.data + (i % (long long)fill.u.s.len), 1);
        kb_putn(&b, s.u.s.data, s.u.s.len);
    } else {
        kb_putn(&b, s.u.s.data, s.u.s.len);
        for (long long i=0;i<need;i++) kb_putn(&b, fill.u.s.data + (i % (long long)fill.u.s.len), 1);
    }
    return kv_res(1, kv_strn(b.buf,b.len));
}
static KValue k_string_lines(KValue s) {
    KValue *items=(KValue*)kalloc(sizeof(KValue)*8); size_t cap=8,n=0;
    size_t start=0;
    for (size_t i=0;i<=s.u.s.len;i++) {
        if (i==s.u.s.len || s.u.s.data[i]=='\n') {
            if (n==cap) { size_t nc=cap*2; KValue *ni=(KValue*)kalloc(sizeof(KValue)*nc); memcpy(ni,items,sizeof(KValue)*n); items=ni; cap=nc; }
            items[n++]=kv_strn(s.u.s.data+start, i-start);
            start=i+1;
        }
    }
    return kv_arr(items,n);
}
static KValue k_string_chars(KValue s) {
    size_t total=k_utf8_count(s.u.s.data,s.u.s.len);
    KValue *items=(KValue*)kalloc(sizeof(KValue)*(total?total:1));
    size_t c=0,i=0;
    while (i<s.u.s.len) {
        unsigned char ch=(unsigned char)s.u.s.data[i]; size_t w;
        if (ch<0x80) w=1; else if ((ch>>5)==0x6) w=2; else if ((ch>>4)==0xe) w=3; else if ((ch>>3)==0x1e) w=4; else w=1;
        items[c++]=kv_strn(s.u.s.data+i,w); i+=w;
    }
    return kv_arr(items,c);
}
static KValue k_string_case(KValue s, int upper) {
    char *buf=(char*)kalloc(s.u.s.len+1);
    for (size_t i=0;i<s.u.s.len;i++) {
        unsigned char c=(unsigned char)s.u.s.data[i];
        if (upper && c>='a' && c<='z') c=(unsigned char)(c-32);
        else if (!upper && c>='A' && c<='Z') c=(unsigned char)(c+32);
        buf[i]=(char)c;
    }
    buf[s.u.s.len]=0;
    return kv_strn(buf,s.u.s.len);
}
static KValue k_array_sort(KValue arr) {
    size_t n=arr.u.a.len;
    KValue *v=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    memcpy(v,arr.u.a.items,sizeof(KValue)*n);
    for (size_t i=1;i<n;i++) {
        KValue key=v[i]; size_t j=i;
        while (j>0 && k_less(key,v[j-1])) { v[j]=v[j-1]; j--; }
        v[j]=key;
    }
    return kv_arr(v,n);
}
static KValue k_array_index_of(KValue arr, KValue needle) {
    for (size_t i=0;i<arr.u.a.len;i++)
        if (k_equal(arr.u.a.items[i],needle)) return kv_opt(1, kv_int((long long)i));
    return kv_opt(0, kv_nil());
}
static KValue k_array_sum(KValue arr) {
    long long total=0;
    for (size_t i=0;i<arr.u.a.len;i++) {
        long long x=arr.u.a.items[i].u.i;
        if ((x>0 && total>LLONG_MAX-x) || (x<0 && total<LLONG_MIN-x)) kfail("array_sum overflow");
        total+=x;
    }
    return kv_int(total);
}
static KValue k_array_minmax(KValue arr, int wantmax) {
    if (arr.u.a.len==0) return kv_opt(0, kv_nil());
    long long best=arr.u.a.items[0].u.i;
    for (size_t i=1;i<arr.u.a.len;i++) {
        long long x=arr.u.a.items[i].u.i;
        if ((wantmax && x>best) || (!wantmax && x<best)) best=x;
    }
    return kv_opt(1, kv_int(best));
}
static KValue k_array_take_drop(KValue arr, KValue cnt, int take) {
    long long n=cnt.u.i;
    if (n<0 || n>(long long)arr.u.a.len) kfail("array_take/array_drop count out of range");
    size_t start = take ? 0 : (size_t)n;
    size_t len = take ? (size_t)n : arr.u.a.len-(size_t)n;
    KValue *v=(KValue*)kalloc(sizeof(KValue)*(len?len:1));
    memcpy(v, arr.u.a.items+start, sizeof(KValue)*len);
    return kv_arr(v,len);
}
static KValue k_map_contains_key(KValue m, KValue key) {
    for (size_t i=0;i<m.u.m.len;i++) if (k_equal(m.u.m.keys[i],key)) return kv_bool(1);
    return kv_bool(0);
}
static KValue k_map_values(KValue m) {
    size_t n=m.u.m.len;
    KValue *v=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) v[i]=m.u.m.vals[i];
    return kv_arr(v,n);
}
static KValue k_map_remove(KValue m, KValue key) {
    KValue *keys=(KValue*)kalloc(sizeof(KValue)*(m.u.m.len?m.u.m.len:1));
    KValue *vals=(KValue*)kalloc(sizeof(KValue)*(m.u.m.len?m.u.m.len:1));
    size_t n=0;
    for (size_t i=0;i<m.u.m.len;i++) if (!k_equal(m.u.m.keys[i],key)) { keys[n]=m.u.m.keys[i]; vals[n]=m.u.m.vals[i]; n++; }
    return kv_map(keys,vals,n);
}
static KValue k_set_remove(KValue st, KValue val) {
    KValue *v=(KValue*)kalloc(sizeof(KValue)*(st.u.a.len?st.u.a.len:1));
    size_t n=0;
    for (size_t i=0;i<st.u.a.len;i++) if (!k_equal(st.u.a.items[i],val)) v[n++]=st.u.a.items[i];
    return kv_set(v,n);
}
static KValue k_set_to_array(KValue st) {
    size_t n=st.u.a.len;
    KValue *v=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    memcpy(v,st.u.a.items,sizeof(KValue)*n);
    return kv_arr(v,n);
}
static KValue k_tan(KValue v) { double q=tan(v.u.f); if (!isfinite(q)) kfail("tan result must be finite"); return kv_float(q); }
static KValue k_atan(KValue v) { return kv_float(atan(v.u.f)); }
static KValue k_atan2(KValue y, KValue x) { return kv_float(atan2(y.u.f,x.u.f)); }
static KValue k_exp(KValue v) { double q=exp(v.u.f); if (!isfinite(q)) kfail("exp result must be finite"); return kv_float(q); }
static KValue k_log10(KValue v) { if (v.u.f<=0) kfail("log10 domain error"); return kv_float(log10(v.u.f)); }
static KValue k_log2(KValue v) { if (v.u.f<=0) kfail("log2 domain error"); return kv_float(log2(v.u.f)); }
static KValue k_trunc(KValue v) { if (!isfinite(v.u.f)) kfail("trunc requires a finite value"); return kv_int((long long)trunc(v.u.f)); }
static KValue k_sign(KValue v) {
    if (v.tag==K_INT) return kv_int(v.u.i>0?1:(v.u.i<0?-1:0));
    return kv_int(v.u.f>0?1:(v.u.f<0?-1:0));
}
static KValue k_clamp(KValue v, KValue lo, KValue hi) {
    if (v.tag==K_INT) {
        if (lo.u.i>hi.u.i) kfail("clamp lower bound exceeds upper bound");
        long long x=v.u.i; if (x<lo.u.i) x=lo.u.i; if (x>hi.u.i) x=hi.u.i;
        return kv_int(x);
    }
    if (lo.u.f>hi.u.f) kfail("clamp lower bound exceeds upper bound");
    double x=v.u.f; if (x<lo.u.f) x=lo.u.f; if (x>hi.u.f) x=hi.u.f;
    return kv_float(x);
}

/* ---- bounded TCP client ------------------------------------------------ */
struct KTcpSocket {
    KSocketFD fd;
    int closed;
    struct KTcpSocket *next;
};
static KTcpSocket *k_tcp_sockets = NULL;
#ifdef _WIN32
static int k_tcp_winsock_started = 0;
#define K_TCP_CLOSE(fd) closesocket(fd)
#define K_TCP_INTERRUPTED(error) ((error)==WSAEINTR)
#define K_TCP_WOULD_BLOCK(error) ((error)==WSAEWOULDBLOCK)
#define K_TCP_CONNECT_PENDING(error) ((error)==WSAEWOULDBLOCK || (error)==WSAEINPROGRESS || (error)==WSAEALREADY)
#else
#define K_TCP_CLOSE(fd) close(fd)
#define K_TCP_INTERRUPTED(error) ((error)==EINTR)
#define K_TCP_WOULD_BLOCK(error) ((error)==EAGAIN || (error)==EWOULDBLOCK)
#define K_TCP_CONNECT_PENDING(error) ((error)==EINPROGRESS || (error)==EWOULDBLOCK || (error)==EALREADY)
#endif
#ifndef MSG_NOSIGNAL
#define MSG_NOSIGNAL 0
#endif

static KValue kv_tcp(KTcpSocket *socket) {
    KValue value; memset(&value,0,sizeof(value)); value.tag=K_TCP;
    value.u.tcp.socket=socket; return value;
}
static int k_tcp_socket_error(void) {
#ifdef _WIN32
    return WSAGetLastError();
#else
    return errno;
#endif
}
static const char *k_tcp_error_text(int error) {
#ifdef _WIN32
    switch (error) {
    case WSAECONNREFUSED: return "connection refused";
    case WSAETIMEDOUT: return "i/o timeout";
    case WSAECONNRESET: return "connection reset";
    case WSAEHOSTUNREACH: return "no route to host";
    case WSAENETUNREACH: return "network unreachable";
    case WSAEWOULDBLOCK: return "i/o timeout";
    default: return "network operation failed";
    }
#else
    switch (error) {
    case ECONNREFUSED: return "connection refused";
    case ETIMEDOUT: case EAGAIN: return "i/o timeout";
    case ECONNRESET: return "connection reset";
    case EHOSTUNREACH: return "no route to host";
    case ENETUNREACH: return "network unreachable";
    default: return "network operation failed";
    }
#endif
}
static unsigned long long k_tcp_now_ms(void) {
#ifdef _WIN32
    return (unsigned long long)GetTickCount64();
#else
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC,&now)!=0) return 0;
    return (unsigned long long)now.tv_sec*1000ULL+(unsigned long long)now.tv_nsec/1000000ULL;
#endif
}
static unsigned long long k_tcp_deadline_after(unsigned long long now, unsigned long long duration) {
    return now>ULLONG_MAX-duration?ULLONG_MAX:now+duration;
}
static int k_tcp_deadline_reached(unsigned long long now, unsigned long long deadline) {
    return now>=deadline;
}
static int k_tcp_set_blocking(KSocketFD fd, int blocking) {
#ifdef _WIN32
    u_long nonblocking=blocking?0UL:1UL;
    return ioctlsocket(fd,FIONBIO,&nonblocking)==0 ? 0 : -1;
#else
    int flags=fcntl(fd,F_GETFL,0);
    if (flags<0) return -1;
    if (blocking) flags&=~O_NONBLOCK; else flags|=O_NONBLOCK;
    return fcntl(fd,F_SETFL,flags);
#endif
}
static unsigned long long k_tcp_operation_deadline(void) {
    long long timeout=k_max_wall_ms>0?k_max_wall_ms:30000;
    return k_tcp_deadline_after(k_tcp_now_ms(),(unsigned long long)timeout);
}
static void k_tcp_set_timeout_error(void) {
#ifdef _WIN32
    WSASetLastError(WSAETIMEDOUT);
#else
    errno=ETIMEDOUT;
#endif
}
static int k_tcp_deadline_expired(unsigned long long deadline) {
    if (!k_tcp_deadline_reached(k_tcp_now_ms(),deadline)) return 0;
    k_tcp_set_timeout_error();
    return 1;
}
static int k_tcp_wait_connected(KSocketFD fd, unsigned long long deadline) {
    for (;;) {
        unsigned long long now=k_tcp_now_ms();
        if (k_tcp_deadline_reached(now,deadline)) return 0;
        unsigned long long remaining=deadline-now;
        struct timeval timeout;
        timeout.tv_sec=(long)(remaining/1000ULL);
        timeout.tv_usec=(long)((remaining%1000ULL)*1000ULL);
        fd_set writable, exceptional;
        FD_ZERO(&writable); FD_ZERO(&exceptional);
#ifndef _WIN32
        if ((unsigned long long)fd>=FD_SETSIZE) return -1;
#endif
        FD_SET(fd,&writable); FD_SET(fd,&exceptional);
#ifdef _WIN32
        int ready=select(0,NULL,&writable,&exceptional,&timeout);
#else
        int ready=select(fd+1,NULL,&writable,&exceptional,&timeout);
#endif
        if (ready>0) {
            int error=0;
#ifdef _WIN32
            int length=(int)sizeof(error);
#else
            socklen_t length=(socklen_t)sizeof(error);
#endif
            if (getsockopt(fd,SOL_SOCKET,SO_ERROR,(char*)&error,&length)!=0) return -1;
            if (error!=0) {
#ifdef _WIN32
                WSASetLastError(error);
#else
                errno=error;
#endif
                return -1;
            }
            return 1;
        }
        if (ready==0) return 0;
        int error=k_tcp_socket_error();
        if (!K_TCP_INTERRUPTED(error)) return -1;
    }
}
static int k_tcp_wait_io(KSocketFD fd, int writing, unsigned long long deadline) {
    for (;;) {
        unsigned long long now=k_tcp_now_ms();
        if (k_tcp_deadline_reached(now,deadline)) {
            k_tcp_set_timeout_error();
            return 0;
        }
        unsigned long long remaining=deadline-now;
        struct timeval timeout;
        timeout.tv_sec=(long)(remaining/1000ULL);
        timeout.tv_usec=(long)((remaining%1000ULL)*1000ULL);
        fd_set ready;
        FD_ZERO(&ready);
#ifndef _WIN32
        if ((unsigned long long)fd>=FD_SETSIZE) {
            errno=EINVAL;
            return -1;
        }
#endif
        FD_SET(fd,&ready);
#ifdef _WIN32
        int selected=select(0,writing?NULL:&ready,writing?&ready:NULL,NULL,&timeout);
#else
        int selected=select(fd+1,writing?NULL:&ready,writing?&ready:NULL,NULL,&timeout);
#endif
        if (selected>0) return 1;
        if (selected==0) {
            k_tcp_set_timeout_error();
            return 0;
        }
        int error=k_tcp_socket_error();
        if (!K_TCP_INTERRUPTED(error)) return -1;
    }
}
static KValue k_tcp_connect_error(KValue host, KValue port, const char *reason) {
    char port_text[32]; snprintf(port_text,sizeof(port_text),"%lld",port.u.i);
    KBuf message; kb_init(&message);
    kb_puts(&message,"dial tcp "); kb_putn(&message,host.u.s.data,host.u.s.len);
    kb_putc(&message,':'); kb_puts(&message,port_text); kb_puts(&message,": connect: "); kb_puts(&message,reason);
    return kv_res(0,kv_str_take(message.buf,message.len));
}
static KValue k_tcp_connect_until(KValue host, KValue port, unsigned long long deadline) {
    if (port.u.i<1 || port.u.i>65535) return kv_res(0,kv_cstr("port must be between 1 and 65535"));
    if (memchr(host.u.s.data,0,host.u.s.len)) return kv_res(0,kv_cstr("host contains NUL"));
#ifdef _WIN32
    if (!k_tcp_winsock_started) {
        WSADATA data;
        if (WSAStartup(MAKEWORD(2,2),&data)!=0) return kv_res(0,kv_cstr("cannot initialize TCP sockets"));
        k_tcp_winsock_started=1;
    }
#endif
    char service[8]; snprintf(service,sizeof(service),"%lld",port.u.i);
    struct addrinfo hints; memset(&hints,0,sizeof(hints));
    hints.ai_family=AF_UNSPEC; hints.ai_socktype=SOCK_STREAM; hints.ai_protocol=IPPROTO_TCP;
    struct addrinfo *addresses=NULL;
    int lookup=getaddrinfo(host.u.s.data,service,&hints,&addresses);
    if (lookup!=0) {
        if (k_tcp_deadline_expired(deadline))
            return k_tcp_connect_error(host,port,k_tcp_error_text(k_tcp_socket_error()));
        return k_tcp_connect_error(host,port,"name resolution failed");
    }
    if (k_tcp_deadline_expired(deadline)) {
        freeaddrinfo(addresses);
        return k_tcp_connect_error(host,port,k_tcp_error_text(k_tcp_socket_error()));
    }
    /* Reserve before opening descriptors: kalloc can longjmp on budget failure. */
    KTcpSocket *handle=(KTcpSocket*)kalloc(sizeof(KTcpSocket));
    memset(handle,0,sizeof(*handle));
    int last_error=0;
    KSocketFD connected=K_INVALID_SOCKET;
    for (struct addrinfo *address=addresses;address;address=address->ai_next) {
        KSocketFD fd=socket(address->ai_family,address->ai_socktype,address->ai_protocol);
        if (fd==K_INVALID_SOCKET) { last_error=k_tcp_socket_error(); continue; }
        if (k_tcp_set_blocking(fd,0)!=0) { last_error=k_tcp_socket_error(); K_TCP_CLOSE(fd); continue; }
        int result=connect(fd,address->ai_addr,(int)address->ai_addrlen);
        if (result!=0) {
            int error=k_tcp_socket_error();
            if (!K_TCP_CONNECT_PENDING(error)) { last_error=error; K_TCP_CLOSE(fd); continue; }
            result=k_tcp_wait_connected(fd,deadline);
            if (result<=0) {
                last_error=result==0 ?
#ifdef _WIN32
                    WSAETIMEDOUT
#else
                    ETIMEDOUT
#endif
                    : k_tcp_socket_error();
                K_TCP_CLOSE(fd); continue;
            }
        }
        connected=fd; break;
    }
    freeaddrinfo(addresses);
    if (connected==K_INVALID_SOCKET) return k_tcp_connect_error(host,port,k_tcp_error_text(last_error));
    handle->fd=connected; handle->next=k_tcp_sockets; k_tcp_sockets=handle;
    return kv_res(1,kv_tcp(handle));
}
static KValue k_tcp_connect(KValue host, KValue port) {
    return k_tcp_connect_until(host,port,k_tcp_operation_deadline());
}
static KValue k_tcp_send_until(KValue value, KValue bytes, unsigned long long deadline) {
    KTcpSocket *socket=value.tag==K_TCP?value.u.tcp.socket:NULL;
    if (!socket) kfail("invalid TcpSocket handle");
    if (socket->closed) return kv_res(0,kv_cstr("TcpSocket handle is closed"));
    size_t written=0;
    while (written<bytes.u.s.len) {
        if (k_tcp_deadline_expired(deadline)) {
            KBuf message; kb_init(&message); kb_puts(&message,"write tcp: "); kb_puts(&message,k_tcp_error_text(k_tcp_socket_error()));
            return kv_res(0,kv_str_take(message.buf,message.len));
        }
#ifdef _WIN32
        size_t remaining=bytes.u.s.len-written;
        int amount=remaining>(size_t)INT_MAX?INT_MAX:(int)remaining;
        int n=send(socket->fd,bytes.u.s.data+written,amount,0);
#else
        ssize_t n=send(socket->fd,bytes.u.s.data+written,bytes.u.s.len-written,MSG_NOSIGNAL);
#endif
        if (n>0) { written+=(size_t)n; continue; }
        if (n==0) return kv_res(0,kv_cstr("TCP write made no progress"));
        int error=k_tcp_socket_error();
        if (K_TCP_INTERRUPTED(error)) continue;
        if (K_TCP_WOULD_BLOCK(error)) {
            int ready=k_tcp_wait_io(socket->fd,1,deadline);
            if (ready>0) continue;
            error=k_tcp_socket_error();
        }
        KBuf message; kb_init(&message); kb_puts(&message,"write tcp: "); kb_puts(&message,k_tcp_error_text(error));
        return kv_res(0,kv_str_take(message.buf,message.len));
    }
    return kv_res(1,kv_int((long long)written));
}
static KValue k_tcp_send(KValue value, KValue bytes) {
    return k_tcp_send_until(value,bytes,k_tcp_operation_deadline());
}
static KValue k_tcp_receive(KValue value, KValue maximum) {
    if (maximum.u.i<1 || maximum.u.i>k_max_tcp_receive)
        return kv_res(0,kv_cstr("receive size is outside configured limits"));
    KTcpSocket *socket=value.tag==K_TCP?value.u.tcp.socket:NULL;
    if (!socket) kfail("invalid TcpSocket handle");
    if (socket->closed) return kv_res(0,kv_cstr("TcpSocket handle is closed"));
    size_t length=(size_t)maximum.u.i;
    unsigned long long deadline=k_tcp_operation_deadline();
    char *data=(char*)kalloc(length+1);
    for (;;) {
        if (k_tcp_deadline_expired(deadline)) {
            KBuf message; kb_init(&message); kb_puts(&message,"read tcp: "); kb_puts(&message,k_tcp_error_text(k_tcp_socket_error()));
            return kv_res(0,kv_str_take(message.buf,message.len));
        }
#ifdef _WIN32
        int received=recv(socket->fd,data,(int)length,0);
#else
        ssize_t received=recv(socket->fd,data,length,0);
#endif
        if (received==0) return kv_res(0,kv_cstr("EOF"));
        if (received>0) return kv_res(1,kv_bytesn(data,(size_t)received));
        int error=k_tcp_socket_error();
        if (K_TCP_INTERRUPTED(error)) continue;
        if (K_TCP_WOULD_BLOCK(error)) {
            int ready=k_tcp_wait_io(socket->fd,0,deadline);
            if (ready>0) continue;
            error=k_tcp_socket_error();
        }
        KBuf message; kb_init(&message);
        kb_puts(&message,"read tcp: "); kb_puts(&message,k_tcp_error_text(error));
        return kv_res(0,kv_str_take(message.buf,message.len));
    }
}
static KValue k_tcp_close(KValue value) {
    KTcpSocket *socket=value.tag==K_TCP?value.u.tcp.socket:NULL;
    if (!socket) kfail("invalid TcpSocket handle");
    if (socket->closed) kfail("TcpSocket handle is already closed");
    socket->closed=1;
    if (K_TCP_CLOSE(socket->fd)!=0) kfail(k_tcp_error_text(k_tcp_socket_error()));
    return kv_nil();
}
static int k_tcp_has_open_sockets(void) {
    for (KTcpSocket *socket=k_tcp_sockets;socket;socket=socket->next) if (!socket->closed) return 1;
    return 0;
}
static void k_tcp_cleanup(void) {
    for (KTcpSocket *socket=k_tcp_sockets;socket;socket=socket->next) {
        if (!socket->closed) { socket->closed=1; K_TCP_CLOSE(socket->fd); }
    }
#ifdef _WIN32
    if (k_tcp_winsock_started) { WSACleanup(); k_tcp_winsock_started=0; }
#endif
}

/* ---- bounded plain HTTP client ----------------------------------------- */
static int k_http_hex(unsigned char c) {
    if (c>='0'&&c<='9') return c-'0';
    if (c>='a'&&c<='f') return c-'a'+10;
    if (c>='A'&&c<='F') return c-'A'+10;
    return -1;
}
static int k_http_iequal(const char *a, size_t n, const char *b) {
    if (strlen(b)!=n) return 0;
    for (size_t i=0;i<n;i++) {
        unsigned char x=(unsigned char)a[i], y=(unsigned char)b[i];
        if (x>='A'&&x<='Z') x=(unsigned char)(x+('a'-'A'));
        if (y>='A'&&y<='Z') y=(unsigned char)(y+('a'-'A'));
        if (x!=y) return 0;
    }
    return 1;
}
static int k_http_token(unsigned char c) {
    return (c>='0'&&c<='9')||(c>='A'&&c<='Z')||(c>='a'&&c<='z')||
        c=='!'||c=='#'||c=='$'||c=='%'||c=='&'||c=='\''||c=='*'||c=='+'||
        c=='-'||c=='.'||c=='^'||c=='_'||c==0x60||c=='|'||c=='~';
}
static int k_http_unreserved(unsigned char c) {
    return (c>='0'&&c<='9')||(c>='A'&&c<='Z')||(c>='a'&&c<='z')||
        c=='-'||c=='.'||c=='_'||c=='~';
}
static int k_http_pchar(unsigned char c, int query) {
    return k_http_unreserved(c)||c=='!'||c=='$'||c=='&'||c=='\''||c=='('||
        c==')'||c=='*'||c=='+'||c==','||c==';'||c=='='||c==':'||c=='@'||
        c=='/'||(query&&c=='?');
}
static int k_http_valid_escapes(const char *s, size_t n) {
    for (size_t i=0;i<n;i++) if (s[i]=='%') {
        if (i+2>=n||k_http_hex((unsigned char)s[i+1])<0||k_http_hex((unsigned char)s[i+2])<0) return 0;
        i+=2;
    }
    return 1;
}
static void k_http_escape(KBuf *out, const char *s, size_t n, int query) {
    static const char hex[]="0123456789ABCDEF";
    for (size_t i=0;i<n;i++) {
        unsigned char c=(unsigned char)s[i];
        if (k_http_pchar(c,query)||c=='%') kb_putc(out,(char)c);
        else { kb_putc(out,'%'); kb_putc(out,hex[c>>4]); kb_putc(out,hex[c&15]); }
    }
}
static int k_http_parse_url(KValue url, KBuf *host, KBuf *port, KBuf *target, KBuf *authority, const char **error) {
    const char *s=url.u.s.data; size_t n=url.u.s.len, at=0, auth_start, auth_end, host_start, host_end, path_end, query_at;
    if (memchr(s,0,n)) { *error="URL contains NUL"; return 0; }
    if (n>=8&&k_http_iequal(s,7,"http://")) at=7;
    else if (n>=8&&k_http_iequal(s,8,"https://")) { *error="HTTPS is not supported by the C AOT backend"; return 0; }
    else { *error="unsupported protocol scheme"; return 0; }
    auth_start=at; auth_end=at;
    while (auth_end<n&&s[auth_end]!='/'&&s[auth_end]!='?'&&s[auth_end]!='#') auth_end++;
    if (auth_end==auth_start) { *error="missing host"; return 0; }
    if (memchr(s+auth_start,'@',auth_end-auth_start)) { *error="URL user information is not supported by the C AOT backend"; return 0; }
    for (size_t i=auth_start;i<auth_end;i++) if ((unsigned char)s[i]<=0x20||s[i]==0x7f) { *error="invalid character in host name"; return 0; }
    kb_putn(authority,s+auth_start,auth_end-auth_start);
    host_start=auth_start; host_end=auth_end;
    if (s[host_start]=='[') {
        size_t close=host_start+1;
        while (close<auth_end&&s[close]!=']') close++;
        if (close==auth_end||close==host_start+1) { *error="invalid IPv6 host"; return 0; }
        host_start++; host_end=close;
        if (close+1<auth_end) {
            if (s[close+1]!=':') { *error="invalid port in URL"; return 0; }
            if (close+2==auth_end) { *error="invalid port in URL"; return 0; }
            kb_putn(port,s+close+2,auth_end-close-2);
        } else if (close+1==auth_end) { }
        kb_putn(host,s+host_start,host_end-host_start);
    } else {
        size_t colon=auth_end;
        for (size_t i=auth_start;i<auth_end;i++) if (s[i]==':') {
            if (colon!=auth_end) { *error="IPv6 address must be enclosed in brackets"; return 0; }
            colon=i;
        }
        if (colon<auth_end) {
            host_end=colon;
            if (colon+1==auth_end) { *error="invalid port in URL"; return 0; }
            kb_putn(port,s+colon+1,auth_end-colon-1);
        }
        if (host_end==host_start) { *error="missing host"; return 0; }
        kb_putn(host,s+host_start,host_end-host_start);
    }
    if (host->len==0) { *error="missing host"; return 0; }
    if (port->len==0) kb_puts(port,"80");
    unsigned long long port_number=0;
    for (size_t i=0;i<port->len;i++) {
        unsigned char c=(unsigned char)port->buf[i];
        if (c<'0'||c>'9'||port_number>65535) { *error="invalid port in URL"; return 0; }
        port_number=port_number*10+(unsigned long long)(c-'0');
    }
    if (port_number<1||port_number>65535) { *error="invalid port in URL"; return 0; }
    at=auth_end; path_end=at;
    while (path_end<n&&s[path_end]!='?'&&s[path_end]!='#') path_end++;
    if (at==path_end||s[at]!='/' ) kb_putc(target,'/');
    if (at<path_end) {
        if (!k_http_valid_escapes(s+at,path_end-at)) { *error="invalid URL escape"; return 0; }
        k_http_escape(target,s+at,path_end-at,0);
    }
    query_at=path_end;
    if (query_at<n&&s[query_at]=='?') {
        size_t query_end=query_at+1;
        while (query_end<n&&s[query_end]!='#') query_end++;
        if (!k_http_valid_escapes(s+query_at+1,query_end-query_at-1)) { *error="invalid URL escape"; return 0; }
        kb_putc(target,'?');
        k_http_escape(target,s+query_at+1,query_end-query_at-1,1);
    }
    return 1;
}
static long long k_http_recv_some(KTcpSocket *socket, char *data, size_t cap, unsigned long long deadline) {
    for (;;) {
        if (k_tcp_deadline_expired(deadline)) return -1;
#ifdef _WIN32
        int want=cap>(size_t)INT_MAX?INT_MAX:(int)cap;
        int received=recv(socket->fd,data,want,0);
#else
        ssize_t received=recv(socket->fd,data,cap,0);
#endif
        if (received>=0) return (long long)received;
        int error=k_tcp_socket_error();
        if (K_TCP_INTERRUPTED(error)) continue;
        if (K_TCP_WOULD_BLOCK(error)) {
            int ready=k_tcp_wait_io(socket->fd,0,deadline);
            if (ready>0) continue;
            return -1;
        }
        return -1;
    }
}
typedef struct { KTcpSocket *socket; unsigned long long deadline; char data[8192]; size_t at,len; int eof; } KHTTPReader;
static int k_http_fill(KHTTPReader *reader) {
    if (reader->at<reader->len) return 1;
    reader->at=reader->len=0;
    long long n=k_http_recv_some(reader->socket,reader->data,sizeof(reader->data),reader->deadline);
    if (n<0) return -1;
    if (n==0) { reader->eof=1; return 0; }
    reader->len=(size_t)n;
    return 1;
}
static KValue k_http_read_error(KHTTPReader *reader, int status, const char *eof_message, const char *invalid_message) {
    if (status==-1) {
        const char *message=k_tcp_deadline_reached(k_tcp_now_ms(),reader->deadline)?"i/o timeout":k_tcp_error_text(k_tcp_socket_error());
        return kv_res(0,kv_cstr(message));
    }
    return kv_res(0,kv_cstr(status==0?eof_message:invalid_message));
}
static int k_http_read_line(KHTTPReader *reader, KBuf *line, size_t max) {
    for (;;) {
        int ready=k_http_fill(reader);
        if (ready<=0) return ready;
        char c=reader->data[reader->at++];
        if (c=='\n') {
            if (line->len==0||line->buf[line->len-1]!='\r') return -2;
            line->len--; line->buf[line->len]=0;
            return 1;
        }
        if (c=='\r'&&reader->at<reader->len&&reader->data[reader->at]!='\n') return -2;
        if (line->len>=max) return -2;
        kb_putc(line,c);
    }
}
static int k_http_read_exact(KHTTPReader *reader, KBuf *body, size_t length, size_t limit) {
    if (length>limit-body->len) return -2;
    while (length) {
        int ready=k_http_fill(reader);
        if (ready<=0) return ready;
        size_t avail=reader->len-reader->at;
        size_t take=avail<length?avail:length;
        if (body->len>SIZE_MAX-take-1) return -2;
        while (body->cap<=body->len+take) kb_grow(body);
        memcpy(body->buf+body->len,reader->data+reader->at,take);
        body->len+=take; body->buf[body->len]=0;
        reader->at+=take; length-=take;
    }
    return 1;
}
static KValue k_http_request(KValue method, KValue url, KValue body_value) {
    KBuf host,port,target,authority,request,response_line,header,body;
    const char *url_error=NULL;
    if (method.u.s.len==0) return kv_res(0,kv_cstr("method must not be empty"));
    for (size_t i=0;i<method.u.s.len;i++) if (!k_http_token((unsigned char)method.u.s.data[i])) return kv_res(0,kv_cstr("invalid method"));
    kb_init(&host); kb_init(&port); kb_init(&target); kb_init(&authority);
    if (!k_http_parse_url(url,&host,&port,&target,&authority,&url_error)) return kv_res(0,kv_cstr(url_error));
    if (body_value.u.s.len>(size_t)LLONG_MAX||target.len>(size_t)LLONG_MAX) return kv_res(0,kv_cstr("request is too large"));
    unsigned long long deadline=k_tcp_operation_deadline();
    KValue connect_result=k_tcp_connect_until(kv_strn(host.buf,host.len),kv_int(atoll(port.buf)),deadline);
    if (!connect_result.u.res.ok) return connect_result;
    KValue connected=*connect_result.u.res.inner;
    KTcpSocket *socket=connected.u.tcp.socket;
    kb_init(&request);
    kb_putn(&request,method.u.s.data,method.u.s.len); kb_putc(&request,' '); kb_putn(&request,target.buf,target.len); kb_puts(&request," HTTP/1.1\r\nHost: "); kb_putn(&request,authority.buf,authority.len);
    kb_puts(&request,"\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: identity\r\nConnection: close\r\nContent-Length: ");
    char body_length[32]; snprintf(body_length,sizeof(body_length),"%llu",(unsigned long long)body_value.u.s.len); kb_puts(&request,body_length); kb_puts(&request,"\r\n\r\n"); kb_putn(&request,body_value.u.s.data,body_value.u.s.len);
    KValue sent=k_tcp_send_until(connected,kv_bytesn(request.buf,request.len),deadline);
    if (!sent.u.res.ok) { KValue error=sent; k_tcp_close(connected); return error; }
    if ((unsigned long long)sent.u.res.inner->u.i!=(unsigned long long)request.len) { k_tcp_close(connected); return kv_res(0,kv_cstr("write tcp: short write")); }
    KHTTPReader reader={0}; reader.socket=socket; reader.deadline=deadline;
    kb_init(&response_line); kb_init(&header); kb_init(&body);
    int status=0, chunked=0, has_length=0, no_body=0; size_t content_length=0, headers_size=0;
    for (int interim=0;interim<6;interim++) {
        response_line.len=0; response_line.buf[0]=0;
        int line_status=k_http_read_line(&reader,&response_line,8192);
        if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP status","invalid HTTP response"); }
        if (response_line.len<12||memcmp(response_line.buf,"HTTP/1.",7)!=0||(response_line.buf[7]!='0'&&response_line.buf[7]!='1')||response_line.buf[8]!=' '||response_line.buf[9]<'1'||response_line.buf[9]>'5'||response_line.buf[10]<'0'||response_line.buf[10]>'9'||response_line.buf[11]<'0'||response_line.buf[11]>'9'||(response_line.len>12&&response_line.buf[12]!=' ')) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response status")); }
        status=(response_line.buf[9]-'0')*100+(response_line.buf[10]-'0')*10+(response_line.buf[11]-'0');
        headers_size=0; has_length=0; chunked=0; content_length=0;
        for (;;) {
            header.len=0; header.buf[0]=0;
            line_status=k_http_read_line(&reader,&header,65536);
            if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP headers","invalid HTTP response headers"); }
            headers_size+=header.len+2;
            if (headers_size>65536) { k_tcp_close(connected); return kv_res(0,kv_cstr("HTTP response headers exceed configured limit")); }
            if (header.len==0) break;
            char *colon=memchr(header.buf,':',header.len);
            if (!colon||colon==header.buf) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
            size_t name_len=(size_t)(colon-header.buf), value_at=name_len+1;
            for (size_t i=0;i<name_len;i++) if (!k_http_token((unsigned char)header.buf[i])) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
            while (value_at<header.len&&(header.buf[value_at]==' '||header.buf[value_at]=='\t')) value_at++;
            size_t value_end=header.len; while (value_end>value_at&&(header.buf[value_end-1]==' '||header.buf[value_end-1]=='\t')) value_end--;
            if (k_http_iequal(header.buf,name_len,"content-length")) {
                if (has_length||value_at==value_end) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
                size_t parsed=0;
                for (size_t i=value_at;i<value_end;i++) {
                    unsigned char c=(unsigned char)header.buf[i];
                    if (c<'0'||c>'9'||parsed>(SIZE_MAX-(size_t)(c-'0'))/10) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
                    parsed=parsed*10+(size_t)(c-'0');
                }
                if (parsed>(size_t)LLONG_MAX) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
                content_length=parsed; has_length=1;
            } else if (k_http_iequal(header.buf,name_len,"transfer-encoding")) {
                if (chunked||!k_http_iequal(header.buf+value_at,value_end-value_at,"chunked")) { k_tcp_close(connected); return kv_res(0,kv_cstr("unsupported HTTP transfer encoding")); }
                chunked=1;
            }
        }
        if (status<200&&status!=101) continue;
        no_body=(method.u.s.len==4&&k_http_iequal(method.u.s.data,method.u.s.len,"HEAD"))||status==204||status==304||status==101;
        break;
    }
    if (status<200&&status!=101) { k_tcp_close(connected); return kv_res(0,kv_cstr("too many informational HTTP responses")); }
    if (chunked&&has_length) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
    if (!no_body) {
        size_t limit=k_max_tcp_receive<0?0:(size_t)k_max_tcp_receive;
        if (has_length) {
            if (content_length>limit) { k_tcp_close(connected); return kv_res(0,kv_cstr("response exceeds configured input limit")); }
            int read_status=k_http_read_exact(&reader,&body,content_length,limit);
            if (read_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,read_status,"unexpected EOF reading HTTP response","invalid HTTP response body"); }
        } else if (chunked) {
            for (;;) {
                KBuf chunk; kb_init(&chunk);
                int line_status=k_http_read_line(&reader,&chunk,8192);
                if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP chunk","invalid HTTP chunk"); }
                size_t digits=0; unsigned long long chunk_size=0;
                while (digits<chunk.len&&chunk.buf[digits]!=';') {
                    int digit=k_http_hex((unsigned char)chunk.buf[digits]);
                    if (digit<0||chunk_size>(ULLONG_MAX-(unsigned long long)digit)/16ULL) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP chunk")); }
                    chunk_size=chunk_size*16ULL+(unsigned long long)digit; digits++;
                }
                if (digits==0||chunk_size>(unsigned long long)(limit-body.len)) { k_tcp_close(connected); return kv_res(0,kv_cstr(chunk_size>(unsigned long long)(limit-body.len)?"response exceeds configured input limit":"invalid HTTP chunk")); }
                if (chunk_size==0) {
                    for (;;) {
                        KBuf trailer; kb_init(&trailer);
                        line_status=k_http_read_line(&reader,&trailer,65536);
                        if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP chunk trailer","invalid HTTP chunk trailer"); }
                        headers_size+=trailer.len+2;
                        if (headers_size>65536) { k_tcp_close(connected); return kv_res(0,kv_cstr("HTTP response headers exceed configured limit")); }
                        if (trailer.len==0) break;
                        if (!memchr(trailer.buf,':',trailer.len)) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP chunk trailer")); }
                    }
                    break;
                }
                int read_status=k_http_read_exact(&reader,&body,(size_t)chunk_size,limit);
                if (read_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,read_status,"unexpected EOF reading HTTP chunk","invalid HTTP chunk"); }
                char crlf[2]; size_t got=0;
                while (got<2) { int ready=k_http_fill(&reader); if (ready<=0) { k_tcp_close(connected); return k_http_read_error(&reader,ready,"unexpected EOF reading HTTP chunk","invalid HTTP chunk"); } crlf[got++]=reader.data[reader.at++]; }
                if (crlf[0]!='\r'||crlf[1]!='\n') { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP chunk")); }
            }
        } else {
            for (;;) {
                int ready=k_http_fill(&reader);
                if (ready<0) { k_tcp_close(connected); return k_http_read_error(&reader,ready,"unexpected EOF reading HTTP response","invalid HTTP response body"); }
                if (ready==0) break;
                size_t available=reader.len-reader.at;
                if (available>limit-body.len) { k_tcp_close(connected); return kv_res(0,kv_cstr("response exceeds configured input limit")); }
                if (k_http_read_exact(&reader,&body,available,limit)<=0) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response body")); }
            }
        }
    }
    k_tcp_close(connected);
    if (status<200||status>=300) {
        KBuf message; kb_init(&message); kb_puts(&message,"HTTP status "); char status_text[16]; snprintf(status_text,sizeof(status_text),"%d",status); kb_puts(&message,status_text);
        return kv_res(0,kv_str_take(message.buf,message.len));
    }
    if (!k_utf8_valid(body.buf,body.len)) return kv_res(0,kv_cstr("response is not valid UTF-8"));
    return kv_res(1,kv_strn(body.buf,body.len));
}

/* ---- dynamically loaded SQLite ---------------------------------------- */
typedef struct sqlite3 KSQLiteNativeDB;
typedef struct sqlite3_stmt KSQLiteNativeStatement;

struct KSQLiteHandle {
    KSQLiteNativeDB *db;
    int closed;
    struct KSQLiteHandle *next;
};

typedef struct KSQLiteTrackedStatement {
    KSQLiteNativeStatement *statement;
    struct KSQLiteTrackedStatement *next;
} KSQLiteTrackedStatement;

typedef struct {
    int (*open_v2)(const char*,KSQLiteNativeDB**,int,const char*);
    int (*close_v2)(KSQLiteNativeDB*);
    const char *(*errmsg)(KSQLiteNativeDB*);
    int (*exec)(KSQLiteNativeDB*,const char*,int(*)(void*,int,char**,char**),void*,char**);
    int (*prepare_v2)(KSQLiteNativeDB*,const char*,int,KSQLiteNativeStatement**,const char**);
    int (*step)(KSQLiteNativeStatement*);
    int (*finalize)(KSQLiteNativeStatement*);
    int (*column_count)(KSQLiteNativeStatement*);
    int (*column_type)(KSQLiteNativeStatement*,int);
    const unsigned char *(*column_text)(KSQLiteNativeStatement*,int);
    const void *(*column_blob)(KSQLiteNativeStatement*,int);
    int (*column_bytes)(KSQLiteNativeStatement*,int);
    long long (*column_int64)(KSQLiteNativeStatement*,int);
    double (*column_double)(KSQLiteNativeStatement*,int);
    int (*changes)(KSQLiteNativeDB*);
    int (*busy_timeout)(KSQLiteNativeDB*,int);
} KSQLiteAPI;

static KSQLiteAPI k_sqlite_api;
static void *k_sqlite_library;
static KSQLiteHandle *k_sqlite_handles;
static KSQLiteTrackedStatement *k_sqlite_statements;

static void k_sqlite_unload(void) {
#ifdef _WIN32
    if (k_sqlite_library) FreeLibrary((HMODULE)k_sqlite_library);
#else
    if (k_sqlite_library) dlclose(k_sqlite_library);
#endif
    k_sqlite_library=NULL;
    memset(&k_sqlite_api,0,sizeof(k_sqlite_api));
}

static int k_sqlite_resolve(void *library, KSQLiteAPI *api) {
#ifdef _WIN32
#define K_SQLITE_SYM(name) api->name=(void*)GetProcAddress((HMODULE)library,"sqlite3_" #name)
#else
#define K_SQLITE_SYM(name) api->name=dlsym(library,"sqlite3_" #name)
#endif
    memset(api,0,sizeof(*api));
    K_SQLITE_SYM(open_v2);
    K_SQLITE_SYM(close_v2);
    K_SQLITE_SYM(errmsg);
    K_SQLITE_SYM(exec);
    K_SQLITE_SYM(prepare_v2);
    K_SQLITE_SYM(step);
    K_SQLITE_SYM(finalize);
    K_SQLITE_SYM(column_count);
    K_SQLITE_SYM(column_type);
    K_SQLITE_SYM(column_text);
    K_SQLITE_SYM(column_blob);
    K_SQLITE_SYM(column_bytes);
    K_SQLITE_SYM(column_int64);
    K_SQLITE_SYM(column_double);
    K_SQLITE_SYM(changes);
    K_SQLITE_SYM(busy_timeout);
#undef K_SQLITE_SYM
    return api->open_v2 && api->close_v2 && api->errmsg && api->exec &&
           api->prepare_v2 && api->step && api->finalize && api->column_count &&
           api->column_type && api->column_text && api->column_blob && api->column_bytes &&
           api->column_int64 && api->column_double && api->changes && api->busy_timeout;
}

static const char *k_sqlite_load(void) {
    if (k_sqlite_library) return NULL;
#ifdef _WIN32
    const char *names[]={"winsqlite3.dll","sqlite3.dll",NULL};
    for (int i=0;names[i];i++) {
        HMODULE library=LoadLibraryA(names[i]);
        if (!library) continue;
        KSQLiteAPI api;
        if (k_sqlite_resolve((void*)library,&api)) {
            k_sqlite_library=(void*)library; k_sqlite_api=api; return NULL;
        }
        FreeLibrary(library);
    }
    return "SQLite library unavailable: install winsqlite3.dll or sqlite3.dll";
#else
    const char *names[]={"libsqlite3.so.0","libsqlite3.so",NULL};
    for (int i=0;names[i];i++) {
        void *library=dlopen(names[i],RTLD_NOW|RTLD_LOCAL);
        if (!library) continue;
        KSQLiteAPI api;
        if (k_sqlite_resolve(library,&api)) {
            k_sqlite_library=library; k_sqlite_api=api; return NULL;
        }
        dlclose(library);
    }
    return "SQLite library unavailable: install libsqlite3.so.0 or libsqlite3.so";
#endif
}

static void k_sqlite_track_statement(KSQLiteTrackedStatement *tracked, KSQLiteNativeStatement *statement) {
    tracked->statement=statement;
    tracked->next=k_sqlite_statements;
    k_sqlite_statements=tracked;
}

static int k_sqlite_finalize(KSQLiteTrackedStatement *tracked) {
    if (!tracked || !tracked->statement || !k_sqlite_api.finalize) return 0;
    KSQLiteNativeStatement *statement=tracked->statement;
    tracked->statement=NULL;
    return k_sqlite_api.finalize(statement);
}

static void k_sqlite_cleanup(void) {
    for (KSQLiteTrackedStatement *tracked=k_sqlite_statements;tracked;tracked=tracked->next)
        (void)k_sqlite_finalize(tracked);
    for (KSQLiteHandle *handle=k_sqlite_handles;handle;handle=handle->next) {
        if (handle->db && k_sqlite_api.close_v2) {
            handle->closed=1;
            (void)k_sqlite_api.close_v2(handle->db);
            handle->db=NULL;
        }
    }
    k_sqlite_statements=NULL;
    k_sqlite_handles=NULL;
    k_sqlite_unload();
}

static int k_sqlite_has_open_handles(void) {
    for (KSQLiteHandle *handle=k_sqlite_handles;handle;handle=handle->next)
        if (!handle->closed) return 1;
    return 0;
}

static int k_sqlite_string_has_nul(KValue value) {
    return value.tag!=K_STRING || memchr(value.u.s.data,0,value.u.s.len)!=NULL;
}

static KSQLiteHandle *k_sqlite_handle(KValue value) {
    return value.tag==K_SQLITE?value.u.sqlite.database:NULL;
}

static const char *k_sqlite_error(KSQLiteHandle *handle, int status) {
    if (handle && handle->db && k_sqlite_api.errmsg) {
        const char *message=k_sqlite_api.errmsg(handle->db);
        if (message && *message) return message;
    }
    (void)status;
    return "SQLite operation failed";
}

static void k_sqlite_copy_error(KSQLiteHandle *handle, int status, char *buffer, size_t capacity) {
    const char *message=k_sqlite_error(handle,status);
    const char *prefixes[]={"SQL logic error: ","constraint failed: ",NULL};
    for (int i=0;prefixes[i];i++) {
        size_t prefix_length=strlen(prefixes[i]);
        if (strncmp(message,prefixes[i],prefix_length)==0) { message+=prefix_length; break; }
    }
    size_t length=strlen(message);
    if (length>1 && message[length-1]==')') {
        for (size_t i=length-1;i>1;i--) {
            if (message[i-1]=='(' && message[i-2]==' ') {
                int numeric=i<length-1;
                for (size_t j=i;j<length-1;j++) if (message[j]<'0'||message[j]>'9') numeric=0;
                if (numeric) length=i-2;
                break;
            }
        }
    }
    if (capacity==0) return;
    if (length>=capacity) length=capacity-1;
    memcpy(buffer,message,length);
    buffer[length]=0;
}

static KValue k_sqlite_open(KValue path) {
    if (path.tag!=K_STRING) kfail("invalid SQLite path");
    if (k_sqlite_string_has_nul(path)) return kv_res(0,kv_cstr("path contains NUL"));
    const char *load_error=k_sqlite_load();
    if (load_error) return kv_res(0,kv_cstr(load_error));

    KSQLiteHandle *handle=(KSQLiteHandle*)kalloc(sizeof(KSQLiteHandle));
    memset(handle,0,sizeof(*handle));
    int status=k_sqlite_api.open_v2(path.u.s.data,&handle->db,2|4|64|0x10000,NULL);
    if (status!=0) {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        if (handle->db) (void)k_sqlite_api.close_v2(handle->db);
        handle->db=NULL;
        return kv_res(0,kv_cstr(message));
    }
    status=k_sqlite_api.busy_timeout(handle->db,5000);
    if (status!=0) {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        (void)k_sqlite_api.close_v2(handle->db);
        handle->db=NULL;
        return kv_res(0,kv_cstr(message));
    }
    handle->closed=0;
    handle->next=k_sqlite_handles;
    k_sqlite_handles=handle;
    KValue database; memset(&database,0,sizeof(database));
    database.tag=K_SQLITE; database.u.sqlite.database=handle;
    return kv_res(1,database);
}

static KValue k_sqlite_exec(KValue value, KValue query) {
    KSQLiteHandle *handle=k_sqlite_handle(value);
    if (!handle) kfail("invalid SQLite handle");
    if (handle->closed || !handle->db) return kv_res(0,kv_cstr("SQLite handle is closed"));
    if (query.tag!=K_STRING) kfail("invalid SQLite query");
    if (k_sqlite_string_has_nul(query)) return kv_res(0,kv_cstr("query contains NUL"));
    int status=k_sqlite_api.exec(handle->db,query.u.s.data,NULL,NULL,NULL);
    if (status!=0) {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        return kv_res(0,kv_cstr(message));
    }
    return kv_res(1,kv_int(k_sqlite_api.changes(handle->db)));
}

static KValue *k_sqlite_grow_rows(KValue *rows, size_t old_capacity, size_t new_capacity) {
    if (new_capacity>SIZE_MAX/sizeof(KValue)) kfail("memory budget exceeded");
    size_t old_bytes=old_capacity*sizeof(KValue), new_bytes=new_capacity*sizeof(KValue);
    if (new_bytes>(size_t)LLONG_MAX || k_mem<(long long)old_bytes ||
        (long long)new_bytes>k_max_mem || k_mem-(long long)old_bytes>k_max_mem-(long long)new_bytes)
        kfail("memory budget exceeded");
    KValue *grown=(KValue*)realloc(rows,new_bytes);
    if (!grown) kfail("out of memory");
    k_mem=k_mem-(long long)old_bytes+(long long)new_bytes;
    return grown;
}

static void k_fmt_float(double v, char *out, size_t outsz);
static void k_sqlite_format_float(double number, char *buffer, size_t capacity) {
    if (number==0.0 && signbit(number)) { snprintf(buffer,capacity,"-0"); return; }
    k_fmt_float(number,buffer,capacity);
}

static KValue k_sqlite_column_value(KSQLiteNativeStatement *statement, int column) {
    int type=k_sqlite_api.column_type(statement,column);
    if (type==5) return kv_cstr("");
    if (type==1) {
        char buffer[32];
        snprintf(buffer,sizeof(buffer),"%lld",k_sqlite_api.column_int64(statement,column));
        return kv_cstr(buffer);
    }
    if (type==2) {
        char buffer[64];
        k_sqlite_format_float(k_sqlite_api.column_double(statement,column),buffer,sizeof(buffer));
        return kv_cstr(buffer);
    }
    int length=k_sqlite_api.column_bytes(statement,column);
    if (length<0) kfail("SQLite returned an invalid column length");
    const void *data=type==4?k_sqlite_api.column_blob(statement,column):(const void*)k_sqlite_api.column_text(statement,column);
    if (!data && length>0) kfail("SQLite returned an invalid column value");
    if (!data) data="";
    return kv_strn((const char*)data,(size_t)length);
}

static int k_sqlite_tail_has_sql(const char *tail, const char *end) {
    const char *cursor=tail;
    while (cursor<end) {
        if (*cursor==' '||*cursor=='\t'||*cursor=='\r'||*cursor=='\n'||*cursor=='\f'||*cursor=='\v'||*cursor==';') { cursor++; continue; }
        if (end-cursor>=2 && cursor[0]=='-' && cursor[1]=='-') {
            cursor+=2;
            while (cursor<end && *cursor!='\n') cursor++;
            continue;
        }
        if (end-cursor>=2 && cursor[0]=='/' && cursor[1]=='*') {
            cursor+=2;
            while (end-cursor>=2 && !(cursor[0]=='*' && cursor[1]=='/')) cursor++;
            if (end-cursor>=2) cursor+=2;
            continue;
        }
        return 1;
    }
    return 0;
}

static KValue k_sqlite_query(KValue value, KValue query) {
    KSQLiteHandle *handle=k_sqlite_handle(value);
    if (!handle) kfail("invalid SQLite handle");
    if (k_max_array_elements<1) return kv_res(0,kv_cstr("SQLite row limit must be positive"));
    if (handle->closed || !handle->db) return kv_res(0,kv_cstr("SQLite handle is closed"));
    if (query.tag!=K_STRING) kfail("invalid SQLite query");
    if (k_sqlite_string_has_nul(query)) return kv_res(0,kv_cstr("query contains NUL"));
    if (query.u.s.len>(size_t)INT_MAX) return kv_res(0,kv_cstr("SQL query is too large"));

    KSQLiteTrackedStatement *tracked=(KSQLiteTrackedStatement*)kalloc(sizeof(KSQLiteTrackedStatement));
    tracked->statement=NULL;
    tracked->next=k_sqlite_statements;
    k_sqlite_statements=tracked;
    KValue *rows=NULL;
    size_t row_count=0, capacity=0;
    const char *cursor=query.u.s.data;
    const char *end=cursor+query.u.s.len;
    while (cursor<end) {
        const char *tail=cursor;
        int status=k_sqlite_api.prepare_v2(handle->db,cursor,(int)(end-cursor),&tracked->statement,&tail);
        if (status!=0) {
            char message[512];
            k_sqlite_copy_error(handle,status,message,sizeof(message));
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr(message));
        }
        if (!tail || tail<cursor || tail>end) {
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr("SQLite returned an invalid SQL parser position"));
        }
        if (!tracked->statement) {
            if (tail==cursor) break;
            cursor=tail;
            continue;
        }

        int is_final=!k_sqlite_tail_has_sql(tail,end);
        int column_count=k_sqlite_api.column_count(tracked->statement);
        if (column_count<0 || (size_t)column_count>SIZE_MAX/sizeof(KValue)) {
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr("SQLite returned an invalid column count"));
        }
        if (is_final && (long long)column_count>k_max_array_elements) {
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr("SQLite result exceeds configured column limit"));
        }
        if (is_final) { rows=NULL; row_count=0; capacity=0; }
        for (;;) {
            status=k_sqlite_api.step(tracked->statement);
            if (status==101) break;
            if (status!=100) {
                char message[512];
                k_sqlite_copy_error(handle,status,message,sizeof(message));
                (void)k_sqlite_finalize(tracked);
                return kv_res(0,kv_cstr(message));
            }
            if (!is_final) continue;
            if ((long long)row_count>=k_max_array_elements) {
                (void)k_sqlite_finalize(tracked);
                return kv_res(0,kv_cstr("SQLite result exceeds configured row limit"));
            }
            if (row_count==capacity) {
                size_t next=capacity?capacity*2:8;
                if (next<capacity || (long long)next>k_max_array_elements) next=(size_t)k_max_array_elements;
                rows=k_sqlite_grow_rows(rows,capacity,next);
                capacity=next;
            }
            if ((size_t)column_count>SIZE_MAX/sizeof(KValue)) kfail("memory budget exceeded");
            KValue *cells=(KValue*)kalloc((size_t)column_count*sizeof(KValue));
            for (int column=0;column<column_count;column++) cells[column]=k_sqlite_column_value(tracked->statement,column);
            rows[row_count++]=kv_arr(cells,(size_t)column_count);
        }
        status=k_sqlite_finalize(tracked);
        if (status!=0) {
            char message[512];
            k_sqlite_copy_error(handle,status,message,sizeof(message));
            return kv_res(0,kv_cstr(message));
        }
        cursor=tail;
    }
    return kv_res(1,kv_arr(rows,row_count));
}

static KValue k_sqlite_close(KValue value) {
    KSQLiteHandle *handle=k_sqlite_handle(value);
    if (!handle) kfail("invalid SQLite handle");
    if (handle->closed || !handle->db) kfail("SQLite handle is already closed");
    handle->closed=1;
    int status=k_sqlite_api.close_v2(handle->db);
    if (status==0) handle->db=NULL;
    else {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        kfail(message);
    }
    return kv_nil();
}
`

// cRuntimeCrypto holds the extended cryptographic primitives for the native
// backend: SHA-512/384, SHA-1, MD5, AES-256-GCM, PBKDF2-HMAC-SHA-256,
// HKDF-SHA-256, constant-time comparison, XOR, and URL-safe base64. Every
// implementation is self-contained so native output links against no external
// crypto library and matches the Go interpreter byte for byte.
const cRuntimeCrypto = `
/* ---- SHA-512 / SHA-384 ------------------------------------------------- */
typedef struct { uint64_t h[8]; uint64_t len; unsigned char buf[128]; size_t n; } KSha512;
static const uint64_t k_sha512_k[80] = {
0x428a2f98d728ae22ULL,0x7137449123ef65cdULL,0xb5c0fbcfec4d3b2fULL,0xe9b5dba58189dbbcULL,
0x3956c25bf348b538ULL,0x59f111f1b605d019ULL,0x923f82a4af194f9bULL,0xab1c5ed5da6d8118ULL,
0xd807aa98a3030242ULL,0x12835b0145706fbeULL,0x243185be4ee4b28cULL,0x550c7dc3d5ffb4e2ULL,
0x72be5d74f27b896fULL,0x80deb1fe3b1696b1ULL,0x9bdc06a725c71235ULL,0xc19bf174cf692694ULL,
0xe49b69c19ef14ad2ULL,0xefbe4786384f25e3ULL,0x0fc19dc68b8cd5b5ULL,0x240ca1cc77ac9c65ULL,
0x2de92c6f592b0275ULL,0x4a7484aa6ea6e483ULL,0x5cb0a9dcbd41fbd4ULL,0x76f988da831153b5ULL,
0x983e5152ee66dfabULL,0xa831c66d2db43210ULL,0xb00327c898fb213fULL,0xbf597fc7beef0ee4ULL,
0xc6e00bf33da88fc2ULL,0xd5a79147930aa725ULL,0x06ca6351e003826fULL,0x142929670a0e6e70ULL,
0x27b70a8546d22ffcULL,0x2e1b21385c26c926ULL,0x4d2c6dfc5ac42aedULL,0x53380d139d95b3dfULL,
0x650a73548baf63deULL,0x766a0abb3c77b2a8ULL,0x81c2c92e47edaee6ULL,0x92722c851482353bULL,
0xa2bfe8a14cf10364ULL,0xa81a664bbc423001ULL,0xc24b8b70d0f89791ULL,0xc76c51a30654be30ULL,
0xd192e819d6ef5218ULL,0xd69906245565a910ULL,0xf40e35855771202aULL,0x106aa07032bbd1b8ULL,
0x19a4c116b8d2d0c8ULL,0x1e376c085141ab53ULL,0x2748774cdf8eeb99ULL,0x34b0bcb5e19b48a8ULL,
0x391c0cb3c5c95a63ULL,0x4ed8aa4ae3418acbULL,0x5b9cca4f7763e373ULL,0x682e6ff3d6b2b8a3ULL,
0x748f82ee5defb2fcULL,0x78a5636f43172f60ULL,0x84c87814a1f0ab72ULL,0x8cc702081a6439ecULL,
0x90befffa23631e28ULL,0xa4506cebde82bde9ULL,0xbef9a3f7b2c67915ULL,0xc67178f2e372532bULL,
0xca273eceea26619cULL,0xd186b8c721c0c207ULL,0xeada7dd6cde0eb1eULL,0xf57d4f7fee6ed178ULL,
0x06f067aa72176fbaULL,0x0a637dc5a2c898a6ULL,0x113f9804bef90daeULL,0x1b710b35131c471bULL,
0x28db77f523047d84ULL,0x32caab7b40c72493ULL,0x3c9ebe0a15c9bebcULL,0x431d67c49c100d4cULL,
0x4cc5d4becb3e42b6ULL,0x597f299cfc657e2aULL,0x5fcb6fab3ad6faecULL,0x6c44198c4a475817ULL};
#define K_ROTR64(x,n) (((x)>>(n))|((x)<<(64-(n))))
static void k_sha512_block(KSha512 *s, const unsigned char *p) {
    uint64_t w[80];
    for (int i=0;i<16;i++) {
        w[i]=0; for (int j=0;j<8;j++) w[i]=(w[i]<<8)|(uint64_t)p[i*8+j];
    }
    for (int i=16;i<80;i++) {
        uint64_t s0=K_ROTR64(w[i-15],1)^K_ROTR64(w[i-15],8)^(w[i-15]>>7);
        uint64_t s1=K_ROTR64(w[i-2],19)^K_ROTR64(w[i-2],61)^(w[i-2]>>6);
        w[i]=w[i-16]+s0+w[i-7]+s1;
    }
    uint64_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4],f=s->h[5],g=s->h[6],h=s->h[7];
    for (int i=0;i<80;i++) {
        uint64_t S1=K_ROTR64(e,14)^K_ROTR64(e,18)^K_ROTR64(e,41);
        uint64_t ch=(e&f)^((~e)&g);
        uint64_t t1=h+S1+ch+k_sha512_k[i]+w[i];
        uint64_t S0=K_ROTR64(a,28)^K_ROTR64(a,34)^K_ROTR64(a,39);
        uint64_t maj=(a&b)^(a&c)^(b&c);
        uint64_t t2=S0+maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e; s->h[5]+=f; s->h[6]+=g; s->h[7]+=h;
}
static void k_sha512_init(KSha512 *s, int is384) {
    if (is384) {
        s->h[0]=0xcbbb9d5dc1059ed8ULL; s->h[1]=0x629a292a367cd507ULL; s->h[2]=0x9159015a3070dd17ULL; s->h[3]=0x152fecd8f70e5939ULL;
        s->h[4]=0x67332667ffc00b31ULL; s->h[5]=0x8eb44a8768581511ULL; s->h[6]=0xdb0c2e0d64f98fa7ULL; s->h[7]=0x47b5481dbefa4fa4ULL;
    } else {
        s->h[0]=0x6a09e667f3bcc908ULL; s->h[1]=0xbb67ae8584caa73bULL; s->h[2]=0x3c6ef372fe94f82bULL; s->h[3]=0xa54ff53a5f1d36f1ULL;
        s->h[4]=0x510e527fade682d1ULL; s->h[5]=0x9b05688c2b3e6c1fULL; s->h[6]=0x1f83d9abfb41bd6bULL; s->h[7]=0x5be0cd19137e2179ULL;
    }
    s->len=0; s->n=0;
}
static void k_sha512_update(KSha512 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 128 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 128) { k_sha512_block(s, s->buf); s->n = 0; }
    }
}
static void k_sha512_final(KSha512 *s, unsigned char *out, int outlen) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_sha512_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 112) k_sha512_update(s, &z, 1);
    unsigned char lb[16];
    for (int i=0;i<8;i++) lb[i]=0;
    for (int i=0;i<8;i++) lb[8+i]=(unsigned char)(bits >> (56 - i*8));
    k_sha512_update(s, lb, 16);
    for (int i=0;i<8;i++) {
        out[i*8]=(unsigned char)(s->h[i]>>56); out[i*8+1]=(unsigned char)(s->h[i]>>48);
        out[i*8+2]=(unsigned char)(s->h[i]>>40); out[i*8+3]=(unsigned char)(s->h[i]>>32);
        out[i*8+4]=(unsigned char)(s->h[i]>>24); out[i*8+5]=(unsigned char)(s->h[i]>>16);
        out[i*8+6]=(unsigned char)(s->h[i]>>8); out[i*8+7]=(unsigned char)(s->h[i]);
    }
    (void)outlen;
}
static KValue k_crypto_sha512(KValue data) {
    KSha512 s; k_sha512_init(&s,0);
    k_sha512_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[64]; k_sha512_final(&s,out,64);
    return kv_bytesn((const char*)out,64);
}
static KValue k_crypto_sha384(KValue data) {
    KSha512 s; k_sha512_init(&s,1);
    k_sha512_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[64]; k_sha512_final(&s,out,64);
    return kv_bytesn((const char*)out,48);
}

/* ---- SHA-1 ------------------------------------------------------------- */
typedef struct { uint32_t h[5]; uint64_t len; unsigned char buf[64]; size_t n; } KSha1;
#define K_ROTL(x,n) (((x)<<(n))|((x)>>(32-(n))))
static void k_sha1_block(KSha1 *s, const unsigned char *p) {
    uint32_t w[80];
    for (int i=0;i<16;i++) w[i]=((uint32_t)p[i*4]<<24)|((uint32_t)p[i*4+1]<<16)|((uint32_t)p[i*4+2]<<8)|((uint32_t)p[i*4+3]);
    for (int i=16;i<80;i++) w[i]=K_ROTL(w[i-3]^w[i-8]^w[i-14]^w[i-16],1);
    uint32_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4];
    for (int i=0;i<80;i++) {
        uint32_t f,k;
        if (i<20) { f=(b&c)|((~b)&d); k=0x5a827999; }
        else if (i<40) { f=b^c^d; k=0x6ed9eba1; }
        else if (i<60) { f=(b&c)|(b&d)|(c&d); k=0x8f1bbcdc; }
        else { f=b^c^d; k=0xca62c1d6; }
        uint32_t t=K_ROTL(a,5)+f+e+k+w[i];
        e=d; d=c; c=K_ROTL(b,30); b=a; a=t;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e;
}
static void k_sha1_init(KSha1 *s) {
    s->h[0]=0x67452301; s->h[1]=0xefcdab89; s->h[2]=0x98badcfe; s->h[3]=0x10325476; s->h[4]=0xc3d2e1f0;
    s->len=0; s->n=0;
}
static void k_sha1_update(KSha1 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 64 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 64) { k_sha1_block(s, s->buf); s->n = 0; }
    }
}
static void k_sha1_final(KSha1 *s, unsigned char out[20]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_sha1_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 56) k_sha1_update(s, &z, 1);
    unsigned char lb[8];
    for (int i=0;i<8;i++) lb[i]=(unsigned char)(bits >> (56 - i*8));
    k_sha1_update(s, lb, 8);
    for (int i=0;i<5;i++) {
        out[i*4]=(unsigned char)(s->h[i]>>24); out[i*4+1]=(unsigned char)(s->h[i]>>16);
        out[i*4+2]=(unsigned char)(s->h[i]>>8); out[i*4+3]=(unsigned char)(s->h[i]);
    }
}
static KValue k_crypto_sha1(KValue data) {
    KSha1 s; k_sha1_init(&s);
    k_sha1_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[20]; k_sha1_final(&s,out);
    return kv_bytesn((const char*)out,20);
}

/* ---- MD5 --------------------------------------------------------------- */
typedef struct { uint32_t h[4]; uint64_t len; unsigned char buf[64]; size_t n; } KMd5;
#define K_ROTL32(x,n) (((x)<<(n))|((x)>>(32-(n))))
static const uint32_t k_md5_k[64] = {
0xd76aa478,0xe8c7b756,0x242070db,0xc1bdceee,0xf57c0faf,0x4787c62a,0xa8304613,0xfd469501,
0x698098d8,0x8b44f7af,0xffff5bb1,0x895cd7be,0x6b901122,0xfd987193,0xa679438e,0x49b40821,
0xf61e2562,0xc040b340,0x265e5a51,0xe9b6c7aa,0xd62f105d,0x02441453,0xd8a1e681,0xe7d3fbc8,
0x21e1cde6,0xc33707d6,0xf4d50d87,0x455a14ed,0xa9e3e905,0xfcefa3f8,0x676f02d9,0x8d2a4c8a,
0xfffa3942,0x8771f681,0x6d9d6122,0xfde5380c,0xa4beea44,0x4bdecfa9,0xf6bb4b60,0xbebfbc70,
0x289b7ec6,0xeaa127fa,0xd4ef3085,0x04881d05,0xd9d4d039,0xe6db99e5,0x1fa27cf8,0xc4ac5665,
0xf4292244,0x432aff97,0xab9423a7,0xfc93a039,0x655b59c3,0x8f0ccc92,0xffeff47d,0x85845dd1,
0x6fa87e4f,0xfe2ce6e0,0xa3014314,0x4e0811a1,0xf7537e82,0xbd3af235,0x2ad7d2bb,0xeb86d391};
static const int k_md5_s[64] = {
7,12,17,22,7,12,17,22,7,12,17,22,7,12,17,22,
5,9,14,20,5,9,14,20,5,9,14,20,5,9,14,20,
4,11,16,23,4,11,16,23,4,11,16,23,4,11,16,23,
6,10,15,21,6,10,15,21,6,10,15,21,6,10,15,21};
static void k_md5_block(KMd5 *s, const unsigned char *p) {
    uint32_t m[16];
    for (int i=0;i<16;i++) m[i]=((uint32_t)p[i*4])|((uint32_t)p[i*4+1]<<8)|((uint32_t)p[i*4+2]<<16)|((uint32_t)p[i*4+3]<<24);
    uint32_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3];
    for (int i=0;i<64;i++) {
        uint32_t f; int g;
        if (i<16) { f=(b&c)|((~b)&d); g=i; }
        else if (i<32) { f=(d&b)|((~d)&c); g=(5*i+1)%16; }
        else if (i<48) { f=b^c^d; g=(3*i+5)%16; }
        else { f=c^(b|(~d)); g=(7*i)%16; }
        uint32_t tmp=d; d=c; c=b;
        b=b+K_ROTL32(a+f+k_md5_k[i]+m[g],k_md5_s[i]);
        a=tmp;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d;
}
static void k_md5_init(KMd5 *s) {
    s->h[0]=0x67452301; s->h[1]=0xefcdab89; s->h[2]=0x98badcfe; s->h[3]=0x10325476;
    s->len=0; s->n=0;
}
static void k_md5_update(KMd5 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 64 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 64) { k_md5_block(s, s->buf); s->n = 0; }
    }
}
static void k_md5_final(KMd5 *s, unsigned char out[16]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_md5_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 56) k_md5_update(s, &z, 1);
    unsigned char lb[8];
    for (int i=0;i<8;i++) lb[i]=(unsigned char)(bits >> (i*8));
    k_md5_update(s, lb, 8);
    for (int i=0;i<4;i++) {
        out[i*4]=(unsigned char)(s->h[i]); out[i*4+1]=(unsigned char)(s->h[i]>>8);
        out[i*4+2]=(unsigned char)(s->h[i]>>16); out[i*4+3]=(unsigned char)(s->h[i]>>24);
    }
}
static KValue k_crypto_md5(KValue data) {
    KMd5 s; k_md5_init(&s);
    k_md5_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[16]; k_md5_final(&s,out);
    return kv_bytesn((const char*)out,16);
}

/* ---- AES-256-GCM ------------------------------------------------------- */
static const unsigned char k_aes_sbox[256] = {
0x63,0x7c,0x77,0x7b,0xf2,0x6b,0x6f,0xc5,0x30,0x01,0x67,0x2b,0xfe,0xd7,0xab,0x76,
0xca,0x82,0xc9,0x7d,0xfa,0x59,0x47,0xf0,0xad,0xd4,0xa2,0xaf,0x9c,0xa4,0x72,0xc0,
0xb7,0xfd,0x93,0x26,0x36,0x3f,0xf7,0xcc,0x34,0xa5,0xe5,0xf1,0x71,0xd8,0x31,0x15,
0x04,0xc7,0x23,0xc3,0x18,0x96,0x05,0x9a,0x07,0x12,0x80,0xe2,0xeb,0x27,0xb2,0x75,
0x09,0x83,0x2c,0x1a,0x1b,0x6e,0x5a,0xa0,0x52,0x3b,0xd6,0xb3,0x29,0xe3,0x2f,0x84,
0x53,0xd1,0x00,0xed,0x20,0xfc,0xb1,0x5b,0x6a,0xcb,0xbe,0x39,0x4a,0x4c,0x58,0xcf,
0xd0,0xef,0xaa,0xfb,0x43,0x4d,0x33,0x85,0x45,0xf9,0x02,0x7f,0x50,0x3c,0x9f,0xa8,
0x51,0xa3,0x40,0x8f,0x92,0x9d,0x38,0xf5,0xbc,0xb6,0xda,0x21,0x10,0xff,0xf3,0xd2,
0xcd,0x0c,0x13,0xec,0x5f,0x97,0x44,0x17,0xc4,0xa7,0x7e,0x3d,0x64,0x5d,0x19,0x73,
0x60,0x81,0x4f,0xdc,0x22,0x2a,0x90,0x88,0x46,0xee,0xb8,0x14,0xde,0x5e,0x0b,0xdb,
0xe0,0x32,0x3a,0x0a,0x49,0x06,0x24,0x5c,0xc2,0xd3,0xac,0x62,0x91,0x95,0xe4,0x79,
0xe7,0xc8,0x37,0x6d,0x8d,0xd5,0x4e,0xa9,0x6c,0x56,0xf4,0xea,0x65,0x7a,0xae,0x08,
0xba,0x78,0x25,0x2e,0x1c,0xa6,0xb4,0xc6,0xe8,0xdd,0x74,0x1f,0x4b,0xbd,0x8b,0x8a,
0x70,0x3e,0xb5,0x66,0x48,0x03,0xf6,0x0e,0x61,0x35,0x57,0xb9,0x86,0xc1,0x1d,0x9e,
0xe1,0xf8,0x98,0x11,0x69,0xd9,0x8e,0x94,0x9b,0x1e,0x87,0xe9,0xce,0x55,0x28,0xdf,
0x8c,0xa1,0x89,0x0d,0xbf,0xe6,0x42,0x68,0x41,0x99,0x2d,0x0f,0xb0,0x54,0xbb,0x16};
static const unsigned char k_aes_rcon[11] = {0x00,0x01,0x02,0x04,0x08,0x10,0x20,0x40,0x80,0x1b,0x36};
static unsigned char k_aes_xtime(unsigned char x) { return (unsigned char)((x<<1)^((x&0x80)?0x1b:0)); }
static void k_aes_expand_key(const unsigned char key[32], unsigned char rk[240]) {
    memcpy(rk,key,32);
    int bytes=32, rcon=1;
    unsigned char t[4];
    while (bytes<240) {
        memcpy(t,rk+bytes-4,4);
        if (bytes%32==0) {
            unsigned char tmp=t[0]; t[0]=k_aes_sbox[t[1]]^k_aes_rcon[rcon++]; t[1]=k_aes_sbox[t[2]]; t[2]=k_aes_sbox[t[3]]; t[3]=k_aes_sbox[tmp];
        } else if (bytes%32==16) {
            t[0]=k_aes_sbox[t[0]]; t[1]=k_aes_sbox[t[1]]; t[2]=k_aes_sbox[t[2]]; t[3]=k_aes_sbox[t[3]];
        }
        for (int i=0;i<4;i++) { rk[bytes]=rk[bytes-32]^t[i]; bytes++; }
    }
}
static void k_aes_encrypt_block(const unsigned char rk[240], const unsigned char in[16], unsigned char out[16]) {
    unsigned char s[16]; memcpy(s,in,16);
    for (int i=0;i<16;i++) s[i]^=rk[i];
    for (int round=1;round<=14;round++) {
        for (int i=0;i<16;i++) s[i]=k_aes_sbox[s[i]];
        unsigned char t[16];
        t[0]=s[0]; t[1]=s[5]; t[2]=s[10]; t[3]=s[15];
        t[4]=s[4]; t[5]=s[9]; t[6]=s[14]; t[7]=s[3];
        t[8]=s[8]; t[9]=s[13]; t[10]=s[2]; t[11]=s[7];
        t[12]=s[12]; t[13]=s[1]; t[14]=s[6]; t[15]=s[11];
        if (round!=14) {
            for (int c=0;c<4;c++) {
                unsigned char a0=t[c*4],a1=t[c*4+1],a2=t[c*4+2],a3=t[c*4+3];
                unsigned char x=a0^a1^a2^a3;
                s[c*4]=a0^x^k_aes_xtime(a0^a1);
                s[c*4+1]=a1^x^k_aes_xtime(a1^a2);
                s[c*4+2]=a2^x^k_aes_xtime(a2^a3);
                s[c*4+3]=a3^x^k_aes_xtime(a3^a0);
            }
        } else memcpy(s,t,16);
        for (int i=0;i<16;i++) s[i]^=rk[round*16+i];
    }
    memcpy(out,s,16);
}
static void k_gf_mul(unsigned char *x, const unsigned char *y) {
    unsigned char z[16]; memset(z,0,16);
    unsigned char v[16]; memcpy(v,y,16);
    for (int i=0;i<128;i++) {
        if (x[i>>3]>>(7-(i&7)) & 1) for (int j=0;j<16;j++) z[j]^=v[j];
        int lsb=v[15]&1;
        for (int j=15;j>0;j--) v[j]=(unsigned char)((v[j]>>1)|((v[j-1]&1)<<7));
        v[0]>>=1;
        if (lsb) v[0]^=0xe1;
    }
    memcpy(x,z,16);
}
static void k_ghash(const unsigned char H[16], const unsigned char *a, size_t alen, unsigned char out[16]) {
    unsigned char y[16]; memset(y,0,16);
    for (size_t i=0;i<alen;i+=16) {
        unsigned char blk[16]; memset(blk,0,16);
        size_t take = alen-i; if (take>16) take=16;
        memcpy(blk,a+i,take);
        for (int j=0;j<16;j++) y[j]^=blk[j];
        k_gf_mul(y,H);
    }
    memcpy(out,y,16);
}
static void k_gcm_inc32(unsigned char *c) {
    for (int i=15;i>=12;i--) { if (++c[i]!=0) break; }
}
static void k_gcm_ctr(const unsigned char rk[240], const unsigned char iv[12], const unsigned char *in, size_t len, unsigned char *out) {
    unsigned char ctr[16]; memset(ctr,0,16); memcpy(ctr,iv,12); ctr[15]=2;
    unsigned char ks[16];
    for (size_t i=0;i<len;i+=16) {
        k_aes_encrypt_block(rk,ctr,ks);
        size_t take=len-i; if (take>16) take=16;
        for (size_t j=0;j<take;j++) out[i+j]=in[i+j]^ks[j];
        k_gcm_inc32(ctr);
    }
}
static void k_gcm_tag(const unsigned char rk[240], const unsigned char H[16], const unsigned char iv[12], const unsigned char *ct, size_t ctlen, unsigned char tag[16]) {
    unsigned char j0[16]; memset(j0,0,16); memcpy(j0,iv,12); j0[15]=1;
    unsigned char s[16]; memset(s,0,16);
    unsigned char htmp[16]; k_ghash(H,ct,ctlen,htmp);
    for (int i=0;i<16;i++) s[i]^=htmp[i];
    unsigned char lenblk[16]; memset(lenblk,0,16);
    uint64_t bl = (uint64_t)ctlen*8;
    for (int i=0;i<8;i++) lenblk[8+i]=(unsigned char)(bl>>(56-i*8));
    for (int i=0;i<16;i++) s[i]^=lenblk[i];
    k_gf_mul(s,H);
    unsigned char ek[16]; k_aes_encrypt_block(rk,j0,ek);
    for (int i=0;i<16;i++) tag[i]=s[i]^ek[i];
}
static KValue k_crypto_aes_gcm_encrypt(KValue key, KValue nonce, KValue pt) {
    if (key.u.s.len!=32) return kv_res(0,kv_cstr("AES-256-GCM key must be 32 bytes"));
    if (nonce.u.s.len!=12) return kv_res(0,kv_cstr("AES-GCM nonce must be 12 bytes"));
    unsigned char rk[240]; k_aes_expand_key((const unsigned char*)key.u.s.data,rk);
    unsigned char H[16]; unsigned char zero[16]; memset(zero,0,16); k_aes_encrypt_block(rk,zero,H);
    size_t n=pt.u.s.len;
    unsigned char *ct=(unsigned char*)kalloc(n+16);
    k_gcm_ctr(rk,(const unsigned char*)nonce.u.s.data,(const unsigned char*)pt.u.s.data,n,ct);
    unsigned char tag[16]; k_gcm_tag(rk,H,(const unsigned char*)nonce.u.s.data,ct,n,tag);
    memcpy(ct+n,tag,16);
    return kv_res(1,kv_bytesn((const char*)ct,n+16));
}
static KValue k_crypto_aes_gcm_decrypt(KValue key, KValue nonce, KValue ct) {
    if (key.u.s.len!=32) return kv_res(0,kv_cstr("AES-256-GCM key must be 32 bytes"));
    if (nonce.u.s.len!=12) return kv_res(0,kv_cstr("AES-GCM nonce must be 12 bytes"));
    if (ct.u.s.len<16) return kv_res(0,kv_cstr("AES-GCM authentication failed"));
    unsigned char rk[240]; k_aes_expand_key((const unsigned char*)key.u.s.data,rk);
    unsigned char H[16]; unsigned char zero[16]; memset(zero,0,16); k_aes_encrypt_block(rk,zero,H);
    size_t n=ct.u.s.len-16;
    unsigned char tag[16]; k_gcm_tag(rk,H,(const unsigned char*)nonce.u.s.data,(const unsigned char*)ct.u.s.data,n,tag);
    unsigned char diff=0; for (int i=0;i<16;i++) diff|=(unsigned char)(tag[i]^((const unsigned char*)ct.u.s.data)[n+i]);
    if (diff) return kv_res(0,kv_cstr("AES-GCM authentication failed"));
    unsigned char *pt=(unsigned char*)kalloc(n+1);
    k_gcm_ctr(rk,(const unsigned char*)nonce.u.s.data,(const unsigned char*)ct.u.s.data,n,pt);
    return kv_res(1,kv_bytesn((const char*)pt,n));
}

/* ---- PBKDF2-HMAC-SHA-256 ----------------------------------------------- */
static void k_hmac_sha256_raw(const unsigned char *key, size_t keylen, const unsigned char *msg, size_t msglen, unsigned char out[32]) {
    unsigned char k[64]; memset(k,0,64);
    if (keylen>64) { KSha256 s; k_sha_init(&s); k_sha_update(&s,key,keylen); k_sha_final(&s,k); }
    else memcpy(k,key,keylen);
    unsigned char ipad[64],opad[64];
    for (int i=0;i<64;i++) { ipad[i]=k[i]^0x36; opad[i]=k[i]^0x5c; }
    KSha256 s; k_sha_init(&s); k_sha_update(&s,ipad,64); k_sha_update(&s,msg,msglen);
    unsigned char inner[32]; k_sha_final(&s,inner);
    k_sha_init(&s); k_sha_update(&s,opad,64); k_sha_update(&s,inner,32); k_sha_final(&s,out);
}
static KValue k_crypto_pbkdf2_sha256(KValue pw, KValue salt, KValue itv, KValue lenv) {
    long long it=itv.u.i, len=lenv.u.i;
    if (it<1) return kv_res(0,kv_cstr("PBKDF2 iterations must be at least 1"));
    if (len<1 || len>1048576) return kv_res(0,kv_cstr("PBKDF2 length must be between 1 and 1048576"));
    unsigned char *out=(unsigned char*)kalloc((size_t)len);
    size_t hlen=32; long long blocks=(len+hlen-1)/hlen;
    unsigned char *saltbuf=(unsigned char*)kalloc(salt.u.s.len+4);
    memcpy(saltbuf,salt.u.s.data,salt.u.s.len);
    long long produced=0;
    for (long long b=1;b<=blocks;b++) {
        saltbuf[salt.u.s.len]=(unsigned char)(b>>24); saltbuf[salt.u.s.len+1]=(unsigned char)(b>>16);
        saltbuf[salt.u.s.len+2]=(unsigned char)(b>>8); saltbuf[salt.u.s.len+3]=(unsigned char)(b);
        unsigned char u[32]; k_hmac_sha256_raw((const unsigned char*)pw.u.s.data,pw.u.s.len,saltbuf,salt.u.s.len+4,u);
        unsigned char t[32]; memcpy(t,u,32);
        for (long long i=1;i<it;i++) { k_hmac_sha256_raw((const unsigned char*)pw.u.s.data,pw.u.s.len,u,32,u); for (int j=0;j<32;j++) t[j]^=u[j]; }
        for (int j=0;j<32 && produced<len;j++) out[produced++]=t[j];
    }
    return kv_res(1,kv_bytesn((const char*)out,(size_t)len));
}

/* ---- HKDF-SHA-256 ------------------------------------------------------ */
static KValue k_crypto_hkdf_sha256(KValue ikm, KValue salt, KValue info, KValue lenv) {
    long long len=lenv.u.i;
    if (len<1 || len>8160) return kv_res(0,kv_cstr("HKDF length must be between 1 and 8160"));
    unsigned char zerosalt[32]; memset(zerosalt,0,32);
    const unsigned char *saltp = salt.u.s.len? (const unsigned char*)salt.u.s.data : zerosalt;
    size_t saltlen = salt.u.s.len? salt.u.s.len : 32;
    unsigned char prk[32]; k_hmac_sha256_raw(saltp,saltlen,(const unsigned char*)ikm.u.s.data,ikm.u.s.len,prk);
    unsigned char *out=(unsigned char*)kalloc((size_t)len);
    unsigned char t[32]; size_t tlen=0; long long produced=0; unsigned char counter=1;
    while (produced<len) {
        unsigned char *msg=(unsigned char*)kalloc(tlen+info.u.s.len+1);
        memcpy(msg,t,tlen); memcpy(msg+tlen,info.u.s.data,info.u.s.len); msg[tlen+info.u.s.len]=counter;
        k_hmac_sha256_raw(prk,32,msg,tlen+info.u.s.len+1,t);
        tlen=32;
        for (size_t j=0;j<32 && produced<len;j++) out[produced++]=t[j];
        counter++;
    }
    return kv_res(1,kv_bytesn((const char*)out,(size_t)len));
}

/* ---- constant-time compare, XOR, base64url ----------------------------- */
static KValue k_crypto_constant_time_equal(KValue a, KValue b) {
    if (a.u.s.len!=b.u.s.len) return kv_bool(0);
    unsigned char diff=0;
    for (size_t i=0;i<a.u.s.len;i++) diff|=(unsigned char)(a.u.s.data[i]^b.u.s.data[i]);
    return kv_bool(diff==0);
}
static KValue k_crypto_xor(KValue a, KValue b) {
    if (a.u.s.len!=b.u.s.len) return kv_res(0,kv_cstr("xor requires equal-length byte sequences"));
    size_t n=a.u.s.len; char *p=(char*)kalloc(n+1);
    for (size_t i=0;i<n;i++) p[i]=(char)(a.u.s.data[i]^b.u.s.data[i]);
    return kv_res(1,kv_bytesn(p,n));
}
static const char k_b64url[]="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
static KValue k_base64url_encode(KValue v) {
    size_t n=v.u.s.len; size_t olen=((n+2)/3)*4;
    char *p=(char*)kalloc(olen+1); size_t j=0;
    for (size_t i=0;i<n;i+=3) {
        unsigned int b0=(unsigned char)v.u.s.data[i];
        unsigned int b1=i+1<n?(unsigned char)v.u.s.data[i+1]:0;
        unsigned int b2=i+2<n?(unsigned char)v.u.s.data[i+2]:0;
        unsigned int t=(b0<<16)|(b1<<8)|b2;
        p[j++]=k_b64url[(t>>18)&0x3f]; p[j++]=k_b64url[(t>>12)&0x3f];
        if (i+1<n) p[j++]=k_b64url[(t>>6)&0x3f];
        if (i+2<n) p[j++]=k_b64url[t&0x3f];
    }
    p[j]=0;
    return kv_strn(p,j);
}
static KValue k_base64url_decode(KValue v) {
    size_t n=v.u.s.len;
    while (n>0 && v.u.s.data[n-1]=='=') n--;
    size_t olen=(n/4)*3+3; char *p=(char*)kalloc(olen+1); size_t j=0;
    unsigned int buf=0; int bits=0;
    for (size_t i=0;i<n;i++) {
        char c=v.u.s.data[i];
        const char *f=strchr(k_b64url,c);
        if (!f) return kv_res(0,kv_cstr("invalid base64url input"));
        buf=(buf<<6)|(unsigned int)(f-k_b64url); bits+=6;
        if (bits>=8) { bits-=8; p[j++]=(char)((buf>>bits)&0xff); }
    }
    return kv_res(1,kv_bytesn(p,j));
}

/* ---- obfuscated string literals ---------------------------------------- */
/* k_obf_str rebuilds a string literal that was XOR-masked at build time. The
   mask is derived from the literal's own length and index, so no key material
   is stored in the binary and the plaintext never appears in the executable. */
static KValue k_obf_str(const unsigned char *enc, size_t n) {
    char *p=(char*)kalloc(n+1);
    for (size_t i=0;i<n;i++) p[i]=(char)(enc[i] ^ (unsigned char)(0x5a + (i*31) + (n*7)));
    p[n]=0;
    return kv_strn(p,n);
}

/* ---- Python-style convenience builtins --------------------------------- */
static long long k_norm_index(long long i, long long len) {
    if (i<0) i+=len;
    if (i<0) return 0;
    if (i>len) return len;
    return i;
}
static KValue k_string_slice(KValue v, KValue s, KValue e) {
    size_t total=k_utf8_count(v.u.s.data,v.u.s.len);
    long long start=k_norm_index(s.u.i,(long long)total);
    long long end=k_norm_index(e.u.i,(long long)total);
    if (end<start) end=start;
    size_t b0=k_utf8_off(v.u.s.data,v.u.s.len,(size_t)start);
    size_t b1=k_utf8_off(v.u.s.data,v.u.s.len,(size_t)end);
    return kv_res(1,kv_strn(v.u.s.data+b0,b1-b0));
}
static KValue k_array_slice_range(KValue a, KValue s, KValue e) {
    if (a.tag!=K_ARRAY) kfail("array_slice_range expects Array[T]");
    long long len=(long long)a.u.a.len;
    long long start=k_norm_index(s.u.i,len);
    long long end=k_norm_index(e.u.i,len);
    if (end<start) end=start;
    long long cnt=end-start;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(cnt?cnt:1));
    memcpy(p,a.u.a.items+start,sizeof(KValue)*cnt);
    return kv_arr(p,(size_t)cnt);
}
static KValue k_string_format(KValue tmpl, KValue args) {
    if (args.tag!=K_ARRAY) kfail("string_format expects Array[String]");
    size_t cap=tmpl.u.s.len+1;
    for (size_t i=0;i<args.u.a.len;i++) cap+=args.u.a.items[i].u.s.len;
    char *out=(char*)kalloc(cap+1);
    size_t j=0, next=0;
    for (size_t i=0;i<tmpl.u.s.len;i++) {
        char c=tmpl.u.s.data[i];
        if (c=='{' && i+1<tmpl.u.s.len && tmpl.u.s.data[i+1]=='{') { out[j++]='{'; i++; continue; }
        if (c=='}' && i+1<tmpl.u.s.len && tmpl.u.s.data[i+1]=='}') { out[j++]='}'; i++; continue; }
        if (c=='{' && i+1<tmpl.u.s.len && tmpl.u.s.data[i+1]=='}') {
            if (next<args.u.a.len) {
                KValue a=args.u.a.items[next++];
                memcpy(out+j,a.u.s.data,a.u.s.len); j+=a.u.s.len;
            } else { out[j++]='{'; out[j++]='}'; }
            i++;
            continue;
        }
        out[j++]=c;
    }
    out[j]=0;
    return kv_strn(out,j);
}
static KValue k_array_indices(KValue a) {
    if (a.tag!=K_ARRAY) kfail("array_indices expects Array[T]");
    size_t n=a.u.a.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) p[i]=kv_int((long long)i);
    return kv_arr(p,n);
}
static KValue k_array_zip(KValue left, KValue right) {
    if (left.tag!=K_ARRAY || right.tag!=K_ARRAY) kfail("array_zip expects Array[T]");
    size_t n=left.u.a.len; if (right.u.a.len<n) n=right.u.a.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) {
        KValue *pair=(KValue*)kalloc(sizeof(KValue)*2);
        pair[0]=left.u.a.items[i]; pair[1]=right.u.a.items[i];
        p[i]=kv_arr(pair,2);
    }
    return kv_arr(p,n);
}
`
