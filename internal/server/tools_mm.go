package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hkjang/mmcp/internal/mattermost"
)

var mmIDPattern = regexp.MustCompile(`^[a-z0-9]{26}$`)
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func isMMID(s string) bool { return mmIDPattern.MatchString(s) }

func msTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func truncate(s string, max int) (string, bool) {
	if utf8.RuneCountInString(s) <= max {
		return s, false
	}
	r := []rune(s)
	return string(r[:max]) + "…", true
}

func hashQuery(q string) string {
	sum := sha256.Sum256([]byte(q))
	return hex.EncodeToString(sum[:8])
}

// ---- lookups shared by tools ----

func (tc *toolCtx) myTeams(ctx context.Context, token string) ([]mattermost.Team, error) {
	teams, err := tc.mm.MyTeams(ctx, token)
	if err != nil {
		return nil, mmError(err, "팀 목록")
	}
	out := teams[:0]
	for _, t := range teams {
		if t.DeleteAt == 0 {
			out = append(out, t)
		}
	}
	return out, nil
}

func (tc *toolCtx) resolveTeam(ctx context.Context, token, ref string) (*mattermost.Team, error) {
	teams, err := tc.myTeams(ctx, token)
	if err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	for i := range teams {
		t := &teams[i]
		if t.ID == ref || strings.EqualFold(t.Name, ref) || strings.EqualFold(t.DisplayName, ref) {
			return t, nil
		}
	}
	return nil, &toolError{Code: "TEAM_NOT_FOUND", Message: "참여 중인 팀에서 '" + ref + "'을(를) 찾을 수 없습니다", Details: teamNames(teams)}
}

func teamNames(teams []mattermost.Team) []string {
	out := make([]string, 0, len(teams))
	for _, t := range teams {
		out = append(out, t.Name)
	}
	return out
}

// resolveChannel accepts a channel id, "team/channel", "~channel",
// "channel-name", a display name, or "@username" (direct message).
func (tc *toolCtx) resolveChannel(ctx context.Context, token, ref string) (*mattermost.Channel, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "channel 인자가 필요합니다"}
	}
	var ch *mattermost.Channel
	switch {
	case strings.HasPrefix(ref, "@"):
		other, err := tc.resolveRecipient(ctx, ref)
		if err != nil {
			return nil, err
		}
		m, err := tc.ensureMapping(ctx)
		if err != nil {
			return nil, err
		}
		c, err := tc.mm.CreateDirectChannel(ctx, token, m.MMUserID, other.ID)
		if err != nil {
			return nil, mmError(err, "DM 채널")
		}
		ch = c
	case isMMID(ref):
		c, err := tc.mm.Channel(ctx, token, ref)
		if err != nil {
			if mattermost.StatusOf(err) == http.StatusNotFound || mattermost.StatusOf(err) == http.StatusForbidden {
				return nil, &toolError{Code: "CHANNEL_NOT_FOUND", Message: "채널을 찾을 수 없거나 접근 권한이 없습니다: " + ref}
			}
			return nil, mmError(err, "채널")
		}
		ch = c
	case strings.Contains(ref, "/"):
		teamRef, name, _ := strings.Cut(ref, "/")
		team, err := tc.resolveTeam(ctx, token, teamRef)
		if err != nil {
			return nil, err
		}
		c, err := tc.mm.ChannelByName(ctx, token, team.ID, strings.TrimPrefix(name, "~"))
		if err != nil {
			return nil, &toolError{Code: "CHANNEL_NOT_FOUND", Message: "채널을 찾을 수 없거나 접근 권한이 없습니다: " + ref}
		}
		ch = c
	default:
		name := strings.TrimPrefix(ref, "~")
		teams, err := tc.myTeams(ctx, token)
		if err != nil {
			return nil, err
		}
		var matches []mattermost.Channel
		seen := map[string]bool{}
		for _, t := range teams {
			if c, err := tc.mm.ChannelByName(ctx, token, t.ID, strings.ToLower(name)); err == nil && !seen[c.ID] {
				seen[c.ID] = true
				matches = append(matches, *c)
			}
		}
		if len(matches) == 0 {
			// fall back to display-name match among my channels
			for _, t := range teams {
				chs, err := tc.mm.MyChannels(ctx, token, t.ID)
				if err != nil {
					continue
				}
				for _, c := range chs {
					if (c.Type == "O" || c.Type == "P") && strings.EqualFold(c.DisplayName, name) && !seen[c.ID] {
						seen[c.ID] = true
						matches = append(matches, c)
					}
				}
			}
		}
		switch len(matches) {
		case 0:
			return nil, &toolError{Code: "CHANNEL_NOT_FOUND", Message: "참여 중인 팀에서 채널 '" + ref + "'을(를) 찾을 수 없습니다. mattermost_channels로 목록을 확인하세요"}
		case 1:
			ch = &matches[0]
		default:
			cands := []map[string]string{}
			for _, c := range matches {
				cands = append(cands, map[string]string{"channel_id": c.ID, "name": c.Name, "display_name": c.DisplayName, "team_id": c.TeamID})
			}
			return nil, &toolError{Code: "AMBIGUOUS_CHANNEL", Message: "같은 이름의 채널이 여러 팀에 있습니다. channel_id 또는 'team/channel' 형식으로 지정하세요", Details: cands}
		}
	}
	if ch.DeleteAt > 0 {
		return nil, &toolError{Code: "CHANNEL_NOT_FOUND", Message: "보관(삭제)된 채널입니다"}
	}
	if err := tc.checkChannelType(ch); err != nil {
		return nil, err
	}
	tc.details["channel_id"] = ch.ID
	return ch, nil
}

type postFormatter struct {
	tc       *toolCtx
	token    string
	users    map[string]mattermost.User
	channels map[string]*mattermost.Channel
	teams    map[string]string
	anyTeam  string
	filtered int
}

func (tc *toolCtx) newFormatter(ctx context.Context, token string) *postFormatter {
	f := &postFormatter{tc: tc, token: token, users: map[string]mattermost.User{}, channels: map[string]*mattermost.Channel{}, teams: map[string]string{}}
	if teams, err := tc.mm.MyTeams(ctx, token); err == nil {
		for _, t := range teams {
			f.teams[t.ID] = t.Name
			if f.anyTeam == "" {
				f.anyTeam = t.Name
			}
		}
	}
	return f
}

func (f *postFormatter) channel(ctx context.Context, id string) *mattermost.Channel {
	if c, ok := f.channels[id]; ok {
		return c
	}
	c, err := f.tc.mm.Channel(ctx, f.token, id)
	if err != nil {
		c = nil
	}
	f.channels[id] = c
	return c
}

