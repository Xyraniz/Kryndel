package cruntime

// Bounded HTTP parsing and client requests.
const cRuntimeHTTP = `/* ---- bounded plain HTTP client ----------------------------------------- */
static int k_http_hex(unsigned char c) {
    if (c>='0'&&c<='9') return c-'0';
    if (c>='a'&&c<='f') return c-'a'+10;
    if (c>='A'&&c<='F') return c-'A'+10;
    return -1;
}
static int k_http_iequal(const char *a, size_t n, const char *b) {
    if (strlen(b)!=n) return 0;
    for (size_t i=0;i<n;i++) {
        unsigned char x=(unsigned char)a[i], y=(unsigned char)b[i];
        if (x>='A'&&x<='Z') x=(unsigned char)(x+('a'-'A'));
        if (y>='A'&&y<='Z') y=(unsigned char)(y+('a'-'A'));
        if (x!=y) return 0;
    }
    return 1;
}
static int k_http_token(unsigned char c) {
    return (c>='0'&&c<='9')||(c>='A'&&c<='Z')||(c>='a'&&c<='z')||
        c=='!'||c=='#'||c=='$'||c=='%'||c=='&'||c=='\''||c=='*'||c=='+'||
        c=='-'||c=='.'||c=='^'||c=='_'||c==0x60||c=='|'||c=='~';
}
static int k_http_unreserved(unsigned char c) {
    return (c>='0'&&c<='9')||(c>='A'&&c<='Z')||(c>='a'&&c<='z')||
        c=='-'||c=='.'||c=='_'||c=='~';
}
static int k_http_pchar(unsigned char c, int query) {
    return k_http_unreserved(c)||c=='!'||c=='$'||c=='&'||c=='\''||c=='('||
        c==')'||c=='*'||c=='+'||c==','||c==';'||c=='='||c==':'||c=='@'||
        c=='/'||(query&&c=='?');
}
static int k_http_valid_escapes(const char *s, size_t n) {
    for (size_t i=0;i<n;i++) if (s[i]=='%') {
        if (i+2>=n||k_http_hex((unsigned char)s[i+1])<0||k_http_hex((unsigned char)s[i+2])<0) return 0;
        i+=2;
    }
    return 1;
}
static void k_http_escape(KBuf *out, const char *s, size_t n, int query) {
    static const char hex[]="0123456789ABCDEF";
    for (size_t i=0;i<n;i++) {
        unsigned char c=(unsigned char)s[i];
        if (k_http_pchar(c,query)||c=='%') kb_putc(out,(char)c);
        else { kb_putc(out,'%'); kb_putc(out,hex[c>>4]); kb_putc(out,hex[c&15]); }
    }
}
static int k_http_parse_url(KValue url, KBuf *host, KBuf *port, KBuf *target, KBuf *authority, const char **error) {
    const char *s=url.u.s.data; size_t n=url.u.s.len, at=0, auth_start, auth_end, host_start, host_end, path_end, query_at;
    if (memchr(s,0,n)) { *error="URL contains NUL"; return 0; }
    if (n>=8&&k_http_iequal(s,7,"http://")) at=7;
    else if (n>=8&&k_http_iequal(s,8,"https://")) { *error="HTTPS is not supported by the C AOT backend"; return 0; }
    else { *error="unsupported protocol scheme"; return 0; }
    auth_start=at; auth_end=at;
    while (auth_end<n&&s[auth_end]!='/'&&s[auth_end]!='?'&&s[auth_end]!='#') auth_end++;
    if (auth_end==auth_start) { *error="missing host"; return 0; }
    if (memchr(s+auth_start,'@',auth_end-auth_start)) { *error="URL user information is not supported by the C AOT backend"; return 0; }
    for (size_t i=auth_start;i<auth_end;i++) if ((unsigned char)s[i]<=0x20||s[i]==0x7f) { *error="invalid character in host name"; return 0; }
    kb_putn(authority,s+auth_start,auth_end-auth_start);
    host_start=auth_start; host_end=auth_end;
    if (s[host_start]=='[') {
        size_t close=host_start+1;
        while (close<auth_end&&s[close]!=']') close++;
        if (close==auth_end||close==host_start+1) { *error="invalid IPv6 host"; return 0; }
        host_start++; host_end=close;
        if (close+1<auth_end) {
            if (s[close+1]!=':') { *error="invalid port in URL"; return 0; }
            if (close+2==auth_end) { *error="invalid port in URL"; return 0; }
            kb_putn(port,s+close+2,auth_end-close-2);
        } else if (close+1==auth_end) { }
        kb_putn(host,s+host_start,host_end-host_start);
    } else {
        size_t colon=auth_end;
        for (size_t i=auth_start;i<auth_end;i++) if (s[i]==':') {
            if (colon!=auth_end) { *error="IPv6 address must be enclosed in brackets"; return 0; }
            colon=i;
        }
        if (colon<auth_end) {
            host_end=colon;
            if (colon+1==auth_end) { *error="invalid port in URL"; return 0; }
            kb_putn(port,s+colon+1,auth_end-colon-1);
        }
        if (host_end==host_start) { *error="missing host"; return 0; }
        kb_putn(host,s+host_start,host_end-host_start);
    }
    if (host->len==0) { *error="missing host"; return 0; }
    if (port->len==0) kb_puts(port,"80");
    unsigned long long port_number=0;
    for (size_t i=0;i<port->len;i++) {
        unsigned char c=(unsigned char)port->buf[i];
        if (c<'0'||c>'9'||port_number>65535) { *error="invalid port in URL"; return 0; }
        port_number=port_number*10+(unsigned long long)(c-'0');
    }
    if (port_number<1||port_number>65535) { *error="invalid port in URL"; return 0; }
    at=auth_end; path_end=at;
    while (path_end<n&&s[path_end]!='?'&&s[path_end]!='#') path_end++;
    if (at==path_end||s[at]!='/' ) kb_putc(target,'/');
    if (at<path_end) {
        if (!k_http_valid_escapes(s+at,path_end-at)) { *error="invalid URL escape"; return 0; }
        k_http_escape(target,s+at,path_end-at,0);
    }
    query_at=path_end;
    if (query_at<n&&s[query_at]=='?') {
        size_t query_end=query_at+1;
        while (query_end<n&&s[query_end]!='#') query_end++;
        if (!k_http_valid_escapes(s+query_at+1,query_end-query_at-1)) { *error="invalid URL escape"; return 0; }
        kb_putc(target,'?');
        k_http_escape(target,s+query_at+1,query_end-query_at-1,1);
    }
    return 1;
}
static long long k_http_recv_some(KTcpSocket *socket, char *data, size_t cap, unsigned long long deadline) {
    for (;;) {
        if (k_tcp_deadline_expired(deadline)) return -1;
#ifdef _WIN32
        int want=cap>(size_t)INT_MAX?INT_MAX:(int)cap;
        int received=recv(socket->fd,data,want,0);
#else
        ssize_t received=recv(socket->fd,data,cap,0);
#endif
        if (received>=0) return (long long)received;
        int error=k_tcp_socket_error();
        if (K_TCP_INTERRUPTED(error)) continue;
        if (K_TCP_WOULD_BLOCK(error)) {
            int ready=k_tcp_wait_io(socket->fd,0,deadline);
            if (ready>0) continue;
            return -1;
        }
        return -1;
    }
}
typedef struct { KTcpSocket *socket; unsigned long long deadline; char data[8192]; size_t at,len; int eof; } KHTTPReader;
static int k_http_fill(KHTTPReader *reader) {
    if (reader->at<reader->len) return 1;
    reader->at=reader->len=0;
    long long n=k_http_recv_some(reader->socket,reader->data,sizeof(reader->data),reader->deadline);
    if (n<0) return -1;
    if (n==0) { reader->eof=1; return 0; }
    reader->len=(size_t)n;
    return 1;
}
static KValue k_http_read_error(KHTTPReader *reader, int status, const char *eof_message, const char *invalid_message) {
    if (status==-1) {
        const char *message=k_tcp_deadline_reached(k_tcp_now_ms(),reader->deadline)?"i/o timeout":k_tcp_error_text(k_tcp_socket_error());
        return kv_res(0,kv_cstr(message));
    }
    return kv_res(0,kv_cstr(status==0?eof_message:invalid_message));
}
static int k_http_read_line(KHTTPReader *reader, KBuf *line, size_t max) {
    for (;;) {
        int ready=k_http_fill(reader);
        if (ready<=0) return ready;
        char c=reader->data[reader->at++];
        if (c=='\n') {
            if (line->len==0||line->buf[line->len-1]!='\r') return -2;
            line->len--; line->buf[line->len]=0;
            return 1;
        }
        if (c=='\r'&&reader->at<reader->len&&reader->data[reader->at]!='\n') return -2;
        if (line->len>=max) return -2;
        kb_putc(line,c);
    }
}
static int k_http_read_exact(KHTTPReader *reader, KBuf *body, size_t length, size_t limit) {
    if (length>limit-body->len) return -2;
    while (length) {
        int ready=k_http_fill(reader);
        if (ready<=0) return ready;
        size_t avail=reader->len-reader->at;
        size_t take=avail<length?avail:length;
        if (body->len>SIZE_MAX-take-1) return -2;
        while (body->cap<=body->len+take) kb_grow(body);
        memcpy(body->buf+body->len,reader->data+reader->at,take);
        body->len+=take; body->buf[body->len]=0;
        reader->at+=take; length-=take;
    }
    return 1;
}
static KValue k_http_request(KValue method, KValue url, KValue body_value) {
    KBuf host,port,target,authority,request,response_line,header,body;
    const char *url_error=NULL;
    if (method.u.s.len==0) return kv_res(0,kv_cstr("method must not be empty"));
    for (size_t i=0;i<method.u.s.len;i++) if (!k_http_token((unsigned char)method.u.s.data[i])) return kv_res(0,kv_cstr("invalid method"));
    kb_init(&host); kb_init(&port); kb_init(&target); kb_init(&authority);
    if (!k_http_parse_url(url,&host,&port,&target,&authority,&url_error)) return kv_res(0,kv_cstr(url_error));
    if (body_value.u.s.len>(size_t)LLONG_MAX||target.len>(size_t)LLONG_MAX) return kv_res(0,kv_cstr("request is too large"));
    unsigned long long deadline=k_tcp_operation_deadline();
    KValue connect_result=k_tcp_connect_until(kv_strn(host.buf,host.len),kv_int(atoll(port.buf)),deadline);
    if (!connect_result.u.res.ok) return connect_result;
    KValue connected=*connect_result.u.res.inner;
    KTcpSocket *socket=connected.u.tcp.socket;
    kb_init(&request);
    kb_putn(&request,method.u.s.data,method.u.s.len); kb_putc(&request,' '); kb_putn(&request,target.buf,target.len); kb_puts(&request," HTTP/1.1\r\nHost: "); kb_putn(&request,authority.buf,authority.len);
    kb_puts(&request,"\r\nUser-Agent: Go-http-client/1.1\r\nAccept-Encoding: identity\r\nConnection: close\r\nContent-Length: ");
    char body_length[32]; snprintf(body_length,sizeof(body_length),"%llu",(unsigned long long)body_value.u.s.len); kb_puts(&request,body_length); kb_puts(&request,"\r\n\r\n"); kb_putn(&request,body_value.u.s.data,body_value.u.s.len);
    KValue sent=k_tcp_send_until(connected,kv_bytesn(request.buf,request.len),deadline);
    if (!sent.u.res.ok) { KValue error=sent; k_tcp_close(connected); return error; }
    if ((unsigned long long)sent.u.res.inner->u.i!=(unsigned long long)request.len) { k_tcp_close(connected); return kv_res(0,kv_cstr("write tcp: short write")); }
    KHTTPReader reader={0}; reader.socket=socket; reader.deadline=deadline;
    kb_init(&response_line); kb_init(&header); kb_init(&body);
    int status=0, chunked=0, has_length=0, no_body=0; size_t content_length=0, headers_size=0;
    for (int interim=0;interim<6;interim++) {
        response_line.len=0; response_line.buf[0]=0;
        int line_status=k_http_read_line(&reader,&response_line,8192);
        if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP status","invalid HTTP response"); }
        if (response_line.len<12||memcmp(response_line.buf,"HTTP/1.",7)!=0||(response_line.buf[7]!='0'&&response_line.buf[7]!='1')||response_line.buf[8]!=' '||response_line.buf[9]<'1'||response_line.buf[9]>'5'||response_line.buf[10]<'0'||response_line.buf[10]>'9'||response_line.buf[11]<'0'||response_line.buf[11]>'9'||(response_line.len>12&&response_line.buf[12]!=' ')) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response status")); }
        status=(response_line.buf[9]-'0')*100+(response_line.buf[10]-'0')*10+(response_line.buf[11]-'0');
        headers_size=0; has_length=0; chunked=0; content_length=0;
        for (;;) {
            header.len=0; header.buf[0]=0;
            line_status=k_http_read_line(&reader,&header,65536);
            if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP headers","invalid HTTP response headers"); }
            headers_size+=header.len+2;
            if (headers_size>65536) { k_tcp_close(connected); return kv_res(0,kv_cstr("HTTP response headers exceed configured limit")); }
            if (header.len==0) break;
            char *colon=memchr(header.buf,':',header.len);
            if (!colon||colon==header.buf) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
            size_t name_len=(size_t)(colon-header.buf), value_at=name_len+1;
            for (size_t i=0;i<name_len;i++) if (!k_http_token((unsigned char)header.buf[i])) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
            while (value_at<header.len&&(header.buf[value_at]==' '||header.buf[value_at]=='\t')) value_at++;
            size_t value_end=header.len; while (value_end>value_at&&(header.buf[value_end-1]==' '||header.buf[value_end-1]=='\t')) value_end--;
            if (k_http_iequal(header.buf,name_len,"content-length")) {
                if (has_length||value_at==value_end) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
                size_t parsed=0;
                for (size_t i=value_at;i<value_end;i++) {
                    unsigned char c=(unsigned char)header.buf[i];
                    if (c<'0'||c>'9'||parsed>(SIZE_MAX-(size_t)(c-'0'))/10) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
                    parsed=parsed*10+(size_t)(c-'0');
                }
                if (parsed>(size_t)LLONG_MAX) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
                content_length=parsed; has_length=1;
            } else if (k_http_iequal(header.buf,name_len,"transfer-encoding")) {
                if (chunked||!k_http_iequal(header.buf+value_at,value_end-value_at,"chunked")) { k_tcp_close(connected); return kv_res(0,kv_cstr("unsupported HTTP transfer encoding")); }
                chunked=1;
            }
        }
        if (status<200&&status!=101) continue;
        no_body=(method.u.s.len==4&&k_http_iequal(method.u.s.data,method.u.s.len,"HEAD"))||status==204||status==304||status==101;
        break;
    }
    if (status<200&&status!=101) { k_tcp_close(connected); return kv_res(0,kv_cstr("too many informational HTTP responses")); }
    if (chunked&&has_length) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response headers")); }
    if (!no_body) {
        size_t limit=k_max_tcp_receive<0?0:(size_t)k_max_tcp_receive;
        if (has_length) {
            if (content_length>limit) { k_tcp_close(connected); return kv_res(0,kv_cstr("response exceeds configured input limit")); }
            int read_status=k_http_read_exact(&reader,&body,content_length,limit);
            if (read_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,read_status,"unexpected EOF reading HTTP response","invalid HTTP response body"); }
        } else if (chunked) {
            for (;;) {
                KBuf chunk; kb_init(&chunk);
                int line_status=k_http_read_line(&reader,&chunk,8192);
                if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP chunk","invalid HTTP chunk"); }
                size_t digits=0; unsigned long long chunk_size=0;
                while (digits<chunk.len&&chunk.buf[digits]!=';') {
                    int digit=k_http_hex((unsigned char)chunk.buf[digits]);
                    if (digit<0||chunk_size>(ULLONG_MAX-(unsigned long long)digit)/16ULL) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP chunk")); }
                    chunk_size=chunk_size*16ULL+(unsigned long long)digit; digits++;
                }
                if (digits==0||chunk_size>(unsigned long long)(limit-body.len)) { k_tcp_close(connected); return kv_res(0,kv_cstr(chunk_size>(unsigned long long)(limit-body.len)?"response exceeds configured input limit":"invalid HTTP chunk")); }
                if (chunk_size==0) {
                    for (;;) {
                        KBuf trailer; kb_init(&trailer);
                        line_status=k_http_read_line(&reader,&trailer,65536);
                        if (line_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,line_status,"unexpected EOF reading HTTP chunk trailer","invalid HTTP chunk trailer"); }
                        headers_size+=trailer.len+2;
                        if (headers_size>65536) { k_tcp_close(connected); return kv_res(0,kv_cstr("HTTP response headers exceed configured limit")); }
                        if (trailer.len==0) break;
                        if (!memchr(trailer.buf,':',trailer.len)) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP chunk trailer")); }
                    }
                    break;
                }
                int read_status=k_http_read_exact(&reader,&body,(size_t)chunk_size,limit);
                if (read_status<=0) { k_tcp_close(connected); return k_http_read_error(&reader,read_status,"unexpected EOF reading HTTP chunk","invalid HTTP chunk"); }
                char crlf[2]; size_t got=0;
                while (got<2) { int ready=k_http_fill(&reader); if (ready<=0) { k_tcp_close(connected); return k_http_read_error(&reader,ready,"unexpected EOF reading HTTP chunk","invalid HTTP chunk"); } crlf[got++]=reader.data[reader.at++]; }
                if (crlf[0]!='\r'||crlf[1]!='\n') { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP chunk")); }
            }
        } else {
            for (;;) {
                int ready=k_http_fill(&reader);
                if (ready<0) { k_tcp_close(connected); return k_http_read_error(&reader,ready,"unexpected EOF reading HTTP response","invalid HTTP response body"); }
                if (ready==0) break;
                size_t available=reader.len-reader.at;
                if (available>limit-body.len) { k_tcp_close(connected); return kv_res(0,kv_cstr("response exceeds configured input limit")); }
                if (k_http_read_exact(&reader,&body,available,limit)<=0) { k_tcp_close(connected); return kv_res(0,kv_cstr("invalid HTTP response body")); }
            }
        }
    }
    k_tcp_close(connected);
    if (status<200||status>=300) {
        KBuf message; kb_init(&message); kb_puts(&message,"HTTP status "); char status_text[16]; snprintf(status_text,sizeof(status_text),"%d",status); kb_puts(&message,status_text);
        return kv_res(0,kv_str_take(message.buf,message.len));
    }
    if (!k_utf8_valid(body.buf,body.len)) return kv_res(0,kv_cstr("response is not valid UTF-8"));
    return kv_res(1,kv_strn(body.buf,body.len));
}

`
