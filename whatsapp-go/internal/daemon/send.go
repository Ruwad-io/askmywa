package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/media"
	"whatsapp-mcp/internal/security"
	"whatsapp-mcp/internal/store"
)

// Handle implements ipc.Handler.
func (d *Daemon) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case ipc.MethodPing:
		return "pong", nil
	case ipc.MethodStatus:
		return d.status(ctx)
	case ipc.MethodSendMessage:
		var p ipc.SendMessageParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.sendText(ctx, p)
	case ipc.MethodSendFile:
		var p ipc.SendFileParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.sendFile(ctx, p)
	case ipc.MethodSendAudio:
		var p ipc.SendAudioParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.sendAudio(ctx, p)
	case ipc.MethodPair:
		var p ipc.PairParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.pairPhone(p)
	case ipc.MethodLogout:
		return d.logout(ctx)
	case ipc.MethodDownloadMedia:
		var p ipc.DownloadParams
		if err := decode(params, &p); err != nil {
			return nil, err
		}
		return d.download(ctx, p)
	}
	return nil, fmt.Errorf("unknown method %q", method)
}

func decode(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return errors.New("missing params")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (d *Daemon) status(ctx context.Context) (ipc.StatusResult, error) {
	counts, err := d.store.Count(ctx)
	if err != nil {
		return ipc.StatusResult{}, err
	}
	res := ipc.StatusResult{Connected: d.cli.IsConnected(), LoggedIn: d.isLoggedIn(), SendRoot: d.opts.Paths.SendRoot,
		MediaDir: d.opts.Paths.MediaDir, Counts: counts, Uptime: time.Since(d.started).Round(time.Second).String()}
	d.mu.Lock()
	res.LoggedOut = d.loggedOut
	res.Pairing = d.pair.active && time.Now().Before(d.pair.expires)
	res.PairError = d.pair.lastErr
	d.mu.Unlock()
	if d.cli.Store.ID != nil {
		res.JID = d.cli.Store.ID.ToNonAD().String()
	}
	if !d.cli.Store.LID.IsEmpty() {
		res.LID = d.cli.Store.LID.ToNonAD().String()
	}
	return res, nil
}

// ParseRecipient turns a phone number or JID into the JID to send to.
func ParseRecipient(to string) (types.JID, error) {
	to = strings.TrimSpace(to)
	if to == "" {
		return types.EmptyJID, errors.New("recipient is empty: pass a phone number with country code (no +) or a JID such as 123@s.whatsapp.net or 123@g.us")
	}
	if strings.Contains(to, "@") {
		jid, err := types.ParseJID(to)
		if err != nil {
			return types.EmptyJID, fmt.Errorf("invalid recipient JID %q: %w", to, err)
		}
		switch jid.Server {
		case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer:
		case "c.us":
			jid.Server = types.DefaultUserServer
		default:
			return types.EmptyJID, fmt.Errorf("unsupported recipient JID %q: use x@s.whatsapp.net, x@lid or x@g.us", to)
		}
		if jid.User == "" {
			return types.EmptyJID, fmt.Errorf("invalid recipient JID %q", to)
		}
		return jid.ToNonAD(), nil
	}
	digits := store.DigitsOf(to)
	if digits == "" || len(digits) < 6 {
		return types.EmptyJID, fmt.Errorf("invalid recipient %q: pass a phone number with country code (no +) or a JID", to)
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

func (d *Daemon) requireConnected() error {
	if !d.isLoggedIn() {
		return errors.New("WhatsApp is not paired yet: call connect_whatsapp with the user's phone number, or run `whatsapp-mcp daemon` in a terminal and scan the QR code")
	}
	if !d.cli.IsConnected() {
		return errors.New("the daemon is not connected to WhatsApp right now: retry in a few seconds")
	}
	return nil
}

func (d *Daemon) sendText(ctx context.Context, p ipc.SendMessageParams) (ipc.SendResult, error) {
	if strings.TrimSpace(p.Text) == "" {
		return ipc.SendResult{}, errors.New("message text is empty")
	}
	to, err := ParseRecipient(p.To)
	if err != nil {
		return ipc.SendResult{}, err
	}
	if err := d.requireConnected(); err != nil {
		return ipc.SendResult{}, err
	}
	msg := &waE2E.Message{Conversation: proto.String(p.Text)}
	return d.send(ctx, to, msg, p.Text, nil)
}

func (d *Daemon) sendFile(ctx context.Context, p ipc.SendFileParams) (ipc.SendResult, error) {
	to, err := ParseRecipient(p.To)
	if err != nil {
		return ipc.SendResult{}, err
	}
	path, err := security.ValidateSendPath(p.Path, d.opts.Paths.SendRoot)
	if err != nil {
		return ipc.SendResult{}, err
	}
	if err := d.requireConnected(); err != nil {
		return ipc.SendResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ipc.SendResult{}, fmt.Errorf("read %s: %w", path, err)
	}
	kind, mime := media.Classify(path)
	var waType whatsmeow.MediaType
	switch kind {
	case media.KindImage:
		waType = whatsmeow.MediaImage
	case media.KindVideo:
		waType = whatsmeow.MediaVideo
	case media.KindAudio:
		waType = whatsmeow.MediaAudio
	default:
		waType = whatsmeow.MediaDocument
	}
	up, err := d.cli.Upload(ctx, data, waType)
	if err != nil {
		return ipc.SendResult{}, fmt.Errorf("upload %s: %w", filepath.Base(path), err)
	}
	caption := strings.TrimSpace(p.Caption)
	msg := &waE2E.Message{}
	common := func() (string, string, []byte, []byte, []byte, uint64) {
		return up.URL, up.DirectPath, up.MediaKey, up.FileEncSHA256, up.FileSHA256, up.FileLength
	}
	u, dp, key, encSHA, sha, length := common()
	switch kind {
	case media.KindImage:
		msg.ImageMessage = &waE2E.ImageMessage{Caption: optString(caption), Mimetype: proto.String(mime), URL: &u, DirectPath: &dp,
			MediaKey: key, FileEncSHA256: encSHA, FileSHA256: sha, FileLength: &length}
	case media.KindVideo:
		msg.VideoMessage = &waE2E.VideoMessage{Caption: optString(caption), Mimetype: proto.String(mime), URL: &u, DirectPath: &dp,
			MediaKey: key, FileEncSHA256: encSHA, FileSHA256: sha, FileLength: &length}
	case media.KindAudio:
		msg.AudioMessage = &waE2E.AudioMessage{Mimetype: proto.String(mime), URL: &u, DirectPath: &dp,
			MediaKey: key, FileEncSHA256: encSHA, FileSHA256: sha, FileLength: &length, PTT: proto.Bool(false)}
	default:
		msg.DocumentMessage = &waE2E.DocumentMessage{Title: proto.String(filepath.Base(path)), FileName: proto.String(filepath.Base(path)),
			Caption: optString(caption), Mimetype: proto.String(mime), URL: &u, DirectPath: &dp,
			MediaKey: key, FileEncSHA256: encSHA, FileSHA256: sha, FileLength: &length}
	}
	info := &store.MediaInfo{MediaType: string(kind), Filename: filepath.Base(path), MimeType: mime, URL: u, DirectPath: dp,
		MediaKey: key, FileSHA256: sha, FileEncSHA256: encSHA, FileLength: length}
	return d.send(ctx, to, msg, caption, info)
}

func (d *Daemon) sendAudio(ctx context.Context, p ipc.SendAudioParams) (ipc.SendResult, error) {
	to, err := ParseRecipient(p.To)
	if err != nil {
		return ipc.SendResult{}, err
	}
	path, err := security.ValidateSendPath(p.Path, d.opts.Paths.SendRoot)
	if err != nil {
		return ipc.SendResult{}, err
	}
	if err := d.requireConnected(); err != nil {
		return ipc.SendResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ipc.SendResult{}, fmt.Errorf("read %s: %w", path, err)
	}
	if !media.IsOggOpus(data) {
		converted, err := media.ConvertToOpusOgg(ctx, path, filepath.Join(d.opts.Paths.DataDir, "tmp"))
		if err != nil {
			return ipc.SendResult{}, err
		}
		defer os.Remove(converted)
		data, err = os.ReadFile(converted)
		if err != nil {
			return ipc.SendResult{}, err
		}
	}
	seconds, waveform, err := media.AnalyzeOggOpus(data)
	if err != nil {
		return ipc.SendResult{}, fmt.Errorf("analyse voice note: %w", err)
	}
	up, err := d.cli.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return ipc.SendResult{}, fmt.Errorf("upload voice note: %w", err)
	}
	mime := "audio/ogg; codecs=opus"
	msg := &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String(mime), URL: &up.URL, DirectPath: &up.DirectPath,
		MediaKey: up.MediaKey, FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: &up.FileLength,
		Seconds: proto.Uint32(seconds), PTT: proto.Bool(true), Waveform: waveform}}
	info := &store.MediaInfo{MediaType: "audio", Filename: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".ogg", MimeType: mime,
		URL: up.URL, DirectPath: up.DirectPath, MediaKey: up.MediaKey, FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256, FileLength: up.FileLength}
	return d.send(ctx, to, msg, "", info)
}

