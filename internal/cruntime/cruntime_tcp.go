package cruntime

// Bounded TCP sockets, deadlines, and cleanup.
const cRuntimeTCP = `/* ---- bounded TCP client ------------------------------------------------ */
struct KTcpSocket {
    KSocketFD fd;
    int closed;
    struct KTcpSocket *next;
};
static KTcpSocket *k_tcp_sockets = NULL;
#ifdef _WIN32
static int k_tcp_winsock_started = 0;
#define K_TCP_CLOSE(fd) closesocket(fd)
#define K_TCP_INTERRUPTED(error) ((error)==WSAEINTR)
#define K_TCP_WOULD_BLOCK(error) ((error)==WSAEWOULDBLOCK)
#define K_TCP_CONNECT_PENDING(error) ((error)==WSAEWOULDBLOCK || (error)==WSAEINPROGRESS || (error)==WSAEALREADY)
#else
#define K_TCP_CLOSE(fd) close(fd)
#define K_TCP_INTERRUPTED(error) ((error)==EINTR)
#define K_TCP_WOULD_BLOCK(error) ((error)==EAGAIN || (error)==EWOULDBLOCK)
#define K_TCP_CONNECT_PENDING(error) ((error)==EINPROGRESS || (error)==EWOULDBLOCK || (error)==EALREADY)
#endif
#ifndef MSG_NOSIGNAL
#define MSG_NOSIGNAL 0
#endif

static KValue kv_tcp(KTcpSocket *socket) {
    KValue value; memset(&value,0,sizeof(value)); value.tag=K_TCP;
    value.u.tcp.socket=socket; return value;
}
static int k_tcp_socket_error(void) {
#ifdef _WIN32
    return WSAGetLastError();
#else
    return errno;
#endif
}
static const char *k_tcp_error_text(int error) {
#ifdef _WIN32
    switch (error) {
    case WSAECONNREFUSED: return "connection refused";
    case WSAETIMEDOUT: return "i/o timeout";
    case WSAECONNRESET: return "connection reset";
    case WSAEHOSTUNREACH: return "no route to host";
    case WSAENETUNREACH: return "network unreachable";
    case WSAEWOULDBLOCK: return "i/o timeout";
    default: return "network operation failed";
    }
#else
    switch (error) {
    case ECONNREFUSED: return "connection refused";
    case ETIMEDOUT: case EAGAIN: return "i/o timeout";
    case ECONNRESET: return "connection reset";
    case EHOSTUNREACH: return "no route to host";
    case ENETUNREACH: return "network unreachable";
    default: return "network operation failed";
    }
#endif
}
static unsigned long long k_tcp_now_ms(void) {
#ifdef _WIN32
    return (unsigned long long)GetTickCount64();
#else
    struct timespec now;
    if (clock_gettime(CLOCK_MONOTONIC,&now)!=0) return 0;
    return (unsigned long long)now.tv_sec*1000ULL+(unsigned long long)now.tv_nsec/1000000ULL;
#endif
}
static unsigned long long k_tcp_deadline_after(unsigned long long now, unsigned long long duration) {
    return now>ULLONG_MAX-duration?ULLONG_MAX:now+duration;
}
static int k_tcp_deadline_reached(unsigned long long now, unsigned long long deadline) {
    return now>=deadline;
}
static int k_tcp_set_blocking(KSocketFD fd, int blocking) {
#ifdef _WIN32
    u_long nonblocking=blocking?0UL:1UL;
    return ioctlsocket(fd,FIONBIO,&nonblocking)==0 ? 0 : -1;
#else
    int flags=fcntl(fd,F_GETFL,0);
    if (flags<0) return -1;
    if (blocking) flags&=~O_NONBLOCK; else flags|=O_NONBLOCK;
    return fcntl(fd,F_SETFL,flags);
#endif
}
static unsigned long long k_tcp_operation_deadline(void) {
    long long timeout=k_max_wall_ms>0?k_max_wall_ms:10000;
    unsigned long long deadline=k_tcp_deadline_after(k_tcp_now_ms(),(unsigned long long)timeout);
    if (k_runtime_deadline_ms && k_runtime_deadline_ms<deadline) deadline=k_runtime_deadline_ms;
    return deadline;
}
static void k_tcp_set_timeout_error(void) {
#ifdef _WIN32
    WSASetLastError(WSAETIMEDOUT);
#else
    errno=ETIMEDOUT;
#endif
}
static int k_tcp_deadline_expired(unsigned long long deadline) {
    if (!k_tcp_deadline_reached(k_tcp_now_ms(),deadline)) return 0;
    k_tcp_set_timeout_error();
    return 1;
}
static int k_tcp_wait_connected(KSocketFD fd, unsigned long long deadline) {
    for (;;) {
        unsigned long long now=k_tcp_now_ms();
        if (k_tcp_deadline_reached(now,deadline)) return 0;
        unsigned long long remaining=deadline-now;
        struct timeval timeout;
        timeout.tv_sec=(long)(remaining/1000ULL);
        timeout.tv_usec=(long)((remaining%1000ULL)*1000ULL);
        fd_set writable, exceptional;
        FD_ZERO(&writable); FD_ZERO(&exceptional);
#ifndef _WIN32
        if ((unsigned long long)fd>=FD_SETSIZE) return -1;
#endif
        FD_SET(fd,&writable); FD_SET(fd,&exceptional);
#ifdef _WIN32
        int ready=select(0,NULL,&writable,&exceptional,&timeout);
#else
        int ready=select(fd+1,NULL,&writable,&exceptional,&timeout);
#endif
        if (ready>0) {
            int error=0;
#ifdef _WIN32
            int length=(int)sizeof(error);
#else
            socklen_t length=(socklen_t)sizeof(error);
#endif
            if (getsockopt(fd,SOL_SOCKET,SO_ERROR,(char*)&error,&length)!=0) return -1;
            if (error!=0) {
#ifdef _WIN32
                WSASetLastError(error);
#else
                errno=error;
#endif
                return -1;
            }
            return 1;
        }
        if (ready==0) {
            // select may round a sub-millisecond remainder down. Re-check the
            // absolute deadline before reporting a timeout.
            if (k_tcp_deadline_reached(k_tcp_now_ms(),deadline)) return 0;
            continue;
        }
        int error=k_tcp_socket_error();
        if (!K_TCP_INTERRUPTED(error)) return -1;
    }
}
static int k_tcp_wait_io(KSocketFD fd, int writing, unsigned long long deadline) {
    for (;;) {
        unsigned long long now=k_tcp_now_ms();
        if (k_tcp_deadline_reached(now,deadline)) {
            k_tcp_set_timeout_error();
            return 0;
        }
        unsigned long long remaining=deadline-now;
        struct timeval timeout;
        timeout.tv_sec=(long)(remaining/1000ULL);
        timeout.tv_usec=(long)((remaining%1000ULL)*1000ULL);
        fd_set ready;
        FD_ZERO(&ready);
#ifndef _WIN32
        if ((unsigned long long)fd>=FD_SETSIZE) {
            errno=EINVAL;
            return -1;
        }
#endif
        FD_SET(fd,&ready);
#ifdef _WIN32
        int selected=select(0,writing?NULL:&ready,writing?&ready:NULL,NULL,&timeout);
#else
        int selected=select(fd+1,writing?NULL:&ready,writing?&ready:NULL,NULL,&timeout);
#endif
        if (selected>0) return 1;
        if (selected==0) {
            // Do not let select's timeval rounding end an operation before
            // its absolute deadline. Loop until the monotonic clock confirms it.
            if (k_tcp_deadline_reached(k_tcp_now_ms(),deadline)) {
                k_tcp_set_timeout_error();
                return 0;
            }
            continue;
        }
        int error=k_tcp_socket_error();
        if (!K_TCP_INTERRUPTED(error)) return -1;
    }
}
static KValue k_tcp_connect_error(KValue host, KValue port, const char *reason) {
    char port_text[32]; snprintf(port_text,sizeof(port_text),"%lld",port.u.i);
    KBuf message; kb_init(&message);
    kb_puts(&message,"dial tcp "); kb_putn(&message,host.u.s.data,host.u.s.len);
    kb_putc(&message,':'); kb_puts(&message,port_text); kb_puts(&message,": connect: "); kb_puts(&message,reason);
    return kv_res(0,kv_str_take(message.buf,message.len));
}
static KValue k_tcp_connect_until(KValue host, KValue port, unsigned long long deadline) {
    if (port.u.i<1 || port.u.i>65535) return kv_res(0,kv_cstr("port must be between 1 and 65535"));
    if (memchr(host.u.s.data,0,host.u.s.len)) return kv_res(0,kv_cstr("host contains NUL"));
#ifdef _WIN32
    if (!k_tcp_winsock_started) {
        WSADATA data;
        if (WSAStartup(MAKEWORD(2,2),&data)!=0) return kv_res(0,kv_cstr("cannot initialize TCP sockets"));
        k_tcp_winsock_started=1;
    }
#endif
    char service[8]; snprintf(service,sizeof(service),"%lld",port.u.i);
    struct addrinfo hints; memset(&hints,0,sizeof(hints));
    hints.ai_family=AF_UNSPEC; hints.ai_socktype=SOCK_STREAM; hints.ai_protocol=IPPROTO_TCP;
    struct addrinfo *addresses=NULL;
    int lookup=getaddrinfo(host.u.s.data,service,&hints,&addresses);
    if (lookup!=0) {
        if (k_tcp_deadline_expired(deadline))
            return k_tcp_connect_error(host,port,k_tcp_error_text(k_tcp_socket_error()));
        return k_tcp_connect_error(host,port,"name resolution failed");
    }
    if (k_tcp_deadline_expired(deadline)) {
        freeaddrinfo(addresses);
        return k_tcp_connect_error(host,port,k_tcp_error_text(k_tcp_socket_error()));
    }
    /* Reserve before opening descriptors: kalloc can longjmp on budget failure. */
    KTcpSocket *handle=(KTcpSocket*)kalloc(sizeof(KTcpSocket));
    memset(handle,0,sizeof(*handle));
    int last_error=0;
    KSocketFD connected=K_INVALID_SOCKET;
    for (struct addrinfo *address=addresses;address;address=address->ai_next) {
        KSocketFD fd=socket(address->ai_family,address->ai_socktype,address->ai_protocol);
        if (fd==K_INVALID_SOCKET) { last_error=k_tcp_socket_error(); continue; }
        if (k_tcp_set_blocking(fd,0)!=0) { last_error=k_tcp_socket_error(); K_TCP_CLOSE(fd); continue; }
        int result=connect(fd,address->ai_addr,(int)address->ai_addrlen);
        if (result!=0) {
            int error=k_tcp_socket_error();
            if (!K_TCP_CONNECT_PENDING(error)) { last_error=error; K_TCP_CLOSE(fd); continue; }
            result=k_tcp_wait_connected(fd,deadline);
            if (result<=0) {
                last_error=result==0 ?
#ifdef _WIN32
                    WSAETIMEDOUT
#else
                    ETIMEDOUT
#endif
                    : k_tcp_socket_error();
                K_TCP_CLOSE(fd); continue;
            }
        }
        connected=fd; break;
    }
    freeaddrinfo(addresses);
    if (connected==K_INVALID_SOCKET) return k_tcp_connect_error(host,port,k_tcp_error_text(last_error));
    handle->fd=connected; handle->next=k_tcp_sockets; k_tcp_sockets=handle;
    return kv_res(1,kv_tcp(handle));
}
static KValue k_tcp_connect(KValue host, KValue port) {
    return k_tcp_connect_until(host,port,k_tcp_operation_deadline());
}
static KValue k_tcp_send_until(KValue value, KValue bytes, unsigned long long deadline) {
    KTcpSocket *socket=value.tag==K_TCP?value.u.tcp.socket:NULL;
    if (!socket) kfail("invalid TcpSocket handle");
    if (socket->closed) return kv_res(0,kv_cstr("TcpSocket handle is closed"));
    size_t written=0;
    while (written<bytes.u.s.len) {
        if (k_tcp_deadline_expired(deadline)) {
            KBuf message; kb_init(&message); kb_puts(&message,"write tcp: "); kb_puts(&message,k_tcp_error_text(k_tcp_socket_error()));
            return kv_res(0,kv_str_take(message.buf,message.len));
        }
#ifdef _WIN32
        size_t remaining=bytes.u.s.len-written;
        int amount=remaining>(size_t)INT_MAX?INT_MAX:(int)remaining;
        int n=send(socket->fd,bytes.u.s.data+written,amount,0);
#else
        ssize_t n=send(socket->fd,bytes.u.s.data+written,bytes.u.s.len-written,MSG_NOSIGNAL);
#endif
        if (n>0) { written+=(size_t)n; continue; }
        if (n==0) return kv_res(0,kv_cstr("TCP write made no progress"));
        int error=k_tcp_socket_error();
        if (K_TCP_INTERRUPTED(error)) continue;
        if (K_TCP_WOULD_BLOCK(error)) {
            int ready=k_tcp_wait_io(socket->fd,1,deadline);
            if (ready>0) continue;
            error=k_tcp_socket_error();
        }
        KBuf message; kb_init(&message); kb_puts(&message,"write tcp: "); kb_puts(&message,k_tcp_error_text(error));
        return kv_res(0,kv_str_take(message.buf,message.len));
    }
    return kv_res(1,kv_int((long long)written));
}
static KValue k_tcp_send(KValue value, KValue bytes) {
    return k_tcp_send_until(value,bytes,k_tcp_operation_deadline());
}
static KValue k_tcp_receive(KValue value, KValue maximum) {
    if (maximum.u.i<1 || maximum.u.i>k_max_tcp_receive)
        return kv_res(0,kv_cstr("receive size is outside configured limits"));
    KTcpSocket *socket=value.tag==K_TCP?value.u.tcp.socket:NULL;
    if (!socket) kfail("invalid TcpSocket handle");
    if (socket->closed) return kv_res(0,kv_cstr("TcpSocket handle is closed"));
    size_t length=(size_t)maximum.u.i;
    unsigned long long deadline=k_tcp_operation_deadline();
    char *data=(char*)kalloc(length+1);
    for (;;) {
        if (k_tcp_deadline_expired(deadline)) {
            KBuf message; kb_init(&message); kb_puts(&message,"read tcp: "); kb_puts(&message,k_tcp_error_text(k_tcp_socket_error()));
            return kv_res(0,kv_str_take(message.buf,message.len));
        }
#ifdef _WIN32
        int received=recv(socket->fd,data,(int)length,0);
#else
        ssize_t received=recv(socket->fd,data,length,0);
#endif
        if (received==0) return kv_res(0,kv_cstr("EOF"));
        if (received>0) return kv_res(1,kv_bytesn(data,(size_t)received));
        int error=k_tcp_socket_error();
        if (K_TCP_INTERRUPTED(error)) continue;
        if (K_TCP_WOULD_BLOCK(error)) {
            int ready=k_tcp_wait_io(socket->fd,0,deadline);
            if (ready>0) continue;
            error=k_tcp_socket_error();
        }
        KBuf message; kb_init(&message);
        kb_puts(&message,"read tcp: "); kb_puts(&message,k_tcp_error_text(error));
        return kv_res(0,kv_str_take(message.buf,message.len));
    }
}
static KValue k_tcp_close(KValue value) {
    KTcpSocket *socket=value.tag==K_TCP?value.u.tcp.socket:NULL;
    if (!socket) kfail("invalid TcpSocket handle");
    if (socket->closed) kfail("TcpSocket handle is already closed");
    socket->closed=1;
    if (K_TCP_CLOSE(socket->fd)!=0) kfail(k_tcp_error_text(k_tcp_socket_error()));
    return kv_nil();
}
static int k_tcp_has_open_sockets(void) {
    for (KTcpSocket *socket=k_tcp_sockets;socket;socket=socket->next) if (!socket->closed) return 1;
    return 0;
}
static void k_tcp_cleanup(void) {
    for (KTcpSocket *socket=k_tcp_sockets;socket;socket=socket->next) {
        if (!socket->closed) { socket->closed=1; K_TCP_CLOSE(socket->fd); }
    }
#ifdef _WIN32
    if (k_tcp_winsock_started) { WSACleanup(); k_tcp_winsock_started=0; }
#endif
}

`
