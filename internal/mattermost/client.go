// Package mattermost is a small, typed client for the Mattermost REST API v4.
//
// Every call takes the bearer token explicitly: the gateway calls Mattermost
// with the *mapped user's* credential so Mattermost's own permission model
// (channel membership, private channels, DMs) is enforced for that user. The
// service token is only used for directory lookups and token issuance.
package mattermost

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	BaseURL            string
	Timeout            time.Duration
	InsecureSkipVerify bool
	CAPEM              string
}

type Client struct {
	base string
	http *http.Client
}

func New(o Options) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(o.BaseURL), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("Mattermost URL must be an absolute http(s) URL")
	}
	base := strings.TrimSuffix(u.String(), "/api/v4")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil                                                                         // offline networks: never route to an environment proxy implicitly
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: o.InsecureSkipVerify} //nolint:gosec // admin opt-in
	if strings.TrimSpace(o.CAPEM) != "" {
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(o.CAPEM)) {
			return nil, errors.New("CA certificate PEM could not be parsed")
		}
		tlsCfg.RootCAs = pool
	}
	transport.TLSClientConfig = tlsCfg
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{base: base, http: &http.Client{Timeout: timeout, Transport: transport}}, nil
}

func (c *Client) BaseURL() string { return c.base }

// APIError is a non-2xx response from Mattermost.
type APIError struct {
	Status  int
	ID      string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("mattermost %d %s: %s", e.Status, e.ID, e.Message)
}

func StatusOf(err error) int {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func (c *Client) do(ctx context.Context, token, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v4"+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, token, out)
}

func (c *Client) send(req *http.Request, token string, out any) error {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mattermost request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode}
		var payload struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &payload) == nil {
			ae.ID, ae.Message = payload.ID, payload.Message
		}
		if ae.Message == "" {
			ae.Message = http.StatusText(resp.StatusCode)
		}
		return ae
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

// ---- types ----

type User struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Nickname  string `json:"nickname"`
	Position  string `json:"position"`
	Roles     string `json:"roles"`
	Locale    string `json:"locale"`
	DeleteAt  int64  `json:"delete_at"`
	IsBot     bool   `json:"is_bot"`
}

func (u User) DisplayName() string {
	name := strings.TrimSpace(u.LastName + u.FirstName)
	if name == "" {
		name = strings.TrimSpace(u.FirstName + " " + u.LastName)
	}
	if name == "" {
		name = u.Nickname
	}
	return name
}

type Team struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	DeleteAt    int64  `json:"delete_at"`
}

type Channel struct {
	ID            string `json:"id"`
	TeamID        string `json:"team_id"`
	Type          string `json:"type"` // O public, P private, D direct, G group
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Header        string `json:"header"`
	Purpose       string `json:"purpose"`
	TotalMsgCount int64  `json:"total_msg_count"`
	LastPostAt    int64  `json:"last_post_at"`
	CreateAt      int64  `json:"create_at"`
	DeleteAt      int64  `json:"delete_at"`
	CreatorID     string `json:"creator_id"`
}

type ChannelMember struct {
	ChannelID    string `json:"channel_id"`
	UserID       string `json:"user_id"`
	Roles        string `json:"roles"`
	LastViewedAt int64  `json:"last_viewed_at"`
	MsgCount     int64  `json:"msg_count"`
	MentionCount int64  `json:"mention_count"`
}

type Post struct {
	ID         string         `json:"id"`
	CreateAt   int64          `json:"create_at"`
	UpdateAt   int64          `json:"update_at"`
	EditAt     int64          `json:"edit_at"`
	DeleteAt   int64          `json:"delete_at"`
	IsPinned   bool           `json:"is_pinned"`
	UserID     string         `json:"user_id"`
	ChannelID  string         `json:"channel_id"`
	RootID     string         `json:"root_id"`
	Message    string         `json:"message"`
	Type       string         `json:"type"`
	FileIDs    []string       `json:"file_ids"`
	ReplyCount int64          `json:"reply_count"`
	Props      map[string]any `json:"props"`
}

type PostList struct {
	Order []string        `json:"order"`
	Posts map[string]Post `json:"posts"`
}

