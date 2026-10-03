package cruntime

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
static int k_json_string_key_compare(const void *left, const void *right) {
    const KValue *a=(const KValue*)left, *b=(const KValue*)right;
    size_t n=a->u.s.len<b->u.s.len?a->u.s.len:b->u.s.len;
    int order=memcmp(a->u.s.data,b->u.s.data,n);
    if (order) return order;
    return a->u.s.len<b->u.s.len?-1:(a->u.s.len>b->u.s.len?1:0);
}
static KValue k_json_object_keys(KValue value) {
    KValue node=k_json_unwrap(value);
    if (node.tag!=K_MAP) return kv_res(0,kv_cstr("JSON value is not an object"));
    if (k_max_array_elements<0 || node.u.m.len>(unsigned long long)k_max_array_elements)
        return kv_res(0,kv_cstr("JSON object key count exceeds configured limit"));
    size_t count=node.u.m.len;
    KValue *keys=(KValue*)kalloc(sizeof(KValue)*(count?count:1));
    if (count) memcpy(keys,node.u.m.keys,sizeof(KValue)*count);
    qsort(keys,count,sizeof(KValue),k_json_string_key_compare);
    return kv_res(1,kv_arr(keys,count));
}
static int k_json_field_set_valid(KValue fields) {
    if (fields.tag!=K_STRING) return 0;
    if (!fields.u.s.len) return 1;
    if (fields.u.s.len<3 || fields.u.s.data[0]!='|' || fields.u.s.data[fields.u.s.len-1]!='|') return 0;
    for (size_t i=1;i<fields.u.s.len;i++) if (fields.u.s.data[i]=='|' && fields.u.s.data[i-1]=='|') return 0;
    return 1;
}
static int k_json_field_set_contains(KValue fields, KValue key) {
    if (key.tag!=K_STRING || !key.u.s.len || !fields.u.s.len || memchr(key.u.s.data,'|',key.u.s.len)) return 0;
    size_t wanted=key.u.s.len+2;
    for (size_t i=0;i+wanted<=fields.u.s.len;i++) {
        if (fields.u.s.data[i]=='|' && !memcmp(fields.u.s.data+i+1,key.u.s.data,key.u.s.len) && fields.u.s.data[i+wanted-1]=='|') return 1;
    }
    return 0;
}
static KValue k_json_object_keys_allowed(KValue value, KValue allowed) {
    if (!k_json_field_set_valid(allowed)) return kv_bool(0);
    KValue object=k_json_unwrap(value);
    if (object.tag!=K_MAP) return kv_bool(0);
    for (size_t i=0;i<object.u.m.len;i++) {
        if (!k_json_field_set_contains(allowed,object.u.m.keys[i])) return kv_bool(0);
    }
    return kv_bool(1);
}
static KValue k_json_object_fields_empty_except(KValue value, KValue candidates, KValue allowed, KValue empty_arrays) {
    if (!k_json_field_set_valid(candidates) || !k_json_field_set_valid(allowed) || !k_json_field_set_valid(empty_arrays)) return kv_bool(0);
    KValue object=k_json_unwrap(value);
    if (object.tag!=K_MAP) return kv_bool(0);
    for (size_t i=1;i+1<candidates.u.s.len;) {
        size_t end=i; while (end<candidates.u.s.len && candidates.u.s.data[end]!='|') end++;
        if (end==i || end>=candidates.u.s.len) return kv_bool(0);
        KValue key=kv_nil(), child=kv_nil(); key.tag=K_STRING; key.u.s.data=candidates.u.s.data+i; key.u.s.len=end-i; int exists=0;
        for (size_t j=0;j<object.u.m.len;j++) if (object.u.m.keys[j].u.s.len==key.u.s.len && !memcmp(object.u.m.keys[j].u.s.data,key.u.s.data,key.u.s.len)) { child=object.u.m.vals[j]; exists=1; break; }
        if (exists && !k_json_field_set_contains(allowed,key) && child.tag!=K_NIL && !(k_json_field_set_contains(empty_arrays,key) && child.tag==K_ARRAY && child.u.a.len==0)) return kv_bool(0);
        i=end+1;
    }
    return kv_bool(1);
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
