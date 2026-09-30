package cruntime

// Shared cells, actor mailboxes, threads, and task-group runtime support.
const cRuntimeConcurrency = `/* ---- concurrency: shared cells, actors, threads, task groups ----------- */
/* The native backend runs single-threaded. Shared cells, actor mailboxes and
   task groups are modelled faithfully for the deterministic subset of the
   language: values are immutable, so a shared cell is a heap pointer that
   shared_read/shared_write/shared_swap mutate in place, and an actor mailbox
   is a FIFO queue. Worker threads are not executed concurrently; a thread
   handle records the worker function and its arguments so await() can run it
   on demand and return its result. */

typedef struct KChan { KValue *items; size_t len, cap; int closed; } KChan;
typedef struct KHandle {
    int kind;              /* 0 = thread, 1 = task group */
    KValue (*fn)(void);    /* worker entry (threads) */
    KValue *args; int nargs;
    int done; KValue result;
    struct KHandle **members; size_t nmembers; int cancelled;
} KHandle;

static KValue kv_shared(KValue *cell) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_SHARED; v.u.sh.cell=cell; return v; }
static KValue kv_actor(KChan *ch) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_ACTOR; v.u.ac.ch=ch; return v; }
static KValue kv_thread(KHandle *h) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_THREAD; v.u.hd.h=h; return v; }
static KValue kv_taskgroup(KHandle *h) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_TASKGROUP; v.u.hd.h=h; return v; }
static KValue kv_channel(KChan *ch) { KValue v; memset(&v,0,sizeof(v)); v.tag=K_CHANNEL; v.u.ac.ch=ch; return v; }

static KChan *k_chan_new(size_t cap) {
    KChan *c=(KChan*)kalloc(sizeof(KChan));
    c->cap = cap ? cap : 64; c->len=0; c->closed=0;
    c->items=(KValue*)kalloc(sizeof(KValue)*c->cap);
    return c;
}
static void k_chan_push(KChan *c, KValue v) {
    if (c->closed) kfail("closed actor mailbox");
    if (c->len >= c->cap) kfail("actor mailbox is full");
    c->items[c->len++]=v;
}
static int k_chan_pop(KChan *c, KValue *out) {
    if (c->len==0) return 0;
    *out=c->items[0];
    for (size_t i=1;i<c->len;i++) c->items[i-1]=c->items[i];
    c->len--;
    return 1;
}

static KValue k_shared_new(KValue v) {
    KValue *cell=(KValue*)kalloc(sizeof(KValue)); *cell=v;
    return kv_shared(cell);
}
static KValue k_shared_read(KValue s) {
    if (s.tag!=K_SHARED) kfail("invalid shared cell");
    return *s.u.sh.cell;
}
static KValue k_shared_write(KValue s, KValue v) {
    if (s.tag!=K_SHARED) kfail("invalid shared cell");
    *s.u.sh.cell=v; return kv_nil();
}
static KValue k_shared_swap(KValue s, KValue v) {
    if (s.tag!=K_SHARED) kfail("invalid shared cell");
    KValue old=*s.u.sh.cell; *s.u.sh.cell=v; return old;
}

static KValue k_actor_channel(void) { return kv_actor(k_chan_new(64)); }
static KValue k_actor_channel_cap(KValue capv) {
    long long cap=capv.u.i;
    if (cap<1 || cap>1024) kfail("actor mailbox capacity is outside configured limits");
    return kv_actor(k_chan_new((size_t)cap));
}
static KValue k_actor_send(KValue a, KValue v) {
    if (a.tag!=K_ACTOR) kfail("invalid actor");
    k_chan_push(a.u.ac.ch, v); return kv_nil();
}
static KValue k_actor_try_receive(KValue a) {
    if (a.tag!=K_ACTOR) kfail("invalid actor");
    KValue out;
    if (k_chan_pop(a.u.ac.ch,&out)) return kv_res(1,out);
    if (a.u.ac.ch->closed) return kv_res(0, kv_cstr("closed"));
    return kv_res(0, kv_cstr("empty"));
}
static KValue k_actor_receive_timeout(KValue a, KValue msv) {
    if (msv.u.i<0) kfail("timeout duration cannot be negative");
    if (a.tag!=K_ACTOR) kfail("invalid actor");
    KValue out;
    if (k_chan_pop(a.u.ac.ch,&out)) return out;
    if (a.u.ac.ch->closed) kfail("closed actor mailbox");
    kfail("actor receive timed out");
    return kv_nil();
}
static KValue k_actor_close(KValue a) {
    if (a.tag==K_ACTOR) a.u.ac.ch->closed=1;
    return kv_nil();
}

static KHandle *k_handle_new(int kind) {
    KHandle *h=(KHandle*)kalloc(sizeof(KHandle));
    memset(h,0,sizeof(KHandle)); h->kind=kind; h->result=kv_nil();
    return h;
}
static KValue k_task_group(void) { return kv_taskgroup(k_handle_new(1)); }
static KValue k_task_spawn(KValue g, KValue (*fn)(void), KValue *args, int nargs) {
    if (g.tag!=K_TASKGROUP) kfail("invalid task group");
    if (g.u.hd.h->cancelled) kfail("task group is cancelled");
    KHandle *h=k_handle_new(0); h->fn=fn; h->args=args; h->nargs=nargs;
    KHandle *grp=g.u.hd.h;
    KHandle **m=(KHandle**)kalloc(sizeof(KHandle*)*(grp->nmembers+1));
    for (size_t i=0;i<grp->nmembers;i++) m[i]=grp->members[i];
    m[grp->nmembers]=h; grp->members=m; grp->nmembers++;
    return kv_thread(h);
}
static KValue k_task_group_cancel(KValue g) {
    if (g.tag!=K_TASKGROUP) kfail("invalid task group");
    g.u.hd.h->cancelled=1; return kv_nil();
}
static KValue k_task_group_wait(KValue g) {
    if (g.tag!=K_TASKGROUP) kfail("invalid task group");
    KHandle *grp=g.u.hd.h;
    for (size_t i=0;i<grp->nmembers;i++) {
        KHandle *h=grp->members[i];
        if (!h->done) { h->result=h->fn(); h->done=1; }
    }
    if (grp->cancelled) return kv_res(0, kv_cstr("task group cancelled"));
    return kv_res(1, kv_nil());
}
static KValue k_await(KValue t) {
    if (t.tag!=K_THREAD) kfail("invalid thread");
    KHandle *h=t.u.hd.h;
    if (!h->done) { h->result=h->fn(); h->done=1; }
    return h->result;
}
static KValue k_await_timeout(KValue t, KValue msv) {
    if (msv.u.i<0) kfail("timeout duration cannot be negative");
    return k_await(t);
}
static KValue k_thread_spawn(KValue (*fn)(void)) {
    KHandle *h=k_handle_new(0); h->fn=fn; return kv_thread(h);
}

`