func (f *postFormatter) loadUsers(ctx context.Context, posts []mattermost.Post) {
	var ids []string
	for _, p := range posts {
		if _, ok := f.users[p.UserID]; !ok && p.UserID != "" {
			ids = append(ids, p.UserID)
			f.users[p.UserID] = mattermost.User{ID: p.UserID}
		}
	}
	if len(ids) == 0 {
		return
	}
	if us, err := f.tc.mm.UsersByIDs(ctx, f.token, ids); err == nil {
		for _, u := range us {
			f.users[u.ID] = u
		}
	}
}

// allowedPost applies channel-type policy to an individual result.
func (f *postFormatter) allowedPost(ctx context.Context, p mattermost.Post, dmSearch bool) bool {
	c := f.channel(ctx, p.ChannelID)
	if c == nil {
		f.filtered++
		return false
	}
	m := f.tc.st.MCP
	ok := map[string]bool{"O": m.AllowPublic, "P": m.AllowPrivate, "D": m.AllowDM, "G": m.AllowGroup}[c.Type]
	if (c.Type == "D" || c.Type == "G") && !dmSearch {
		ok = false
	}
	if !ok {
		f.filtered++
	}
	return ok
}

func (f *postFormatter) format(ctx context.Context, posts []mattermost.Post) []map[string]any {
	f.loadUsers(ctx, posts)
	max := f.tc.st.MCP.MaxMessageChars
	site := f.tc.st.Mattermost.SiteURL
	if site == "" {
		site = f.tc.mm.BaseURL()
	}
	out := make([]map[string]any, 0, len(posts))
	for _, p := range posts {
		msg, cut := truncate(p.Message, max)
		u := f.users[p.UserID]
		item := map[string]any{
			"post_id": p.ID, "channel_id": p.ChannelID, "user": u.Username, "user_display_name": u.DisplayName(),
			"created_at": msTime(p.CreateAt), "message": msg,
		}
		if cut {
			item["truncated"] = true
		}
		if p.RootID != "" {
			item["root_id"] = p.RootID
		}
		if p.ReplyCount > 0 {
			item["reply_count"] = p.ReplyCount
		}
		if p.EditAt > 0 {
			item["edited_at"] = msTime(p.EditAt)
		}
		if p.IsPinned {
			item["pinned"] = true
		}
		if len(p.FileIDs) > 0 {
			item["file_ids"] = p.FileIDs
		}
		if p.Type != "" {
			item["system_type"] = p.Type
		}
		if c := f.channel(ctx, p.ChannelID); c != nil {
			item["channel_name"] = c.Name
			item["channel_display_name"] = c.DisplayName
			item["channel_type"] = channelTypeName(c.Type)
		}
		team := f.anyTeam
		if c := f.channels[p.ChannelID]; c != nil && f.teams[c.TeamID] != "" {
			team = f.teams[c.TeamID]
		}
		if team != "" {
			item["permalink"] = strings.TrimRight(site, "/") + "/" + team + "/pl/" + p.ID
		}
		out = append(out, item)
	}
	return out
}

func channelTypeName(t string) string {
	switch t {
	case "O":
		return "public"
	case "P":
		return "private"
	case "D":
		return "direct"
	case "G":
		return "group"
	}
	return t
}

func channelInfo(c *mattermost.Channel, team string) map[string]any {
	out := map[string]any{"channel_id": c.ID, "name": c.Name, "display_name": c.DisplayName, "type": channelTypeName(c.Type),
		"purpose": c.Purpose, "header": c.Header, "last_post_at": msTime(c.LastPostAt), "total_msg_count": c.TotalMsgCount}
	if team != "" {
		out["team"] = team
	}
	return out
}

func userInfo(u *mattermost.User) map[string]any {
	return map[string]any{"user_id": u.ID, "username": u.Username, "display_name": u.DisplayName(), "nickname": u.Nickname,
		"position": u.Position, "email": u.Email, "is_bot": u.IsBot, "active": u.DeleteAt == 0}
}

// ---- registration ----

