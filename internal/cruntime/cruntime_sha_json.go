package cruntime

// SHA-256 and HMAC-SHA-256 primitives required by JSON and artifact helpers.
const cRuntimeSHA256 = `/* ---- SHA-256 ----------------------------------------------------------- */
typedef struct { uint32_t h[8]; uint64_t len; unsigned char buf[64]; size_t n; } KSha256;
static const uint32_t k_sha_k[64] = {
 0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
 0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
 0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
 0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
 0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
 0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
 0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
 0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};
#define K_ROTR(x,n) (((x)>>(n))|((x)<<(32-(n))))
static void k_sha_block(KSha256 *s, const unsigned char *p) {
    uint32_t w[64];
    for (int i=0;i<16;i++) w[i]=((uint32_t)p[i*4]<<24)|((uint32_t)p[i*4+1]<<16)|((uint32_t)p[i*4+2]<<8)|((uint32_t)p[i*4+3]);
    for (int i=16;i<64;i++) {
        uint32_t s0=K_ROTR(w[i-15],7)^K_ROTR(w[i-15],18)^(w[i-15]>>3);
        uint32_t s1=K_ROTR(w[i-2],17)^K_ROTR(w[i-2],19)^(w[i-2]>>10);
        w[i]=w[i-16]+s0+w[i-7]+s1;
    }
    uint32_t a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4],f=s->h[5],g=s->h[6],h=s->h[7];
    for (int i=0;i<64;i++) {
        uint32_t S1=K_ROTR(e,6)^K_ROTR(e,11)^K_ROTR(e,25);
        uint32_t ch=(e&f)^((~e)&g);
        uint32_t t1=h+S1+ch+k_sha_k[i]+w[i];
        uint32_t S0=K_ROTR(a,2)^K_ROTR(a,13)^K_ROTR(a,22);
        uint32_t maj=(a&b)^(a&c)^(b&c);
        uint32_t t2=S0+maj;
        h=g; g=f; f=e; e=d+t1; d=c; c=b; b=a; a=t1+t2;
    }
    s->h[0]+=a; s->h[1]+=b; s->h[2]+=c; s->h[3]+=d; s->h[4]+=e; s->h[5]+=f; s->h[6]+=g; s->h[7]+=h;
}
static void k_sha_init(KSha256 *s) {
    s->h[0]=0x6a09e667; s->h[1]=0xbb67ae85; s->h[2]=0x3c6ef372; s->h[3]=0xa54ff53a;
    s->h[4]=0x510e527f; s->h[5]=0x9b05688c; s->h[6]=0x1f83d9ab; s->h[7]=0x5be0cd19;
    s->len=0; s->n=0;
}
static void k_sha_update(KSha256 *s, const unsigned char *p, size_t n) {
    s->len += n;
    while (n) {
        size_t take = 64 - s->n; if (take > n) take = n;
        memcpy(s->buf + s->n, p, take); s->n += take; p += take; n -= take;
        if (s->n == 64) { k_sha_block(s, s->buf); s->n = 0; }
    }
}
static void k_sha_final(KSha256 *s, unsigned char out[32]) {
    uint64_t bits = s->len * 8;
    unsigned char pad = 0x80; k_sha_update(s, &pad, 1);
    unsigned char z = 0;
    while (s->n != 56) k_sha_update(s, &z, 1);
    unsigned char lb[8];
    for (int i=0;i<8;i++) lb[i]=(unsigned char)(bits >> (56 - i*8));
    k_sha_update(s, lb, 8);
    for (int i=0;i<8;i++) {
        out[i*4]=(unsigned char)(s->h[i]>>24); out[i*4+1]=(unsigned char)(s->h[i]>>16);
        out[i*4+2]=(unsigned char)(s->h[i]>>8); out[i*4+3]=(unsigned char)(s->h[i]);
    }
}
static KValue k_crypto_sha256(KValue data) {
    KSha256 s; k_sha_init(&s);
    k_sha_update(&s, (const unsigned char*)data.u.s.data, data.u.s.len);
    unsigned char out[32]; k_sha_final(&s, out);
    return kv_bytesn((const char*)out, 32);
}
static KValue k_crypto_hmac_sha256(KValue key, KValue msg) {
    unsigned char k[64]; memset(k,0,64);
    if (key.u.s.len > 64) { KSha256 s; k_sha_init(&s); k_sha_update(&s,(const unsigned char*)key.u.s.data,key.u.s.len); k_sha_final(&s,k); }
    else memcpy(k, key.u.s.data, key.u.s.len);
    unsigned char ipad[64], opad[64];
    for (int i=0;i<64;i++) { ipad[i]=k[i]^0x36; opad[i]=k[i]^0x5c; }
    KSha256 s; k_sha_init(&s);
    k_sha_update(&s, ipad, 64);
    k_sha_update(&s, (const unsigned char*)msg.u.s.data, msg.u.s.len);
    unsigned char inner[32]; k_sha_final(&s, inner);
    k_sha_init(&s);
    k_sha_update(&s, opad, 64);
    k_sha_update(&s, inner, 32);
    unsigned char out[32]; k_sha_final(&s, out);
    return kv_bytesn((const char*)out, 32);
}
static KValue k_crypto_random_bytes(KValue nv) {
    long long n = nv.u.i;
    if (n <= 0 || n > 1024) return kv_res(0, kv_cstr("random bytes length must be between 1 and 1024"));
    unsigned char *buf = (unsigned char*)kalloc((size_t)n);
    FILE *f = fopen("/dev/urandom", "rb");
    if (f) { size_t got = fread(buf, 1, (size_t)n, f); fclose(f); if (got != (size_t)n) return kv_res(0, kv_cstr("cannot read random bytes")); }
    else { for (long long i=0;i<n;i++) buf[i]=(unsigned char)(rand() & 0xff); }
    return kv_res(1, kv_bytesn((const char*)buf, (size_t)n));
}

`
