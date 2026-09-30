package cruntime

const cRuntimeOutput = `/* ---- output ------------------------------------------------------------ */
static void k_print(KValue v, int newline) {
    KValue s = k_display(v);
    if (s.u.s.len>(size_t)LLONG_MAX) kfail("output limit exceeded");
    long long bytes=(long long)s.u.s.len+(newline?1:0);
    if (bytes>k_max_out || k_out>k_max_out-bytes) kfail("output limit exceeded");
    if (fwrite(s.u.s.data,1,s.u.s.len,stdout)!=s.u.s.len) kfail("stream failure");
    if (newline && fputc('\n',stdout)==EOF) kfail("stream failure");
    if (fflush(stdout)!=0) kfail("stream failure");
    k_out += bytes;
}
`
