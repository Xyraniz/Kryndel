package cruntime

// Process execution and timing builtins.
const cRuntimeProcess = `/* ---- process and timing ------------------------------------------------ */
static KValue k_sleep(KValue ms) {
    long long m = ms.u.i;
    if (m < 0) return kv_res(0, kv_cstr("sleep duration must be non-negative"));
    if (k_runtime_deadline_ms) {
        unsigned long long now=k_tcp_now_ms();
        if (now>=k_runtime_deadline_ms) return kv_res(0,kv_cstr("sleep cancelled"));
        unsigned long long remaining=k_runtime_deadline_ms-now;
        if ((unsigned long long)m>=remaining) {
            k_sleep_ms((long long)remaining);
            return kv_res(0,kv_cstr("sleep cancelled"));
        }
    }
    k_sleep_ms(m);
    return kv_res(1, kv_nil());
}
static KValue k_yield_now(void) { return kv_nil(); }
static KValue k_process_run(KValue prog, KValue args) {
    char *p = k_cpath(prog);
    size_t n = args.u.a.len;
    char **argv = (char**)kalloc(sizeof(char*)*(n+2));
    argv[0] = p;
    for (size_t i=0;i<n;i++) argv[i+1] = k_cpath(args.u.a.items[i]);
    argv[n+1] = 0;
#ifdef _WIN32
    intptr_t rc = _spawnvp(_P_WAIT, p, (const char* const*)argv);
    if (rc == -1) return kv_res(0, kv_cstr("cannot run process"));
    if (rc == 0) return kv_res(1, kv_int(0));
    char msg[64]; snprintf(msg,sizeof(msg),"process exited with code %d",(int)rc);
    return kv_res(0, kv_cstr(msg));
#else
    pid_t pid = fork();
    if (pid < 0) return kv_res(0, kv_cstr("cannot fork"));
    if (pid == 0) {
        /* The interpreter captures and discards child output; match it. */
        int devnull = open("/dev/null", O_WRONLY);
        if (devnull >= 0) { dup2(devnull, 1); dup2(devnull, 2); close(devnull); }
        execvp(p, argv); _exit(127);
    }
    int status; waitpid(pid, &status, 0);
    if (WIFEXITED(status) && WEXITSTATUS(status)==0) return kv_res(1, kv_int(0));
    int code = WIFEXITED(status) ? WEXITSTATUS(status) : -1;
    char msg[64]; snprintf(msg,sizeof(msg),"process exited with code %d",code);
    return kv_res(0, kv_cstr(msg));
#endif
}

`
