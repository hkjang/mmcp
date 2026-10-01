package server

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/hkjang/mmcp/internal/mattermost"
)

// resolveRecipient turns "kim", "@kim", "kim@corp.example", "김철수" or a user
// id into exactly one active Mattermost user. Several matches never pick one
// silently: the candidates go back so the person can choose.
func (tc *toolCtx) resolveRecipient(ctx context.Context, ref string) (*mattermost.User, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "@")
	if ref == "" {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "받는 사람이 필요합니다"}
	}
	token, err := tc.lookupToken(ctx)
	if err != nil {
		return nil, err
	}
	active := func(u *mattermost.User) (*mattermost.User, error) {
		if u.DeleteAt > 0 {
			return nil, &toolError{Code: "RECIPIENT_INACTIVE", Message: u.Username + " 계정은 비활성화되어 있습니다"}
		}
		tc.details["recipient"] = u.Username
		return u, nil
	}
	if isMMID(ref) {
		if u, err := tc.mm.User(ctx, token, ref); err == nil {
			return active(u)
		}
	}
	if strings.Contains(ref, "@") && strings.Contains(ref, ".") {
		u, err := tc.mm.UserByEmail(ctx, token, ref)
		if err == nil {
			return active(u)
		}
		if mattermost.StatusOf(err) != http.StatusNotFound && mattermost.StatusOf(err) != http.StatusForbidden {
			return nil, mmError(err, "사용자 조회")
		}
	}
	if u, err := tc.mm.UserByUsername(ctx, token, strings.ToLower(ref)); err == nil {
		return active(u)
	} else if mattermost.StatusOf(err) != http.StatusNotFound {
		return nil, mmError(err, "사용자 조회")
	}
	// Mattermost matches first and last name separately, so "김철수" alone
	// finds nobody. For non-ASCII names also search the given name with a
	// one- or two-character family name removed (김|철수, 남궁|민수).
	terms := []string{ref}
	if r := []rune(strings.ReplaceAll(ref, " ", "")); len(r) >= 2 && r[0] > 127 {
		terms = append(terms, string(r[1:]))
		if len(r) >= 4 {
			terms = append(terms, string(r[2:]))
		}
	}
	var cands []mattermost.User
	seenID := map[string]bool{}
	for _, t := range terms {
		found, err := tc.mm.SearchUsers(ctx, token, t, 20)
		if err != nil {
			return nil, mmError(err, "사용자 검색")
		}
		for _, u := range found {
			if u.DeleteAt == 0 && !u.IsBot && !seenID[u.ID] {
				seenID[u.ID] = true
				cands = append(cands, u)
			}
		}
	}
	// Prefer exact full-name / nickname matches over prefix hits.
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "")) }
	var exact []mattermost.User
	for _, u := range cands {
		for _, n := range []string{u.FirstName + u.LastName, u.LastName + u.FirstName, u.Nickname, u.DisplayName()} {
			if n != "" && norm(n) == norm(ref) {
				exact = append(exact, u)
				break
			}
		}
	}
	if len(exact) > 0 {
		cands = exact
	} else if len(terms) > 1 {
		// given-name hits that are not the full name are not the person asked for
		var primary []mattermost.User
		for _, u := range cands {
			if strings.Contains(norm(u.Username+u.FirstName+u.LastName+u.Nickname+u.Email), norm(ref)) {
				primary = append(primary, u)
			}
		}
		cands = primary
	}
	switch len(cands) {
	case 0:
		return nil, &toolError{Code: "RECIPIENT_NOT_FOUND", Message: "'" + ref + "'에 해당하는 사용자를 찾을 수 없습니다. mattermost_users로 검색해 보세요"}
	case 1:
		return active(&cands[0])
	}
	list := []map[string]string{}
	for _, u := range cands {
		list = append(list, map[string]string{"username": u.Username, "display_name": u.DisplayName(), "nickname": u.Nickname, "position": u.Position, "email": u.Email})
	}
	return nil, &toolError{Code: "AMBIGUOUS_RECIPIENT", Message: "'" + ref + "'에 해당하는 사용자가 " + strconv.Itoa(len(cands)) + "명입니다. 임의로 보내지 말고 사용자에게 아래 후보 중 하나를 고르게 한 뒤 username으로 다시 호출하세요", Details: list}
}

