package daemon

import (
	"context"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"whatsapp-mcp/internal/store"
)

// handleMessage stores one message (live or from a history sync) with its
// sender resolved to a name.
func (d *Daemon) handleMessage(ctx context.Context, evt *events.Message) {
	info := evt.Info
	chat := info.Chat
	if chat.Server == types.BroadcastServer || chat.Server == types.NewsletterServer {
		return
	}
	msg := unwrap(evt.Message)
	text := extractText(msg)
	media := extractMedia(msg, info.ID, info.Timestamp)
	if text == "" && media == nil {
		return
	}

	var phone, lid string
	if info.IsFromMe {
		phone, lid = d.ownIdentity()
	} else {
		phone, lid = identityOf(info.Sender, info.SenderAlt)
		d.learnIdentity(ctx, phone, lid, info.PushName)
	}

	isGroup := chat.Server == types.GroupServer
	chatJID := chat.ToNonAD().String()
	chatName := ""
	if isGroup {
		chatName = d.groupName(ctx, chat)
	} else {
		var pPhone, pLID string
		if info.IsFromMe {
			pPhone, pLID = identityOf(chat, info.RecipientAlt)
		} else {
			pPhone, pLID = phone, lid
		}
		pPhone, pLID = d.completeIdentity(ctx, pPhone, pLID)
		if pPhone != "" {
			chatJID = pPhone + "@" + types.DefaultUserServer
			if pLID != "" {
				if err := d.store.MergeLIDChat(ctx, pLID, pPhone); err != nil {
					d.log.Warn("merge LID chat failed", "lid", pLID, "error", err)
				}
			}
		} else if pLID != "" {
			chatJID = pLID + "@" + types.HiddenUserServer
		}
		chatName, _ = d.store.ContactName(ctx, pPhone, pLID)
		if chatName == "" && !info.IsFromMe {
			chatName = strings.TrimSpace(info.PushName)
		}
	}

	senderName := ""
	if !info.IsFromMe {
		senderName, _ = d.store.ContactName(ctx, phone, lid)
		if senderName == "" {
			senderName = strings.TrimSpace(info.PushName)
		}
	}
	if err := d.store.PutChat(ctx, chatJID, chatName, isGroup, info.Timestamp); err != nil {
		d.log.Warn("store chat failed", "chat", chatJID, "error", err)
	}
	ok, err := d.store.PutMessage(ctx, store.NewMessage{
		ID: info.ID, ChatJID: chatJID, SenderPhone: phone, SenderLID: lid, SenderName: senderName,
		Content: text, Timestamp: info.Timestamp, IsFromMe: info.IsFromMe, Media: media,
	})
	if err != nil {
		d.log.Warn("store message failed", "id", info.ID, "chat", chatJID, "error", err)
		return
	}
	if ok && evt.SourceWebMsg == nil {
		kind := "text"
		if media != nil {
			kind = media.MediaType
		}
		d.log.Info("message stored", "chat", chatJID, "from_me", info.IsFromMe, "kind", kind)
	}
}

// completeIdentity fills the missing half of (phone, lid) from the contacts table.
func (d *Daemon) completeIdentity(ctx context.Context, phone, lid string) (string, string) {
	if phone != "" && lid != "" {
		return phone, lid
	}
	if c, ok, _ := d.store.LookupContact(ctx, phone, lid); ok {
		if phone == "" {
			phone = c.Phone
		}
		if lid == "" {
			lid = c.LID
		}
	}
	return phone, lid
}

// learnIdentity records the phone/LID link and the push name carried by a
// message, once per value (the maps avoid a write per message).
func (d *Daemon) learnIdentity(ctx context.Context, phone, lid, pushName string) {
	if phone == "" && lid == "" {
		return
	}
	key := phone + "|" + lid
	d.mu.Lock()
	needLink := phone != "" && lid != "" && !d.linked[key]
	if needLink {
		d.linked[key] = true
	}
	pushName = strings.TrimSpace(pushName)
	needPush := pushName != "" && d.pushNames[key] != pushName
	if needPush {
		d.pushNames[key] = pushName
	}
	d.mu.Unlock()
	if needLink && !needPush {
		d.putName(ctx, phone, lid, "", "")
	}
	if needPush {
		d.putName(ctx, phone, lid, pushName, store.SourcePushName)
	}
}

// putName writes a contact and propagates the consequences: sender names
// in messages, direct chat names, and the merge of a LID chat into the
// phone chat when the link is new.
func (d *Daemon) putName(ctx context.Context, phone, lid, name, source string) {
	if phone == "" && lid == "" {
		return
	}
	res, err := d.store.PutContact(ctx, phone, lid, name, source)
	if err != nil {
		d.log.Warn("store contact failed", "phone", phone, "lid", lid, "error", err)
		return
	}
	if res.Linked && res.Phone != "" && res.LID != "" {
		if err := d.store.MergeLIDChat(ctx, res.LID, res.Phone); err != nil {
			d.log.Warn("merge LID chat failed", "lid", res.LID, "error", err)
		}
	}
	if res.NameChanged || res.Linked {
		if res.Name != "" {
			if err := d.store.RefreshSenderName(ctx, res.Phone, res.LID, res.Name); err != nil {
				d.log.Warn("refresh sender names failed", "error", err)
			}
			for _, jid := range (store.Identity{Phone: res.Phone, LID: res.LID}).JIDs() {
				if err := d.store.RenameChat(ctx, jid, res.Name); err != nil {
					d.log.Warn("rename chat failed", "jid", jid, "error", err)
				}
			}
		}
	}
}

