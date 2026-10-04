package graphtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// BodySentinel appears in the body of every injected failure so tests can
// prove the adapter never echoes a response body.
const BodySentinel = "SERVER-BODY-SENTINEL"

// Prefix is the path prefix of every route, mirroring graph.microsoft.com/v1.0.
const Prefix = "/v1.0"

// Message is a seeded or posted message in Graph shape.
type Message struct {
	ID          string
	ReplyToID   string // non-empty for replies
	MessageType string // default "message"
	Created     time.Time
	Modified    time.Time // default Created
	Deleted     bool
	// Sender: Kind is "user" (default), "application", "bot" or "none"
	// (from: null).
	FromKind     string
	FromUserID   string
	FromTenantID string
	FromName     string
	BodyType     string // default "text"
	Content      string
	Mentions     []Mention
	NullBody     bool // body: null
	seq          int
}

// Mention is a mention on a message.
type Mention struct {
	ID     int
	Text   string
	UserID string
}

// Request is one recorded request. The Authorization value is never kept.
type Request struct {
	Method, Path, RawQuery string
	HasAuth                bool
	Header                 http.Header
	Body                   string
}

// Rule injects a failure for requests whose "METHOD /path" starts with Match
// (the path excludes Prefix). Times <= 0 means forever.
type Rule struct {
	Match      string
	Status     int
	RetryAfter string
	Header     map[string]string
	Body       string
	Times      int
}

type chatInfo struct {
	id      string
	members []string
	status  int // forced status for GET /chats/{id} and its messages; 0 = ok
}

type channelKey struct{ team, channel string }

// Server is the fake Graph.
type Server struct {
	srv *httptest.Server
	t   testing.TB

	mu          sync.Mutex
	meID        string
	meName      string
	meUPN       string
	chats       map[string]*chatInfo
	chatMsgs    map[string][]*Message
	chanMsgs    map[channelKey][]*Message
	chanSeq     map[channelKey]int
	rules       []*Rule
	reqs        []Request
	unauth      int
	noAuth      bool // allow requests without Authorization
	pageSize    int
	deltaStatus int // when non-zero, delta routes answer with it
	createStat  int // when non-zero, POST /chats answers with it
	nextID      int
	chatSeq     int
	now         func() time.Time
}