// dmChannel returns the direct channel used to deliver a DM, according to the
// send identity and the bot DM channel policy.
func (tc *toolCtx) dmChannel(ctx context.Context, token, mode string, to *mattermost.User) (*mattermost.Channel, error) {
	if mode == "user" {
		m, err := tc.ensureMapping(ctx)
		if err != nil {
			return nil, err
		}
		ch, err := tc.mm.CreateDirectChannel(ctx, token, m.MMUserID, to.ID)
		if err != nil {
			return nil, mmError(err, "DM 채널")
		}
		return ch, nil
	}
	if tc.st.Mattermost.BotDMChannel == "requester_pair" {
		m, err := tc.ensureMapping(ctx)
		if err != nil {
			return nil, err
		}
		creator := tc.st.Mattermost.ServiceToken
		if creator == "" {
			creator = token
		}
		ch, err := tc.mm.CreateDirectChannel(ctx, creator, m.MMUserID, to.ID)
		if err != nil {
			if mattermost.StatusOf(err) == http.StatusForbidden {
				return nil, &toolError{Code: "PERMISSION_DENIED", Message: "요청자↔수신자 DM 채널을 만들려면 서비스 토큰에 시스템 관리자 권한이, 봇 토큰에 post:all 권한이 필요합니다"}
			}
			return nil, mmError(err, "DM 채널")
		}
		return ch, nil
	}
	bot, err := tc.s.mm.botUser(ctx)
	if err != nil {
		return nil, err
	}
	ch, err := tc.mm.CreateDirectChannel(ctx, token, bot.ID, to.ID)
	if err != nil {
		return nil, mmError(err, "봇 DM 채널")
	}
	return ch, nil
}

