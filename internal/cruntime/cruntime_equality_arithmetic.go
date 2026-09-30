package cruntime

const cRuntimeEqualityArithmetic = `/* ---- equality ---------------------------------------------------------- */
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

`