func (s *Server) registerTools() {
	dmRead := func(m MCPSettings) (bool, string) {
		if !m.AllowDM && !m.AllowGroup {
			return false, "DM/그룹 메시지 접근이 정책으로 꺼져 있습니다"
		}
		return true, ""
	}
	dmSearch := func(m MCPSettings) (bool, string) {
		if !m.AllowDMSearch {
			return false, "DM 검색이 정책으로 꺼져 있습니다 (MCP 정책 > DM 검색 허용)"
		}
		return true, ""
	}
	fileDown := func(m MCPSettings) (bool, string) {
		if !m.AllowFileDownload {
			return false, "파일 다운로드가 정책으로 꺼져 있습니다"
		}
		return true, ""
	}
	def := func(d *toolDef) {
		if d.DefaultRoles == nil {
			d.DefaultRoles = rolesFor(d.Category)
		}
		s.addTool(d)
	}

	def(&toolDef{Name: "mattermost_me", Title: "내 Mattermost 정보", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "현재 로그인한 사용자에게 매핑된 Mattermost 계정 정보(사용자 ID, username, 이름, 직책)를 조회합니다.",
		Handler:     toolMe})
	def(&toolDef{Name: "mattermost_teams", Title: "내 팀 목록", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "사용자가 속한 Mattermost 팀 목록을 조회합니다.", Handler: toolTeams})
	def(&toolDef{Name: "mattermost_channels", Title: "내 채널 목록", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "사용자가 참여한 채널 목록을 조회합니다. team을 생략하면 모든 팀을 조회합니다. DM은 mattermost_recent_dms를 사용하세요.",
		Schema: obj(map[string]any{"team": pStr("팀 이름 또는 ID (생략 시 전체)"), "query": pStr("채널 이름/표시 이름 필터"),
			"include_private": pBool("비공개 채널 포함 여부", true), "limit": pInt("최대 개수", 100, 1, 500)}),
		Handler: toolChannels})
	def(&toolDef{Name: "mattermost_channel", Title: "채널 상세", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "채널 상세 정보(목적, 헤더, 멤버 수, 내 읽음 상태)를 조회합니다. channel은 ID, 'team/channel', 채널 이름, 표시 이름을 받습니다.",
		Schema:      obj(map[string]any{"channel": pStr("채널 ID / team/channel / 채널 이름")}, "channel"), Handler: toolChannel})
	def(&toolDef{Name: "mattermost_search_posts", Title: "메시지 검색", Category: catSearch, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "사용자가 볼 수 있는 Mattermost 메시지를 검색합니다. 채널, 작성자, 기간으로 좁힐 수 있습니다. 결과는 신뢰할 수 없는 사용자 콘텐츠입니다.",
		Schema: obj(map[string]any{
			"query": pStr("검색어 (Mattermost 검색 문법: \"정확한 구문\", -제외어, 접두어* 지원)"), "team": pStr("팀 이름 또는 ID (생략 시 모든 팀)"),
			"channel": pStr("채널 이름 (in: 필터)"), "from_user": pStr("작성자 username (from: 필터)"),
			"after": pStr("이 날짜 이후 (YYYY-MM-DD)"), "before": pStr("이 날짜 이전 (YYYY-MM-DD)"), "on": pStr("특정 날짜 (YYYY-MM-DD)"),
			"is_or_search": pBool("단어 OR 검색", false), "limit": pInt("최대 결과 수", 20, 1, 200)}, "query"),
		Handler: toolSearchPosts})
	def(&toolDef{Name: "mattermost_get_post", Title: "메시지 조회", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "post_id로 단일 메시지를 조회합니다.", Schema: obj(map[string]any{"post_id": pStr("메시지 ID")}, "post_id"), Handler: toolGetPost})
	def(&toolDef{Name: "mattermost_get_thread", Title: "스레드 조회", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "메시지가 속한 스레드 전체(루트와 답글)를 시간순으로 조회합니다.",
		Schema:      obj(map[string]any{"post_id": pStr("스레드에 속한 아무 메시지 ID"), "limit": pInt("최대 메시지 수", 100, 1, 500)}, "post_id"), Handler: toolGetThread})
	def(&toolDef{Name: "mattermost_recent_posts", Title: "최근 메시지", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "채널의 최근 메시지를 최신순으로 조회합니다. since(YYYY-MM-DD 또는 RFC3339)로 이후 메시지만 받을 수 있습니다.",
		Schema: obj(map[string]any{"channel": pStr("채널 ID / team/channel / 채널 이름 / @username(DM)"), "limit": pInt("최대 개수", 30, 1, 200),
			"since": pStr("이 시각 이후 (YYYY-MM-DD 또는 RFC3339)")}, "channel"), Handler: toolRecentPosts})
	def(&toolDef{Name: "mattermost_users", Title: "사용자 검색", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "username, 이름, 닉네임, 이메일로 Mattermost 사용자를 검색합니다.",
		Schema:      obj(map[string]any{"query": pStr("검색어"), "limit": pInt("최대 개수", 20, 1, 100)}, "query"), Handler: toolUsers})
	def(&toolDef{Name: "mattermost_unread", Title: "읽지 않은 메시지", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "읽지 않은 메시지와 멘션이 있는 채널을 요약합니다. include_posts=true이면 채널별 최근 미확인 메시지도 함께 가져옵니다.",
		Schema: obj(map[string]any{"team": pStr("팀 이름 또는 ID (생략 시 전체)"), "include_posts": pBool("미확인 메시지 본문 포함", false),
			"posts_per_channel": pInt("채널당 메시지 수", 5, 1, 30)}), Handler: toolUnread})
	def(&toolDef{Name: "mattermost_pinned_posts", Title: "고정 메시지", Category: catRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true,
		Description: "채널에 고정(pin)된 메시지를 조회합니다.", Schema: obj(map[string]any{"channel": pStr("채널 ID / team/channel / 채널 이름")}, "channel"), Handler: toolPinned})
	def(&toolDef{Name: "mattermost_get_file", Title: "첨부 파일 조회", Category: catFile, ReadOnly: true, Idempotent: true, DefaultEnabled: true, available: fileDown,
		Description: "첨부 파일 메타데이터와 (텍스트 파일인 경우) 내용을 가져옵니다.", Schema: obj(map[string]any{"file_id": pStr("파일 ID")}, "file_id"), Handler: toolGetFile})

	// writes
	def(&toolDef{Name: "mattermost_post_message", Title: "채널에 메시지 작성", Category: catWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm",
		Description: "채널에 메시지를 게시합니다. root_id를 주면 해당 스레드에 답글로 게시합니다.",
		Schema: obj(map[string]any{"channel": pStr("채널 ID / team/channel / 채널 이름"), "message": pStr("메시지 (Markdown)"),
			"root_id": pStr("답글을 달 스레드 루트 ID (선택)")}, "channel", "message"), Handler: toolPostMessage})
	def(&toolDef{Name: "mattermost_reply", Title: "스레드 답글", Category: catWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm",
		Description: "기존 메시지의 스레드에 답글을 작성합니다.",
		Schema:      obj(map[string]any{"post_id": pStr("답글을 달 메시지 ID"), "message": pStr("메시지 (Markdown)")}, "post_id", "message"), Handler: toolReply})
	def(&toolDef{Name: "mattermost_update_post", Title: "메시지 수정", Category: catWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm", Idempotent: true,
		Description: "본인이 작성한 메시지 내용을 수정합니다.",
		Schema:      obj(map[string]any{"post_id": pStr("수정할 메시지 ID"), "message": pStr("새 메시지")}, "post_id", "message"), Handler: toolUpdatePost})
	def(&toolDef{Name: "mattermost_add_reaction", Title: "이모지 반응", Category: catWrite, DefaultEnabled: true, DefaultMode: "auto", Idempotent: true,
		Description: "메시지에 이모지 반응을 추가합니다 (예: +1, white_check_mark, eyes).",
		Schema:      obj(map[string]any{"post_id": pStr("메시지 ID"), "emoji": pStr("이모지 이름 (콜론 제외)")}, "post_id", "emoji"), Handler: toolAddReaction})
	def(&toolDef{Name: "mattermost_upload_file", Title: "파일 첨부 게시", Category: catFile, Sends: true, DefaultEnabled: true, DefaultMode: "confirm",
		Description: "텍스트(또는 base64) 내용을 파일로 만들어 채널에 첨부해 게시합니다.",
		Schema: obj(map[string]any{"channel": pStr("채널 ID / team/channel / 채널 이름"), "filename": pStr("파일 이름 (예: report.md)"),
			"content": pStr("파일 내용 (텍스트)"), "content_base64": pStr("파일 내용 (base64, 바이너리용)"), "message": pStr("함께 보낼 메시지 (선택)")}, "channel", "filename"),
		Handler: toolUploadFile})
	def(&toolDef{Name: "mattermost_create_channel", Title: "채널 생성", Category: catChannel, DefaultEnabled: true, DefaultMode: "confirm",
		Description: "팀에 새 공개/비공개 채널을 만듭니다.",
		Schema: obj(map[string]any{"team": pStr("팀 이름 또는 ID"), "name": pStr("채널 URL 이름 (영문 소문자, 숫자, -)"), "display_name": pStr("표시 이름"),
			"type": map[string]any{"type": "string", "enum": []string{"O", "P"}, "description": "O=공개, P=비공개", "default": "O"}, "purpose": pStr("채널 목적")}, "team", "name", "display_name"),
		Handler: toolCreateChannel})
	def(&toolDef{Name: "mattermost_add_member", Title: "채널 멤버 추가", Category: catChannel, DefaultEnabled: true, DefaultMode: "confirm", Idempotent: true,
		Description: "채널에 사용자를 추가합니다.",
		Schema:      obj(map[string]any{"channel": pStr("채널"), "user": pStr("추가할 사용자 (username, 이메일, 이름)")}, "channel", "user"), Handler: toolAddMember})
	def(&toolDef{Name: "mattermost_delete_post", Title: "메시지 삭제", Category: catDanger, Destructive: true, DefaultEnabled: false, DefaultMode: "confirm",
		Description: "메시지를 삭제합니다. 기본 비활성화된 위험 도구입니다.", Schema: obj(map[string]any{"post_id": pStr("삭제할 메시지 ID")}, "post_id"), Handler: toolDeletePost})
	def(&toolDef{Name: "mattermost_archive_channel", Title: "채널 보관", Category: catDanger, Destructive: true, DefaultEnabled: false, DefaultMode: "confirm",
		Description: "채널을 보관(아카이브)합니다. 기본 비활성화된 위험 도구입니다.", Schema: obj(map[string]any{"channel": pStr("채널")}, "channel"), Handler: toolArchiveChannel})

	// DM family
	def(&toolDef{Name: "mattermost_send_dm", Title: "DM 보내기", Category: catDMWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm", available: dmRead,
		Description: "사용자에게 1:1 DM을 보냅니다. recipient는 username, 이메일, 한글 이름을 받으며 동명이인이면 후보 목록을 반환하니 사용자에게 고르게 하세요.",
		Schema:      obj(map[string]any{"recipient": pStr("받는 사람 (username / 이메일 / 이름)"), "message": pStr("메시지 (Markdown)")}, "recipient", "message"), Handler: toolSendDM})
	def(&toolDef{Name: "mattermost_send_group_dm", Title: "그룹 DM 보내기", Category: catDMWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm", available: dmRead,
		Description: "여러 사용자(2~7명)에게 그룹 메시지를 보냅니다.",
		Schema:      obj(map[string]any{"recipients": pStrs("받는 사람 목록 (username / 이메일 / 이름)", 2, 7), "message": pStr("메시지 (Markdown)")}, "recipients", "message"), Handler: toolSendGroupDM})
	def(&toolDef{Name: "mattermost_reply_dm", Title: "DM 답장", Category: catDMWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm", available: dmRead,
		Description: "DM/그룹 메시지의 특정 메시지에 스레드 답장을 보냅니다.",
		Schema:      obj(map[string]any{"post_id": pStr("답장할 DM 메시지 ID"), "message": pStr("메시지")}, "post_id", "message"), Handler: toolReplyDM})
	def(&toolDef{Name: "mattermost_upload_dm_file", Title: "파일 포함 DM", Category: catDMWrite, Sends: true, DefaultEnabled: true, DefaultMode: "confirm", available: dmRead,
		DefaultRoles: []string{RoleMCPWriter, RoleMCPFile},
		Description:  "파일을 첨부해 1:1 DM을 보냅니다.",
		Schema: obj(map[string]any{"recipient": pStr("받는 사람"), "filename": pStr("파일 이름"), "content": pStr("파일 내용 (텍스트)"),
			"content_base64": pStr("파일 내용 (base64)"), "message": pStr("함께 보낼 메시지 (선택)")}, "recipient", "filename"), Handler: toolUploadDMFile})
	def(&toolDef{Name: "mattermost_read_dm", Title: "DM 대화 조회", Category: catDMRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true, available: dmRead,
		Description: "특정 사용자와의 1:1 DM 최근 대화를 조회합니다 (항상 본인 자격증명으로 조회).",
		Schema: obj(map[string]any{"user": pStr("상대방 (username / 이메일 / 이름) — channel과 둘 중 하나"), "channel": pStr("DM/그룹 채널 ID"),
			"limit": pInt("최대 메시지 수", 30, 1, 200), "since": pStr("이 시각 이후 (YYYY-MM-DD 또는 RFC3339)")}), Handler: toolReadDM})
	def(&toolDef{Name: "mattermost_recent_dms", Title: "최근 DM 목록", Category: catDMRead, ReadOnly: true, Idempotent: true, DefaultEnabled: true, available: dmRead,
		Description: "최근 활동 순으로 DM/그룹 메시지 대화 목록과 미확인 건수를 조회합니다.",
		Schema:      obj(map[string]any{"limit": pInt("최대 대화 수", 20, 1, 100), "unread_only": pBool("미확인 대화만", false)}), Handler: toolRecentDMs})
	def(&toolDef{Name: "mattermost_search_dm", Title: "DM 검색", Category: catDMSearch, ReadOnly: true, Idempotent: true, DefaultEnabled: true, available: dmSearch,
		Description: "본인의 DM/그룹 메시지에서 검색합니다. user를 주면 해당 사용자와의 대화로 한정합니다.",
		Schema: obj(map[string]any{"query": pStr("검색어"), "user": pStr("상대방으로 한정 (선택)"), "after": pStr("YYYY-MM-DD"), "before": pStr("YYYY-MM-DD"),
			"limit": pInt("최대 결과 수", 20, 1, 100)}, "query"), Handler: toolSearchDM})

	// Aliases matching the official Mattermost MCP tool names, for easier
	// migration or side-by-side use. Off by default (MCP 정책 > 호환 별칭).
	alias := func(name, target, title string) {
		t := s.tools[target]
		a := *t
		a.Name, a.AliasOf, a.Title = name, target, title
		a.Description = t.Description + " (Mattermost 공식 MCP 호환 이름, " + target + "와 동일)"
		s.addTool(&a)
	}
	alias("dm", "mattermost_send_dm", "DM 보내기 (호환)")
	alias("group_message", "mattermost_send_group_dm", "그룹 메시지 (호환)")
	alias("create_post", "mattermost_post_message", "메시지 작성 (호환)")
	alias("search_users", "mattermost_users", "사용자 검색 (호환)")
}

