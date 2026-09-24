package kry

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const discordAPIBase = "https://discord.com/api/v10"
const discordMaxWebhookFiles = 10

type discordRateBucket struct {
	remaining int64
	resetAt   time.Time
}

type discordRateLimiter struct {
	mu          sync.Mutex
	routeBucket map[string]string
	buckets     map[string]discordRateBucket
	globalUntil map[string]time.Time
}

func newDiscordRateLimiter() *discordRateLimiter {
	return &discordRateLimiter{
		routeBucket: make(map[string]string),
		buckets:     make(map[string]discordRateBucket),
		globalUntil: make(map[string]time.Time),
	}
}

func discordApplicationKey(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:16])
}

func redactDiscordSecrets(value, secrets string) string {
	for _, secret := range strings.Split(secrets, "|") {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

func discordRequestRedactions(route, credential string) string {
	secrets := credential
	parsed, err := url.Parse(route)
	path := route
	if err == nil {
		path = parsed.Path
	}
	parts := strings.Split(path, "/")
	if len(parts) > 3 && (parts[1] == "webhooks" || parts[1] == "interactions") && parts[3] != "" {
		secrets += "|" + parts[3]
	}
	for _, part := range parts {
		if len(part) > 32 {
			secrets += "|" + part
		}
	}
	return secrets
}

func discordRouteKey(route string) string {
	u, err := url.Parse(route)
	if err == nil && u.RawQuery != "" {
		route = u.Path
	}
	parts := strings.Split(strings.Trim(route, "/"), "/")
	canonical := make([]string, len(parts))
	major := make([]string, 0, 3)
	for i, part := range parts {
		canonical[i] = part
		if i > 0 && (parts[i-1] == "channels" || parts[i-1] == "guilds" || parts[i-1] == "webhooks" || parts[i-1] == "applications") {
			major = append(major, parts[i-1], part)
			continue
		}
		if part != "@me" && allDecimalDiscordID(part) {
			canonical[i] = ":id"
		} else if len(part) > 32 {
			digest := sha256.Sum256([]byte(part))
			canonical[i] = ":opaque:" + hex.EncodeToString(digest[:8])
		}
	}
	return strings.Join(canonical, "/") + "|" + strings.Join(major, "/")
}

func allDecimalDiscordID(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (l *discordRateLimiter) wait(ctx context.Context, appKey, routeKey string) error {
	for {
		l.mu.Lock()
		now := time.Now()
		until := l.globalUntil[appKey]
		bucketID := l.routeBucket[appKey+"|"+routeKey]
		if bucketID != "" {
			if bucket, ok := l.buckets[bucketID]; ok && bucket.remaining <= 0 && bucket.resetAt.After(until) {
				until = bucket.resetAt
			}
		}
		if !until.After(now) {
			if bucketID != "" {
				bucket := l.buckets[bucketID]
				if bucket.remaining > 0 {
					bucket.remaining--
					l.buckets[bucketID] = bucket
				}
			}
			l.mu.Unlock()
			return nil
		}
		wait := time.Until(until)
		l.mu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *discordRateLimiter) observe(appKey, routeKey string, response *http.Response, retryAfter time.Duration, global bool) {
	if l == nil || response == nil {
		return
	}
	now := time.Now()
	resetAfter, _ := strconv.ParseFloat(response.Header.Get("X-RateLimit-Reset-After"), 64)
	if resetAfter < 0 || resetAfter > 3600 {
		resetAfter = 0
	}
	remaining, remainErr := strconv.ParseInt(response.Header.Get("X-RateLimit-Remaining"), 10, 64)
	if remainErr != nil || remaining < 0 {
		remaining = 0
	}
	bucketHash := response.Header.Get("X-RateLimit-Bucket")
	global = global || strings.EqualFold(response.Header.Get("X-RateLimit-Scope"), "global")
	if response.StatusCode == http.StatusTooManyRequests && retryAfter > 0 {
		resetAfter = retryAfter.Seconds()
		remaining = 0
		if response.Header.Get("X-RateLimit-Global") == "true" {
			global = true
		}
	}
	resetAt := now.Add(time.Duration(resetAfter * float64(time.Second)))
	l.mu.Lock()
	defer l.mu.Unlock()
	if global && resetAt.After(l.globalUntil[appKey]) {
		l.globalUntil[appKey] = resetAt
	}
	if bucketHash == "" {
		return
	}
	bucketID := appKey + "|" + bucketHash + "|" + strings.SplitN(routeKey, "|", 2)[1]
	lookupKey := appKey + "|" + routeKey
	l.routeBucket[lookupKey] = bucketID
	old, exists := l.buckets[bucketID]
	if exists && old.resetAt.After(now) && old.resetAt.Before(resetAt) {
		resetAt = old.resetAt
	}
	if exists && old.resetAt.After(now) && old.remaining < remaining {
		remaining = old.remaining
	}
	l.buckets[bucketID] = discordRateBucket{remaining: remaining, resetAt: resetAt}
}

func validDiscordRoute(route string) bool {
	if route == "" || len(route) > 4096 || route[0] != '/' || strings.ContainsAny(route, "\x00\r\n#|\\") || strings.Contains(route, "//") {
		return false
	}
	lower := strings.ToLower(route)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%00") {
		return false
	}
	u, err := url.Parse(route)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil {
		return false
	}
	path, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (r *Runtime) discordAPIRequest(method, route, body, token string) (Value, *Diagnostic) {
	if body != "" && !json.Valid([]byte(body)) {
		return resVal(false, stringVal("Discord API request body must be valid JSON")), nil
	}
	return r.discordHTTPRequest(method, route, strings.NewReader(body), "application/json", token, true, discordRequestRedactions(route, token))
}

func (r *Runtime) discordInteractionRequest(method, route, body string) (Value, *Diagnostic) {
	if method != "GET" && method != "POST" && method != "PATCH" && method != "DELETE" && method != "PUT" {
		return resVal(false, stringVal("unsupported Discord interaction method")), nil
	}
	if !validDiscordRoute(route) || !(strings.HasPrefix(route, "/interactions/") || strings.HasPrefix(route, "/webhooks/")) {
		return resVal(false, stringVal("interaction request route must be under /interactions or /webhooks")), nil
	}
	if body != "" && !json.Valid([]byte(body)) {
		return resVal(false, stringVal("Discord interaction request body must be valid JSON")), nil
	}
	redact := discordRequestRedactions(route, "")
	return r.discordHTTPRequest(method, route, strings.NewReader(body), "application/json", "", false, redact)
}

type discordUploadFile struct {
	filename string
	data     []byte
}

func (r *Runtime) discordAPIUpload(method, route, payload, filename string, data []byte, token string) (Value, *Diagnostic) {
	files := []discordUploadFile{{filename: filename, data: data}}
	return r.discordMultipartUpload(method, route, payload, files, token, true)
}

func (r *Runtime) discordWebhookUpload(method, route, payload string, files []discordUploadFile, token string) (Value, *Diagnostic) {
	return r.discordMultipartUpload(method, route, payload, files, token, false)
}

func (r *Runtime) discordMultipartUpload(method, route, payload string, files []discordUploadFile, token string, botAuth bool) (Value, *Diagnostic) {
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return resVal(false, stringVal("Discord uploads require POST, PUT, or PATCH")), nil
	}
	if !validDiscordRoute(route) || token == "" || (!botAuth && !strings.HasPrefix(route, "/webhooks/")) || len(files) == 0 || len(files) > discordMaxWebhookFiles {
		return resVal(false, stringVal("invalid Discord upload route, token, or filename")), nil
	}
	if !json.Valid([]byte(payload)) {
		return resVal(false, stringVal("Discord upload payload must be valid JSON")), nil
	}
	totalFileBytes := 0
	for _, file := range files {
		if strings.ContainsAny(file.filename, "/\\\x00\r\n") || strings.TrimSpace(file.filename) == "" || len(file.filename) > 255 {
			return resVal(false, stringVal("invalid Discord upload route, token, or filename")), nil
		}
		if len(file.data) > r.Lim.MaxSourceBytes-totalFileBytes {
			return resVal(false, stringVal("Discord upload exceeds configured input limit")), nil
		}
		totalFileBytes += len(file.data)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("payload_json", payload); err != nil {
		return resVal(false, stringVal("could not create Discord upload payload")), nil
	}
	for index, file := range files {
		part, err := writer.CreateFormFile("files["+strconv.Itoa(index)+"]", file.filename)
		if err != nil {
			return resVal(false, stringVal("invalid Discord upload filename")), nil
		}
		if _, err = part.Write(file.data); err != nil {
			return resVal(false, stringVal("could not write Discord upload data")), nil
		}
	}
	if err := writer.Close(); err != nil {
		return resVal(false, stringVal("could not finish Discord upload")), nil
	}
	if body.Len() > r.Lim.MaxSourceBytes {
		return resVal(false, stringVal("multipart Discord upload exceeds configured input limit")), nil
	}
	return r.discordHTTPRequest(method, route, &body, writer.FormDataContentType(), token, botAuth, discordRequestRedactions(route, token))
}

func (r *Runtime) discordHTTPRequest(method, route string, body io.Reader, contentType, credential string, botAuth bool, redact string) (Value, *Diagnostic) {
	if credential == "" && botAuth {
		return resVal(false, stringVal("invalid Discord bot token")), nil
	}
	if credential != "" && strings.IndexFunc(credential, func(value rune) bool { return value < 32 || value == 127 }) >= 0 {
		return resVal(false, stringVal("invalid Discord credential")), nil
	}
	if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
		return resVal(false, stringVal("unsupported Discord API method")), nil
	}
	if !validDiscordRoute(route) {
		return resVal(false, stringVal("invalid Discord API route")), nil
	}
	appKey := discordApplicationKey(credential)
	if r.discordRates == nil {
		r.discordRates = newDiscordRateLimiter()
	}
	routeKey := discordRouteKey(route)
	client := &http.Client{
		Timeout:       time.Duration(r.Lim.MaxWallTimeMS) * time.Millisecond,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	if redact == "" {
		redact = credential
	}
	var requestBytes []byte
	if body != nil {
		var readErr error
		requestBytes, readErr = io.ReadAll(io.LimitReader(body, int64(r.Lim.MaxSourceBytes)+1))
		if readErr != nil {
			return resVal(false, stringVal("failed to read Discord API request body")), nil
		}
		if len(requestBytes) > r.Lim.MaxSourceBytes {
			return resVal(false, stringVal("Discord API request exceeds configured input limit")), nil
		}
	}
	for attempt := 0; attempt <= 5; attempt++ {
		if err := r.discordRates.wait(r.Ctx.Ctx, appKey, routeKey); err != nil {
			return resVal(false, stringVal("Discord API request canceled while waiting for a rate limit")), nil
		}
		var requestBody io.Reader
		if body != nil {
			requestBody = bytes.NewReader(requestBytes)
		}
		baseURL := r.discordAPIBaseURL
		if baseURL == "" {
			baseURL = discordAPIBase
		}
		req, err := http.NewRequestWithContext(r.Ctx.Ctx, method, baseURL+route, requestBody)
		if err != nil {
			return resVal(false, stringVal("invalid Discord API request")), nil
		}
		if botAuth {
			req.Header.Set("Authorization", "Bot "+credential)
		}
		req.Header.Set("User-Agent", "Kryndel (https://github.com/Xyraniz/Kryndel)")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := client.Do(req)
		if err != nil {
			return resVal(false, stringVal(redactDiscordSecrets(err.Error(), redact))), nil
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(r.Lim.MaxSourceBytes)+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return resVal(false, stringVal("failed to read Discord API response")), nil
		}
		if len(data) > r.Lim.MaxSourceBytes {
			return resVal(false, stringVal("Discord API response exceeds configured input limit")), nil
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if closeErr != nil {
				return resVal(false, stringVal("failed to close Discord rate-limit response")), nil
			}
			if attempt == 5 {
				return resVal(false, stringVal("Discord API rate limit persisted after 5 retries")), nil
			}
			var retry struct {
				RetryAfter float64 `json:"retry_after"`
				Global     bool    `json:"global"`
			}
			_ = json.Unmarshal(data, &retry)
			if retry.RetryAfter <= 0 {
				retry.RetryAfter, _ = strconv.ParseFloat(resp.Header.Get("Retry-After"), 64)
			}
			if retry.RetryAfter <= 0 {
				retry.RetryAfter = 1
			}
			if retry.RetryAfter > 300 {
				return resVal(false, stringVal("Discord API retry_after exceeds the 5 minute safety limit")), nil
			}
			delay := time.Duration(retry.RetryAfter * float64(time.Second))
			r.discordRates.observe(appKey, routeKey, resp, delay, retry.Global)
			timer := time.NewTimer(delay)
			select {
			case <-r.Ctx.Ctx.Done():
				timer.Stop()
				return resVal(false, stringVal("Discord API request canceled while waiting for a rate limit")), nil
			case <-timer.C:
			}
			continue
		}
		if closeErr != nil {
			return resVal(false, stringVal("failed to close Discord API response")), nil
		}
		r.discordRates.observe(appKey, routeKey, resp, 0, false)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message := strings.TrimSpace(string(data))
			message = redactDiscordSecrets(message, redact)
			if len(message) > 1024 {
				message = message[:1024]
			}
			detail := ""
			if message != "" && validUTF8([]byte(message)) {
				detail = ": " + message
			}
			return resVal(false, stringVal(fmt.Sprintf("Discord API HTTP %d%s", resp.StatusCode, detail))), nil
		}
		if !validUTF8(data) {
			return resVal(false, stringVal("Discord API response is not valid UTF-8")), nil
		}
		return resVal(true, stringVal(string(data))), nil
	}
	return resVal(false, stringVal("Discord API request failed")), nil
}

func verifyDiscordInteraction(publicKey, timestamp, body, signature string) (bool, error) {
	key, err := hex.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return false, fmt.Errorf("invalid Discord application public key")
	}
	sig, err := hex.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid Discord interaction signature")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds <= 0 {
		return false, fmt.Errorf("invalid Discord interaction timestamp")
	}
	if delta := time.Since(time.Unix(seconds, 0)); delta > 5*time.Minute || delta < -5*time.Minute {
		return false, nil
	}
	message := make([]byte, 0, len(timestamp)+len(body))
	message = append(message, timestamp...)
	message = append(message, body...)
	return ed25519.Verify(ed25519.PublicKey(key), message, sig), nil
}
