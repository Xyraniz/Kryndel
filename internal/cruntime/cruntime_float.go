package cruntime

const cRuntimeFloat = `/* ---- Go-compatible float formatting ------------------------------------ */
static void k_fmt_float(double v, char *out, size_t outsz) {
    if (isnan(v)) { snprintf(out,outsz,"NaN"); return; }
    if (isinf(v)) { snprintf(out,outsz, v<0?"-Inf":"+Inf"); return; }
    if (v==0) { snprintf(out,outsz,"0"); return; }
    char tmp[64]; int prec;
    for (prec=1; prec<=17; prec++) {
        snprintf(tmp,sizeof(tmp),"%.*g",prec,v);
        if (strtod(tmp,NULL)==v) break;
    }
    if (prec>17) prec=17;
    char ebuf[64];
    snprintf(ebuf,sizeof(ebuf),"%.*e",prec-1,v);
    const char *p=ebuf; int neg=0;
    if (*p=='-') { neg=1; p++; }
    char digits[32]; int nd=0;
    while (*p && *p!='e' && *p!='E') { if (*p>='0'&&*p<='9') digits[nd++]=*p; p++; }
    digits[nd]=0;
    int exp10=0;
    if (*p=='e'||*p=='E') exp10=atoi(p+1);
    while (nd>1 && digits[nd-1]=='0') { nd--; digits[nd]=0; }
    char *o=out;
    if (neg) *o++='-';
    if (exp10 < -4 || exp10 >= 6) {
        *o++=digits[0];
        if (nd>1) { *o++='.'; for (int i=1;i<nd;i++) *o++=digits[i]; }
        *o++='e';
        int e=exp10; *o++ = e<0?'-':'+'; if (e<0) e=-e;
        char eb[8]; int en=0;
        if (e==0) eb[en++]='0';
        while (e>0) { eb[en++]='0'+(e%10); e/=10; }
        if (en<2) eb[en++]='0';
        while (en>0) *o++=eb[--en];
        *o=0;
    } else {
        int dp=exp10+1;
        if (dp<=0) { *o++='0'; *o++='.'; for (int i=0;i<-dp;i++) *o++='0'; for (int i=0;i<nd;i++) *o++=digits[i]; }
        else if (dp>=nd) { for (int i=0;i<nd;i++) *o++=digits[i]; for (int i=nd;i<dp;i++) *o++='0'; }
        else { for (int i=0;i<dp;i++) *o++=digits[i]; *o++='.'; for (int i=dp;i<nd;i++) *o++=digits[i]; }
        *o=0;
    }
}

`