// syncNames copies the whatsmeow contact store and LID map into contacts.
// It returns the number of contacts whose name or link changed.
func (d *Daemon) syncNames(ctx context.Context) (int, error) {
	changed := 0
	rows, err := d.session.QueryContext(ctx, `SELECT lid, pn FROM whatsmeow_lid_map`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var lidStr, pnStr string
		if err := rows.Scan(&lidStr, &pnStr); err != nil {
			rows.Close()
			return changed, err
		}
		lid, _ := store.SplitJID(lidStr)
		pn, _ := store.SplitJID(pnStr)
		if lid == "" || pn == "" {
			continue
		}
		d.mu.Lock()
		d.linked[pn+"|"+lid] = true
		d.mu.Unlock()
		res, err := d.store.PutContact(ctx, pn, lid, "", "")
		if err != nil {
			rows.Close()
			return changed, err
		}
		if res.Linked {
			changed++
			if err := d.store.MergeLIDChat(ctx, lid, pn); err != nil {
				d.log.Warn("merge LID chat failed", "lid", lid, "error", err)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return changed, err
	}

	if d.cli.Store.Contacts == nil {
		return changed, nil
	}
	contacts, err := d.cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return changed, err
	}
	for jid, info := range contacts {
		phone, lid := identityOf(jid, types.EmptyJID)
		if phone == "" && lid == "" {
			continue
		}
		for _, cand := range []struct{ name, source string }{
			{info.FullName, store.SourceFullName},
			{info.FirstName, store.SourceFirstName},
			{info.BusinessName, store.SourceBusinessName},
			{info.PushName, store.SourcePushName},
		} {
			if strings.TrimSpace(cand.name) == "" {
				continue
			}
			res, err := d.store.PutContact(ctx, phone, lid, cand.name, cand.source)
			if err != nil {
				return changed, err
			}
			if res.NameChanged {
				changed++
				if err := d.store.RefreshSenderName(ctx, res.Phone, res.LID, res.Name); err != nil {
					return changed, err
				}
				for _, j := range (store.Identity{Phone: res.Phone, LID: res.LID}).JIDs() {
					_ = d.store.RenameChat(ctx, j, res.Name)
				}
			}
		}
	}
	return changed, nil
}

func (d *Daemon) handleContact(ctx context.Context, evt *events.Contact) {
	phone, lid := identityOf(evt.JID, types.EmptyJID)
	if evt.Action != nil {
		if pn := evt.Action.GetPnJID(); pn != "" {
			if j, err := types.ParseJID(pn); err == nil && phone == "" {
				phone = j.User
			}
		}
		if l := evt.Action.GetLidJID(); l != "" {
			if j, err := types.ParseJID(l); err == nil && lid == "" {
				lid = j.User
			}
		}
		if full := evt.Action.GetFullName(); full != "" {
			d.putName(ctx, phone, lid, full, store.SourceFullName)
		}
		if first := evt.Action.GetFirstName(); first != "" {
			d.putName(ctx, phone, lid, first, store.SourceFirstName)
		}
		if evt.Action.GetFullName() == "" && evt.Action.GetFirstName() == "" {
			d.putName(ctx, phone, lid, "", "")
		}
	}
}

func (d *Daemon) handlePushName(ctx context.Context, evt *events.PushName) {
	phone, lid := identityOf(evt.JID, evt.JIDAlt)
	d.putName(ctx, phone, lid, evt.NewPushName, store.SourcePushName)
}

func (d *Daemon) setGroupName(ctx context.Context, jid types.JID, name string) {
	d.mu.Lock()
	d.groupNames[jid.String()] = name
	d.mu.Unlock()
	if err := d.store.PutChat(ctx, jid.String(), name, true, time.Time{}); err != nil {
		d.log.Warn("store group name failed", "jid", jid.String(), "error", err)
	}
}

// groupName returns the group's name from the cache, the database, or WhatsApp.
func (d *Daemon) groupName(ctx context.Context, jid types.JID) string {
	key := jid.String()
	d.mu.Lock()
	name, ok := d.groupNames[key]
	d.mu.Unlock()
	if ok {
		return name
	}
	if name, _ := d.store.ChatName(ctx, key); name != "" {
		d.mu.Lock()
		d.groupNames[key] = name
		d.mu.Unlock()
		return name
	}
	if !d.cli.IsConnected() {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := d.cli.GetGroupInfo(ctx, jid)
	if err != nil {
		d.log.Debug("group info unavailable", "jid", key, "error", err)
		return ""
	}
	d.mu.Lock()
	d.groupNames[key] = info.Name
	d.mu.Unlock()
	return info.Name
}

func (d *Daemon) handleHistorySync(ctx context.Context, evt *events.HistorySync) {
	convs := evt.Data.GetConversations()
	d.log.Info("history sync", "type", evt.Data.GetSyncType().String(), "conversations", len(convs), "progress", evt.Data.GetProgress())
	stored := 0
	for _, conv := range convs {
		jid, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		if conv.GetLidJID() != "" && conv.GetPnJID() != "" {
			l, err1 := types.ParseJID(conv.GetLidJID())
			p, err2 := types.ParseJID(conv.GetPnJID())
			if err1 == nil && err2 == nil {
				phone, lid := identityOf(p, l)
				d.learnIdentity(ctx, phone, lid, "")
			}
		}
		if jid.Server == types.GroupServer {
			if name := firstNonEmpty(conv.GetDisplayName(), conv.GetName()); name != "" {
				d.setGroupName(ctx, jid, name)
			}
		}
		for _, hm := range conv.GetMessages() {
			m, err := d.cli.ParseWebMessage(jid, hm.GetMessage())
			if err != nil {
				d.log.Debug("history message skipped", "error", err)
				continue
			}
			d.handleMessage(ctx, m)
			stored++
		}
	}
	d.log.Info("history sync processed", "messages", stored)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