// ---- read handlers ----

func toolMe(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	me, err := tc.mm.Me(ctx, token)
	if err != nil {
		return nil, mmError(err, "내 정보")
	}
	info := userInfo(me)
	info["mmcp_username"] = tc.p.User.Username
	info["mmcp_roles"] = tc.p.User.Roles
	return info, nil
}

func toolTeams(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	teams, err := tc.myTeams(ctx, token)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, t := range teams {
		out = append(out, map[string]any{"team_id": t.ID, "name": t.Name, "display_name": t.DisplayName, "description": t.Description, "type": map[string]string{"O": "open", "I": "invite"}[t.Type]})
	}
	return map[string]any{"teams": out, "count": len(out)}, nil
}

func toolChannels(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	var teams []mattermost.Team
	if ref := a.str("team"); ref != "" {
		t, err := tc.resolveTeam(ctx, token, ref)
		if err != nil {
			return nil, err
		}
		teams = []mattermost.Team{*t}
	} else if teams, err = tc.myTeams(ctx, token); err != nil {
		return nil, err
	}
	q := strings.ToLower(a.str("query"))
	priv := a.boolv("include_private", true)
	limit := a.intv("limit", 100, 1, 500)
	out := []map[string]any{}
	for _, t := range teams {
		chs, err := tc.mm.MyChannels(ctx, token, t.ID)
		if err != nil {
			return nil, mmError(err, "채널 목록")
		}
		for i := range chs {
			c := &chs[i]
			if c.Type != "O" && c.Type != "P" || c.DeleteAt > 0 || (c.Type == "P" && !priv) || tc.checkChannelType(c) != nil {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(c.Name), q) && !strings.Contains(strings.ToLower(c.DisplayName), q) {
				continue
			}
			out = append(out, channelInfo(c, t.Name))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["last_post_at"].(string) > out[j]["last_post_at"].(string) })
	total := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return map[string]any{"channels": out, "count": len(out), "total": total}, nil
}