// send delivers the message and records it as sent by us.
func (d *Daemon) send(ctx context.Context, to types.JID, msg *waE2E.Message, text string, info *store.MediaInfo) (ipc.SendResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := d.cli.SendMessage(ctx, to, msg)
	if err != nil {
		return ipc.SendResult{}, fmt.Errorf("send to %s: %w", to.String(), err)
	}
	phone, lid := d.ownIdentity()
	chatJID := to.String()
	isGroup := to.Server == types.GroupServer
	chatName := ""
	if isGroup {
		chatName = d.groupName(ctx, to)
	} else {
		pPhone, pLID := identityOf(to, types.EmptyJID)
		pPhone, pLID = d.completeIdentity(ctx, pPhone, pLID)
		if pPhone != "" {
			chatJID = pPhone + "@" + types.DefaultUserServer
		}
		chatName, _ = d.store.ContactName(ctx, pPhone, pLID)
	}
	if err := d.store.PutChat(ctx, chatJID, chatName, isGroup, resp.Timestamp); err != nil {
		d.log.Warn("store chat failed", "chat", chatJID, "error", err)
	}
	if _, err := d.store.PutMessage(ctx, store.NewMessage{ID: resp.ID, ChatJID: chatJID, SenderPhone: phone, SenderLID: lid,
		Content: text, Timestamp: resp.Timestamp, IsFromMe: true, Media: info}); err != nil {
		d.log.Warn("store sent message failed", "error", err)
	}
	d.log.Info("message sent", "to", chatJID, "id", resp.ID)
	return ipc.SendResult{MessageID: resp.ID, To: chatJID, Timestamp: resp.Timestamp}, nil
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}

