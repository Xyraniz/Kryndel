package cruntime

const cRuntimeDisplay = `/* ---- display ----------------------------------------------------------- */
static void k_disp(KBuf *b, KValue v);

static void k_disp(KBuf *b, KValue v) {
    char num[64];
    switch (v.tag) {
    case K_NIL: kb_puts(b,"nil"); break;
    case K_INT: snprintf(num,sizeof(num),"%lld",v.u.i); kb_puts(b,num); break;
    case K_UINT: snprintf(num,sizeof(num),"%llu",v.u.u64); kb_puts(b,num); break;
    case K_FLOAT: k_fmt_float(v.u.f,num,sizeof(num)); kb_puts(b,num); break;
    case K_BOOL: kb_puts(b, v.u.b?"true":"false"); break;
    case K_STRING: case K_JSON: kb_putn(b,v.u.s.data,v.u.s.len); break;
    case K_JSON_NUMBER: kb_putn(b,v.u.s.data,v.u.s.len); break;
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
    case K_TCP: kb_puts(b,"<TcpSocket>"); break;
    default: kb_puts(b,"<invalid>"); break;
    }
}

static KValue k_display(KValue v) {
    KBuf b; kb_init(&b); k_disp(&b,v);
    return kv_str_take(b.buf,b.len);
}

`
