package cruntime

const cRuntimeEquality = `/* ---- equality ---------------------------------------------------------- */
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

`
