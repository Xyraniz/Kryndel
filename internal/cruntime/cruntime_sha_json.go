package cruntime

// SHA-256 and HMAC-SHA-256 primitives required by JSON and artifact helpers.
const cRuntimeSHA256 = `/* ---- SHA-256 ----------------------------------------------------------- */
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

`

// Canonical JSON serialization, parsing, and typed JSON accessors.
const cRuntimeJSON = `/* ---- JSON -------------------------------------------------------------- */
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

`
