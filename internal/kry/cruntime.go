package kry

// cRuntimePrelude is the embedded C runtime that every native Kryndel program
// links against. It implements the same value model, checked arithmetic,
// deterministic display, and pure builtins as the Go interpreter so that
// native output matches interpreted output byte for byte.
//
// Values are immutable and arena-allocated; because Kryndel collections are
// immutable, sharing value pointers is safe and no deep copy is required.
const cRuntimePrelude = `
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
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
#include <direct.h>
#include <io.h>
#include <process.h>
#include <windows.h>
#define k_mkdir(p) _mkdir(p)
#define k_rmdir(p) _rmdir(p)
static char *k_realpath(const char *p, char *buf) { return _fullpath(buf, p, 4096); }
static void k_sleep_ms(long long m) { Sleep((DWORD)m); }
#else
#include <sys/wait.h>
#define k_mkdir(p) mkdir((p), 0755)
#define k_rmdir(p) rmdir(p)
static char *k_realpath(const char *p, char *buf) { return realpath(p, buf); }
static void k_sleep_ms(long long m) { struct timespec ts; ts.tv_sec=m/1000; ts.tv_nsec=(m%1000)*1000000LL; nanosleep(&ts,NULL); }
#endif

/* ---- error handling ---------------------------------------------------- */
static jmp_buf k_jmp;
static char k_errbuf[512];
static long long k_mem = 0;
static long long k_max_mem = 268435456LL;
static long long k_out = 0;
static long long k_max_out = 16777216LL;

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
typedef struct { char *data; size_t len; } KStr;
typedef struct { KValue *items; size_t len; } KArr;
typedef struct { KValue *keys; KValue *vals; size_t len; } KMap;
typedef struct { int type_id; KValue *fields; } KStruct;
typedef struct { int type_id; int variant; } KEnum;

struct KValue {
    int tag;
    union {
        long long i;
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
    } u;
};

enum { K_NIL=0, K_INT, K_FLOAT, K_BOOL, K_STRING, K_BYTES, K_ARRAY,
       K_STRUCT, K_ENUM, K_OPTION, K_RESULT, K_MAP, K_SET, K_JSON,
       K_SHARED, K_ACTOR, K_THREAD, K_TASKGROUP, K_CHANNEL };

static KValue kv_nil(void) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_NIL; return v; }
static KValue kv_int(long long x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_INT; v.u.i=x; return v; }
static KValue kv_float(double x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_FLOAT; v.u.f=x; return v; }
static KValue kv_bool(int x) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_BOOL; v.u.b=x?1:0; return v; }
static KValue kv_strn(const char *s, size_t n) {
    KValue v; memset(&v,0,sizeof(v)); v.tag=K_STRING;
    v.u.s.data = (char*)kalloc(n+1); memcpy(v.u.s.data,s,n); v.u.s.data[n]=0; v.u.s.len=n; return v;
}
static KValue kv_cstr(const char *s) { return kv_strn(s, strlen(s)); }
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
#define K_MAX_DEFERS 512
static void (*k_defers[K_MAX_DEFERS])(void);
static int k_ndefers = 0;
`

