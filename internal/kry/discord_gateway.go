package kry

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	discordGatewayIntentGuildMembers   int64 = 1 << 1
	discordGatewayIntentGuildPresences int64 = 1 << 8
	discordMemberChunkRequestTTL             = 10 * time.Minute
	discordFullMemberRequestInterval         = 30 * time.Second
	discordGatewayEventWindow                = 60 * time.Second
	discordGatewayEventLimit                 = 110
	discordMemberChunkPendingLimit           = 64
	discordMemberChunkAggregateLimit         = 32 << 20
)

type discordMemberChunkPart struct {
	members   []json.RawMessage
	notFound  []json.RawMessage
	presences []json.RawMessage
	bytes     int
}

type discordMemberChunkRequest struct {
	guildID    string
	chunkCount int
	chunks     map[int]discordMemberChunkPart
	bytes      int
	createdAt  time.Time
}

type discordGatewayState struct {
	sessionMu      sync.Mutex
	mu             sync.Mutex
	socket         *websocketConn
	intents        int64
	chunks         map[string]*discordMemberChunkRequest
	chunkBytes     int
	fullListAt     map[string]time.Time
	outboundEvents []time.Time
	failures       []string
}

func newDiscordGatewayState() *discordGatewayState {
	return &discordGatewayState{
		chunks:     make(map[string]*discordMemberChunkRequest),
		fullListAt: make(map[string]time.Time),
	}
}

type discordGatewayMemberChunkEvent struct {
	GuildID    string            `json:"guild_id"`
	Nonce      string            `json:"nonce"`
	ChunkIndex *int              `json:"chunk_index"`
	ChunkCount *int              `json:"chunk_count"`
	Members    []json.RawMessage `json:"members"`
	NotFound   []json.RawMessage `json:"not_found"`
	Presences  []json.RawMessage `json:"presences"`
}

type discordGatewayMemberQueryResult struct {
	GuildID   string            `json:"guild_id"`
	Nonce     string            `json:"nonce"`
	Members   []json.RawMessage `json:"members"`
	NotFound  []json.RawMessage `json:"not_found,omitempty"`
	Presences []json.RawMessage `json:"presences,omitempty"`
}

func validDiscordSnowflakeString(value string) bool {
	if value == "" || len(value) > 20 || (len(value) > 1 && value[0] == '0') {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func newDiscordMemberRequestNonce() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random[:]), nil
}

func (r *Runtime) discordGatewayBindSession(socket *websocketConn, intents int64) (Value, *Diagnostic) {
	if socket == nil || socket.isClosed() {
		return resVal(false, stringVal("Discord Gateway session requires an open WebSocket")), nil
	}
	if intents < 0 {
		return resVal(false, stringVal("Discord Gateway intent mask is invalid")), nil
	}
	if r.discordGateway == nil {
		r.discordGateway = newDiscordGatewayState()
	}
	state := r.discordGateway
	state.sessionMu.Lock()
	defer state.sessionMu.Unlock()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.fullListAt == nil {
		state.fullListAt = make(map[string]time.Time)
	}
	if state.socket != nil && !state.socket.isClosed() {
		return resVal(false, stringVal("a Discord Gateway session is already active in this runtime")), nil
	}
	state.socket = socket
	state.intents = intents
	state.chunks = make(map[string]*discordMemberChunkRequest)
	state.chunkBytes = 0
	state.outboundEvents = []time.Time{time.Now()}
	state.failures = nil
	return resVal(true, nilVal()), nil
}