// New starts a fake Graph that is closed with the test.
func New(t testing.TB) *Server {
	s := &Server{
		t:        t,
		meID:     "00000000-0000-0000-0000-00000000a001",
		meName:   "Agent One",
		meUPN:    "agent.one@example.com",
		chats:    map[string]*chatInfo{},
		chatMsgs: map[string][]*Message{},
		chanMsgs: map[channelKey][]*Message{},
		chanSeq:  map[channelKey]int{},
		pageSize: 50,
		nextID:   1696000000000,
		now:      time.Now,
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

// BaseURL is the Graph root to configure the adapter with (includes /v1.0).
func (s *Server) BaseURL() string { return s.srv.URL + Prefix }

// Close stops the server early.
func (s *Server) Close() { s.srv.Close() }

// SetMe seeds GET /me.
func (s *Server) SetMe(id, displayName, upn string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meID, s.meName, s.meUPN = id, displayName, upn
}

// MeID returns the seeded own id.
func (s *Server) MeID() string { s.mu.Lock(); defer s.mu.Unlock(); return s.meID }

// SetPageSize sets how many items a page returns before @odata.nextLink.
func (s *Server) SetPageSize(n int) { s.mu.Lock(); s.pageSize = n; s.mu.Unlock() }

// DisableDelta makes the channel delta routes answer with status.
func (s *Server) DisableDelta(status int) { s.mu.Lock(); s.deltaStatus = status; s.mu.Unlock() }

// FailChatCreate makes POST /chats answer with status.
func (s *Server) FailChatCreate(status int) { s.mu.Lock(); s.createStat = status; s.mu.Unlock() }

// AllowNoAuth stops the 401 answer for requests without Authorization.
func (s *Server) AllowNoAuth() { s.mu.Lock(); s.noAuth = true; s.mu.Unlock() }

// Unauthorized401 makes the first n requests answer 401 whatever they carry
// (the 401-then-200 scenario).
func (s *Server) Unauthorized401(n int) { s.mu.Lock(); s.unauth = n; s.mu.Unlock() }

// AddRule injects a failure.
func (s *Server) AddRule(r Rule) { s.mu.Lock(); s.rules = append(s.rules, &r); s.mu.Unlock() }

// AddChat seeds a chat the agent is a member of. members are AAD ids of the
// other participants (the agent is added implicitly for one-on-one lookups).
func (s *Server) AddChat(chatID string, members ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chats[chatID] = &chatInfo{id: chatID, members: append([]string{s.meID}, members...)}
}

// SetChatStatus forces GET /chats/{id} and its message routes to answer with
// status (for example 403 or 404 for a chat the agent is not in).
func (s *Server) SetChatStatus(chatID string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.chats[chatID]
	if c == nil {
		c = &chatInfo{id: chatID}
		s.chats[chatID] = c
	}
	c.status = status
}

// AddChatMessage seeds a chat message.
func (s *Server) AddChatMessage(chatID string, m Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chats[chatID] == nil {
		s.chats[chatID] = &chatInfo{id: chatID, members: []string{s.meID}}
	}
	mm := s.fill(m)
	s.chatMsgs[chatID] = append(s.chatMsgs[chatID], &mm)
}

// AddChannelMessage seeds a top-level channel message (or a reply when
// m.ReplyToID is set).
func (s *Server) AddChannelMessage(team, channel string, m Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addChan(channelKey{team, channel}, m)
}

func (s *Server) addChan(k channelKey, m Message) *Message {
	mm := s.fill(m)
	s.chanSeq[k]++
	mm.seq = s.chanSeq[k]
	s.chanMsgs[k] = append(s.chanMsgs[k], &mm)
	return &mm
}

func (s *Server) fill(m Message) Message {
	if m.ID == "" {
		s.nextID++
		m.ID = strconv.Itoa(s.nextID)
	}
	if m.MessageType == "" {
		m.MessageType = "message"
	}
	if m.Created.IsZero() {
		m.Created = s.now().UTC()
	}
	if m.Modified.IsZero() {
		m.Modified = m.Created
	}
	if m.FromKind == "" {
		m.FromKind = "user"
	}
	if m.BodyType == "" {
		m.BodyType = "text"
	}
	return m
}

// Requests returns a copy of the recorded requests.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Count returns how many recorded requests match "METHOD /path-prefix".
func (s *Server) Count(match string) int {
	n := 0
	for _, r := range s.Requests() {
		if strings.HasPrefix(r.Method+" "+r.Path, match) {
			n++
		}
	}
	return n
}

// Posts returns the bodies of recorded POST requests.
func (s *Server) Posts() []string {
	var out []string
	for _, r := range s.Requests() {
		if r.Method == http.MethodPost {
			out = append(out, r.Body)
		}
	}
	return out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	path := strings.TrimPrefix(r.URL.Path, Prefix)
	hdr := r.Header.Clone()
	hdr.Del("Authorization")

	s.mu.Lock()
	s.reqs = append(s.reqs, Request{Method: r.Method, Path: path, RawQuery: r.URL.RawQuery,
		HasAuth: r.Header.Get("Authorization") != "", Header: hdr, Body: string(body)})
	if s.unauth > 0 {
		s.unauth--
		s.mu.Unlock()
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeErr(w, http.StatusUnauthorized, "InvalidAuthenticationToken")
		return
	}
	if r.Header.Get("Authorization") == "" && !s.noAuth {
		s.mu.Unlock()
		writeErr(w, http.StatusUnauthorized, "InvalidAuthenticationToken")
		return
	}
	if rule := s.matchRule(r.Method + " " + path); rule != nil {
		s.mu.Unlock()
		for k, v := range rule.Header {
			w.Header().Set(k, v)
		}
		if rule.RetryAfter != "" {
			w.Header().Set("Retry-After", rule.RetryAfter)
		}
		b := rule.Body
		if b == "" {
			b = `{"error":{"code":"Injected","message":"` + BodySentinel + `"}}`
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rule.Status)
		_, _ = io.WriteString(w, b)
		return
	}
	defer s.mu.Unlock()
	s.route(w, r, path, body)
}

func (s *Server) matchRule(key string) *Rule {
	for _, r := range s.rules {
		if !strings.HasPrefix(key, r.Match) {
			continue
		}
		if r.Times > 0 {
			r.Times--
			if r.Times == 0 {
				defer s.dropRule(r)
			}
		}
		return r
	}
	return nil
}

func (s *Server) dropRule(r *Rule) {
	for i, x := range s.rules {
		if x == r {
			s.rules = append(s.rules[:i], s.rules[i+1:]...)
			return
		}
	}
}

func writeErr(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":{"code":%q,"message":%q}}`, code, BodySentinel)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// route dispatches; s.mu is held.
func (s *Server) route(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	seg := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && path == "/me":
		writeJSON(w, 200, map[string]any{"id": s.meID, "displayName": s.meName, "userPrincipalName": s.meUPN})
	case r.Method == http.MethodGet && path == "/me/chats":
		s.listChats(w, r)
	case r.Method == http.MethodPost && path == "/chats":
		s.createChat(w, body)
	case len(seg) == 2 && seg[0] == "chats" && r.Method == http.MethodGet:
		s.getChat(w, seg[1])
	case len(seg) == 3 && seg[0] == "chats" && seg[2] == "messages":
		s.chatMessages(w, r, seg[1], body)
	case len(seg) >= 5 && seg[0] == "teams" && seg[2] == "channels" && seg[4] == "messages":
		s.channelRoute(w, r, channelKey{seg[1], seg[3]}, seg[5:], body)
	default:
		writeErr(w, 404, "NotFound")
	}
}

func (s *Server) chat(w http.ResponseWriter, id string) *chatInfo {
	c := s.chats[id]
	if c == nil {
		writeErr(w, 404, "NotFound")
		return nil
	}
	if c.status != 0 {
		writeErr(w, c.status, "Forbidden")
		return nil
	}
	return c
}

func (s *Server) getChat(w http.ResponseWriter, id string) {
	if c := s.chat(w, id); c != nil {
		writeJSON(w, 200, map[string]any{"id": c.id, "chatType": "oneOnOne"})
	}
}

func (s *Server) listChats(w http.ResponseWriter, r *http.Request) {
	ids := make([]string, 0, len(s.chats))
	for id, c := range s.chats {
		if c.status == 0 && len(c.members) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		var members []any
		for _, m := range s.chats[id].members {
			members = append(members, map[string]any{"userId": m, "displayName": "n"})
		}
		items = append(items, map[string]any{"id": id, "chatType": "oneOnOne", "members": members})
	}
	s.pageOut(w, r, items, "")
}

func (s *Server) createChat(w http.ResponseWriter, body []byte) {
	if s.createStat != 0 {
		writeErr(w, s.createStat, "Forbidden")
		return
	}
	var in struct {
		ChatType string `json:"chatType"`
		Members  []struct {
			Bind string `json:"user@odata.bind"`
		} `json:"members"`
	}
	if json.Unmarshal(body, &in) != nil || in.ChatType != "oneOnOne" || len(in.Members) != 2 {
		writeErr(w, 400, "BadRequest")
		return
	}
	var ids []string
	for _, m := range in.Members {
		i, j := strings.Index(m.Bind, "('"), strings.LastIndex(m.Bind, "')")
		if i < 0 || j < i {
			writeErr(w, 400, "BadRequest")
			return
		}
		ids = append(ids, m.Bind[i+2:j])
	}
	// UA-6 assumption: creating returns the existing chat when one exists.
	for _, c := range s.chats {
		if len(c.members) == 2 && has(c.members, ids[0]) && has(c.members, ids[1]) {
			writeJSON(w, 200, map[string]any{"id": c.id, "chatType": "oneOnOne"})
			return
		}
	}
	s.chatSeq++
	id := fmt.Sprintf("19:created%d@unq.gbl.spaces", s.chatSeq)
	s.chats[id] = &chatInfo{id: id, members: ids}
	writeJSON(w, 201, map[string]any{"id": id, "chatType": "oneOnOne"})
}

func has(l []string, v string) bool {
	for _, x := range l {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func (s *Server) chatMessages(w http.ResponseWriter, r *http.Request, id string, body []byte) {
	c := s.chat(w, id)
	if c == nil {
		return
	}
	if r.Method == http.MethodPost {
		m, ok := s.decodePost(w, body, "")
		if ok {
			s.chatMsgs[id] = append(s.chatMsgs[id], m)
			writeJSON(w, 201, render(m))
		}
		return
	}
	list := filterSince(s.chatMsgs[id], r.URL.Query().Get("$filter"))
	sort.SliceStable(list, func(i, j int) bool { return list[i].Modified.After(list[j].Modified) })
	s.pageOut(w, r, renderAll(list), "")
}

func filterSince(in []*Message, filter string) []*Message {
	out := append([]*Message(nil), in...)
	const k = "lastModifiedDateTime gt "
	if i := strings.Index(filter, k); i >= 0 {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(filter[i+len(k):])); err == nil {
			out = out[:0]
			for _, m := range in {
				if m.Modified.After(t) {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

func (s *Server) channelRoute(w http.ResponseWriter, r *http.Request, k channelKey, rest []string, body []byte) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		var top []*Message
		for _, m := range s.chanMsgs[k] {
			if m.ReplyToID == "" {
				top = append(top, m)
			}
		}
		sort.SliceStable(top, func(i, j int) bool { return top[i].Modified.After(top[j].Modified) })
		s.pageOut(w, r, renderAll(top), "")
	case len(rest) == 0 && r.Method == http.MethodPost:
		if m, ok := s.decodePost(w, body, ""); ok {
			stored := s.addChan(k, *m)
			writeJSON(w, 201, render(stored))
		}
	case len(rest) == 1 && rest[0] == "delta" && r.Method == http.MethodGet:
		s.delta(w, r, k)
	case len(rest) == 2 && rest[1] == "replies" && r.Method == http.MethodGet:
		var rep []*Message
		for _, m := range s.chanMsgs[k] {
			if m.ReplyToID == rest[0] {
				rep = append(rep, m)
			}
		}
		s.pageOut(w, r, renderAll(rep), "")
	case len(rest) == 2 && rest[1] == "replies" && r.Method == http.MethodPost:
		if m, ok := s.decodePost(w, body, rest[0]); ok {
			stored := s.addChan(k, *m)
			writeJSON(w, 201, render(stored))
		}
	default:
		writeErr(w, 404, "NotFound")
	}
}

// delta serves the first delta (all top-level messages, paged) and later
// deltas (`$deltatoken=N`: messages changed after sequence N).
func (s *Server) delta(w http.ResponseWriter, r *http.Request, k channelKey) {
	if s.deltaStatus != 0 {
		writeErr(w, s.deltaStatus, "NotSupported")
		return
	}
	after := 0
	if tok := r.URL.Query().Get("$deltatoken"); tok != "" {
		after, _ = strconv.Atoi(tok)
	}
	var list []*Message
	for _, m := range s.chanMsgs[k] {
		if m.ReplyToID == "" && m.seq > after {
			list = append(list, m)
		}
	}
	link := fmt.Sprintf("%s%s/teams/%s/channels/%s/messages/delta?$deltatoken=%d", s.srv.URL, Prefix, k.team, k.channel, s.chanSeq[k])
	s.pageOut(w, r, renderAll(list), link)
}

// pageOut writes items with paging by $skiptoken offset. deltaLink is added to
// the last page when non-empty.
func (s *Server) pageOut(w http.ResponseWriter, r *http.Request, items []any, deltaLink string) {
	q := r.URL.Query()
	size := s.pageSize
	if top, err := strconv.Atoi(q.Get("$top")); err == nil && top > 0 && top < size {
		size = top
	}
	off, _ := strconv.Atoi(q.Get("$skiptoken"))
	if off > len(items) {
		off = len(items)
	}
	end := off + size
	out := map[string]any{}
	if end < len(items) {
		nq := url.Values{}
		for k, v := range q {
			nq[k] = v
		}
		nq.Set("$skiptoken", strconv.Itoa(end))
		out["@odata.nextLink"] = s.srv.URL + r.URL.Path + "?" + nq.Encode()
	} else {
		end = len(items)
		if deltaLink != "" {
			out["@odata.deltaLink"] = deltaLink
		}
	}
	out["value"] = append([]any{}, items[off:end]...)
	writeJSON(w, 200, out)
}

func (s *Server) decodePost(w http.ResponseWriter, body []byte, replyTo string) (*Message, bool) {
	var in struct {
		Body struct {
			ContentType string `json:"contentType"`
			Content     string `json:"content"`
		} `json:"body"`
		Mentions []struct {
			ID          int    `json:"id"`
			MentionText string `json:"mentionText"`
			Mentioned   struct {
				User struct {
					ID string `json:"id"`
				} `json:"user"`
			} `json:"mentioned"`
		} `json:"mentions"`
	}
	if json.Unmarshal(body, &in) != nil || strings.TrimSpace(in.Body.Content) == "" {
		writeErr(w, 400, "BadRequest")
		return nil, false
	}
	m := s.fill(Message{ReplyToID: replyTo, FromUserID: s.meID, FromName: s.meName,
		BodyType: in.Body.ContentType, Content: in.Body.Content})
	for _, x := range in.Mentions {
		m.Mentions = append(m.Mentions, Mention{ID: x.ID, Text: x.MentionText, UserID: x.Mentioned.User.ID})
	}
	return &m, true
}

func renderAll(in []*Message) []any {
	out := make([]any, 0, len(in))
	for _, m := range in {
		out = append(out, render(m))
	}
	return out
}

const gtime = "2006-01-02T15:04:05.000Z"

func render(m *Message) map[string]any {
	o := map[string]any{
		"id": m.ID, "messageType": m.MessageType,
		"createdDateTime":      m.Created.UTC().Format(gtime),
		"lastModifiedDateTime": m.Modified.UTC().Format(gtime),
		"deletedDateTime":      nil,
		"replyToId":            nil,
		"from":                 nil,
		"body":                 nil,
		"mentions":             []any{},
	}
	if m.Deleted {
		o["deletedDateTime"] = m.Modified.UTC().Format(gtime)
	}
	if m.ReplyToID != "" {
		o["replyToId"] = m.ReplyToID
	}
	switch m.FromKind {
	case "user":
		o["from"] = map[string]any{"application": nil, "device": nil,
			"user": map[string]any{"id": m.FromUserID, "displayName": m.FromName, "tenantId": nilIfEmpty(m.FromTenantID), "userIdentityType": "aadUser"}}
	case "application", "bot":
		app := map[string]any{"id": m.FromUserID, "displayName": m.FromName}
		if m.FromKind == "bot" {
			app["applicationIdentityType"] = "bot"
		}
		o["from"] = map[string]any{"user": nil, "device": nil, "application": app}
	}
	if !m.NullBody {
		o["body"] = map[string]any{"contentType": m.BodyType, "content": m.Content}
	}
	if len(m.Mentions) > 0 {
		var ms []any
		for _, x := range m.Mentions {
			ms = append(ms, map[string]any{"id": x.ID, "mentionText": x.Text,
				"mentioned": map[string]any{"user": map[string]any{"id": x.UserID}}})
		}
		o["mentions"] = ms
	}
	return o
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
