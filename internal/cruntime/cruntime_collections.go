package cruntime

// Named runtime-polymorphism dispatch slots.
const cRuntimeDispatch = `/* ---- runtime polymorphism (dispatch slots) ----------------------------- */
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

`

// Extended string, collection, and math builtins.
const cRuntimeCollections = `/* ---- extended string, array, collection and math builtins -------------- */
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