func (tc *toolCtx) dmPreview(action, mode string, to []*mattermost.User, msg string, extra map[string]any) map[string]any {
	names := []map[string]string{}
	for _, u := range to {
		names = append(names, map[string]string{"username": u.Username, "display_name": u.DisplayName()})
	}
	p := map[string]any{"action": action, "recipients": names, "message": msg, "send_as": mode}
	if mode == "bot" {
		p["note"] = "봇 계정으로 발송되며 요청자 표시가 메시지 상단에 붙습니다"
	}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

func toolSendDM(ctx context.Context, tc *toolCtx, a args) (any, error) {
	msg, err := tc.messageArg(a)
	if err != nil {
		return nil, err
	}
	to, err := tc.resolveRecipient(ctx, a.str("recipient", "username", "user", "to"))
	if err != nil {
		return nil, err
	}
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	if !tc.st.MCP.AllowDM {
		return nil, &toolError{Code: "POLICY_BLOCKED", Message: "DM이 관리자 정책으로 차단되어 있습니다"}
	}
	tc.noteMessage(msg)
	if err := tc.approve(ctx, a, tc.dmPreview("DM 발송", mode, []*mattermost.User{to}, msg, nil)); err != nil {
		return nil, err
	}
	ch, err := tc.dmChannel(ctx, token, mode, to)
	if err != nil {
		return nil, err
	}
	tc.details["channel_id"] = ch.ID
	p, err := tc.createPost(ctx, token, mode, mattermost.Post{ChannelID: ch.ID, Message: msg})
	if err != nil {
		return nil, err
	}
	return tc.postedResult(ctx, p, nil, map[string]any{"recipient": to.Username, "recipient_display_name": to.DisplayName()}), nil
}

func toolSendGroupDM(ctx context.Context, tc *toolCtx, a args) (any, error) {
	msg, err := tc.messageArg(a)
	if err != nil {
		return nil, err
	}
	refs := a.list("recipients", "usernames", "users")
	if len(refs) < 2 || len(refs) > 7 {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "recipients는 2~7명이어야 합니다 (1명은 mattermost_send_dm 사용)"}
	}
	if !tc.st.MCP.AllowGroup {
		return nil, &toolError{Code: "POLICY_BLOCKED", Message: "그룹 메시지가 관리자 정책으로 차단되어 있습니다"}
	}
	var to []*mattermost.User
	seen := map[string]bool{}
	for _, r := range refs {
		u, err := tc.resolveRecipient(ctx, r)
		if err != nil {
			return nil, err
		}
		if !seen[u.ID] {
			seen[u.ID] = true
			to = append(to, u)
		}
	}
	tc.details["recipient"] = ""
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	tc.noteMessage(msg)
	if err := tc.approve(ctx, a, tc.dmPreview("그룹 메시지 발송", mode, to, msg, nil)); err != nil {
		return nil, err
	}
	ids := []string{}
	add := func(id string) {
		if id != "" && !seen[id+"#"] {
			seen[id+"#"] = true
			ids = append(ids, id)
		}
	}
	creator := token
	if mode == "user" || tc.st.Mattermost.BotDMChannel == "requester_pair" {
		m, err := tc.ensureMapping(ctx)
		if err != nil {
			return nil, err
		}
		add(m.MMUserID)
		if mode == "bot" && tc.st.Mattermost.ServiceToken != "" {
			creator = tc.st.Mattermost.ServiceToken
		}
	} else {
		bot, err := tc.s.mm.botUser(ctx)
		if err != nil {
			return nil, err
		}
		add(bot.ID)
		if m, err := tc.ensureMapping(ctx); err == nil {
			add(m.MMUserID) // keep the requester in the loop
		}
	}
	for _, u := range to {
		add(u.ID)
	}
	if len(ids) < 3 || len(ids) > 8 {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "그룹 메시지는 본인 포함 3~8명이어야 합니다"}
	}
	ch, err := tc.mm.CreateGroupChannel(ctx, creator, ids)
	if err != nil {
		return nil, mmError(err, "그룹 메시지 채널")
	}
	tc.details["channel_id"] = ch.ID
	p, err := tc.createPost(ctx, token, mode, mattermost.Post{ChannelID: ch.ID, Message: msg})
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, u := range to {
		names = append(names, u.Username)
	}
	tc.details["recipients"] = names
	return tc.postedResult(ctx, p, nil, map[string]any{"recipients": names}), nil
}

func toolReplyDM(ctx context.Context, tc *toolCtx, a args) (any, error) {
	msg, err := tc.messageArg(a)
	if err != nil {
		return nil, err
	}
	id, err := a.required("post_id")
	if err != nil {
		return nil, err
	}
	readTok, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	parent, err := tc.mm.Post(ctx, readTok, id)
	if err != nil {
		return nil, mmError(err, "원본 메시지")
	}
	ch, err := tc.mm.Channel(ctx, readTok, parent.ChannelID)
	if err != nil {
		return nil, mmError(err, "채널")
	}
	if ch.Type != "D" && ch.Type != "G" {
		return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "DM/그룹 메시지가 아닙니다. 채널 메시지는 mattermost_reply를 사용하세요"}
	}
	if err := tc.checkChannelType(ch); err != nil {
		return nil, err
	}
	tc.details["channel_id"] = ch.ID
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	root := parent.RootID
	if root == "" {
		root = parent.ID
	}
	tc.noteMessage(msg)
	orig, _ := truncate(parent.Message, 200)
	if err := tc.approve(ctx, a, map[string]any{"action": "DM 답장", "reply_to": orig, "message": msg, "send_as": mode}); err != nil {
		return nil, err
	}
	p, err := tc.createPost(ctx, token, mode, mattermost.Post{ChannelID: ch.ID, Message: msg, RootID: root})
	if err != nil {
		return nil, err
	}
	return tc.postedResult(ctx, p, nil, map[string]any{"root_id": root}), nil
}

