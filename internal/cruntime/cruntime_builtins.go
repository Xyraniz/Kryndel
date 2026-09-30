package cruntime

// Runtime builtins and UTF-8 helpers.
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