// Ordered returns posts in the order Mattermost gave (newest first).
func (l PostList) Ordered() []Post {
	out := make([]Post, 0, len(l.Order))
	for _, id := range l.Order {
		if p, ok := l.Posts[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

type FileInfo struct {
	ID        string `json:"id"`
	PostID    string `json:"post_id"`
	ChannelID string `json:"channel_id"`
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	CreateAt  int64  `json:"create_at"`
}

type UserAccessToken struct {
	ID          string `json:"id"`
	Token       string `json:"token"`
	UserID      string `json:"user_id"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
}

// ---- system ----

func (c *Client) Ping(ctx context.Context) error {
	var out map[string]any
	return c.do(ctx, "", http.MethodGet, "/system/ping", nil, &out)
}

func (c *Client) ServerVersion(ctx context.Context) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v4/system/ping", nil)
	if err != nil {
		return ""
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	// X-Version-Id is "11.11.1.<build>.<hash>.<flag>"; the first three parts are the release
	v := strings.Split(resp.Header.Get("X-Version-Id"), ".")
	if len(v) > 3 {
		v = v[:3]
	}
	return strings.Join(v, ".")
}

// ---- users ----

func (c *Client) Me(ctx context.Context, token string) (*User, error) {
	var u User
	return &u, c.do(ctx, token, http.MethodGet, "/users/me", nil, &u)
}

func (c *Client) UserByUsername(ctx context.Context, token, username string) (*User, error) {
	var u User
	return &u, c.do(ctx, token, http.MethodGet, "/users/username/"+url.PathEscape(username), nil, &u)
}

func (c *Client) UserByEmail(ctx context.Context, token, email string) (*User, error) {
	var u User
	return &u, c.do(ctx, token, http.MethodGet, "/users/email/"+url.PathEscape(email), nil, &u)
}

func (c *Client) User(ctx context.Context, token, id string) (*User, error) {
	var u User
	return &u, c.do(ctx, token, http.MethodGet, "/users/"+url.PathEscape(id), nil, &u)
}

func (c *Client) UsersByIDs(ctx context.Context, token string, ids []string) ([]User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []User
	return out, c.do(ctx, token, http.MethodPost, "/users/ids", ids, &out)
}

func (c *Client) SearchUsers(ctx context.Context, token, term string, limit int) ([]User, error) {
	var out []User
	body := map[string]any{"term": term, "limit": limit, "allow_inactive": false}
	return out, c.do(ctx, token, http.MethodPost, "/users/search", body, &out)
}

func (c *Client) CreateUserAccessToken(ctx context.Context, token, userID, description string) (*UserAccessToken, error) {
	var out UserAccessToken
	return &out, c.do(ctx, token, http.MethodPost, "/users/"+url.PathEscape(userID)+"/tokens", map[string]string{"description": description}, &out)
}

func (c *Client) RevokeUserAccessToken(ctx context.Context, token, tokenID string) error {
	return c.do(ctx, token, http.MethodPost, "/users/tokens/revoke", map[string]string{"token_id": tokenID}, nil)
}

// ---- teams & channels ----

func (c *Client) MyTeams(ctx context.Context, token string) ([]Team, error) {
	var out []Team
	return out, c.do(ctx, token, http.MethodGet, "/users/me/teams", nil, &out)
}

func (c *Client) TeamByName(ctx context.Context, token, name string) (*Team, error) {
	var t Team
	return &t, c.do(ctx, token, http.MethodGet, "/teams/name/"+url.PathEscape(name), nil, &t)
}

func (c *Client) MyChannels(ctx context.Context, token, teamID string) ([]Channel, error) {
	var out []Channel
	return out, c.do(ctx, token, http.MethodGet, "/users/me/teams/"+url.PathEscape(teamID)+"/channels", nil, &out)
}

func (c *Client) MyChannelMembers(ctx context.Context, token, teamID string) ([]ChannelMember, error) {
	var out []ChannelMember
	return out, c.do(ctx, token, http.MethodGet, "/users/me/teams/"+url.PathEscape(teamID)+"/channels/members", nil, &out)
}

func (c *Client) Channel(ctx context.Context, token, id string) (*Channel, error) {
	var ch Channel
	return &ch, c.do(ctx, token, http.MethodGet, "/channels/"+url.PathEscape(id), nil, &ch)
}

func (c *Client) ChannelByName(ctx context.Context, token, teamID, name string) (*Channel, error) {
	var ch Channel
	return &ch, c.do(ctx, token, http.MethodGet, "/teams/"+url.PathEscape(teamID)+"/channels/name/"+url.PathEscape(name), nil, &ch)
}

func (c *Client) ChannelMember(ctx context.Context, token, channelID, userID string) (*ChannelMember, error) {
	var m ChannelMember
	return &m, c.do(ctx, token, http.MethodGet, "/channels/"+url.PathEscape(channelID)+"/members/"+url.PathEscape(userID), nil, &m)
}

func (c *Client) ChannelStats(ctx context.Context, token, channelID string) (int64, error) {
	var out struct {
		MemberCount int64 `json:"member_count"`
	}
	err := c.do(ctx, token, http.MethodGet, "/channels/"+url.PathEscape(channelID)+"/stats", nil, &out)
	return out.MemberCount, err
}

func (c *Client) ChannelMembers(ctx context.Context, token, channelID string, perPage int) ([]ChannelMember, error) {
	var out []ChannelMember
	return out, c.do(ctx, token, http.MethodGet, fmt.Sprintf("/channels/%s/members?per_page=%d", url.PathEscape(channelID), perPage), nil, &out)
}

func (c *Client) CreateDirectChannel(ctx context.Context, token, userA, userB string) (*Channel, error) {
	var ch Channel
	return &ch, c.do(ctx, token, http.MethodPost, "/channels/direct", []string{userA, userB}, &ch)
}

func (c *Client) CreateGroupChannel(ctx context.Context, token string, userIDs []string) (*Channel, error) {
	var ch Channel
	return &ch, c.do(ctx, token, http.MethodPost, "/channels/group", userIDs, &ch)
}

func (c *Client) CreateChannel(ctx context.Context, token string, ch Channel) (*Channel, error) {
	var out Channel
	body := map[string]string{"team_id": ch.TeamID, "name": ch.Name, "display_name": ch.DisplayName, "type": ch.Type, "purpose": ch.Purpose, "header": ch.Header}
	return &out, c.do(ctx, token, http.MethodPost, "/channels", body, &out)
}

func (c *Client) AddChannelMember(ctx context.Context, token, channelID, userID string) error {
	return c.do(ctx, token, http.MethodPost, "/channels/"+url.PathEscape(channelID)+"/members", map[string]string{"user_id": userID}, nil)
}

func (c *Client) ArchiveChannel(ctx context.Context, token, channelID string) error {
	return c.do(ctx, token, http.MethodDelete, "/channels/"+url.PathEscape(channelID), nil, nil)
}

// ---- posts ----

func (c *Client) ChannelPosts(ctx context.Context, token, channelID string, perPage int, since int64) (*PostList, error) {
	q := url.Values{}
	q.Set("per_page", strconv.Itoa(perPage))
	if since > 0 {
		q.Set("since", strconv.FormatInt(since, 10))
	}
	var out PostList
	return &out, c.do(ctx, token, http.MethodGet, "/channels/"+url.PathEscape(channelID)+"/posts?"+q.Encode(), nil, &out)
}

func (c *Client) PinnedPosts(ctx context.Context, token, channelID string) (*PostList, error) {
	var out PostList
	return &out, c.do(ctx, token, http.MethodGet, "/channels/"+url.PathEscape(channelID)+"/pinned", nil, &out)
}

func (c *Client) Post(ctx context.Context, token, id string) (*Post, error) {
	var p Post
	return &p, c.do(ctx, token, http.MethodGet, "/posts/"+url.PathEscape(id), nil, &p)
}

func (c *Client) Thread(ctx context.Context, token, id string) (*PostList, error) {
	var out PostList
	return &out, c.do(ctx, token, http.MethodGet, "/posts/"+url.PathEscape(id)+"/thread", nil, &out)
}

func (c *Client) SearchPosts(ctx context.Context, token, teamID, terms string, isOr bool, perPage int) (*PostList, error) {
	var out PostList
	body := map[string]any{"terms": terms, "is_or_search": isOr, "page": 0, "per_page": perPage, "include_deleted_channels": false}
	return &out, c.do(ctx, token, http.MethodPost, "/teams/"+url.PathEscape(teamID)+"/posts/search", body, &out)
}

func (c *Client) CreatePost(ctx context.Context, token string, p Post) (*Post, error) {
	var out Post
	body := map[string]any{"channel_id": p.ChannelID, "message": p.Message}
	if p.RootID != "" {
		body["root_id"] = p.RootID
	}
	if len(p.FileIDs) > 0 {
		body["file_ids"] = p.FileIDs
	}
	if len(p.Props) > 0 {
		body["props"] = p.Props
	}
	return &out, c.do(ctx, token, http.MethodPost, "/posts", body, &out)
}

func (c *Client) PatchPost(ctx context.Context, token, id, message string) (*Post, error) {
	var out Post
	return &out, c.do(ctx, token, http.MethodPut, "/posts/"+url.PathEscape(id)+"/patch", map[string]string{"message": message}, &out)
}

func (c *Client) DeletePost(ctx context.Context, token, id string) error {
	return c.do(ctx, token, http.MethodDelete, "/posts/"+url.PathEscape(id), nil, nil)
}

func (c *Client) AddReaction(ctx context.Context, token, userID, postID, emoji string) error {
	body := map[string]string{"user_id": userID, "post_id": postID, "emoji_name": emoji}
	return c.do(ctx, token, http.MethodPost, "/reactions", body, nil)
}

// ---- files ----

func (c *Client) UploadFile(ctx context.Context, token, channelID, filename string, content []byte) (*FileInfo, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("channel_id", channelID)
	part, err := w.CreateFormFile("files", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/v4/files", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var out struct {
		FileInfos []FileInfo `json:"file_infos"`
	}
	if err := c.send(req, token, &out); err != nil {
		return nil, err
	}
	if len(out.FileInfos) == 0 {
		return nil, errors.New("upload returned no file")
	}
	return &out.FileInfos[0], nil
}

func (c *Client) FileInfo(ctx context.Context, token, id string) (*FileInfo, error) {
	var out FileInfo
	return &out, c.do(ctx, token, http.MethodGet, "/files/"+url.PathEscape(id)+"/info", nil, &out)
}

func (c *Client) FileContent(ctx context.Context, token, id string) ([]byte, error) {
	var out []byte
	return out, c.do(ctx, token, http.MethodGet, "/files/"+url.PathEscape(id), nil, &out)
}