func toolUploadDMFile(ctx context.Context, tc *toolCtx, a args) (any, error) {
	name, data, err := tc.fileArg(a)
	if err != nil {
		return nil, err
	}
	to, err := tc.resolveRecipient(ctx, a.str("recipient", "username", "user"))
	if err != nil {
		return nil, err
	}
	if !tc.st.MCP.AllowDM {
		return nil, &toolError{Code: "POLICY_BLOCKED", Message: "DM이 관리자 정책으로 차단되어 있습니다"}
	}
	token, mode, err := tc.sender(ctx)
	if err != nil {
		return nil, err
	}
	msg := strings.TrimSpace(a.raw("message"))
	tc.noteMessage(msg)
	if err := tc.approve(ctx, a, tc.dmPreview("파일 포함 DM 발송", mode, []*mattermost.User{to}, msg, map[string]any{"filename": name, "bytes": len(data)})); err != nil {
		return nil, err
	}
	ch, err := tc.dmChannel(ctx, token, mode, to)
	if err != nil {
		return nil, err
	}
	tc.details["channel_id"] = ch.ID
	p, err := tc.uploadAndPost(ctx, token, mode, ch, name, data, msg)
	if err != nil {
		return nil, err
	}
	return tc.postedResult(ctx, p, nil, map[string]any{"recipient": to.Username, "filename": name}), nil
}

// findDM locates an existing DM with a user among my channels without
// creating one as a side effect of reading.
func (tc *toolCtx) findDM(ctx context.Context, token string, other *mattermost.User) (*mattermost.Channel, error) {
	m, err := tc.ensureMapping(ctx)
	if err != nil {
		return nil, err
	}
	a, b := m.MMUserID, other.ID
	if a > b {
		a, b = b, a
	}
	name := a + "__" + b
	teams, err := tc.myTeams(ctx, token)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		chs, err := tc.mm.MyChannels(ctx, token, t.ID)
		if err != nil {
			return nil, mmError(err, "채널 목록")
		}
		for i := range chs {
			if chs[i].Type == "D" && chs[i].Name == name {
				return &chs[i], nil
			}
		}
		break // DMs appear in every team's list
	}
	return nil, nil
}

func toolReadDM(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx) // DMs are always read as the user
	if err != nil {
		return nil, err
	}
	since, err := parseSince(a.str("since"))
	if err != nil {
		return nil, err
	}
	limit := min(a.intv("limit", 30, 1, 200), tc.st.MCP.MaxResults)
	var ch *mattermost.Channel
	if id := a.str("channel", "channel_id"); id != "" {
		c, err := tc.mm.Channel(ctx, token, id)
		if err != nil {
			return nil, mmError(err, "DM 채널")
		}
		if c.Type != "D" && c.Type != "G" {
			return nil, &toolError{Code: "INVALID_ARGUMENT", Message: "DM/그룹 메시지 채널이 아닙니다. mattermost_recent_posts를 사용하세요"}
		}
		ch = c
	} else {
		ref, err := a.required("user", "username", "recipient")
		if err != nil {
			return nil, err
		}
		other, err := tc.resolveRecipient(ctx, ref)
		if err != nil {
			return nil, err
		}
		if ch, err = tc.findDM(ctx, token, other); err != nil {
			return nil, err
		}
		if ch == nil {
			return map[string]any{"user": other.Username, "posts": []any{}, "count": 0, "note": "이 사용자와의 DM 대화가 없습니다"}, nil
		}
	}
	if err := tc.checkChannelType(ch); err != nil {
		return nil, err
	}
	tc.details["channel_id"] = ch.ID
	return tc.channelPosts(ctx, token, ch, limit, since)
}

