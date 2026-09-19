package kry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeocodeIPBuiltinUsesValidatedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/8.8.8.8/json/" {
			t.Errorf("unexpected geocoder path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"country_code":"US","latitude":37.751,"ok":true}`))
	}))
	defer server.Close()
	t.Setenv("KRY_GEOCODER_ENDPOINT", server.URL)

	src := `fn main() -> Nil {
    match geocode_ip("8.8.8.8") {
        ok(value) => { println(json_stringify(value)) }
        err(problem) => { println("lookup err") }
    }
    match geocode_ip("not-an-ip") {
        ok(value) => { println("invalid accepted") }
        err(problem) => { println("invalid rejected") }
    }
    return nil
}
`
	got := runInterp(t, src)
	want := "{\"country_code\":\"US\",\"latitude\":37.751,\"ok\":true}\ninvalid rejected\n"
	if got != want {
		t.Fatalf("unexpected geocoder output: got %q want %q", got, want)
	}
}

func TestGeocodeRejectsNonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()
	t.Setenv("KRY_GEOCODER_ENDPOINT", server.URL)

	ctx := &ExecContext{Ctx: context.Background(), Lim: DefaultLimits()}
	if _, err := geocodeIPJSON(ctx, "2001:db8::1", 1024); err == nil {
		t.Fatal("invalid JSON response was accepted")
	}
}