func toolChannel(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := tc.resolveChannel(ctx, token, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	info := channelInfo(ch, "")
	if ch.TeamID != "" {
		if teams, err := tc.myTeams(ctx, token); err == nil {
			for _, t := range teams {
				if t.ID == ch.TeamID {
					info["team"] = t.Name
				}
			}
		}
	}
	if n, err := tc.mm.ChannelStats(ctx, token, ch.ID); err == nil {
		info["member_count"] = n
	}
	if m, err := tc.ensureMapping(ctx); err == nil {
		if mem, err := tc.mm.ChannelMember(ctx, token, ch.ID, m.MMUserID); err == nil {
			info["my_unread"] = max(ch.TotalMsgCount-mem.MsgCount, 0)
			info["my_mentions"] = mem.MentionCount
			info["my_last_viewed_at"] = msTime(mem.LastViewedAt)
			info["my_roles"] = mem.Roles
		}
	}
	return info, nil
}

func validDate(name, v string) error {
	if v != "" && !datePattern.MatchString(v) {
		return &toolError{Code: "INVALID_ARGUMENT", Message: name + "는 YYYY-MM-DD 형식이어야 합니다"}
	}
	return nil
}

func searchTerms(a args) (string, error) {
	q, err := a.required("query", "terms", "q")
	if err != nil {
		return "", err
	}
	terms := []string{q}
	for _, k := range []string{"after", "before", "on"} {
		v := a.str(k)
		if err := validDate(k, v); err != nil {
			return "", err
		}
		if v != "" {
			terms = append(terms, k+":"+v)
		}
	}
	if c := strings.TrimPrefix(a.str("channel", "in"), "~"); c != "" {
		if strings.ContainsAny(c, " \"") {
			return "", &toolError{Code: "INVALID_ARGUMENT", Message: "channel 필터에는 채널 URL 이름(공백 없음)을 사용하세요"}
		}
		terms = append(terms, "in:"+c)
	}
	if u := strings.TrimPrefix(a.str("from_user", "from"), "@"); u != "" {
		if strings.ContainsAny(u, " \"") {
			return "", &toolError{Code: "INVALID_ARGUMENT", Message: "from_user에는 username을 사용하세요"}
		}
		terms = append(terms, "from:"+u)
	}
	return strings.Join(terms, " "), nil
}

func (tc *toolCtx) search(ctx context.Context, token, teamRef, terms string, isOr bool, limit int, dm bool) ([]mattermost.Post, *postFormatter, error) {
	var teams []mattermost.Team
	var err error
	if teamRef != "" {
		t, err := tc.resolveTeam(ctx, token, teamRef)
		if err != nil {
			return nil, nil, err
		}
		teams = []mattermost.Team{*t}
	} else if teams, err = tc.myTeams(ctx, token); err != nil {
		return nil, nil, err
	}
	if len(teams) > 10 {
		teams = teams[:10]
	}
	seen := map[string]bool{}
	var posts []mattermost.Post
	for _, t := range teams {
		list, err := tc.mm.SearchPosts(ctx, token, t.ID, terms, isOr, limit)
		if err != nil {
			return nil, nil, mmError(err, "메시지 검색")
		}
		for _, p := range list.Ordered() {
			if !seen[p.ID] && p.DeleteAt == 0 {
				seen[p.ID] = true
				posts = append(posts, p)
			}
		}
		if dm {
			break // DMs are not team scoped; one team search covers them
		}
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].CreateAt > posts[j].CreateAt })
	f := tc.newFormatter(ctx, token)
	kept := posts[:0]
	for _, p := range posts {
		if dm {
			c := f.channel(ctx, p.ChannelID)
			if c == nil || (c.Type != "D" && c.Type != "G") {
				continue
			}
		}
		if f.allowedPost(ctx, p, tc.st.MCP.AllowDMSearch) {
			kept = append(kept, p)
		}
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept, f, nil
}

func toolSearchPosts(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	terms, err := searchTerms(a)
	if err != nil {
		return nil, err
	}
	limit := min(a.intv("limit", 20, 1, 200), tc.st.MCP.MaxResults)
	posts, f, err := tc.search(ctx, token, a.str("team"), terms, a.boolv("is_or_search", false), limit, false)
	if err != nil {
		return nil, err
	}
	tc.details["query_hash"] = hashQuery(terms)
	tc.details["result_count"] = len(posts)
	if tc.st.MCP.AuditMessageBody {
		tc.details["query"] = terms
	}
	res := map[string]any{"query": terms, "results": f.format(ctx, posts), "count": len(posts)}
	if f.filtered > 0 {
		res["filtered_by_policy"] = f.filtered
	}
	return res, nil
}

func (tc *toolCtx) postAllowed(ctx context.Context, token string, p *mattermost.Post) error {
	ch, err := tc.mm.Channel(ctx, token, p.ChannelID)
	if err != nil {
		return mmError(err, "채널")
	}
	tc.details["channel_id"] = ch.ID
	return tc.checkChannelType(ch)
}

func toolGetPost(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	p, err := tc.mm.Post(ctx, token, id)
	if err != nil {
		return nil, mmError(err, "메시지")
	}
	if err := tc.postAllowed(ctx, token, p); err != nil {
		return nil, err
	}
	return tc.newFormatter(ctx, token).format(ctx, []mattermost.Post{*p})[0], nil
}

func toolGetThread(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	list, err := tc.mm.Thread(ctx, token, id)
	if err != nil {
		return nil, mmError(err, "스레드")
	}
	posts := list.Ordered()
	if len(posts) == 0 {
		return nil, &toolError{Code: "NOT_FOUND", Message: "스레드를 찾을 수 없습니다"}
	}
	if err := tc.postAllowed(ctx, token, &posts[0]); err != nil {
		return nil, err
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].CreateAt < posts[j].CreateAt })
	limit := a.intv("limit", 100, 1, 500)
	truncated := len(posts) > limit
	if truncated {
		posts = posts[len(posts)-limit:]
	}
	res := map[string]any{"posts": tc.newFormatter(ctx, token).format(ctx, posts), "count": len(posts)}
	if truncated {
		res["note"] = "오래된 메시지 일부가 생략되었습니다"
	}
	return res, nil
}

