package cruntime

const cRuntimeStrings = `
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

`
