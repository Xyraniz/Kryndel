package kry

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func geocodeIPJSON(ctx *ExecContext, ip string, maxBytes int) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", fmt.Errorf("invalid IP address")
	}
	endpoint := strings.TrimRight(os.Getenv("KRY_GEOCODER_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "https://ipapi.co"
	}
	base, err := url.Parse(endpoint)
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" {
		return "", fmt.Errorf("KRY_GEOCODER_ENDPOINT must be an http or https URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + url.PathEscape(parsed.String()) + "/json/"
	req, err := http.NewRequestWithContext(ctx.Ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Kryndel-geocoder/1")
	client := &http.Client{Timeout: networkTimeout(ctx.Lim.MaxWallTimeMS)}
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("geocoder returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(maxBytes)+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxBytes {
		return "", fmt.Errorf("geocoder response exceeds configured input limit")
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return "", fmt.Errorf("geocoder returned invalid JSON: %w", err)
	}
	canonical, err := json.Marshal(normalizeJSON(raw))
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}