func parseSince(v string) (int64, error) {
	if v == "" {
		return 0, nil
	}
	if datePattern.MatchString(v) {
		t, err := time.ParseInLocation("2006-01-02", v, time.Local)
		return t.UnixMilli(), err
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return 0, &toolError{Code: "INVALID_ARGUMENT", Message: "since는 YYYY-MM-DD 또는 RFC3339 형식이어야 합니다"}
	}
	return t.UnixMilli(), nil
}

func toolRecentPosts(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := tc.resolveChannel(ctx, token, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	since, err := parseSince(a.str("since"))
	if err != nil {
		return nil, err
	}
	limit := min(a.intv("limit", 30, 1, 200), tc.st.MCP.MaxResults)
	return tc.channelPosts(ctx, token, ch, limit, since)
}

func (tc *toolCtx) channelPosts(ctx context.Context, token string, ch *mattermost.Channel, limit int, since int64) (any, error) {
	list, err := tc.mm.ChannelPosts(ctx, token, ch.ID, limit, since)
	if err != nil {
		return nil, mmError(err, "채널 메시지")
	}
	posts := list.Ordered()
	if since > 0 {
		sort.Slice(posts, func(i, j int) bool { return posts[i].CreateAt > posts[j].CreateAt })
	}
	kept := posts[:0]
	for _, p := range posts {
		if p.DeleteAt == 0 {
			kept = append(kept, p)
		}
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	f := tc.newFormatter(ctx, token)
	f.channels[ch.ID] = ch
	tc.details["result_count"] = len(kept)
	return map[string]any{"channel": channelInfo(ch, ""), "posts": f.format(ctx, kept), "count": len(kept), "order": "newest_first"}, nil
}

func toolUsers(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.lookupToken(ctx)
	if err != nil {
		return nil, err
	}
	q, err := a.required("query", "term")
	if err != nil {
		return nil, err
	}
	us, err := tc.mm.SearchUsers(ctx, token, q, a.intv("limit", 20, 1, 100))
	if err != nil {
		return nil, mmError(err, "사용자 검색")
	}
	out := []map[string]any{}
	for i := range us {
		if us[i].DeleteAt == 0 {
			out = append(out, userInfo(&us[i]))
		}
	}
	return map[string]any{"users": out, "count": len(out)}, nil
}

func toolUnread(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	var teams []mattermost.Team
	if ref := a.str("team"); ref != "" {
		t, err := tc.resolveTeam(ctx, token, ref)
		if err != nil {
			return nil, err
		}
		teams = []mattermost.Team{*t}
	} else if teams, err = tc.myTeams(ctx, token); err != nil {
		return nil, err
	}
	type row struct {
		ch       mattermost.Channel
		team     string
		unread   int64
		mentions int64
		viewed   int64
	}
	var rows []row
	seen := map[string]bool{}
	for _, t := range teams {
		chs, err := tc.mm.MyChannels(ctx, token, t.ID)
		if err != nil {
			return nil, mmError(err, "채널 목록")
		}
		mems, err := tc.mm.MyChannelMembers(ctx, token, t.ID)
		if err != nil {
			return nil, mmError(err, "채널 멤버십")
		}
		byID := map[string]mattermost.ChannelMember{}
		for _, m := range mems {
			byID[m.ChannelID] = m
		}
		for _, c := range chs {
			if seen[c.ID] || c.DeleteAt > 0 || tc.checkChannelType(&c) != nil {
				continue
			}
			seen[c.ID] = true
			m, ok := byID[c.ID]
			if !ok {
				continue
			}
			unread := c.TotalMsgCount - m.MsgCount
			if unread <= 0 && m.MentionCount == 0 {
				continue
			}
			team := t.Name
			if c.Type == "D" || c.Type == "G" {
				team = ""
			}
			rows = append(rows, row{c, team, max(unread, 0), m.MentionCount, m.LastViewedAt})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].mentions != rows[j].mentions {
			return rows[i].mentions > rows[j].mentions
		}
		return rows[i].ch.LastPostAt > rows[j].ch.LastPostAt
	})
	include := a.boolv("include_posts", false)
	per := a.intv("posts_per_channel", 5, 1, 30)
	f := tc.newFormatter(ctx, token)
	out := []map[string]any{}
	var totalUnread, totalMentions int64
	for i, r := range rows {
		totalUnread += r.unread
		totalMentions += r.mentions
		item := channelInfo(&r.ch, r.team)
		item["unread"] = r.unread
		item["mentions"] = r.mentions
		if r.ch.Type == "D" {
			item["display_name"] = tc.dmPartnerName(ctx, f, &r.ch)
		}
		if include && i < 15 {
			if list, err := tc.mm.ChannelPosts(ctx, token, r.ch.ID, per, r.viewed); err == nil {
				ps := list.Ordered()
				if len(ps) > per {
					ps = ps[:per]
				}
				f.channels[r.ch.ID] = &rows[i].ch
				item["posts"] = f.format(ctx, ps)
			}
		}
		out = append(out, item)
	}
	return map[string]any{"channels": out, "channel_count": len(out), "total_unread": totalUnread, "total_mentions": totalMentions}, nil
}

func toolPinned(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := tc.resolveChannel(ctx, token, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	list, err := tc.mm.PinnedPosts(ctx, token, ch.ID)
	if err != nil {
		return nil, mmError(err, "고정 메시지")
	}
	f := tc.newFormatter(ctx, token)
	f.channels[ch.ID] = ch
	posts := list.Ordered()
	return map[string]any{"channel": channelInfo(ch, ""), "posts": f.format(ctx, posts), "count": len(posts)}, nil
}

var textMime = regexp.MustCompile(`^(text/|application/(json|xml|yaml|x-yaml|javascript|x-sh|sql|csv))`)

func toolGetFile(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	id, err := a.required("file_id")
	if err != nil {
		return nil, err
	}
	info, err := tc.mm.FileInfo(ctx, token, id)
	if err != nil {
		return nil, mmError(err, "파일")
	}
	if info.ChannelID != "" {
		ch, err := tc.mm.Channel(ctx, token, info.ChannelID)
		if err != nil {
			return nil, mmError(err, "채널")
		}
		if err := tc.checkChannelType(ch); err != nil {
			return nil, err
		}
		tc.details["channel_id"] = ch.ID
	}
	out := map[string]any{"file_id": info.ID, "name": info.Name, "size": info.Size, "mime_type": info.MimeType, "post_id": info.PostID, "created_at": msTime(info.CreateAt)}
	limit := int64(tc.st.MCP.MaxUploadKB) * 1024
	if textMime.MatchString(info.MimeType) || strings.Contains(".md.txt.csv.log.json.yaml.yml.sql.xml", "."+strings.ToLower(info.Extension)) && info.Extension != "" {
		if info.Size > limit {
			out["note"] = fmt.Sprintf("파일이 %dKB 제한보다 커서 내용을 생략했습니다", tc.st.MCP.MaxUploadKB)
		} else if data, err := tc.mm.FileContent(ctx, token, id); err == nil {
			text, cut := truncate(string(data), tc.st.MCP.MaxMessageChars*4)
			out["content"] = text
			out["truncated"] = cut
		}
	} else {
		out["note"] = "텍스트가 아닌 파일은 메타데이터만 제공합니다"
	}
	return out, nil
}

// ---- write handlers ----

func (tc *toolCtx) messageArg(a args) (string, error) {
	msg := strings.TrimSpace(a.raw("message"))
	if msg == "" {
		return "", &toolError{Code: "INVALID_ARGUMENT", Message: "message 인자가 필요합니다"}
	}
	if utf8.RuneCountInString(msg) > 16383 {
		return "", &toolError{Code: "INVALID_ARGUMENT", Message: "메시지가 너무 깁니다 (최대 16383자)"}
	}
	return msg, nil
}

func (tc *toolCtx) noteMessage(msg string) {
	tc.details["message_chars"] = utf8.RuneCountInString(msg)
	if tc.st.MCP.AuditMessageBody {
		tc.details["message"] = msg
	}
}

func (tc *toolCtx) createPost(ctx context.Context, token, mode string, p mattermost.Post) (*mattermost.Post, error) {
	p.Message, p.Props = tc.decorate(ctx, mode, p.Message)
	out, err := tc.mm.CreatePost(ctx, token, p)
	if err != nil {
		e := mmError(err, "메시지 게시")
		if te := asToolError(e); te.Code == "PERMISSION_DENIED" && mode == "bot" {
			te.Message = "봇 계정이 이 채널에 게시할 권한이 없습니다. 봇을 채널에 추가하거나 post:all/post:channels 권한을 부여하세요"
		}
		return nil, e
	}
	tc.details["post_id"] = out.ID
	return out, nil
}

func (tc *toolCtx) postedResult(ctx context.Context, p *mattermost.Post, ch *mattermost.Channel, extra map[string]any) map[string]any {
	res := map[string]any{"success": true, "post_id": p.ID, "channel_id": p.ChannelID, "created_at": msTime(p.CreateAt), "sent_as": tc.sendMode}
	if ch != nil {
		res["channel"] = ch.DisplayName
		if ch.Type == "O" || ch.Type == "P" {
			res["channel_name"] = ch.Name
		}
	}
	if tc.approval != "" {
		res["approval_id"] = tc.approval
	}
	for k, v := range extra {
		res[k] = v
	}
	return res
}

func toolPostMessage(ctx context.Context, tc *toolCtx, a args) (any, error) {
	msg, err := tc.messageArg(a)
	if err != nil {
		return nil, err
	}
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	// resolve with the reader's view when possible so channel names work
	resolveTok := token
	if mode == "bot" {
		if t, err := tc.lookupToken(ctx); err == nil {
			resolveTok = t
		}
	}
	ch, err := tc.resolveChannel(ctx, resolveTok, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	root := a.str("root_id")
	if root != "" && !isMMID(root) {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "root_id 형식이 올바르지 않습니다"}
	}
	tc.noteMessage(msg)
	if err := tc.approve(ctx, a, map[string]any{"action": "채널 메시지 게시", "channel": ch.DisplayName, "channel_name": ch.Name, "message": msg, "thread_root_id": root, "send_as": mode}); err != nil {
		return nil, err
	}
	p, err := tc.createPost(ctx, token, mode, mattermost.Post{ChannelID: ch.ID, Message: msg, RootID: root})
	if err != nil {
		return nil, err
	}
	return tc.postedResult(ctx, p, ch, nil), nil
}

func toolReply(ctx context.Context, tc *toolCtx, a args) (any, error) {
	msg, err := tc.messageArg(a)
	if err != nil {
		return nil, err
	}
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	readTok := token
	if mode == "bot" {
		if t, err := tc.lookupToken(ctx); err == nil {
			readTok = t
		}
	}
	parent, err := tc.mm.Post(ctx, readTok, id)
	if err != nil {
		return nil, mmError(err, "원본 메시지")
	}
	ch, err := tc.mm.Channel(ctx, readTok, parent.ChannelID)
	if err != nil {
		return nil, mmError(err, "채널")
	}
	if err := tc.checkChannelType(ch); err != nil {
		return nil, err
	}
	tc.details["channel_id"] = ch.ID
	root := parent.RootID
	if root == "" {
		root = parent.ID
	}
	tc.noteMessage(msg)
	orig, _ := truncate(parent.Message, 200)
	if err := tc.approve(ctx, a, map[string]any{"action": "스레드 답글", "channel": ch.DisplayName, "reply_to": orig, "message": msg, "send_as": mode}); err != nil {
		return nil, err
	}
	p, err := tc.createPost(ctx, token, mode, mattermost.Post{ChannelID: ch.ID, Message: msg, RootID: root})
	if err != nil {
		return nil, err
	}
	return tc.postedResult(ctx, p, ch, map[string]any{"root_id": root}), nil
}

func toolUpdatePost(ctx context.Context, tc *toolCtx, a args) (any, error) {
	msg, err := tc.messageArg(a)
	if err != nil {
		return nil, err
	}
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	token, err := tc.userToken(ctx) // editing is always done as the author
	if err != nil {
		return nil, err
	}
	tc.sendMode = "user"
	p, err := tc.mm.Post(ctx, token, id)
	if err != nil {
		return nil, mmError(err, "메시지")
	}
	if err := tc.postAllowed(ctx, token, p); err != nil {
		return nil, err
	}
	if m, err := tc.ensureMapping(ctx); err == nil && p.UserID != m.MMUserID {
		return nil, &toolError{Code: "PERMISSION_DENIED", Message: "본인이 작성한 메시지만 수정할 수 있습니다"}
	}
	tc.noteMessage(msg)
	before, _ := truncate(p.Message, 300)
	if err := tc.approve(ctx, a, map[string]any{"action": "메시지 수정", "before": before, "after": msg}); err != nil {
		return nil, err
	}
	out, err := tc.mm.PatchPost(ctx, token, id, msg)
	if err != nil {
		return nil, mmError(err, "메시지 수정")
	}
	tc.details["post_id"] = out.ID
	return map[string]any{"success": true, "post_id": out.ID, "edited_at": msTime(out.EditAt)}, nil
}

func toolAddReaction(ctx context.Context, tc *toolCtx, a args) (any, error) {
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	emoji := strings.Trim(a.str("emoji", "emoji_name"), ": ")
	if emoji == "" || len(emoji) > 64 {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "emoji 인자가 필요합니다"}
	}
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	tc.sendMode = "user"
	p, err := tc.mm.Post(ctx, token, id)
	if err != nil {
		return nil, mmError(err, "메시지")
	}
	if err := tc.postAllowed(ctx, token, p); err != nil {
		return nil, err
	}
	m, err := tc.ensureMapping(ctx)
	if err != nil {
		return nil, err
	}
	if err := tc.approve(ctx, a, map[string]any{"action": "이모지 반응", "emoji": emoji}); err != nil {
		return nil, err
	}
	if err := tc.mm.AddReaction(ctx, token, m.MMUserID, id, emoji); err != nil {
		return nil, mmError(err, "반응 추가")
	}
	tc.details["post_id"] = id
	return map[string]any{"success": true, "post_id": id, "emoji": emoji}, nil
}

func (tc *toolCtx) fileArg(a args) (string, []byte, error) {
	name := strings.TrimSpace(a.str("filename", "file_name"))
	if name == "" || len(name) > 200 || strings.ContainsAny(name, "/\\\x00") {
		return "", nil, &toolError{Code: "INVALID_ARGUMENT", Message: "filename이 올바르지 않습니다"}
	}
	var data []byte
	if b64 := a.raw("content_base64"); b64 != "" {
		d, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return "", nil, &toolError{Code: "INVALID_ARGUMENT", Message: "content_base64를 해석할 수 없습니다"}
		}
		data = d
	} else {
		data = []byte(a.raw("content"))
	}
	if len(data) == 0 {
		return "", nil, &toolError{Code: "INVALID_ARGUMENT", Message: "content 또는 content_base64가 필요합니다"}
	}
	if len(data) > tc.st.MCP.MaxUploadKB*1024 {
		return "", nil, &toolError{Code: "INVALID_ARGUMENT", Message: fmt.Sprintf("파일이 너무 큽니다 (최대 %dKB)", tc.st.MCP.MaxUploadKB)}
	}
	tc.details["file_name"] = name
	tc.details["file_bytes"] = len(data)
	return name, data, nil
}

