package cruntime

// Array, map, string, bytes indexing, struct field access, and iteration.
const cRuntimeIndex = `
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

`