// downloadable adapts stored media parameters to whatsmeow's download API.
type downloadable struct {
	info   store.MediaInfo
	waType whatsmeow.MediaType
}

func (m *downloadable) GetDirectPath() string             { return m.info.DirectPath }
func (m *downloadable) GetMediaKey() []byte               { return m.info.MediaKey }
func (m *downloadable) GetFileSHA256() []byte             { return m.info.FileSHA256 }
func (m *downloadable) GetFileEncSHA256() []byte          { return m.info.FileEncSHA256 }
func (m *downloadable) GetMediaType() whatsmeow.MediaType { return m.waType }
func (m *downloadable) GetFileLength() uint64             { return m.info.FileLength }
func (m *downloadable) GetURL() string                    { return m.info.URL }

func (d *Daemon) download(ctx context.Context, p ipc.DownloadParams) (ipc.DownloadResult, error) {
	if p.MessageID == "" || p.ChatJID == "" {
		return ipc.DownloadResult{}, errors.New("message_id and chat_jid are both required: take them from a list_messages result")
	}
	info, err := d.store.GetMediaInfo(ctx, p.MessageID, p.ChatJID)
	if err != nil {
		return ipc.DownloadResult{}, err
	}
	dir := filepath.Join(d.opts.Paths.MediaDir, security.SafeDirName(p.ChatJID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ipc.DownloadResult{}, err
	}
	filename := security.SafeFilename(info.Filename)
	target := filepath.Join(dir, filename)
	if st, err := os.Stat(target); err == nil && st.Size() > 0 {
		return ipc.DownloadResult{Path: target, MediaType: info.MediaType, Filename: filename, Size: st.Size(), Cached: true}, nil
	}
	if info.DirectPath == "" && info.URL != "" {
		info.DirectPath = directPathFromURL(info.URL)
	}
	if info.DirectPath == "" || len(info.MediaKey) == 0 || len(info.FileEncSHA256) == 0 {
		return ipc.DownloadResult{}, fmt.Errorf("message %s has incomplete media information; it cannot be downloaded", p.MessageID)
	}
	var waType whatsmeow.MediaType
	switch info.MediaType {
	case "image", "sticker":
		waType = whatsmeow.MediaImage
	case "video":
		waType = whatsmeow.MediaVideo
	case "audio":
		waType = whatsmeow.MediaAudio
	case "document":
		waType = whatsmeow.MediaDocument
	default:
		return ipc.DownloadResult{}, fmt.Errorf("unsupported media type %q", info.MediaType)
	}
	if err := d.requireConnected(); err != nil {
		return ipc.DownloadResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	data, err := d.cli.Download(ctx, &downloadable{info: info, waType: waType})
	if err != nil {
		return ipc.DownloadResult{}, fmt.Errorf("download media of message %s: %w (media older than a few weeks may have expired on WhatsApp's servers)", p.MessageID, err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return ipc.DownloadResult{}, fmt.Errorf("write %s: %w", target, err)
	}
	d.log.Info("media downloaded", "path", target, "bytes", len(data))
	return ipc.DownloadResult{Path: target, MediaType: info.MediaType, Filename: filename, Size: int64(len(data))}, nil
}

// directPathFromURL keeps the query string: whatsmeow appends "&hash=" to it.
func directPathFromURL(u string) string {
	parts := strings.SplitN(u, ".net/", 2)
	if len(parts) < 2 {
		return ""
	}
	return "/" + parts[1]
}