func (r *Runtime) discordGatewaySend(opcode int64, dataJSON string) (Value, *Diagnostic) {
	fail := func(message string) (Value, *Diagnostic) { return resVal(false, stringVal(message)), nil }
	if opcode != 3 && opcode != 4 {
		return fail("Discord Gateway send supports only presence and voice-state updates")
	}
	if !json.Valid([]byte(dataJSON)) {
		return fail("Discord Gateway update data must be valid JSON")
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil || data == nil {
		return fail("Discord Gateway update data must be a JSON object")
	}
	encoded, err := json.Marshal(struct {
		Opcode int64           `json:"op"`
		Data   json.RawMessage `json:"d"`
	}{Opcode: opcode, Data: json.RawMessage(dataJSON)})
	if err != nil || len(encoded) > r.Lim.MaxSourceBytes {
		return fail("Discord Gateway update exceeds configured input limit")
	}
	state := r.discordGateway
	if state == nil {
		return fail("Discord Gateway updates are only available during Bot.run callbacks")
	}
	state.sessionMu.Lock()
	state.mu.Lock()
	socket := state.socket
	if socket == nil || socket.isClosed() {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("Discord Gateway updates are only available during Bot.run callbacks")
	}
	now := time.Now()
	activeEvents := state.outboundEvents[:0]
	for _, sentAt := range state.outboundEvents {
		if now.Sub(sentAt) < discordGatewayEventWindow {
			activeEvents = append(activeEvents, sentAt)
		}
	}
	state.outboundEvents = activeEvents
	if len(activeEvents) >= discordGatewayEventLimit {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("Discord Gateway outgoing event limit reached; retry after the current 60-second window")
	}
	state.outboundEvents = append(state.outboundEvents, now)
	state.mu.Unlock()
	if err := socket.sendText(string(encoded)); err != nil {
		state.sessionMu.Unlock()
		return fail("Discord Gateway update failed: " + err.Error())
	}
	state.sessionMu.Unlock()
	return resVal(true, nilVal()), nil
}

func (r *Runtime) discordGatewayUnbindSession() Value {
	if r.discordGateway != nil {
		state := r.discordGateway
		state.sessionMu.Lock()
		defer state.sessionMu.Unlock()
		state.mu.Lock()
		failures := make([]Value, 0, len(state.failures)+len(state.chunks))
		for _, failure := range state.failures {
			failures = append(failures, stringVal(failure))
		}
		for nonce, request := range state.chunks {
			if encoded := encodeDiscordMemberQueryFailure(discordGatewayMemberQueryFailure{
				GuildID: request.guildID,
				Nonce:   nonce,
				Reason:  "session_ended",
			}); encoded != "" {
				failures = append(failures, stringVal(encoded))
			}
		}
		state.socket = nil
		state.intents = 0
		state.chunks = make(map[string]*discordMemberChunkRequest)
		state.chunkBytes = 0
		state.outboundEvents = nil
		state.failures = nil
		state.mu.Unlock()
		return arrVal(failures)
	}
	return arrVal(nil)
}

func (r *Runtime) discordGatewayRequestMembers(guildID, query string, limit int64, userIDs []Value, presences bool, nonce string, allMembers bool) (Value, *Diagnostic) {
	fail := func(message string) (Value, *Diagnostic) { return resVal(false, stringVal(message)), nil }
	if !validDiscordSnowflakeString(guildID) {
		return fail("guild_id must be a valid Discord snowflake")
	}
	if !utf8.ValidString(query) || !utf8.ValidString(nonce) {
		return fail("Discord member request contains invalid UTF-8")
	}
	if allMembers {
		if query != "" || len(userIDs) != 0 || limit != 0 {
			return fail("requesting all guild members requires an empty query, no user IDs, and limit 0")
		}
	} else if len(userIDs) != 0 {
		if query != "" || len(userIDs) > 100 {
			return fail("member ID requests require an empty query and between 1 and 100 user IDs")
		}
		limit = 100
		seen := make(map[string]struct{}, len(userIDs))
		for _, value := range userIDs {
			if value.Kind != VString || !validDiscordSnowflakeString(value.S) {
				return fail("user_ids must contain valid Discord snowflakes")
			}
			if _, exists := seen[value.S]; exists {
				return fail("user_ids must be unique")
			}
			seen[value.S] = struct{}{}
		}
	} else {
		if query == "" || limit < 1 || limit > 100 {
			return fail("member prefix requests require a non-empty query and a limit from 1 to 100")
		}
	}
	if nonce != "" && !validDiscordMemberChunkNonce(nonce) {
		return fail("Discord Gateway member request nonce must not exceed 32 bytes")
	}
	if nonce == "" {
		var err error
		nonce, err = newDiscordMemberRequestNonce()
		if err != nil {
			return fail("could not generate a Discord Gateway member request nonce")
		}
	}
	request := struct {
		Opcode int `json:"op"`
		Data   struct {
			GuildID   string   `json:"guild_id"`
			Query     *string  `json:"query,omitempty"`
			Limit     int64    `json:"limit"`
			Presences bool     `json:"presences"`
			UserIDs   []string `json:"user_ids,omitempty"`
			Nonce     string   `json:"nonce"`
		} `json:"d"`
	}{Opcode: 8}
	request.Data.GuildID = guildID
	request.Data.Limit = limit
	request.Data.Presences = presences
	request.Data.Nonce = nonce
	if len(userIDs) == 0 {
		request.Data.Query = &query
	} else {
		request.Data.UserIDs = make([]string, len(userIDs))
		for index := range userIDs {
			request.Data.UserIDs[index] = userIDs[index].S
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return fail("could not encode Discord Gateway member request")
	}
	if len(encoded) > r.Lim.MaxSourceBytes {
		return fail("Discord Gateway member request exceeds configured input limit")
	}

	state := r.discordGateway
	if state == nil {
		return fail("Discord member requests are only available during Bot.run Gateway callbacks")
	}
	now := time.Now()
	state.sessionMu.Lock()
	state.mu.Lock()
	r.expireDiscordMemberChunkRequestsLocked(state, now)
	socket := state.socket
	if socket == nil || socket.isClosed() {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("Discord member requests are only available during Bot.run Gateway callbacks")
	}
	if allMembers && state.intents&discordGatewayIntentGuildMembers == 0 {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("requesting all guild members requires the GuildMembers intent")
	}
	if presences && state.intents&discordGatewayIntentGuildPresences == 0 {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("requesting member presences requires the GuildPresences intent")
	}
	for id, sentAt := range state.fullListAt {
		if now.Sub(sentAt) >= discordFullMemberRequestInterval {
			delete(state.fullListAt, id)
		}
	}
	if allMembers {
		if last, exists := state.fullListAt[guildID]; exists && now.Sub(last) < discordFullMemberRequestInterval {
			state.mu.Unlock()
			state.sessionMu.Unlock()
			return fail("a full member-list request was sent for this guild within the last 30 seconds")
		}
	}
	activeEvents := state.outboundEvents[:0]
	for _, sentAt := range state.outboundEvents {
		if now.Sub(sentAt) < discordGatewayEventWindow {
			activeEvents = append(activeEvents, sentAt)
		}
	}
	state.outboundEvents = activeEvents
	if len(state.outboundEvents) >= discordGatewayEventLimit {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("Discord Gateway outgoing event limit reached; retry after the current 60-second window")
	}
	if _, exists := state.chunks[nonce]; exists {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("a Discord member request with this nonce is already pending")
	}
	if len(state.chunks) >= discordMemberChunkPendingLimit {
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("too many Discord member requests are pending")
	}
	pending := &discordMemberChunkRequest{guildID: guildID, chunks: make(map[int]discordMemberChunkPart), createdAt: now}
	state.chunks[nonce] = pending
	state.outboundEvents = append(state.outboundEvents, now)
	if allMembers {
		if state.fullListAt == nil {
			state.fullListAt = make(map[string]time.Time)
		}
		state.fullListAt[guildID] = now
	}
	state.mu.Unlock()

	if err := socket.sendText(string(encoded)); err != nil {
		state.mu.Lock()
		if state.chunks[nonce] == pending {
			r.removeDiscordMemberChunkRequestLocked(state, nonce)
		}
		state.mu.Unlock()
		state.sessionMu.Unlock()
		return fail("Discord Gateway member request failed: " + err.Error())
	}
	state.sessionMu.Unlock()
	return resVal(true, stringVal(nonce)), nil
}

func (r *Runtime) discordGatewayMemberChunkResult(payload string) (Value, *Diagnostic) {
	var event discordGatewayMemberChunkEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil || event.Nonce == "" {
		return resVal(true, stringVal("")), nil
	}
	if !validDiscordSnowflakeString(event.GuildID) || event.ChunkIndex == nil || event.ChunkCount == nil || *event.ChunkCount < 1 || *event.ChunkCount > 4096 || *event.ChunkIndex < 0 || *event.ChunkIndex >= *event.ChunkCount {
		return resVal(false, stringVal("Discord member chunk metadata is invalid")), nil
	}
	part := discordMemberChunkPart{members: event.Members, notFound: event.NotFound, presences: event.Presences}
	for _, values := range [][]json.RawMessage{part.members, part.notFound, part.presences} {
		for _, value := range values {
			if !json.Valid(value) {
				return resVal(false, stringVal("Discord member chunk contains invalid JSON")), nil
			}
			part.bytes += len(value)
		}
	}
	part.bytes += 64
	state := r.discordGateway
	if state == nil {
		return resVal(true, stringVal("")), nil
	}
	now := time.Now()
	state.mu.Lock()
	r.expireDiscordMemberChunkRequestsLocked(state, now)
	request := state.chunks[event.Nonce]
	if request == nil {
		state.mu.Unlock()
		return resVal(true, stringVal("")), nil
	}
	if request.guildID != event.GuildID || (request.chunkCount != 0 && request.chunkCount != *event.ChunkCount) {
		if encoded := encodeDiscordMemberQueryFailure(discordGatewayMemberQueryFailure{
			GuildID: request.guildID,
			Nonce:   event.Nonce,
			Reason:  "chunk_metadata_mismatch",
		}); encoded != "" {
			state.failures = append(state.failures, encoded)
		}
		r.removeDiscordMemberChunkRequestLocked(state, event.Nonce)
		state.mu.Unlock()
		return resVal(false, stringVal("Discord member chunks do not match their request")), nil
	}
	if _, duplicate := request.chunks[*event.ChunkIndex]; duplicate {
		state.mu.Unlock()
		return resVal(true, stringVal("")), nil
	}
	if request.bytes+part.bytes > discordMemberChunkAggregateLimit || state.chunkBytes+part.bytes > discordMemberChunkAggregateLimit {
		if encoded := encodeDiscordMemberQueryFailure(discordGatewayMemberQueryFailure{
			GuildID: request.guildID,
			Nonce:   event.Nonce,
			Reason:  "aggregate_limit_exceeded",
		}); encoded != "" {
			state.failures = append(state.failures, encoded)
		}
		r.removeDiscordMemberChunkRequestLocked(state, event.Nonce)
		state.mu.Unlock()
		return resVal(false, stringVal("Discord member query exceeds the 32 MiB aggregation limit")), nil
	}
	request.chunkCount = *event.ChunkCount
	request.chunks[*event.ChunkIndex] = part
	request.bytes += part.bytes
	state.chunkBytes += part.bytes
	if len(request.chunks) != request.chunkCount {
		state.mu.Unlock()
		return resVal(true, stringVal("")), nil
	}
	result := discordGatewayMemberQueryResult{GuildID: request.guildID, Nonce: event.Nonce, Members: make([]json.RawMessage, 0)}
	for index := 0; index < request.chunkCount; index++ {
		chunk, exists := request.chunks[index]
		if !exists {
			state.mu.Unlock()
			return resVal(true, stringVal("")), nil
		}
		result.Members = append(result.Members, chunk.members...)
		result.NotFound = append(result.NotFound, chunk.notFound...)
		result.Presences = append(result.Presences, chunk.presences...)
	}
	r.removeDiscordMemberChunkRequestLocked(state, event.Nonce)
	state.mu.Unlock()

	encoded, err := json.Marshal(result)
	if err != nil {
		state.mu.Lock()
		if failure := encodeDiscordMemberQueryFailure(discordGatewayMemberQueryFailure{
			GuildID: result.GuildID,
			Nonce:   result.Nonce,
			Reason:  "result_encoding_failed",
		}); failure != "" {
			state.failures = append(state.failures, failure)
		}
		state.mu.Unlock()
		return resVal(false, stringVal("could not encode completed Discord member query")), nil
	}
	if len(encoded) > discordMemberChunkAggregateLimit {
		state.mu.Lock()
		if failure := encodeDiscordMemberQueryFailure(discordGatewayMemberQueryFailure{
			GuildID: result.GuildID,
			Nonce:   result.Nonce,
			Reason:  "aggregate_limit_exceeded",
		}); failure != "" {
			state.failures = append(state.failures, failure)
		}
		state.mu.Unlock()
		return resVal(false, stringVal("completed Discord member query exceeds the 32 MiB aggregation limit")), nil
	}
	return resVal(true, stringVal(string(encoded))), nil
}

type discordGatewayRateLimitEvent struct {
	Opcode     int      `json:"opcode"`
	RetryAfter *float64 `json:"retry_after"`
	Meta       struct {
		GuildID string `json:"guild_id"`
		Nonce   string `json:"nonce"`
	} `json:"meta"`
}

type discordGatewayMemberQueryFailure struct {
	GuildID    string   `json:"guild_id"`
	Nonce      string   `json:"nonce"`
	Reason     string   `json:"reason"`
	RetryAfter *float64 `json:"retry_after,omitempty"`
}

func encodeDiscordMemberQueryFailure(failure discordGatewayMemberQueryFailure) string {
	encoded, err := json.Marshal(failure)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func (r *Runtime) discordGatewayRateLimitResult(payload string) (Value, *Diagnostic) {
	var event discordGatewayRateLimitEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil || event.Opcode != 8 || event.Meta.Nonce == "" {
		return resVal(true, stringVal("")), nil
	}
	if !validDiscordSnowflakeString(event.Meta.GuildID) || event.RetryAfter == nil || *event.RetryAfter < 0 {
		return resVal(false, stringVal("Discord member-query rate-limit event is invalid")), nil
	}
	state := r.discordGateway
	if state == nil {
		return resVal(true, stringVal("")), nil
	}
	state.mu.Lock()
	r.expireDiscordMemberChunkRequestsLocked(state, time.Now())
	request := state.chunks[event.Meta.Nonce]
	if request == nil || request.guildID != event.Meta.GuildID {
		state.mu.Unlock()
		return resVal(true, stringVal("")), nil
	}
	failure := discordGatewayMemberQueryFailure{
		GuildID:    event.Meta.GuildID,
		Nonce:      event.Meta.Nonce,
		Reason:     "rate_limited",
		RetryAfter: event.RetryAfter,
	}
	if encoded := encodeDiscordMemberQueryFailure(failure); encoded != "" {
		state.failures = append(state.failures, encoded)
	}
	r.removeDiscordMemberChunkRequestLocked(state, event.Meta.Nonce)
	state.mu.Unlock()
	return resVal(true, stringVal("")), nil
}

func (r *Runtime) discordGatewayTakeMemberQueryFailures() Value {
	if r.discordGateway == nil {
		return arrVal(nil)
	}
	state := r.discordGateway
	state.mu.Lock()
	r.expireDiscordMemberChunkRequestsLocked(state, time.Now())
	failures := state.failures
	state.failures = nil
	state.mu.Unlock()
	values := make([]Value, len(failures))
	for i, failure := range failures {
		values[i] = stringVal(failure)
	}
	return arrVal(values)
}

func (r *Runtime) expireDiscordMemberChunkRequestsLocked(state *discordGatewayState, now time.Time) {
	for nonce, request := range state.chunks {
		if now.Sub(request.createdAt) > discordMemberChunkRequestTTL {
			if encoded := encodeDiscordMemberQueryFailure(discordGatewayMemberQueryFailure{
				GuildID: request.guildID,
				Nonce:   nonce,
				Reason:  "timeout",
			}); encoded != "" {
				state.failures = append(state.failures, encoded)
			}
			r.removeDiscordMemberChunkRequestLocked(state, nonce)
		}
	}
}

func (r *Runtime) removeDiscordMemberChunkRequestLocked(state *discordGatewayState, nonce string) {
	if request := state.chunks[nonce]; request != nil {
		state.chunkBytes -= request.bytes
		if state.chunkBytes < 0 {
			state.chunkBytes = 0
		}
		delete(state.chunks, nonce)
	}
}

func validDiscordMemberChunkNonce(nonce string) bool {
	return nonce != "" && utf8.ValidString(nonce) && len([]byte(nonce)) <= 32 && !strings.ContainsRune(nonce, '\x00')
}