func (tc *toolCtx) dmPartnerName(ctx context.Context, f *postFormatter, ch *mattermost.Channel) string {
	m, err := tc.ensureMapping(ctx)
	if err != nil {
		return ch.DisplayName
	}
	ids := strings.Split(ch.Name, "__")
	other := ""
	for _, id := range ids {
		if id != m.MMUserID {
			other = id
		}
	}
	if other == "" {
		return "(나와의 대화)"
	}
	if u, ok := f.users[other]; ok && u.Username != "" {
		return "@" + u.Username + " " + u.DisplayName()
	}
	if us, err := tc.mm.UsersByIDs(ctx, f.token, []string{other}); err == nil && len(us) == 1 {
		f.users[other] = us[0]
		return "@" + us[0].Username + " " + us[0].DisplayName()
	}
	return ch.DisplayName
}

func toolRecentDMs(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	teams, err := tc.myTeams(ctx, token)
	if err != nil {
		return nil, err
	}
	if len(teams) == 0 {
		return map[string]any{"conversations": []any{}, "count": 0}, nil
	}
	chs, err := tc.mm.MyChannels(ctx, token, teams[0].ID)
	if err != nil {
		return nil, mmError(err, "채널 목록")
	}
	mems, _ := tc.mm.MyChannelMembers(ctx, token, teams[0].ID)
	byID := map[string]mattermost.ChannelMember{}
	for _, m := range mems {
		byID[m.ChannelID] = m
	}
	var dms []mattermost.Channel
	for _, c := range chs {
		if (c.Type == "D" || c.Type == "G") && c.DeleteAt == 0 && tc.checkChannelType(&c) == nil {
			dms = append(dms, c)
		}
	}
	sort.Slice(dms, func(i, j int) bool { return dms[i].LastPostAt > dms[j].LastPostAt })
	limit := a.intv("limit", 20, 1, 100)
	unreadOnly := a.boolv("unread_only", false)
	f := tc.newFormatter(ctx, token)
	out := []map[string]any{}
	for i := range dms {
		c := &dms[i]
		m := byID[c.ID]
		unread := max(c.TotalMsgCount-m.MsgCount, 0)
		if unreadOnly && unread == 0 && m.MentionCount == 0 {
			continue
		}
		name := c.DisplayName
		if c.Type == "D" {
			name = tc.dmPartnerName(ctx, f, c)
		}
		out = append(out, map[string]any{"channel_id": c.ID, "type": channelTypeName(c.Type), "with": name, "last_post_at": msTime(c.LastPostAt), "unread": unread, "mentions": m.MentionCount})
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{"conversations": out, "count": len(out)}, nil
}

func toolSearchDM(ctx context.Context, tc *toolCtx, a args) (any, error) {
	token, err := tc.userToken(ctx)
	if err != nil {
		return nil, err
	}
	terms, err := searchTerms(args{"query": a.str("query", "terms"), "after": a.str("after"), "before": a.str("before")})
	if err != nil {
		return nil, err
	}
	limit := min(a.intv("limit", 20, 1, 100), tc.st.MCP.MaxResults)
	posts, f, err := tc.search(ctx, token, "", terms, false, limit*3, true)
	if err != nil {
		return nil, err
	}
	if ref := a.str("user", "username"); ref != "" {
		other, err := tc.resolveRecipient(ctx, ref)
		if err != nil {
			return nil, err
		}
		kept := posts[:0]
		for _, p := range posts {
			if c := f.channel(ctx, p.ChannelID); c != nil && strings.Contains(c.Name, other.ID) {
				kept = append(kept, p)
			}
		}
		posts = kept
	}
	if len(posts) > limit {
		posts = posts[:limit]
	}
	tc.details["query_hash"] = hashQuery(terms)
	tc.details["result_count"] = len(posts)
	return map[string]any{"query": terms, "results": f.format(ctx, posts), "count": len(posts)}, nil
}