// cRuntimeDisplay holds the display/equality/arithmetic half of the runtime.
const cRuntimeDisplay = `
/* ---- string builder ---------------------------------------------------- */
typedef struct { char *buf; size_t len; size_t cap; } KBuf;
static void kb_init(KBuf *b) { b->cap=64; b->len=0; b->buf=(char*)kalloc(b->cap); b->buf[0]=0; }
static void kb_grow(KBuf *b) { b->cap*=2; char *n=(char*)kalloc(b->cap); memcpy(n,b->buf,b->len); b->buf=n; }
static void kb_putc(KBuf *b, char c) { if (b->len+1>=b->cap) kb_grow(b); b->buf[b->len++]=c; b->buf[b->len]=0; }
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
    case K_FLOAT: k_fmt_float(v.u.f,num,sizeof(num)); kb_puts(b,num); break;
    case K_BOOL: kb_puts(b, v.u.b?"true":"false"); break;
    case K_STRING: case K_JSON: kb_putn(b,v.u.s.data,v.u.s.len); break;
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
    default: kb_puts(b,"<invalid>"); break;
    }
}

static KValue k_display(KValue v) {
    KBuf b; kb_init(&b); k_disp(&b,v);
    return kv_strn(b.buf,b.len);
}

/* ---- equality ---------------------------------------------------------- */
static int k_equal(KValue a, KValue b) {
    if (a.tag != b.tag) return 0;
    switch (a.tag) {
    case K_NIL: return 1;
    case K_INT: return a.u.i==b.u.i;
    case K_FLOAT: return a.u.f==b.u.f;
    case K_BOOL: return a.u.b==b.u.b;
    case K_STRING: case K_JSON: case K_BYTES:
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
    if (a==0||b==0) return 0;
    if ((a==LLONG_MIN&&b==-1)||(b==LLONG_MIN&&a==-1)) kfail("checked integer arithmetic overflow");
    long long z=a*b;
    if (z/b!=a) kfail("checked integer arithmetic overflow");
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
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { double z=a.u.f-b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_mul(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_mul_i(a.u.i,b.u.i));
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { double z=a.u.f*b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_div(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_div_i(a.u.i,b.u.i));
    if (a.tag==K_FLOAT && b.tag==K_FLOAT) { if (b.u.f==0) kfail("floating division by zero"); double z=a.u.f/b.u.f; if(!isfinite(z)) kfail("floating-point result must be finite"); return kv_float(z); }
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_rem(KValue a, KValue b) {
    if (a.tag==K_INT && b.tag==K_INT) return kv_int(k_rem_i(a.u.i,b.u.i));
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
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f<b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_le(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i<=b.u.i);
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f<=b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_gt(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i>b.u.i);
    if (a.tag==K_FLOAT&&b.tag==K_FLOAT) return kv_bool(a.u.f>b.u.f);
    kfail("operator operands have incompatible types"); return kv_nil();
}
static KValue k_ge(KValue a, KValue b) {
    if (a.tag==K_INT&&b.tag==K_INT) return kv_bool(a.u.i>=b.u.i);
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
    if (k_out + (long long)s.u.s.len + 1 > k_max_out) kfail("output limit exceeded");
    k_out += (long long)s.u.s.len;
    fwrite(s.u.s.data,1,s.u.s.len,stdout);
    if (newline) fputc('\n',stdout);
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
static KValue k_fs_read_text(KValue path) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (!f) return kv_res(0,kv_cstr("cannot open file"));
    fseek(f,0,SEEK_END); long n=ftell(f); fseek(f,0,SEEK_SET);
    if (n<0) { fclose(f); return kv_res(0,kv_cstr("cannot read file")); }
    char *buf=(char*)kalloc((size_t)n+1);
    size_t got=fread(buf,1,(size_t)n,f); fclose(f);
    buf[got]=0;
    if (!k_utf8_valid(buf,got)) return kv_res(0,kv_cstr("file is not valid UTF-8"));
    return kv_res(1,kv_strn(buf,got));
}
static KValue k_fs_write_text(KValue path, KValue text) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"wb");
    if (!f) return kv_res(0,kv_cstr("cannot open file for writing"));
    fwrite(text.u.s.data,1,text.u.s.len,f); fclose(f);
    return kv_res(1,kv_nil());
}
static KValue k_fs_read_bytes(KValue path) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (!f) return kv_res(0,kv_cstr("cannot open file"));
    fseek(f,0,SEEK_END); long n=ftell(f); fseek(f,0,SEEK_SET);
    if (n<0) { fclose(f); return kv_res(0,kv_cstr("cannot read file")); }
    char *buf=(char*)kalloc((size_t)n+1);
    size_t got=fread(buf,1,(size_t)n,f); fclose(f);
    return kv_res(1,kv_bytesn(buf,got));
}
static KValue k_fs_write_bytes(KValue path, KValue data) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"wb");
    if (!f) return kv_res(0,kv_cstr("cannot open file for writing"));
    fwrite(data.u.s.data,1,data.u.s.len,f); fclose(f);
    return kv_res(1,kv_nil());
}
static KValue k_fs_exists(KValue path) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (f) { fclose(f); return kv_bool(1); }
    return kv_bool(0);
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
        switch (c) {
            case '"': kb_puts(b,"\\\""); break;
            case '\\': kb_puts(b,"\\\\"); break;
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
static void k_json_write(KBuf *b, KValue v) {
    switch (v.tag) {
        case K_NIL: kb_puts(b,"null"); break;
        case K_BOOL: kb_puts(b, v.u.b?"true":"false"); break;
        case K_INT: { char t[32]; snprintf(t,sizeof(t),"%lld",v.u.i); kb_puts(b,t); break; }
        case K_FLOAT: k_json_float(b, v.u.f); break;
        case K_STRING: case K_JSON: k_json_escape(b, v.u.s.data, v.u.s.len); break;
        case K_BYTES: k_json_escape(b, v.u.s.data, v.u.s.len); break;
        case K_ARRAY: case K_SET: {
            kb_putc(b,'[');
            for (size_t i=0;i<v.u.a.len;i++) { if (i) kb_putc(b,','); k_json_write(b, v.u.a.items[i]); }
            kb_putc(b,']'); break;
        }
        case K_MAP: {
            /* Go's json.Marshal sorts object keys; sort by rendered key string. */
            size_t n=v.u.m.len;
            size_t *idx=(size_t*)kalloc(sizeof(size_t)*(n?n:1));
            for (size_t i=0;i<n;i++) idx[i]=i;
            for (size_t i=0;i<n;i++) for (size_t j=i+1;j<n;j++) {
                KBuf ka,kb; kb_init(&ka); kb_init(&kb);
                k_json_write(&ka, v.u.m.keys[idx[i]]); k_json_write(&kb, v.u.m.keys[idx[j]]);
                if (strcmp(ka.buf,kb.buf)>0) { size_t t=idx[i]; idx[i]=idx[j]; idx[j]=t; }
            }
            kb_putc(b,'{');
            for (size_t i=0;i<n;i++) {
                if (i) kb_putc(b,',');
                k_json_write(b, v.u.m.keys[idx[i]]);
                kb_putc(b,':');
                k_json_write(b, v.u.m.vals[idx[i]]);
            }
            kb_putc(b,'}'); break;
        }
        case K_OPTION: if (v.u.opt.present) k_json_write(b,*v.u.opt.inner); else kb_puts(b,"null"); break;
        case K_RESULT: k_json_write(b,*v.u.res.inner); break;
        default: kb_puts(b,"null");
    }
}
static KValue k_json_stringify(KValue v) {
    KBuf b; kb_init(&b); k_json_write(&b, v);
    return kv_strn(b.buf, b.len);
}

typedef struct { const char *p; size_t n; size_t i; } KJson;
static void k_json_ws(KJson *j) { while (j->i<j->n) { char c=j->p[j->i]; if (c==' '||c=='\t'||c=='\n'||c=='\r') j->i++; else break; } }
static KValue k_json_value(KJson *j);
static KValue k_json_string(KJson *j) {
    j->i++; /* opening quote */
    KBuf b; kb_init(&b);
    while (j->i<j->n) {
        char c=j->p[j->i++];
        if (c=='"') return kv_strn(b.buf,b.len);
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
                    if (j->i+4>j->n) kfail("invalid JSON string escape");
                    char h[5]; memcpy(h,j->p+j->i,4); h[4]=0; j->i+=4;
                    unsigned cp=(unsigned)strtoul(h,NULL,16);
                    if (cp>=0xD800 && cp<=0xDBFF && j->i+6<=j->n && j->p[j->i]=='\\' && j->p[j->i+1]=='u') {
                        char h2[5]; memcpy(h2,j->p+j->i+2,4); h2[4]=0;
                        unsigned lo=(unsigned)strtoul(h2,NULL,16);
                        if (lo>=0xDC00 && lo<=0xDFFF) { cp=0x10000+((cp-0xD800)<<10)+(lo-0xDC00); j->i+=6; }
                    }
                    if (cp<0x80) kb_putc(&b,(char)cp);
                    else if (cp<0x800) { kb_putc(&b,(char)(0xC0|(cp>>6))); kb_putc(&b,(char)(0x80|(cp&0x3F))); }
                    else if (cp<0x10000) { kb_putc(&b,(char)(0xE0|(cp>>12))); kb_putc(&b,(char)(0x80|((cp>>6)&0x3F))); kb_putc(&b,(char)(0x80|(cp&0x3F))); }
                    else { kb_putc(&b,(char)(0xF0|(cp>>18))); kb_putc(&b,(char)(0x80|((cp>>12)&0x3F))); kb_putc(&b,(char)(0x80|((cp>>6)&0x3F))); kb_putc(&b,(char)(0x80|(cp&0x3F))); }
                    break;
                }
                default: kfail("invalid JSON string escape");
            }
        } else kb_putc(&b,c);
    }
    kfail("unterminated JSON string");
    return kv_nil();
}
static KValue k_json_number(KJson *j) {
    size_t start=j->i; int isf=0;
    if (j->i<j->n && (j->p[j->i]=='-'||j->p[j->i]=='+')) j->i++;
    while (j->i<j->n) {
        char c=j->p[j->i];
        if (c>='0'&&c<='9') j->i++;
        else if (c=='.'||c=='e'||c=='E'||c=='+'||c=='-') { isf=1; j->i++; }
        else break;
    }
    char tmp[64]; size_t len=j->i-start; if (len>=sizeof(tmp)) len=sizeof(tmp)-1;
    memcpy(tmp,j->p+start,len); tmp[len]=0;
    if (isf) return kv_float(strtod(tmp,NULL));
    return kv_int(strtoll(tmp,NULL,10));
}
static KValue k_json_value(KJson *j) {
    k_json_ws(j);
    if (j->i>=j->n) kfail("unexpected end of JSON");
    char c=j->p[j->i];
    if (c=='{') {
        j->i++; k_json_ws(j);
        KValue *keys=(KValue*)kalloc(sizeof(KValue)*8); KValue *vals=(KValue*)kalloc(sizeof(KValue)*8);
        size_t cap=8,n=0;
        if (j->i<j->n && j->p[j->i]=='}') { j->i++; return kv_map(keys,vals,0); }
        for (;;) {
            k_json_ws(j);
            if (j->i>=j->n || j->p[j->i]!='"') kfail("invalid JSON object key");
            KValue key=k_json_string(j);
            k_json_ws(j);
            if (j->i>=j->n || j->p[j->i]!=':') kfail("invalid JSON object");
            j->i++;
            KValue val=k_json_value(j);
            if (n==cap) { size_t nc=cap*2; KValue *nk=(KValue*)kalloc(sizeof(KValue)*nc); KValue *nv=(KValue*)kalloc(sizeof(KValue)*nc); memcpy(nk,keys,sizeof(KValue)*n); memcpy(nv,vals,sizeof(KValue)*n); keys=nk; vals=nv; cap=nc; }
            keys[n]=key; vals[n]=val; n++;
            k_json_ws(j);
            if (j->i<j->n && j->p[j->i]==',') { j->i++; continue; }
            if (j->i<j->n && j->p[j->i]=='}') { j->i++; break; }
            kfail("invalid JSON object");
        }
        return kv_map(keys,vals,n);
    }
    if (c=='[') {
        j->i++; k_json_ws(j);
        KValue *items=(KValue*)kalloc(sizeof(KValue)*8); size_t cap=8,n=0;
        if (j->i<j->n && j->p[j->i]==']') { j->i++; return kv_arr(items,0); }
        for (;;) {
            KValue val=k_json_value(j);
            if (n==cap) { size_t nc=cap*2; KValue *ni=(KValue*)kalloc(sizeof(KValue)*nc); memcpy(ni,items,sizeof(KValue)*n); items=ni; cap=nc; }
            items[n++]=val;
            k_json_ws(j);
            if (j->i<j->n && j->p[j->i]==',') { j->i++; continue; }
            if (j->i<j->n && j->p[j->i]==']') { j->i++; break; }
            kfail("invalid JSON array");
        }
        return kv_arr(items,n);
    }
    if (c=='"') return k_json_string(j);
    if (c=='t') { if (j->i+4<=j->n && !memcmp(j->p+j->i,"true",4)) { j->i+=4; return kv_bool(1); } kfail("invalid JSON literal"); }
    if (c=='f') { if (j->i+5<=j->n && !memcmp(j->p+j->i,"false",5)) { j->i+=5; return kv_bool(0); } kfail("invalid JSON literal"); }
    if (c=='n') { if (j->i+4<=j->n && !memcmp(j->p+j->i,"null",4)) { j->i+=4; return kv_nil(); } kfail("invalid JSON literal"); }
    return k_json_number(j);
}
static KValue k_json_parse(KValue text) {
    KJson j; j.p=text.u.s.data; j.n=text.u.s.len; j.i=0;
    KValue v;
    /* Parse under a nested error guard so malformed input yields err(...). */
    jmp_buf saved; memcpy(&saved,&k_jmp,sizeof(jmp_buf));
    if (setjmp(k_jmp)) { memcpy(&k_jmp,&saved,sizeof(jmp_buf)); return kv_res(0,kv_cstr("invalid JSON")); }
    v = k_json_value(&j);
    k_json_ws(&j);
    if (j.i != j.n) { memcpy(&k_jmp,&saved,sizeof(jmp_buf)); return kv_res(0,kv_cstr("invalid JSON")); }
    memcpy(&k_jmp,&saved,sizeof(jmp_buf));
    return kv_res(1,v);
}

/* ---- extended filesystem ----------------------------------------------- */
static char *k_cpath(KValue path) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0; return p;
}
static KValue k_fs_read_dir(KValue path) {
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
    return kv_res(1, kv_arr(items,n));
}
static KValue k_fs_create_dir(KValue path) {
    char *p=k_cpath(path);
    if (k_mkdir(p)!=0) return kv_res(0, kv_cstr("cannot create directory"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_create_dir_all(KValue path) {
    char *p=k_cpath(path);
    for (char *q=p+1; *q; q++) {
        if (*q=='/') { *q=0; k_mkdir(p); *q='/'; }
    }
    if (k_mkdir(p)!=0) { struct stat st; if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot create directory")); }
    return kv_res(1, kv_nil());
}
static KValue k_fs_remove_file(KValue path) {
    char *p=k_cpath(path);
    if (remove(p)!=0) return kv_res(0, kv_cstr("cannot remove file"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_remove_dir_all(KValue path) {
    char *p=k_cpath(path);
    DIR *d=opendir(p);
    if (d) {
        struct dirent *e;
        while ((e=readdir(d))) {
            if (!strcmp(e->d_name,".")||!strcmp(e->d_name,"..")) continue;
            size_t pl=strlen(p), nl=strlen(e->d_name);
            char *child=(char*)kalloc(pl+nl+2); memcpy(child,p,pl); child[pl]='/'; memcpy(child+pl+1,e->d_name,nl+1);
            struct stat st;
            if (stat(child,&st)==0 && S_ISDIR(st.st_mode)) k_fs_remove_dir_all(kv_cstr(child));
            else remove(child);
        }
        closedir(d);
    }
    if (k_rmdir(p)!=0) return kv_res(0, kv_cstr("cannot remove directory"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_copy_file(KValue src, KValue dst) {
    char *s=k_cpath(src), *d=k_cpath(dst);
    FILE *in=fopen(s,"rb"); if (!in) return kv_res(0, kv_cstr("cannot open source file"));
    FILE *out=fopen(d,"wb"); if (!out) { fclose(in); return kv_res(0, kv_cstr("cannot open destination file")); }
    char buf[8192]; size_t got;
    while ((got=fread(buf,1,sizeof(buf),in))>0) fwrite(buf,1,got,out);
    fclose(in); fclose(out);
    return kv_res(1, kv_nil());
}
static KValue k_fs_move_file(KValue src, KValue dst) {
    char *s=k_cpath(src), *d=k_cpath(dst);
    if (rename(s,d)!=0) return kv_res(0, kv_cstr("cannot move file"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_is_file(KValue path) {
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_bool(0);
    return kv_bool(S_ISREG(st.st_mode));
}
static KValue k_fs_is_dir(KValue path) {
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_bool(0);
    return kv_bool(S_ISDIR(st.st_mode));
}
static KValue k_fs_file_size(KValue path) {
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot stat file"));
    return kv_res(1, kv_int((long long)st.st_size));
}
static KValue k_fs_file_modified_time(KValue path) {
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot stat file"));
    return kv_res(1, kv_int((long long)st.st_mtime));
}
static KValue k_fs_join_path(KValue base, KValue parts) {
    KBuf b; kb_init(&b);
    kb_putn(&b, base.u.s.data, base.u.s.len);
    for (size_t i=0;i<parts.u.a.len;i++) {
        KValue p=parts.u.a.items[i];
        if (b.len>0 && b.buf[b.len-1]!='/') kb_putc(&b,'/');
        kb_putn(&b, p.u.s.data, p.u.s.len);
    }
    return kv_strn(b.buf,b.len);
}
static KValue k_fs_absolute_path(KValue path) {
    char *p=k_cpath(path);
    char buf[4096];
    if (k_realpath(p,buf)) return kv_res(1, kv_cstr(buf));
    if (p[0]=='/') return kv_res(1, kv_cstr(p));
    char cwd[4096];
    if (!getcwd(cwd,sizeof(cwd))) return kv_res(0, kv_cstr("cannot resolve path"));
    KBuf b; kb_init(&b); kb_puts(&b,cwd); kb_putc(&b,'/'); kb_puts(&b,p);
    return kv_res(1, kv_strn(b.buf,b.len));
}
static KValue k_fs_temp_dir(KValue unused) {
    (void)unused;
    const char *t=getenv("TMPDIR"); if (!t||!*t) t="/tmp";
    return kv_cstr(t);
}
static KValue k_fs_temp_file(KValue prefix) {
    char *p=k_cpath(prefix);
    const char *t=getenv("TMPDIR"); if (!t||!*t) t="/tmp";
    char tmpl[4096];
    snprintf(tmpl,sizeof(tmpl),"%s/%s-XXXXXX",t,p);
#ifdef _WIN32
    if (_mktemp_s(tmpl, sizeof(tmpl)) != 0) return kv_res(0, kv_cstr("cannot create temp file"));
    FILE *tf = fopen(tmpl, "wb");
    if (!tf) return kv_res(0, kv_cstr("cannot create temp file"));
    fclose(tf);
#else
    int fd=mkstemp(tmpl);
    if (fd<0) return kv_res(0, kv_cstr("cannot create temp file"));
    close(fd);
#endif
    return kv_res(1, kv_cstr(tmpl));
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
`
