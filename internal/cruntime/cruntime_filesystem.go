package cruntime

// Environment access and the base filesystem read/write operations.
const cRuntimeFileBase = `/* ---- host builtins: filesystem and environment ------------------------- */
static KValue k_fs_create_dir_all(KValue path);
static KValue k_fs_parent_dir(KValue path);
static int k_fs_path_has_nul(KValue path) {
    return memchr(path.u.s.data,0,path.u.s.len)!=NULL;
}
static KValue k_fs_path_nul_result(void) {
    return kv_res(0,kv_cstr("path contains NUL"));
}
static KValue k_fs_path_error(const char *op, KValue path) {
    int code=errno;
    const char *reason=strerror(code);
    size_t op_len=strlen(op), reason_len=strlen(reason);
    size_t cap=op_len+path.u.s.len+reason_len+4;
    char *message=(char*)kalloc(cap);
    snprintf(message,cap,"%s %.*s: %s",op,(int)path.u.s.len,path.u.s.data,reason);
    size_t reason_offset=op_len+path.u.s.len+3;
    if (message[reason_offset]>='A' && message[reason_offset]<='Z') message[reason_offset]=(char)(message[reason_offset]-'A'+'a');
    return kv_cstr(message);
}
static KValue k_fs_write_data(KValue path, const char *data, size_t length);
static KValue k_fs_read_text(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (!f) return kv_res(0,k_fs_path_error("open",path));
    fseek(f,0,SEEK_END); long n=ftell(f); fseek(f,0,SEEK_SET);
    if (n<0) { fclose(f); return kv_res(0,kv_cstr("cannot read file")); }
    char *buf=(char*)kalloc((size_t)n+1);
    size_t got=fread(buf,1,(size_t)n,f); fclose(f);
    buf[got]=0;
    if (!k_utf8_valid(buf,got)) return kv_res(0,kv_cstr("file is not valid UTF-8"));
    return kv_res(1,kv_strn(buf,got));
}
static KValue k_fs_write_text(KValue path, KValue text) {
    if (!k_utf8_valid(text.u.s.data,text.u.s.len)) return kv_res(0,kv_cstr("invalid UTF-8"));
    return k_fs_write_data(path,text.u.s.data,text.u.s.len);
}
static KValue k_fs_read_bytes(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
    FILE *f=fopen(p,"rb");
    if (!f) return kv_res(0,k_fs_path_error("open",path));
    fseek(f,0,SEEK_END); long n=ftell(f); fseek(f,0,SEEK_SET);
    if (n<0) { fclose(f); return kv_res(0,kv_cstr("cannot read file")); }
    char *buf=(char*)kalloc((size_t)n+1);
    size_t got=fread(buf,1,(size_t)n,f); fclose(f);
    return kv_res(1,kv_bytesn(buf,got));
}
static KValue k_fs_write_bytes(KValue path, KValue data) {
    return k_fs_write_data(path,data.u.s.data,data.u.s.len);
}
static KValue k_fs_exists(KValue path) {
    if (k_fs_path_has_nul(path)) kfail("path contains NUL");
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0;
#ifdef _WIN32
    return kv_bool(GetFileAttributesA(p)!=INVALID_FILE_ATTRIBUTES);
#else
    struct stat st;
    return kv_bool(lstat(p,&st)==0);
#endif
}
static KValue k_env_get(KValue name) {
    char *p=(char*)kalloc(name.u.s.len+1); memcpy(p,name.u.s.data,name.u.s.len); p[name.u.s.len]=0;
    const char *v=getenv(p);

    if (!v) return kv_opt(0,kv_nil());
    return kv_opt(1,kv_cstr(v));
}

`

