package cruntime

// Dynamically loaded SQLite API and resource tracking.
const cRuntimeSQLite = `/* ---- dynamically loaded SQLite ---------------------------------------- */
typedef struct sqlite3 KSQLiteNativeDB;
typedef struct sqlite3_stmt KSQLiteNativeStatement;

struct KSQLiteHandle {
    KSQLiteNativeDB *db;
    int closed;
    struct KSQLiteHandle *next;
};

typedef struct KSQLiteTrackedStatement {
    KSQLiteNativeStatement *statement;
    struct KSQLiteTrackedStatement *next;
} KSQLiteTrackedStatement;

typedef struct {
    int (*open_v2)(const char*,KSQLiteNativeDB**,int,const char*);
    int (*close_v2)(KSQLiteNativeDB*);
    const char *(*errmsg)(KSQLiteNativeDB*);
    int (*exec)(KSQLiteNativeDB*,const char*,int(*)(void*,int,char**,char**),void*,char**);
    int (*prepare_v2)(KSQLiteNativeDB*,const char*,int,KSQLiteNativeStatement**,const char**);
    int (*step)(KSQLiteNativeStatement*);
    int (*finalize)(KSQLiteNativeStatement*);
    int (*column_count)(KSQLiteNativeStatement*);
    int (*column_type)(KSQLiteNativeStatement*,int);
    const unsigned char *(*column_text)(KSQLiteNativeStatement*,int);
    const void *(*column_blob)(KSQLiteNativeStatement*,int);
    int (*column_bytes)(KSQLiteNativeStatement*,int);
    long long (*column_int64)(KSQLiteNativeStatement*,int);
    double (*column_double)(KSQLiteNativeStatement*,int);
    int (*changes)(KSQLiteNativeDB*);
    int (*busy_timeout)(KSQLiteNativeDB*,int);
} KSQLiteAPI;

static KSQLiteAPI k_sqlite_api;
static void *k_sqlite_library;
static KSQLiteHandle *k_sqlite_handles;
static KSQLiteTrackedStatement *k_sqlite_statements;

static void k_sqlite_unload(void) {
#ifdef _WIN32
    if (k_sqlite_library) FreeLibrary((HMODULE)k_sqlite_library);
#else
    if (k_sqlite_library) dlclose(k_sqlite_library);
#endif
    k_sqlite_library=NULL;
    memset(&k_sqlite_api,0,sizeof(k_sqlite_api));
}

static int k_sqlite_resolve(void *library, KSQLiteAPI *api) {
#ifdef _WIN32
#define K_SQLITE_SYM(name) api->name=(void*)GetProcAddress((HMODULE)library,"sqlite3_" #name)
#else
#define K_SQLITE_SYM(name) api->name=dlsym(library,"sqlite3_" #name)
#endif
    memset(api,0,sizeof(*api));
    K_SQLITE_SYM(open_v2);
    K_SQLITE_SYM(close_v2);
    K_SQLITE_SYM(errmsg);
    K_SQLITE_SYM(exec);
    K_SQLITE_SYM(prepare_v2);
    K_SQLITE_SYM(step);
    K_SQLITE_SYM(finalize);
    K_SQLITE_SYM(column_count);
    K_SQLITE_SYM(column_type);
    K_SQLITE_SYM(column_text);
    K_SQLITE_SYM(column_blob);
    K_SQLITE_SYM(column_bytes);
    K_SQLITE_SYM(column_int64);
    K_SQLITE_SYM(column_double);
    K_SQLITE_SYM(changes);
    K_SQLITE_SYM(busy_timeout);
#undef K_SQLITE_SYM
    return api->open_v2 && api->close_v2 && api->errmsg && api->exec &&
           api->prepare_v2 && api->step && api->finalize && api->column_count &&
           api->column_type && api->column_text && api->column_blob && api->column_bytes &&
           api->column_int64 && api->column_double && api->changes && api->busy_timeout;
}

static const char *k_sqlite_load(void) {
    if (k_sqlite_library) return NULL;
#ifdef _WIN32
    const char *names[]={"winsqlite3.dll","sqlite3.dll",NULL};
    for (int i=0;names[i];i++) {
        HMODULE library=LoadLibraryA(names[i]);
        if (!library) continue;
        KSQLiteAPI api;
        if (k_sqlite_resolve((void*)library,&api)) {
            k_sqlite_library=(void*)library; k_sqlite_api=api; return NULL;
        }
        FreeLibrary(library);
    }
    return "SQLite library unavailable: install winsqlite3.dll or sqlite3.dll";
#else
    const char *names[]={"libsqlite3.so.0","libsqlite3.so",NULL};
    for (int i=0;names[i];i++) {
        void *library=dlopen(names[i],RTLD_NOW|RTLD_LOCAL);
        if (!library) continue;
        KSQLiteAPI api;
        if (k_sqlite_resolve(library,&api)) {
            k_sqlite_library=library; k_sqlite_api=api; return NULL;
        }
        dlclose(library);
    }
    return "SQLite library unavailable: install libsqlite3.so.0 or libsqlite3.so";
#endif
}

static void k_sqlite_track_statement(KSQLiteTrackedStatement *tracked, KSQLiteNativeStatement *statement) {
    tracked->statement=statement;
    tracked->next=k_sqlite_statements;
    k_sqlite_statements=tracked;
}

static int k_sqlite_finalize(KSQLiteTrackedStatement *tracked) {
    if (!tracked || !tracked->statement || !k_sqlite_api.finalize) return 0;
    KSQLiteNativeStatement *statement=tracked->statement;
    tracked->statement=NULL;
    return k_sqlite_api.finalize(statement);
}

static void k_sqlite_cleanup(void) {
    for (KSQLiteTrackedStatement *tracked=k_sqlite_statements;tracked;tracked=tracked->next)
        (void)k_sqlite_finalize(tracked);
    for (KSQLiteHandle *handle=k_sqlite_handles;handle;handle=handle->next) {
        if (handle->db && k_sqlite_api.close_v2) {
            handle->closed=1;
            (void)k_sqlite_api.close_v2(handle->db);
            handle->db=NULL;
        }
    }
    k_sqlite_statements=NULL;
    k_sqlite_handles=NULL;
    k_sqlite_unload();
}

static int k_sqlite_has_open_handles(void) {
    for (KSQLiteHandle *handle=k_sqlite_handles;handle;handle=handle->next)
        if (!handle->closed) return 1;
    return 0;
}

static int k_sqlite_string_has_nul(KValue value) {
    return value.tag!=K_STRING || memchr(value.u.s.data,0,value.u.s.len)!=NULL;
}

static KSQLiteHandle *k_sqlite_handle(KValue value) {
    return value.tag==K_SQLITE?value.u.sqlite.database:NULL;
}

static const char *k_sqlite_error(KSQLiteHandle *handle, int status) {
    if (handle && handle->db && k_sqlite_api.errmsg) {
        const char *message=k_sqlite_api.errmsg(handle->db);
        if (message && *message) return message;
    }
    (void)status;
    return "SQLite operation failed";
}

static void k_sqlite_copy_error(KSQLiteHandle *handle, int status, char *buffer, size_t capacity) {
    const char *message=k_sqlite_error(handle,status);
    const char *prefixes[]={"SQL logic error: ","constraint failed: ",NULL};
    for (int i=0;prefixes[i];i++) {
        size_t prefix_length=strlen(prefixes[i]);
        if (strncmp(message,prefixes[i],prefix_length)==0) { message+=prefix_length; break; }
    }
    size_t length=strlen(message);
    if (length>1 && message[length-1]==')') {
        for (size_t i=length-1;i>1;i--) {
            if (message[i-1]=='(' && message[i-2]==' ') {
                int numeric=i<length-1;
                for (size_t j=i;j<length-1;j++) if (message[j]<'0'||message[j]>'9') numeric=0;
                if (numeric) length=i-2;
                break;
            }
        }
    }
    if (capacity==0) return;
    if (length>=capacity) length=capacity-1;
    memcpy(buffer,message,length);
    buffer[length]=0;
}

static KValue k_sqlite_open(KValue path) {
    if (path.tag!=K_STRING) kfail("invalid SQLite path");
    if (k_sqlite_string_has_nul(path)) return kv_res(0,kv_cstr("path contains NUL"));
    const char *load_error=k_sqlite_load();
    if (load_error) return kv_res(0,kv_cstr(load_error));

    KSQLiteHandle *handle=(KSQLiteHandle*)kalloc(sizeof(KSQLiteHandle));
    memset(handle,0,sizeof(*handle));
    int status=k_sqlite_api.open_v2(path.u.s.data,&handle->db,2|4|64|0x10000,NULL);
    if (status!=0) {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        if (handle->db) (void)k_sqlite_api.close_v2(handle->db);
        handle->db=NULL;
        return kv_res(0,kv_cstr(message));
    }
    status=k_sqlite_api.busy_timeout(handle->db,5000);
    if (status!=0) {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        (void)k_sqlite_api.close_v2(handle->db);
        handle->db=NULL;
        return kv_res(0,kv_cstr(message));
    }
    handle->closed=0;
    handle->next=k_sqlite_handles;
    k_sqlite_handles=handle;
    KValue database; memset(&database,0,sizeof(database));
    database.tag=K_SQLITE; database.u.sqlite.database=handle;
    return kv_res(1,database);
}

static KValue k_sqlite_exec(KValue value, KValue query) {
    KSQLiteHandle *handle=k_sqlite_handle(value);
    if (!handle) kfail("invalid SQLite handle");
    if (handle->closed || !handle->db) return kv_res(0,kv_cstr("SQLite handle is closed"));
    if (query.tag!=K_STRING) kfail("invalid SQLite query");
    if (k_sqlite_string_has_nul(query)) return kv_res(0,kv_cstr("query contains NUL"));
    int status=k_sqlite_api.exec(handle->db,query.u.s.data,NULL,NULL,NULL);
    if (status!=0) {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        return kv_res(0,kv_cstr(message));
    }
    return kv_res(1,kv_int(k_sqlite_api.changes(handle->db)));
}

static KValue *k_sqlite_grow_rows(KValue *rows, size_t old_capacity, size_t new_capacity) {
    if (new_capacity>SIZE_MAX/sizeof(KValue)) kfail("memory budget exceeded");
    size_t old_bytes=old_capacity*sizeof(KValue), new_bytes=new_capacity*sizeof(KValue);
    if (new_bytes>(size_t)LLONG_MAX || k_mem<(long long)old_bytes ||
        (long long)new_bytes>k_max_mem || k_mem-(long long)old_bytes>k_max_mem-(long long)new_bytes)
        kfail("memory budget exceeded");
    KValue *grown=(KValue*)realloc(rows,new_bytes);
    if (!grown) kfail("out of memory");
    k_mem=k_mem-(long long)old_bytes+(long long)new_bytes;
    return grown;
}

static void k_fmt_float(double v, char *out, size_t outsz);
static void k_sqlite_format_float(double number, char *buffer, size_t capacity) {
    if (number==0.0 && signbit(number)) { snprintf(buffer,capacity,"-0"); return; }
    k_fmt_float(number,buffer,capacity);
}

static KValue k_sqlite_column_value(KSQLiteNativeStatement *statement, int column) {
    int type=k_sqlite_api.column_type(statement,column);
    if (type==5) return kv_cstr("");
    if (type==1) {
        char buffer[32];
        snprintf(buffer,sizeof(buffer),"%lld",k_sqlite_api.column_int64(statement,column));
        return kv_cstr(buffer);
    }
    if (type==2) {
        char buffer[64];
        k_sqlite_format_float(k_sqlite_api.column_double(statement,column),buffer,sizeof(buffer));
        return kv_cstr(buffer);
    }
    int length=k_sqlite_api.column_bytes(statement,column);
    if (length<0) kfail("SQLite returned an invalid column length");
    const void *data=type==4?k_sqlite_api.column_blob(statement,column):(const void*)k_sqlite_api.column_text(statement,column);
    if (!data && length>0) kfail("SQLite returned an invalid column value");
    if (!data) data="";
    return kv_strn((const char*)data,(size_t)length);
}

static int k_sqlite_tail_has_sql(const char *tail, const char *end) {
    const char *cursor=tail;
    while (cursor<end) {
        if (*cursor==' '||*cursor=='\t'||*cursor=='\r'||*cursor=='\n'||*cursor=='\f'||*cursor=='\v'||*cursor==';') { cursor++; continue; }
        if (end-cursor>=2 && cursor[0]=='-' && cursor[1]=='-') {
            cursor+=2;
            while (cursor<end && *cursor!='\n') cursor++;
            continue;
        }
        if (end-cursor>=2 && cursor[0]=='/' && cursor[1]=='*') {
            cursor+=2;
            while (end-cursor>=2 && !(cursor[0]=='*' && cursor[1]=='/')) cursor++;
            if (end-cursor>=2) cursor+=2;
            continue;
        }
        return 1;
    }
    return 0;
}

static KValue k_sqlite_query(KValue value, KValue query) {
    KSQLiteHandle *handle=k_sqlite_handle(value);
    if (!handle) kfail("invalid SQLite handle");
    if (k_max_array_elements<1) return kv_res(0,kv_cstr("SQLite row limit must be positive"));
    if (handle->closed || !handle->db) return kv_res(0,kv_cstr("SQLite handle is closed"));
    if (query.tag!=K_STRING) kfail("invalid SQLite query");
    if (k_sqlite_string_has_nul(query)) return kv_res(0,kv_cstr("query contains NUL"));
    if (query.u.s.len>(size_t)INT_MAX) return kv_res(0,kv_cstr("SQL query is too large"));

    KSQLiteTrackedStatement *tracked=(KSQLiteTrackedStatement*)kalloc(sizeof(KSQLiteTrackedStatement));
    tracked->statement=NULL;
    tracked->next=k_sqlite_statements;
    k_sqlite_statements=tracked;
    KValue *rows=NULL;
    size_t row_count=0, capacity=0;
    const char *cursor=query.u.s.data;
    const char *end=cursor+query.u.s.len;
    while (cursor<end) {
        const char *tail=cursor;
        int status=k_sqlite_api.prepare_v2(handle->db,cursor,(int)(end-cursor),&tracked->statement,&tail);
        if (status!=0) {
            char message[512];
            k_sqlite_copy_error(handle,status,message,sizeof(message));
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr(message));
        }
        if (!tail || tail<cursor || tail>end) {
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr("SQLite returned an invalid SQL parser position"));
        }
        if (!tracked->statement) {
            if (tail==cursor) break;
            cursor=tail;
            continue;
        }

        int is_final=!k_sqlite_tail_has_sql(tail,end);
        int column_count=k_sqlite_api.column_count(tracked->statement);
        if (column_count<0 || (size_t)column_count>SIZE_MAX/sizeof(KValue)) {
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr("SQLite returned an invalid column count"));
        }
        if (is_final && (long long)column_count>k_max_array_elements) {
            (void)k_sqlite_finalize(tracked);
            return kv_res(0,kv_cstr("SQLite result exceeds configured column limit"));
        }
        if (is_final) { rows=NULL; row_count=0; capacity=0; }
        for (;;) {
            status=k_sqlite_api.step(tracked->statement);
            if (status==101) break;
            if (status!=100) {
                char message[512];
                k_sqlite_copy_error(handle,status,message,sizeof(message));
                (void)k_sqlite_finalize(tracked);
                return kv_res(0,kv_cstr(message));
            }
            if (!is_final) continue;
            if ((long long)row_count>=k_max_array_elements) {
                (void)k_sqlite_finalize(tracked);
                return kv_res(0,kv_cstr("SQLite result exceeds configured row limit"));
            }
            if (row_count==capacity) {
                size_t next=capacity?capacity*2:8;
                if (next<capacity || (long long)next>k_max_array_elements) next=(size_t)k_max_array_elements;
                rows=k_sqlite_grow_rows(rows,capacity,next);
                capacity=next;
            }
            if ((size_t)column_count>SIZE_MAX/sizeof(KValue)) kfail("memory budget exceeded");
            KValue *cells=(KValue*)kalloc((size_t)column_count*sizeof(KValue));
            for (int column=0;column<column_count;column++) cells[column]=k_sqlite_column_value(tracked->statement,column);
            rows[row_count++]=kv_arr(cells,(size_t)column_count);
        }
        status=k_sqlite_finalize(tracked);
        if (status!=0) {
            char message[512];
            k_sqlite_copy_error(handle,status,message,sizeof(message));
            return kv_res(0,kv_cstr(message));
        }
        cursor=tail;
    }
    return kv_res(1,kv_arr(rows,row_count));
}

static KValue k_sqlite_close(KValue value) {
    KSQLiteHandle *handle=k_sqlite_handle(value);
    if (!handle) kfail("invalid SQLite handle");
    if (handle->closed || !handle->db) kfail("SQLite handle is already closed");
    handle->closed=1;
    int status=k_sqlite_api.close_v2(handle->db);
    if (status==0) handle->db=NULL;
    else {
        char message[512];
        k_sqlite_copy_error(handle,status,message,sizeof(message));
        kfail(message);
    }
    return kv_nil();
}
`
