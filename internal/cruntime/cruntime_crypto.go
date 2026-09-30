package cruntime

// Extended hashing, AES-GCM, key derivation, comparisons, and URL-safe base64.
const cRuntimeCrypto = `
/* ---- SHA-512 / SHA-384 ------------------------------------------------- */
typedef struct { uint64_t h[8]; uint64_t len; unsigned char buf[128]; size_t n; } KSha512;
static const uint64_t k_sha512_k[80] = {
0x428a2f98d728ae22ULL,0x7137449123ef65cdULL,0xb5c0fbcfec4d3b2fULL,0xe9b5dba58189dbbcULL,
0x3956c25bf348b538ULL,0x59f111f1b605d019ULL,0x923f82a4af194f9bULL,0xab1c5ed5da6d8118ULL,
0xd807aa98a3030242ULL,0x12835b0145706fbeULL,0x243185be4ee4b28cULL,0x550c7dc3d5ffb4e2ULL,
0x72be5d74f27b896fULL,0x80deb1fe3b1696b1ULL,0x9bdc06a725c71235ULL,0xc19bf174cf692694ULL,
0xe49b69c19ef14ad2ULL,0xefbe4786384f25e3ULL,0x0fc19dc68b8cd5b5ULL,0x240ca1cc77ac9c65ULL,
0x2de92c6f592b0275ULL,0x4a7484aa6ea6e483ULL,0x5cb0a9dcbd41fbd4ULL,0x76f988da831153b5ULL,
0x983e5152ee66dfabULL,0xa831c66d2db43210ULL,0xb00327c898fb213fULL,0xbf597fc7beef0ee4ULL,
0xc6e00bf33da88fc2ULL,0xd5a79147930aa725ULL,0x06ca6351e003826fULL,0x142929670a0e6e70ULL,
0x27b70a8546d22ffcULL,0x2e1b21385c26c926ULL,0x4d2c6dfc5ac42aedULL,0x53380d139d95b3dfULL,
0x650a73548baf63deULL,0x766a0abb3c77b2a8ULL,0x81c2c92e47edaee6ULL,0x92722c851482353bULL,
0xa2bfe8a14cf10364ULL,0xa81a664bbc423001ULL,0xc24b8b70d0f89791ULL,0xc76c51a30654be30ULL,
0xd192e819d6ef5218ULL,0xd69906245565a910ULL,0xf40e35855771202aULL,0x106aa07032bbd1b8ULL,
0x19a4c116b8d2d0c8ULL,0x1e376c085141ab53ULL,0x2748774cdf8eeb99ULL,0x34b0bcb5e19b48a8ULL,
0x391c0cb3c5c95a63ULL,0x4ed8aa4ae3418acbULL,0x5b9cca4f7763e373ULL,0x682e6ff3d6b2b8a3ULL,
0x748f82ee5defb2fcULL,0x78a5636f43172f60ULL,0x84c87814a1f0ab72ULL,0x8cc702081a6439ecULL,
0x90befffa23631e28ULL,0xa4506cebde82bde9ULL,0xbef9a3f7b2c67915ULL,0xc67178f2e372532bULL,
0xca273eceea26619cULL,0xd186b8c721c0c207ULL,0xeada7dd6cde0eb1eULL,0xf57d4f7fee6ed178ULL,
0x06f067aa72176fbaULL,0x0a637dc5a2c898a6ULL,0x113f9804bef90daeULL,0x1b710b35131c471bULL,
0x28db77f523047d84ULL,0x32caab7b40c72493ULL,0x3c9ebe0a15c9bebcULL,0x431d67c49c100d4cULL,
0x4cc5d4becb3e42b6ULL,0x597f299cfc657e2aULL,0x5fcb6fab3ad6faecULL,0x6c44198c4a475817ULL};
#define K_ROTR64(x,n) (((x)>>(n))|((x)<<(64-(n))))
static void k_sha512_block(KSha512 *s, const unsigned char *p) {
    uint64_t w[80];
    for (int i=0;i<16;i++) {
        w[i]=0; for (int j=0;j<8;j++) w[i]=(w[i]<<8)|(uint64_t)p[i*8+j];
    }
    for (int i=16;i<80;i++) {
        uint64_t s0=K_ROTR64(w[i-15],1)^K_ROTR64(w[i-15],8)^(w[i-15]>>7);
        uint64_t s1=K_ROTR64(w[i-2],19)^K_ROTR64(w[i-2],61)^(w[i-2]>>6);
        w[i]=w[i-16]+s0+w[i-7]+s1;
    }
    uint64_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4],f=s->h[5],g=s->h[6],h=s->h[7];
    for (int i=0;i<80;i++) {
        uint64_t S1=K_ROTR64(e,14)^K_ROTR64(e,18)^K_ROTR64(e,41);
        uint64_t ch=(e&f)^((~e)&g);
        uint64_t t1=h+S1+ch+k_sha512_k[i]+w[i];
        uint64_t S0=K_ROTR64(a,28)^K_ROTR64(a,34)^K_ROTR64(a,39);
        uint64_t maj=(a&b)^(a&c)^(b&c);
        uint64_t t2=S0+maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e; s->h[5]+=f; s->h[6]+=g; s->h[7]+=h;
}
static void k_sha512_init(KSha512 *s, int is384) {
    if (is384) {
        s->h[0]=0xcbbb9d5dc1059ed8ULL; s->h[1]=0x629a292a367cd507ULL; s->h[2]=0x9159015a3070dd17ULL; s->h[3]=0x152fecd8f70e5939ULL;
        s->h[4]=0x67332667ffc00b31ULL; s->h[5]=0x8eb44a8768581511ULL; s->h[6]=0xdb0c2e0d64f98fa7ULL; s->h[7]=0x47b5481dbefa4fa4ULL;
    } else {
        s->h[0]=0x6a09e667f3bcc908ULL; s->h[1]=0xbb67ae8584caa73bULL; s->h[2]=0x3c6ef372fe94f82bULL; s->h[3]=0xa54ff53a5f1d36f1ULL;
        s->h[4]=0x510e527fade682d1ULL; s->h[5]=0x9b05688c2b3e6c1fULL; s->h[6]=0x1f83d9abfb41bd6bULL; s->h[7]=0x5be0cd19137e2179ULL;
    }
    s->len=0; s->n=0;
}
static void k_sha512_update(KSha512 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 128 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 128) { k_sha512_block(s, s->buf); s->n = 0; }
    }
}
static void k_sha512_final(KSha512 *s, unsigned char *out, int outlen) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_sha512_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 112) k_sha512_update(s, &z, 1);
    unsigned char lb[16];
    for (int i=0;i<8;i++) lb[i]=0;
    for (int i=0;i<8;i++) lb[8+i]=(unsigned char)(bits >> (56 - i*8));
    k_sha512_update(s, lb, 16);
    for (int i=0;i<8;i++) {
        out[i*8]=(unsigned char)(s->h[i]>>56); out[i*8+1]=(unsigned char)(s->h[i]>>48);
        out[i*8+2]=(unsigned char)(s->h[i]>>40); out[i*8+3]=(unsigned char)(s->h[i]>>32);
        out[i*8+4]=(unsigned char)(s->h[i]>>24); out[i*8+5]=(unsigned char)(s->h[i]>>16);
        out[i*8+6]=(unsigned char)(s->h[i]>>8); out[i*8+7]=(unsigned char)(s->h[i]);
    }
    (void)outlen;
}
static KValue k_crypto_sha512(KValue data) {
    KSha512 s; k_sha512_init(&s,0);
    k_sha512_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[64]; k_sha512_final(&s,out,64);
    return kv_bytesn((const char*)out,64);
}
static KValue k_crypto_sha384(KValue data) {
    KSha512 s; k_sha512_init(&s,1);
    k_sha512_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[64]; k_sha512_final(&s,out,64);
    return kv_bytesn((const char*)out,48);
}

/* ---- SHA-1 ------------------------------------------------------------- */
typedef struct { uint32_t h[5]; uint64_t len; unsigned char buf[64]; size_t n; } KSha1;
#define K_ROTL(x,n) (((x)<<(n))|((x)>>(32-(n))))
static void k_sha1_block(KSha1 *s, const unsigned char *p) {
    uint32_t w[80];
    for (int i=0;i<16;i++) w[i]=((uint32_t)p[i*4]<<24)|((uint32_t)p[i*4+1]<<16)|((uint32_t)p[i*4+2]<<8)|((uint32_t)p[i*4+3]);
    for (int i=16;i<80;i++) w[i]=K_ROTL(w[i-3]^w[i-8]^w[i-14]^w[i-16],1);
    uint32_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4];
    for (int i=0;i<80;i++) {
        uint32_t f,k;
        if (i<20) { f=(b&c)|((~b)&d); k=0x5a827999; }
        else if (i<40) { f=b^c^d; k=0x6ed9eba1; }
        else if (i<60) { f=(b&c)|(b&d)|(c&d); k=0x8f1bbcdc; }
        else { f=b^c^d; k=0xca62c1d6; }
        uint32_t t=K_ROTL(a,5)+f+e+k+w[i];
        e=d; d=c; c=K_ROTL(b,30); b=a; a=t;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e;
}
static void k_sha1_init(KSha1 *s) {
    s->h[0]=0x67452301; s->h[1]=0xefcdab89; s->h[2]=0x98badcfe; s->h[3]=0x10325476; s->h[4]=0xc3d2e1f0;
    s->len=0; s->n=0;
}
static void k_sha1_update(KSha1 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 64 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 64) { k_sha1_block(s, s->buf); s->n = 0; }
    }
}
static void k_sha1_final(KSha1 *s, unsigned char out[20]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_sha1_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 56) k_sha1_update(s, &z, 1);
    unsigned char lb[8];
    for (int i=0;i<8;i++) lb[i]=(unsigned char)(bits >> (56 - i*8));
    k_sha1_update(s, lb, 8);
    for (int i=0;i<5;i++) {
        out[i*4]=(unsigned char)(s->h[i]>>24); out[i*4+1]=(unsigned char)(s->h[i]>>16);
        out[i*4+2]=(unsigned char)(s->h[i]>>8); out[i*4+3]=(unsigned char)(s->h[i]);
    }
}
static KValue k_crypto_sha1(KValue data) {
    KSha1 s; k_sha1_init(&s);
    k_sha1_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[20]; k_sha1_final(&s,out);
    return kv_bytesn((const char*)out,20);
}

/* ---- MD5 --------------------------------------------------------------- */
typedef struct { uint32_t h[4]; uint64_t len; unsigned char buf[64]; size_t n; } KMd5;
#define K_ROTL32(x,n) (((x)<<(n))|((x)>>(32-(n))))
static const uint32_t k_md5_k[64] = {
0xd76aa478,0xe8c7b756,0x242070db,0xc1bdceee,0xf57c0faf,0x4787c62a,0xa8304613,0xfd469501,
0x698098d8,0x8b44f7af,0xffff5bb1,0x895cd7be,0x6b901122,0xfd987193,0xa679438e,0x49b40821,
0xf61e2562,0xc040b340,0x265e5a51,0xe9b6c7aa,0xd62f105d,0x02441453,0xd8a1e681,0xe7d3fbc8,
0x21e1cde6,0xc33707d6,0xf4d50d87,0x455a14ed,0xa9e3e905,0xfcefa3f8,0x676f02d9,0x8d2a4c8a,
0xfffa3942,0x8771f681,0x6d9d6122,0xfde5380c,0xa4beea44,0x4bdecfa9,0xf6bb4b60,0xbebfbc70,
0x289b7ec6,0xeaa127fa,0xd4ef3085,0x04881d05,0xd9d4d039,0xe6db99e5,0x1fa27cf8,0xc4ac5665,
0xf4292244,0x432aff97,0xab9423a7,0xfc93a039,0x655b59c3,0x8f0ccc92,0xffeff47d,0x85845dd1,
0x6fa87e4f,0xfe2ce6e0,0xa3014314,0x4e0811a1,0xf7537e82,0xbd3af235,0x2ad7d2bb,0xeb86d391};
static const int k_md5_s[64] = {
7,12,17,22,7,12,17,22,7,12,17,22,7,12,17,22,
5,9,14,20,5,9,14,20,5,9,14,20,5,9,14,20,
4,11,16,23,4,11,16,23,4,11,16,23,4,11,16,23,
6,10,15,21,6,10,15,21,6,10,15,21,6,10,15,21};
static void k_md5_block(KMd5 *s, const unsigned char *p) {
    uint32_t m[16];
    for (int i=0;i<16;i++) m[i]=((uint32_t)p[i*4])|((uint32_t)p[i*4+1]<<8)|((uint32_t)p[i*4+2]<<16)|((uint32_t)p[i*4+3]<<24);
    uint32_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3];
    for (int i=0;i<64;i++) {
        uint32_t f; int g;
        if (i<16) { f=(b&c)|((~b)&d); g=i; }
        else if (i<32) { f=(d&b)|((~d)&c); g=(5*i+1)%16; }
        else if (i<48) { f=b^c^d; g=(3*i+5)%16; }
        else { f=c^(b|(~d)); g=(7*i)%16; }
        uint32_t tmp=d; d=c; c=b;
        b=b+K_ROTL32(a+f+k_md5_k[i]+m[g],k_md5_s[i]);
        a=tmp;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d;
}
static void k_md5_init(KMd5 *s) {
    s->h[0]=0x67452301; s->h[1]=0xefcdab89; s->h[2]=0x98badcfe; s->h[3]=0x10325476;
    s->len=0; s->n=0;
}
static void k_md5_update(KMd5 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 64 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 64) { k_md5_block(s, s->buf); s->n = 0; }
    }
}
static void k_md5_final(KMd5 *s, unsigned char out[16]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_md5_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 56) k_md5_update(s, &z, 1);
    unsigned char lb[8];
    for (int i=0;i<8;i++) lb[i]=(unsigned char)(bits >> (i*8));
    k_md5_update(s, lb, 8);
    for (int i=0;i<4;i++) {
        out[i*4]=(unsigned char)(s->h[i]); out[i*4+1]=(unsigned char)(s->h[i]>>8);
        out[i*4+2]=(unsigned char)(s->h[i]>>16); out[i*4+3]=(unsigned char)(s->h[i]>>24);
    }
}
static KValue k_crypto_md5(KValue data) {
    KMd5 s; k_md5_init(&s);
    k_md5_update(&s,(const unsigned char*)data.u.s.data,data.u.s.len);
    unsigned char out[16]; k_md5_final(&s,out);
    return kv_bytesn((const char*)out,16);
}

/* ---- AES-256-GCM ------------------------------------------------------- */
static const unsigned char k_aes_sbox[256] = {
0x63,0x7c,0x77,0x7b,0xf2,0x6b,0x6f,0xc5,0x30,0x01,0x67,0x2b,0xfe,0xd7,0xab,0x76,
0xca,0x82,0xc9,0x7d,0xfa,0x59,0x47,0xf0,0xad,0xd4,0xa2,0xaf,0x9c,0xa4,0x72,0xc0,
0xb7,0xfd,0x93,0x26,0x36,0x3f,0xf7,0xcc,0x34,0xa5,0xe5,0xf1,0x71,0xd8,0x31,0x15,
0x04,0xc7,0x23,0xc3,0x18,0x96,0x05,0x9a,0x07,0x12,0x80,0xe2,0xeb,0x27,0xb2,0x75,
0x09,0x83,0x2c,0x1a,0x1b,0x6e,0x5a,0xa0,0x52,0x3b,0xd6,0xb3,0x29,0xe3,0x2f,0x84,
0x53,0xd1,0x00,0xed,0x20,0xfc,0xb1,0x5b,0x6a,0xcb,0xbe,0x39,0x4a,0x4c,0x58,0xcf,
0xd0,0xef,0xaa,0xfb,0x43,0x4d,0x33,0x85,0x45,0xf9,0x02,0x7f,0x50,0x3c,0x9f,0xa8,
0x51,0xa3,0x40,0x8f,0x92,0x9d,0x38,0xf5,0xbc,0xb6,0xda,0x21,0x10,0xff,0xf3,0xd2,
0xcd,0x0c,0x13,0xec,0x5f,0x97,0x44,0x17,0xc4,0xa7,0x7e,0x3d,0x64,0x5d,0x19,0x73,
0x60,0x81,0x4f,0xdc,0x22,0x2a,0x90,0x88,0x46,0xee,0xb8,0x14,0xde,0x5e,0x0b,0xdb,
0xe0,0x32,0x3a,0x0a,0x49,0x06,0x24,0x5c,0xc2,0xd3,0xac,0x62,0x91,0x95,0xe4,0x79,
0xe7,0xc8,0x37,0x6d,0x8d,0xd5,0x4e,0xa9,0x6c,0x56,0xf4,0xea,0x65,0x7a,0xae,0x08,
0xba,0x78,0x25,0x2e,0x1c,0xa6,0xb4,0xc6,0xe8,0xdd,0x74,0x1f,0x4b,0xbd,0x8b,0x8a,
0x70,0x3e,0xb5,0x66,0x48,0x03,0xf6,0x0e,0x61,0x35,0x57,0xb9,0x86,0xc1,0x1d,0x9e,
0xe1,0xf8,0x98,0x11,0x69,0xd9,0x8e,0x94,0x9b,0x1e,0x87,0xe9,0xce,0x55,0x28,0xdf,
0x8c,0xa1,0x89,0x0d,0xbf,0xe6,0x42,0x68,0x41,0x99,0x2d,0x0f,0xb0,0x54,0xbb,0x16};
static const unsigned char k_aes_rcon[11] = {0x00,0x01,0x02,0x04,0x08,0x10,0x20,0x40,0x80,0x1b,0x36};
static unsigned char k_aes_xtime(unsigned char x) { return (unsigned char)((x<<1)^((x&0x80)?0x1b:0)); }
static void k_aes_expand_key(const unsigned char key[32], unsigned char rk[240]) {
    memcpy(rk,key,32);
    int bytes=32, rcon=1;
    unsigned char t[4];
    while (bytes<240) {
        memcpy(t,rk+bytes-4,4);
        if (bytes%32==0) {
            unsigned char tmp=t[0]; t[0]=k_aes_sbox[t[1]]^k_aes_rcon[rcon++]; t[1]=k_aes_sbox[t[2]]; t[2]=k_aes_sbox[t[3]]; t[3]=k_aes_sbox[tmp];
        } else if (bytes%32==16) {
            t[0]=k_aes_sbox[t[0]]; t[1]=k_aes_sbox[t[1]]; t[2]=k_aes_sbox[t[2]]; t[3]=k_aes_sbox[t[3]];
        }
        for (int i=0;i<4;i++) { rk[bytes]=rk[bytes-32]^t[i]; bytes++; }
    }
}
static void k_aes_encrypt_block(const unsigned char rk[240], const unsigned char in[16], unsigned char out[16]) {
    unsigned char s[16]; memcpy(s,in,16);
    for (int i=0;i<16;i++) s[i]^=rk[i];
    for (int round=1;round<=14;round++) {
        for (int i=0;i<16;i++) s[i]=k_aes_sbox[s[i]];
        unsigned char t[16];
        t[0]=s[0]; t[1]=s[5]; t[2]=s[10]; t[3]=s[15];
        t[4]=s[4]; t[5]=s[9]; t[6]=s[14]; t[7]=s[3];
        t[8]=s[8]; t[9]=s[13]; t[10]=s[2]; t[11]=s[7];
        t[12]=s[12]; t[13]=s[1]; t[14]=s[6]; t[15]=s[11];
        if (round!=14) {
            for (int c=0;c<4;c++) {
                unsigned char a0=t[c*4],a1=t[c*4+1],a2=t[c*4+2],a3=t[c*4+3];
                unsigned char x=a0^a1^a2^a3;
                s[c*4]=a0^x^k_aes_xtime(a0^a1);
                s[c*4+1]=a1^x^k_aes_xtime(a1^a2);
                s[c*4+2]=a2^x^k_aes_xtime(a2^a3);
                s[c*4+3]=a3^x^k_aes_xtime(a3^a0);
            }
        } else memcpy(s,t,16);
        for (int i=0;i<16;i++) s[i]^=rk[round*16+i];
    }
    memcpy(out,s,16);
}
static void k_gf_mul(unsigned char *x, const unsigned char *y) {
    unsigned char z[16]; memset(z,0,16);
    unsigned char v[16]; memcpy(v,y,16);
    for (int i=0;i<128;i++) {
        if (x[i>>3]>>(7-(i&7)) & 1) for (int j=0;j<16;j++) z[j]^=v[j];
        int lsb=v[15]&1;
        for (int j=15;j>0;j--) v[j]=(unsigned char)((v[j]>>1)|((v[j-1]&1)<<7));
        v[0]>>=1;
        if (lsb) v[0]^=0xe1;
    }
    memcpy(x,z,16);
}
static void k_ghash(const unsigned char H[16], const unsigned char *a, size_t alen, unsigned char out[16]) {
    unsigned char y[16]; memset(y,0,16);
    for (size_t i=0;i<alen;i+=16) {
        unsigned char blk[16]; memset(blk,0,16);
        size_t take = alen-i; if (take>16) take=16;
        memcpy(blk,a+i,take);
        for (int j=0;j<16;j++) y[j]^=blk[j];
        k_gf_mul(y,H);
    }
    memcpy(out,y,16);
}
static void k_gcm_inc32(unsigned char *c) {
    for (int i=15;i>=12;i--) { if (++c[i]!=0) break; }
}
static void k_gcm_ctr(const unsigned char rk[240], const unsigned char iv[12], const unsigned char *in, size_t len, unsigned char *out) {
    unsigned char ctr[16]; memset(ctr,0,16); memcpy(ctr,iv,12); ctr[15]=2;
    unsigned char ks[16];
    for (size_t i=0;i<len;i+=16) {
        k_aes_encrypt_block(rk,ctr,ks);
        size_t take=len-i; if (take>16) take=16;
        for (size_t j=0;j<take;j++) out[i+j]=in[i+j]^ks[j];
        k_gcm_inc32(ctr);
    }
}
static void k_gcm_tag(const unsigned char rk[240], const unsigned char H[16], const unsigned char iv[12], const unsigned char *ct, size_t ctlen, unsigned char tag[16]) {
    unsigned char j0[16]; memset(j0,0,16); memcpy(j0,iv,12); j0[15]=1;
    unsigned char s[16]; memset(s,0,16);
    unsigned char htmp[16]; k_ghash(H,ct,ctlen,htmp);
    for (int i=0;i<16;i++) s[i]^=htmp[i];
    unsigned char lenblk[16]; memset(lenblk,0,16);
    uint64_t bl = (uint64_t)ctlen*8;
    for (int i=0;i<8;i++) lenblk[8+i]=(unsigned char)(bl>>(56-i*8));
    for (int i=0;i<16;i++) s[i]^=lenblk[i];
    k_gf_mul(s,H);
    unsigned char ek[16]; k_aes_encrypt_block(rk,j0,ek);
    for (int i=0;i<16;i++) tag[i]=s[i]^ek[i];
}
static KValue k_crypto_aes_gcm_encrypt(KValue key, KValue nonce, KValue pt) {
    if (key.u.s.len!=32) return kv_res(0,kv_cstr("AES-256-GCM key must be 32 bytes"));
    if (nonce.u.s.len!=12) return kv_res(0,kv_cstr("AES-GCM nonce must be 12 bytes"));
    unsigned char rk[240]; k_aes_expand_key((const unsigned char*)key.u.s.data,rk);
    unsigned char H[16]; unsigned char zero[16]; memset(zero,0,16); k_aes_encrypt_block(rk,zero,H);
    size_t n=pt.u.s.len;
    unsigned char *ct=(unsigned char*)kalloc(n+16);
    k_gcm_ctr(rk,(const unsigned char*)nonce.u.s.data,(const unsigned char*)pt.u.s.data,n,ct);
    unsigned char tag[16]; k_gcm_tag(rk,H,(const unsigned char*)nonce.u.s.data,ct,n,tag);
    memcpy(ct+n,tag,16);
    return kv_res(1,kv_bytesn((const char*)ct,n+16));
}
static KValue k_crypto_aes_gcm_decrypt(KValue key, KValue nonce, KValue ct) {
    if (key.u.s.len!=32) return kv_res(0,kv_cstr("AES-256-GCM key must be 32 bytes"));
    if (nonce.u.s.len!=12) return kv_res(0,kv_cstr("AES-GCM nonce must be 12 bytes"));
    if (ct.u.s.len<16) return kv_res(0,kv_cstr("AES-GCM authentication failed"));
    unsigned char rk[240]; k_aes_expand_key((const unsigned char*)key.u.s.data,rk);
    unsigned char H[16]; unsigned char zero[16]; memset(zero,0,16); k_aes_encrypt_block(rk,zero,H);
    size_t n=ct.u.s.len-16;
    unsigned char tag[16]; k_gcm_tag(rk,H,(const unsigned char*)nonce.u.s.data,(const unsigned char*)ct.u.s.data,n,tag);
    unsigned char diff=0; for (int i=0;i<16;i++) diff|=(unsigned char)(tag[i]^((const unsigned char*)ct.u.s.data)[n+i]);
    if (diff) return kv_res(0,kv_cstr("AES-GCM authentication failed"));
    unsigned char *pt=(unsigned char*)kalloc(n+1);
    k_gcm_ctr(rk,(const unsigned char*)nonce.u.s.data,(const unsigned char*)ct.u.s.data,n,pt);
    return kv_res(1,kv_bytesn((const char*)pt,n));
}

/* ---- PBKDF2-HMAC-SHA-256 ----------------------------------------------- */
static void k_hmac_sha256_raw(const unsigned char *key, size_t keylen, const unsigned char *msg, size_t msglen, unsigned char out[32]) {
    unsigned char k[64]; memset(k,0,64);
    if (keylen>64) { KSha256 s; k_sha_init(&s); k_sha_update(&s,key,keylen); k_sha_final(&s,k); }
    else memcpy(k,key,keylen);
    unsigned char ipad[64],opad[64];
    for (int i=0;i<64;i++) { ipad[i]=k[i]^0x36; opad[i]=k[i]^0x5c; }
    KSha256 s; k_sha_init(&s); k_sha_update(&s,ipad,64); k_sha_update(&s,msg,msglen);
    unsigned char inner[32]; k_sha_final(&s,inner);
    k_sha_init(&s); k_sha_update(&s,opad,64); k_sha_update(&s,inner,32); k_sha_final(&s,out);
}
static KValue k_crypto_pbkdf2_sha256(KValue pw, KValue salt, KValue itv, KValue lenv) {
    long long it=itv.u.i, len=lenv.u.i;
    if (it<1) return kv_res(0,kv_cstr("PBKDF2 iterations must be at least 1"));
    if (len<1 || len>1048576) return kv_res(0,kv_cstr("PBKDF2 length must be between 1 and 1048576"));
    unsigned char *out=(unsigned char*)kalloc((size_t)len);
    size_t hlen=32; long long blocks=(len+hlen-1)/hlen;
    unsigned char *saltbuf=(unsigned char*)kalloc(salt.u.s.len+4);
    memcpy(saltbuf,salt.u.s.data,salt.u.s.len);
    long long produced=0;
    for (long long b=1;b<=blocks;b++) {
        saltbuf[salt.u.s.len]=(unsigned char)(b>>24); saltbuf[salt.u.s.len+1]=(unsigned char)(b>>16);
        saltbuf[salt.u.s.len+2]=(unsigned char)(b>>8); saltbuf[salt.u.s.len+3]=(unsigned char)(b);
        unsigned char u[32]; k_hmac_sha256_raw((const unsigned char*)pw.u.s.data,pw.u.s.len,saltbuf,salt.u.s.len+4,u);
        unsigned char t[32]; memcpy(t,u,32);
        for (long long i=1;i<it;i++) { k_hmac_sha256_raw((const unsigned char*)pw.u.s.data,pw.u.s.len,u,32,u); for (int j=0;j<32;j++) t[j]^=u[j]; }
        for (int j=0;j<32 && produced<len;j++) out[produced++]=t[j];
    }
    return kv_res(1,kv_bytesn((const char*)out,(size_t)len));
}

/* ---- HKDF-SHA-256 ------------------------------------------------------ */
static KValue k_crypto_hkdf_sha256(KValue ikm, KValue salt, KValue info, KValue lenv) {
    long long len=lenv.u.i;
    if (len<1 || len>8160) return kv_res(0,kv_cstr("HKDF length must be between 1 and 8160"));
    unsigned char zerosalt[32]; memset(zerosalt,0,32);
    const unsigned char *saltp = salt.u.s.len? (const unsigned char*)salt.u.s.data : zerosalt;
    size_t saltlen = salt.u.s.len? salt.u.s.len : 32;
    unsigned char prk[32]; k_hmac_sha256_raw(saltp,saltlen,(const unsigned char*)ikm.u.s.data,ikm.u.s.len,prk);
    unsigned char *out=(unsigned char*)kalloc((size_t)len);
    unsigned char t[32]; size_t tlen=0; long long produced=0; unsigned char counter=1;
    while (produced<len) {
        unsigned char *msg=(unsigned char*)kalloc(tlen+info.u.s.len+1);
        memcpy(msg,t,tlen); memcpy(msg+tlen,info.u.s.data,info.u.s.len); msg[tlen+info.u.s.len]=counter;
        k_hmac_sha256_raw(prk,32,msg,tlen+info.u.s.len+1,t);
        tlen=32;
        for (size_t j=0;j<32 && produced<len;j++) out[produced++]=t[j];
        counter++;
    }
    return kv_res(1,kv_bytesn((const char*)out,(size_t)len));
}

/* ---- constant-time compare, XOR, base64url ----------------------------- */
static KValue k_crypto_constant_time_equal(KValue a, KValue b) {
    if (a.u.s.len!=b.u.s.len) return kv_bool(0);
    unsigned char diff=0;
    for (size_t i=0;i<a.u.s.len;i++) diff|=(unsigned char)(a.u.s.data[i]^b.u.s.data[i]);
    return kv_bool(diff==0);
}
static KValue k_crypto_xor(KValue a, KValue b) {
    if (a.u.s.len!=b.u.s.len) return kv_res(0,kv_cstr("xor requires equal-length byte sequences"));
    size_t n=a.u.s.len; char *p=(char*)kalloc(n+1);
    for (size_t i=0;i<n;i++) p[i]=(char)(a.u.s.data[i]^b.u.s.data[i]);
    return kv_res(1,kv_bytesn(p,n));
}
static const char k_b64url[]="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
static KValue k_base64url_encode(KValue v) {
    size_t n=v.u.s.len; size_t olen=((n+2)/3)*4;
    char *p=(char*)kalloc(olen+1); size_t j=0;
    for (size_t i=0;i<n;i+=3) {
        unsigned int b0=(unsigned char)v.u.s.data[i];
        unsigned int b1=i+1<n?(unsigned char)v.u.s.data[i+1]:0;
        unsigned int b2=i+2<n?(unsigned char)v.u.s.data[i+2]:0;
        unsigned int t=(b0<<16)|(b1<<8)|b2;
        p[j++]=k_b64url[(t>>18)&0x3f]; p[j++]=k_b64url[(t>>12)&0x3f];
        if (i+1<n) p[j++]=k_b64url[(t>>6)&0x3f];
        if (i+2<n) p[j++]=k_b64url[t&0x3f];
    }
    p[j]=0;
    return kv_strn(p,j);
}
static KValue k_base64url_decode(KValue v) {
    size_t n=v.u.s.len;
    while (n>0 && v.u.s.data[n-1]=='=') n--;
    size_t olen=(n/4)*3+3; char *p=(char*)kalloc(olen+1); size_t j=0;
    unsigned int buf=0; int bits=0;
    for (size_t i=0;i<n;i++) {
        char c=v.u.s.data[i];
        const char *f=strchr(k_b64url,c);
        if (!f) return kv_res(0,kv_cstr("invalid base64url input"));
        buf=(buf<<6)|(unsigned int)(f-k_b64url); bits+=6;
        if (bits>=8) { bits-=8; p[j++]=(char)((buf>>bits)&0xff); }
    }
    return kv_res(1,kv_bytesn(p,j));
}

/* ---- obfuscated string literals ---------------------------------------- */
/* k_obf_str rebuilds a string literal that was XOR-masked at build time. The
   mask is derived from the literal's own length and index, so no key material
   is stored in the binary and the plaintext never appears in the executable. */
static KValue k_obf_str(const unsigned char *enc, size_t n) {
    char *p=(char*)kalloc(n+1);
    for (size_t i=0;i<n;i++) p[i]=(char)(enc[i] ^ (unsigned char)(0x5a + (i*31) + (n*7)));
    p[n]=0;
    return kv_strn(p,n);
}

/* ---- Python-style convenience builtins --------------------------------- */
static long long k_norm_index(long long i, long long len) {
    if (i<0) i+=len;
    if (i<0) return 0;
    if (i>len) return len;
    return i;
}
static KValue k_string_slice(KValue v, KValue s, KValue e) {
    size_t total=k_utf8_count(v.u.s.data,v.u.s.len);
    long long start=k_norm_index(s.u.i,(long long)total);
    long long end=k_norm_index(e.u.i,(long long)total);
    if (end<start) end=start;
    size_t b0=k_utf8_off(v.u.s.data,v.u.s.len,(size_t)start);
    size_t b1=k_utf8_off(v.u.s.data,v.u.s.len,(size_t)end);
    return kv_res(1,kv_strn(v.u.s.data+b0,b1-b0));
}
static KValue k_array_slice_range(KValue a, KValue s, KValue e) {
    if (a.tag!=K_ARRAY) kfail("array_slice_range expects Array[T]");
    long long len=(long long)a.u.a.len;
    long long start=k_norm_index(s.u.i,len);
    long long end=k_norm_index(e.u.i,len);
    if (end<start) end=start;
    long long cnt=end-start;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(cnt?cnt:1));
    memcpy(p,a.u.a.items+start,sizeof(KValue)*cnt);
    return kv_arr(p,(size_t)cnt);
}
static KValue k_string_format(KValue tmpl, KValue args) {
    if (args.tag!=K_ARRAY) kfail("string_format expects Array[String]");
    size_t cap=tmpl.u.s.len+1;
    for (size_t i=0;i<args.u.a.len;i++) cap+=args.u.a.items[i].u.s.len;
    char *out=(char*)kalloc(cap+1);
    size_t j=0, next=0;
    for (size_t i=0;i<tmpl.u.s.len;i++) {
        char c=tmpl.u.s.data[i];
        if (c=='{' && i+1<tmpl.u.s.len && tmpl.u.s.data[i+1]=='{') { out[j++]='{'; i++; continue; }
        if (c=='}' && i+1<tmpl.u.s.len && tmpl.u.s.data[i+1]=='}') { out[j++]='}'; i++; continue; }
        if (c=='{' && i+1<tmpl.u.s.len && tmpl.u.s.data[i+1]=='}') {
            if (next<args.u.a.len) {
                KValue a=args.u.a.items[next++];
                memcpy(out+j,a.u.s.data,a.u.s.len); j+=a.u.s.len;
            } else { out[j++]='{'; out[j++]='}'; }
            i++;
            continue;
        }
        out[j++]=c;
    }
    out[j]=0;
    return kv_strn(out,j);
}
static KValue k_array_indices(KValue a) {
    if (a.tag!=K_ARRAY) kfail("array_indices expects Array[T]");
    size_t n=a.u.a.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) p[i]=kv_int((long long)i);
    return kv_arr(p,n);
}
static KValue k_array_zip(KValue left, KValue right) {
    if (left.tag!=K_ARRAY || right.tag!=K_ARRAY) kfail("array_zip expects Array[T]");
    size_t n=left.u.a.len; if (right.u.a.len<n) n=right.u.a.len;
    KValue *p=(KValue*)kalloc(sizeof(KValue)*(n?n:1));
    for (size_t i=0;i<n;i++) {
        KValue *pair=(KValue*)kalloc(sizeof(KValue)*2);
        pair[0]=left.u.a.items[i]; pair[1]=right.u.a.items[i];
        p[i]=kv_arr(pair,2);
    }
    return kv_arr(p,n);
}
`