// Filesystem mutation, path handling, and temporary-file operations.
const cRuntimeFileSystem = `/* ---- extended filesystem ----------------------------------------------- */
static char *k_cpath(KValue path) {
    char *p=(char*)kalloc(path.u.s.len+1); memcpy(p,path.u.s.data,path.u.s.len); p[path.u.s.len]=0; return p;
}
#ifdef _WIN32
static void k_fs_set_errno(DWORD error) {
    switch (error) {
    case ERROR_FILE_NOT_FOUND:
    case ERROR_PATH_NOT_FOUND: errno=ENOENT; break;
    case ERROR_ACCESS_DENIED:
    case ERROR_SHARING_VIOLATION:
    case ERROR_LOCK_VIOLATION: errno=EACCES; break;
    case ERROR_ALREADY_EXISTS:
    case ERROR_FILE_EXISTS: errno=EEXIST; break;
    case ERROR_INVALID_NAME:
    case ERROR_INVALID_PARAMETER: errno=EINVAL; break;
    case ERROR_DIR_NOT_EMPTY: errno=ENOTEMPTY; break;
    case ERROR_DIRECTORY: errno=ENOTDIR; break;
    default: errno=EIO; break;
    }
}
static int k_fs_lstat(const char *path, struct stat *st, int *is_link, int *is_dir) {
    HANDLE handle=CreateFileA(path,FILE_READ_ATTRIBUTES,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,NULL,OPEN_EXISTING,FILE_FLAG_BACKUP_SEMANTICS|FILE_FLAG_OPEN_REPARSE_POINT,NULL);
    if (handle==INVALID_HANDLE_VALUE) { k_fs_set_errno(GetLastError()); return -1; }
    BY_HANDLE_FILE_INFORMATION info;
    if (!GetFileInformationByHandle(handle,&info)) {
        DWORD error=GetLastError(); CloseHandle(handle); k_fs_set_errno(error); return -1;
    }
    CloseHandle(handle);
    *is_link=(info.dwFileAttributes&FILE_ATTRIBUTE_REPARSE_POINT)!=0;
    *is_dir=(info.dwFileAttributes&FILE_ATTRIBUTE_DIRECTORY)!=0;
    if (*is_link) { memset(st,0,sizeof(*st)); return 0; }
    if (stat(path,st)!=0) return -1;
    *is_dir=S_ISDIR(st->st_mode);
    return 0;
}
static int k_fs_remove_entry(const char *path, int is_dir) {
    if (is_dir) {
        if (RemoveDirectoryA(path)) return 0;
        k_fs_set_errno(GetLastError()); return -1;
    }
    if (DeleteFileA(path)) return 0;
    k_fs_set_errno(GetLastError()); return -1;
}
#else
static int k_fs_lstat(const char *path, struct stat *st, int *is_link, int *is_dir) {
    if (lstat(path,st)!=0) return -1;
    *is_link=S_ISLNK(st->st_mode);
    *is_dir=S_ISDIR(st->st_mode);
    return 0;
}
static int k_fs_remove_entry(const char *path, int is_dir) {
    (void)is_dir;
    return remove(path);
}
#endif
static char *k_fs_temp_path(const char *destination) {
    size_t parent_len=0;
    for (size_t i=0;destination[i];i++) {
#ifdef _WIN32
        if (destination[i]=='/' || destination[i]=='\\') parent_len=i+1;
#else
        if (destination[i]=='/') parent_len=i+1;
#endif
    }
    size_t prefix_len=parent_len ? 0 : 2;
    const char *name=".kryndel-copy-XXXXXX";
    size_t name_len=strlen(name);
    char *path=(char*)kalloc(parent_len+prefix_len+name_len+1);
    if (parent_len) memcpy(path,destination,parent_len);
    else {
        path[0]='.';
#ifdef _WIN32
        path[1]='\\';
#else
        path[1]='/';
#endif
    }
    memcpy(path+parent_len+prefix_len,name,name_len+1);
    return path;
}
static int k_fs_open_temp(char *path, size_t path_size) {
#ifdef _WIN32
    if (_mktemp_s(path,path_size)!=0) {
        if (!errno) errno=EIO;
        return -1;
    }
    return _open(path,_O_CREAT|_O_EXCL|_O_BINARY|_O_WRONLY,_S_IREAD|_S_IWRITE);
#else
    (void)path_size;
    return mkstemp(path);
#endif
}
static int k_fs_replace_file(const char *source, const char *destination) {
#ifdef _WIN32
    if (MoveFileExA(source,destination,MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)) return 0;
    k_fs_set_errno(GetLastError());
    return -1;
#else
    return rename(source,destination);
#endif
}
static int k_fs_close_fd(int fd) {
#ifdef _WIN32
    return _close(fd);
#else
    return close(fd);
#endif
}
static KValue k_fs_begin_write(KValue path, char **destination, char **temporary, FILE **output) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    *destination=k_cpath(path);
    KValue parent=k_fs_parent_dir(path);
    if (!parent.u.res.ok) return parent;
    *temporary=k_fs_temp_path(*destination);
    int fd=k_fs_open_temp(*temporary,strlen(*temporary)+1);
    if (fd<0) return kv_res(0,k_fs_path_error("create",path));
#ifdef _WIN32
    *output=_fdopen(fd,"wb");
#else
    *output=fdopen(fd,"wb");
#endif
    if (!*output) {
        int error=errno ? errno : EIO;
        k_fs_close_fd(fd); remove(*temporary); errno=error;
        return kv_res(0,k_fs_path_error("open",path));
    }
    return kv_res(1,kv_nil());
}
static KValue k_fs_finish_write(KValue path, char *destination, char *temporary, FILE *output, int write_error) {
    if (!write_error && fflush(output)!=0) write_error=errno ? errno : EIO;
#ifdef _WIN32
    if (!write_error && _commit(_fileno(output))!=0) write_error=errno ? errno : EIO;
#else
    if (!write_error && fsync(fileno(output))!=0) write_error=errno ? errno : EIO;
#endif
    if (fclose(output)!=0 && !write_error) write_error=errno ? errno : EIO;
    if (write_error) {
        remove(temporary); errno=write_error;
        return kv_res(0,k_fs_path_error("write",path));
    }
    struct stat st;
    int is_link=0, is_dir=0;
    if (k_fs_lstat(destination,&st,&is_link,&is_dir)==0 && is_link) {
        remove(temporary);
        return kv_res(0,kv_cstr("path denied by sandbox"));
    }
    if (k_fs_replace_file(temporary,destination)!=0) {
        int error=errno;
        remove(temporary); errno=error;
        return kv_res(0,k_fs_path_error("rename",path));
    }
    return kv_res(1,kv_nil());
}
static KValue k_fs_write_data(KValue path, const char *data, size_t length) {
    char *destination=NULL, *temporary=NULL;
    FILE *output=NULL;
    KValue result=k_fs_begin_write(path,&destination,&temporary,&output);
    if (!result.u.res.ok) return result;
    int write_error=0;
    if (fwrite(data,1,length,output)!=length) write_error=errno ? errno : EIO;
    return k_fs_finish_write(path,destination,temporary,output,write_error);
}
static int k_fs_dir_entry_compare(const void *left, const void *right) {
    const KValue *a=(const KValue*)left, *b=(const KValue*)right;
    return strcmp(a->u.s.data,b->u.s.data);
}
static KValue k_fs_read_dir(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    DIR *d=opendir(p);
    if (!d) return kv_res(0, kv_cstr("cannot read directory"));
    KValue *items=(KValue*)kalloc(sizeof(KValue)*8); size_t cap=8,n=0;
    struct dirent *e;
    while ((e=readdir(d))) {
        if (!strcmp(e->d_name,".")||!strcmp(e->d_name,"..")) continue;
        if (n==cap) { size_t nc=cap*2; KValue *ni=(KValue*)kalloc(sizeof(KValue)*nc); memcpy(ni,items,sizeof(KValue)*n); items=ni; cap=nc; }
        items[n++]=kv_cstr(e->d_name);
    }
    closedir(d);
    qsort(items,n,sizeof(*items),k_fs_dir_entry_compare);
    return kv_res(1, kv_arr(items,n));
}
static KValue k_fs_create_dir(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    if (k_mkdir(p)!=0) return kv_res(0, kv_cstr("cannot create directory"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_create_dir_all(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    char *start=p+1;
#ifdef _WIN32
    if (p[0] && p[1]==':' && (p[2]=='/' || p[2]=='\\')) start=p+3;
#endif
    for (char *q=start; *q; q++) {
#ifdef _WIN32
        if (*q=='/' || *q=='\\') {
#else
        if (*q=='/') {
#endif
            char sep=*q; *q=0; if (*p) k_mkdir(p); *q=sep;
        }
    }
    if (k_mkdir(p)!=0) {
        int mkdir_errno=errno; struct stat st;
        if (stat(p,&st)==0) {
            if (!S_ISDIR(st.st_mode)) { errno=ENOTDIR; return kv_res(0,kv_cstr("cannot create directory")); }
        } else { errno=mkdir_errno; return kv_res(0,kv_cstr("cannot create directory")); }
    }
    return kv_res(1, kv_nil());
}
static KValue k_fs_parent_dir(KValue path) {
    char *p=k_cpath(path), *last=NULL;
    for (char *q=p; *q; q++) {
#ifdef _WIN32
        if (*q=='/' || *q=='\\') last=q;
#else
        if (*q=='/') last=q;
#endif
    }
    if (!last) return kv_res(1,kv_nil());
    if (last==p) last++;
#ifdef _WIN32
    else if (last==p+2 && p[1]==':') last++;
#endif
    size_t parent_len=(size_t)(last-p);
    KValue parent=kv_strn(p,parent_len);
    KValue created=k_fs_create_dir_all(parent);
    if (!created.u.res.ok) return kv_res(0,k_fs_path_error("mkdir",parent));
    return created;
}
static KValue k_fs_remove_file(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    if (remove(p)!=0) return kv_res(0, kv_cstr("cannot remove file"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_remove_dir_all(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path);
    struct stat st;
    int is_link=0, is_dir=0;
    if (k_fs_lstat(p,&st,&is_link,&is_dir)!=0) {
        if (errno==ENOENT) return kv_res(1,kv_nil());
        return kv_res(0,k_fs_path_error("unlinkat",path));
    }
    if (is_link || !is_dir) {
        if (k_fs_remove_entry(p,is_dir)==0 || errno==ENOENT) return kv_res(1,kv_nil());
        return kv_res(0,k_fs_path_error("unlinkat",path));
    }
    DIR *d=opendir(p);
    if (!d) {
        if (errno==ENOENT) return kv_res(1,kv_nil());
        return kv_res(0,k_fs_path_error("open",path));
    }
    struct dirent *e;
    KValue result=kv_res(1,kv_nil());
    int read_error=0;
    for (;;) {
        errno=0;
        e=readdir(d);
        if (!e) { read_error=errno; break; }
        if (!strcmp(e->d_name,".")||!strcmp(e->d_name,"..")) continue;
        size_t pl=strlen(p), nl=strlen(e->d_name);
        char *child=(char*)kalloc(pl+nl+2); memcpy(child,p,pl); child[pl]='/'; memcpy(child+pl+1,e->d_name,nl+1);
        result=k_fs_remove_dir_all(kv_cstr(child));
        if (!result.u.res.ok) break;
    }
    int close_error=closedir(d)==0 ? 0 : errno;
    if (!result.u.res.ok) return result;
    if (read_error) { errno=read_error; return kv_res(0,k_fs_path_error("readdirent",path)); }
    if (close_error) { errno=close_error; return kv_res(0,k_fs_path_error("closedir",path)); }
    if (k_rmdir(p)!=0 && errno!=ENOENT) return kv_res(0,k_fs_path_error("unlinkat",path));
    return kv_res(1, kv_nil());
}
static KValue k_fs_copy_file(KValue src, KValue dst) {
    if (k_fs_path_has_nul(src)) return k_fs_path_nul_result();
    char *s=k_cpath(src), *d=k_cpath(dst);
    FILE *in=fopen(s,"rb");
    if (!in) return kv_res(0,k_fs_path_error("open",src));
    char *destination=NULL, *temporary=NULL;
    FILE *out=NULL;
    KValue result=k_fs_begin_write(dst,&destination,&temporary,&out);
    if (!result.u.res.ok) { fclose(in); return result; }
    char buf[8192]; size_t got; int copy_error=0;
    while ((got=fread(buf,1,sizeof(buf),in))>0) {
        if (fwrite(buf,1,got,out)!=got) { copy_error=errno ? errno : EIO; break; }
    }
    if (!copy_error && ferror(in)) copy_error=errno ? errno : EIO;
    if (fclose(in)!=0 && !copy_error) copy_error=errno ? errno : EIO;
    return k_fs_finish_write(dst,d,temporary,out,copy_error);
}
static KValue k_fs_move_file(KValue src, KValue dst) {
    if (k_fs_path_has_nul(src) || k_fs_path_has_nul(dst)) return k_fs_path_nul_result();
    char *s=k_cpath(src), *d=k_cpath(dst);
    if (k_fs_replace_file(s,d)!=0) return kv_res(0, kv_cstr("cannot move file"));
    return kv_res(1, kv_nil());
}
static KValue k_fs_is_file(KValue path) {
    if (k_fs_path_has_nul(path)) return kv_bool(0);
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_bool(0);
    return kv_bool(!S_ISDIR(st.st_mode));
}
static KValue k_fs_is_dir(KValue path) {
    if (k_fs_path_has_nul(path)) return kv_bool(0);
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_bool(0);
    return kv_bool(S_ISDIR(st.st_mode));
}
static KValue k_fs_file_size(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot stat file"));
    return kv_res(1, kv_int((long long)st.st_size));
}
static KValue k_fs_file_modified_time(KValue path) {
    if (k_fs_path_has_nul(path)) return k_fs_path_nul_result();
    char *p=k_cpath(path); struct stat st;
    if (stat(p,&st)!=0) return kv_res(0, kv_cstr("cannot stat file"));
    return kv_res(1, kv_int((long long)st.st_mtime));
}
static int k_path_is_separator(char c) {
#ifdef _WIN32
    return c=='/' || c=='\\';
#else
    return c=='/';
#endif
}
static char k_path_native_separator(void) {
#ifdef _WIN32
    return '\\';
#else
    return '/';
#endif
}
static int k_path_is_absolute(const char *path, size_t length) {
#ifdef _WIN32
    if (length>=3 && path[1]==':' && k_path_is_separator(path[2])) return 1;
    return length>0 && k_path_is_separator(path[0]);
#else
    return length>0 && path[0]=='/';
#endif
}
static KValue k_path_clean_bytes(const char *path, size_t length) {
    char *clean=(char*)kalloc(length+4);
    size_t read=0, written=0, volume=0, root=0;
    int absolute=0;
#ifdef _WIN32
    if (length>=2 && path[1]==':') {
        clean[written++]=path[0]; clean[written++]=':';
        volume=written; root=written; read=2;
        if (read<length && k_path_is_separator(path[read])) {
            clean[written++]=k_path_native_separator(); root=written; absolute=1;
            while (read<length && k_path_is_separator(path[read])) read++;
        }
    } else if (length>=2 && k_path_is_separator(path[0]) && k_path_is_separator(path[1])) {
        size_t server, server_end, share, share_end;
        read=2;
        while (read<length && k_path_is_separator(path[read])) read++;
        server=read;
        while (read<length && !k_path_is_separator(path[read])) read++;
        server_end=read;
        while (read<length && k_path_is_separator(path[read])) read++;
        share=read;
        while (read<length && !k_path_is_separator(path[read])) read++;
        share_end=read;
        if (server_end>server && share_end>share) {
            clean[written++]=k_path_native_separator();
            clean[written++]=k_path_native_separator();
            memcpy(clean+written,path+server,server_end-server); written+=server_end-server;
            clean[written++]=k_path_native_separator();
            memcpy(clean+written,path+share,share_end-share); written+=share_end-share;
            volume=written; root=written; absolute=1;
            while (read<length && k_path_is_separator(path[read])) read++;
        } else {
            read=0;
            clean[written++]=k_path_native_separator(); root=written; absolute=1;
            while (read<length && k_path_is_separator(path[read])) read++;
        }
    } else if (length>0 && k_path_is_separator(path[0])) {
        clean[written++]=k_path_native_separator(); root=written; absolute=1;
        while (read<length && k_path_is_separator(path[read])) read++;
    }
#else
    if (length>0 && path[0]=='/') {
        clean[written++]='/'; root=written; absolute=1;
        while (read<length && path[read]=='/') read++;
    }
#endif
    while (read<length) {
        size_t start, part_length;
        while (read<length && k_path_is_separator(path[read])) read++;
        if (read>=length) break;
        start=read;
        while (read<length && !k_path_is_separator(path[read])) read++;
        part_length=read-start;
        if (part_length==1 && path[start]=='.') continue;
        if (part_length==2 && path[start]=='.' && path[start+1]=='.') {
            if (written>root) {
                size_t part_start=written;
                while (part_start>root && !k_path_is_separator(clean[part_start-1])) part_start--;
                size_t previous_length=written-part_start;
                int previous_parent=previous_length==2 && clean[part_start]=='.' && clean[part_start+1]=='.';
                if (!previous_parent) {
                    written=part_start;
                    if (written>root && k_path_is_separator(clean[written-1])) written--;
                    continue;
                }
            }
            if (absolute) continue;
        }
        if (written>0 && !k_path_is_separator(clean[written-1]) && !(volume>0 && written==volume && !absolute))
            clean[written++]=k_path_native_separator();
        memcpy(clean+written,path+start,part_length);
        written+=part_length;
    }
#ifdef _WIN32
    if (written==volume && volume>0 && !absolute) clean[written++]='.';
#endif
    if (written==0) clean[written++]='.';
    return kv_strn(clean,written);
}
static KValue k_fs_join_path(KValue base, KValue parts) {
    KBuf b; kb_init(&b);
    kb_putn(&b, base.u.s.data, base.u.s.len);
    for (size_t i=0;i<parts.u.a.len;i++) {
        KValue p=parts.u.a.items[i];
        if (b.len>0 && !k_path_is_separator(b.buf[b.len-1])) kb_putc(&b,k_path_native_separator());
        kb_putn(&b, p.u.s.data, p.u.s.len);
    }
    if (b.len==0) return kv_strn("",0);
    return k_path_clean_bytes(b.buf,b.len);
}
static KValue k_fs_absolute_path(KValue path) {
#ifdef _WIN32
    if (!k_fs_path_has_nul(path)) {
        char *p=k_cpath(path);
        char buf[4096];
        if (_fullpath(buf,p,sizeof(buf))) return kv_res(1,kv_cstr(buf));
        return kv_res(0,kv_cstr("cannot resolve path"));
    }
#endif
    KBuf b; kb_init(&b);
    if (k_path_is_absolute(path.u.s.data,path.u.s.len)) {
        kb_putn(&b,path.u.s.data,path.u.s.len);
    } else {
        char cwd[4096];
        if (!getcwd(cwd,sizeof(cwd))) return kv_res(0,kv_cstr("cannot resolve path"));
        kb_puts(&b,cwd);
        if (b.len>0 && !k_path_is_separator(b.buf[b.len-1])) kb_putc(&b,k_path_native_separator());
        kb_putn(&b,path.u.s.data,path.u.s.len);
    }
    return kv_res(1,k_path_clean_bytes(b.buf,b.len));
}
static KValue k_fs_temp_dir(KValue unused) {
    (void)unused;
#ifdef _WIN32
    char path[4096];
    DWORD length=GetTempPathA((DWORD)sizeof(path),path);
    if (!length || length>=sizeof(path)) return kv_cstr(".");
    size_t used=(size_t)length;
    while (used>1 && k_path_is_separator(path[used-1]) && !(used==3 && path[1]==':')) used--;
    return kv_strn(path,used);
#else
    const char *t=getenv("TMPDIR"); if (!t||!*t) t="/tmp";
    return kv_cstr(t);
#endif
}
static KValue k_fs_temp_file(KValue prefix) {
	if (k_fs_path_has_nul(prefix)) return kv_res(0,kv_cstr("cannot create temp file"));
	KValue directory=k_fs_temp_dir(kv_nil());
	KBuf path; kb_init(&path);
	kb_putn(&path,directory.u.s.data,directory.u.s.len);
	if (path.len>0 && !k_path_is_separator(path.buf[path.len-1])) kb_putc(&path,k_path_native_separator());
	kb_putn(&path,prefix.u.s.data,prefix.u.s.len);
	kb_puts(&path,"-XXXXXX");
	int fd=k_fs_open_temp(path.buf,path.cap);
	if (fd<0) return kv_res(0,kv_cstr("cannot create temp file"));
	if (k_fs_close_fd(fd)!=0) {
		int error=errno ? errno : EIO;
		remove(path.buf); errno=error;
		return kv_res(0,kv_cstr("cannot create temp file"));
	}
	return kv_res(1,kv_strn(path.buf,path.len));
}

`
