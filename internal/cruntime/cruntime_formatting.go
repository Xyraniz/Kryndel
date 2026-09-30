package cruntime

const cRuntimeFormatting = `
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

`