func (tc *toolCtx) uploadAndPost(ctx context.Context, token, mode string, ch *mattermost.Channel, name string, data []byte, msg string) (*mattermost.Post, error) {
	fi, err := tc.mm.UploadFile(ctx, token, ch.ID, name, data)
	if err != nil {
		return nil, mmError(err, "파일 업로드")
	}
	return tc.createPost(ctx, token, mode, mattermost.Post{ChannelID: ch.ID, Message: msg, FileIDs: []string{fi.ID}})
}

func toolUploadFile(ctx context.Context, tc *toolCtx, a args) (any, error) {
	name, data, err := tc.fileArg(a)
	if err != nil {
		return nil, err
	}
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	resolveTok := token
	if mode == "bot" {
		if t, err := tc.lookupToken(ctx); err == nil {
			resolveTok = t
		}
	}
	ch, err := tc.resolveChannel(ctx, resolveTok, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	msg := strings.TrimSpace(a.raw("message"))
	tc.noteMessage(msg)
	if err := tc.approve(ctx, a, map[string]any{"action": "파일 첨부 게시", "channel": ch.DisplayName, "filename": name, "bytes": len(data), "message": msg, "send_as": mode}); err != nil {
		return nil, err
	}
	p, err := tc.uploadAndPost(ctx, token, mode, ch, name, data, msg)
	if err != nil {
		return nil, err
	}
	return tc.postedResult(ctx, p, ch, map[string]any{"filename": name}), nil
}

var channelNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,63}$`)

func toolCreateChannel(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	tc.sendMode = "user"
	team, err := tc.resolveTeam(ctx, token, a.str("team", "team_id"))
	if err != nil {
		return nil, err
	}
	name := strings.ToLower(a.str("name"))
	if !channelNamePattern.MatchString(name) {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "name은 영문 소문자/숫자/-/_ 2~64자여야 합니다"}
	}
	display, err := a.required("display_name")
	if err != nil {
		return nil, err
	}
	typ := strings.ToUpper(a.str("type"))
	if typ == "" {
		typ = "O"
	}
	if typ != "O" && typ != "P" {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "type은 O 또는 P여야 합니다"}
	}
	if err := tc.checkChannelType(&mattermost.Channel{Type: typ}); err != nil {
		return nil, err
	}
	if err := tc.approve(ctx, a, map[string]any{"action": "채널 생성", "team": team.Name, "name": name, "display_name": display, "type": channelTypeName(typ)}); err != nil {
		return nil, err
	}
	ch, err := tc.mm.CreateChannel(ctx, token, mattermost.Channel{TeamID: team.ID, Name: name, DisplayName: display, Type: typ, Purpose: a.str("purpose")})
	if err != nil {
		return nil, mmError(err, "채널 생성")
	}
	tc.details["channel_id"] = ch.ID
	return map[string]any{"success": true, "channel": channelInfo(ch, team.Name)}, nil
}

func toolAddMember(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	tc.sendMode = "user"
	ch, err := tc.resolveChannel(ctx, token, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	if ch.Type == "D" || ch.Type == "G" {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "DM/그룹 메시지에는 멤버를 추가할 수 없습니다"}
	}
	user, err := tc.resolveRecipient(ctx, a.str("user", "username", "user_id"))
	if err != nil {
		return nil, err
	}
	if err := tc.approve(ctx, a, map[string]any{"action": "채널 멤버 추가", "channel": ch.DisplayName, "user": user.Username, "user_display_name": user.DisplayName()}); err != nil {
		return nil, err
	}
	if err := tc.mm.AddChannelMember(ctx, token, ch.ID, user.ID); err != nil {
		return nil, mmError(err, "멤버 추가")
	}
	tc.details["recipient"] = user.Username
	return map[string]any{"success": true, "channel_id": ch.ID, "user": user.Username}, nil
}

func toolDeletePost(ctx context.Context, tc *toolCtx, a args) (any, error) {
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	tc.sendMode = "user"
	p, err := tc.mm.Post(ctx, token, id)
	if err != nil {
		return nil, mmError(err, "메시지")
	}
	if err := tc.postAllowed(ctx, token, p); err != nil {
		return nil, err
	}
	preview, _ := truncate(p.Message, 300)
	if err := tc.approve(ctx, a, map[string]any{"action": "메시지 삭제 (되돌릴 수 없음)", "message": preview}); err != nil {
		return nil, err
	}
	if err := tc.mm.DeletePost(ctx, token, id); err != nil {
		return nil, mmError(err, "메시지 삭제")
	}
	tc.details["post_id"] = id
	return map[string]any{"success": true, "post_id": id, "deleted": true}, nil
}

func toolArchiveChannel(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	tc.sendMode = "user"
	ch, err := tc.resolveChannel(ctx, token, a.str("channel", "channel_id"))
	if err != nil {
		return nil, err
	}
	if err := tc.approve(ctx, a, map[string]any{"action": "채널 보관", "channel": ch.DisplayName, "name": ch.Name}); err != nil {
		return nil, err
	}
	if err := tc.mm.ArchiveChannel(ctx, token, ch.ID); err != nil {
		return nil, mmError(err, "채널 보관")
	}
	return map[string]any{"success": true, "channel_id": ch.ID, "archived": true}, nil
}
