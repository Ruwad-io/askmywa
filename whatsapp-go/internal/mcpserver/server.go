// Package mcpserver exposes messages.db and the daemon socket as MCP tools
// over stdio, with the official github.com/modelcontextprotocol/go-sdk.
//
// Every tool has typed input and output (the SDK derives both JSON schemas),
// read tools are annotated ReadOnlyHint, and the three send tools preview
// by default: nothing leaves the account until the same call is repeated
// with confirm=true.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"whatsapp-mcp/internal/config"
	"whatsapp-mcp/internal/ipc"
	"whatsapp-mcp/internal/security"
	"whatsapp-mcp/internal/store"
)

// Version is set by main from the build.
var Version = "dev"

// Server holds the dependencies of the tool handlers.
type Server struct {
	store  *store.Store
	sender ipc.Sender
	svc    ServiceManager
	paths  config.Paths
	log    *slog.Logger

	storeMu sync.Mutex
}

// Instructions is sent to the client at initialisation.
const Instructions = `WhatsApp tools backed by a local message archive.
If WhatsApp is not set up yet, or a tool says it is not connected, call whatsapp_status
and follow its next_step; connect_whatsapp links this computer with a code the user
types on their phone.
Reads (list_chats, list_messages, search_contacts, ...) are instant and local.
Every send tool (send_message, send_file, send_audio_message) only returns a preview
unless confirm=true: show the preview to the user, and call again with confirm=true
only once they approved that exact recipient and content. Never set confirm=true on
the first call. Contacts can be given as a phone number, a JID or a name.`

// New builds the MCP server with every tool registered. st may be nil: the
// archive is then opened on first use, once the daemon has created it.
// svc may be nil when the service cannot be managed (tests, other OSes).
func New(st *store.Store, sender ipc.Sender, svc ServiceManager, paths config.Paths, log *slog.Logger) *mcp.Server {
	s := &Server{store: st, sender: sender, svc: svc, paths: paths, log: log}
	srv := mcp.NewServer(&mcp.Implementation{Name: "whatsapp", Title: "WhatsApp", Version: Version},
		&mcp.ServerOptions{Instructions: Instructions, Logger: log})
	s.registerSetup(srv)
	s.register(srv)
	return srv
}

func readOnly() *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &f, OpenWorldHint: &f, IdempotentHint: true}
}

func sending() *mcp.ToolAnnotations {
	t := true
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &t, OpenWorldHint: &t, IdempotentHint: false}
}

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "search_contacts", Title: "Search contacts", Annotations: readOnly(),
		Description: "Search WhatsApp contacts by name or phone number. Matching is case- and accent-insensitive (\"elodie\" finds \"Élodie\"). Each result gives the best known name, the phone number and/or LID, and the JID to pass to list_messages (chat_jid) or to the send tools (recipient)."},
		withStore(s, s.searchContacts))
	mcp.AddTool(srv, &mcp.Tool{Name: "list_messages", Title: "List messages", Annotations: readOnly(),
		Description: "List stored WhatsApp messages, newest first, optionally filtered by chat, sender, date range and a full-text query (words matched by prefix, accent- and case-insensitive). Results are paginated: pass next_cursor back as cursor while has_more is true. Set include_context to get the neighbouring messages of each hit."},
		withStore(s, s.listMessages))
	mcp.AddTool(srv, &mcp.Tool{Name: "list_chats", Title: "List chats", Annotations: readOnly(),
		Description: "List WhatsApp chats (people and groups), most recently active first or by name, optionally filtered by a name, JID or contact. Paginated by cursor."},
		withStore(s, s.listChats))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_chat", Title: "Get chat", Annotations: readOnly(),
		Description: "Get one chat by JID, with its latest message."},
		withStore(s, s.getChat))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_direct_chat_by_contact", Title: "Direct chat with a contact", Annotations: readOnly(),
		Description: "Get the one-to-one chat with a contact given their phone number, JID or name."},
		withStore(s, s.getDirectChat))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_contact_chats", Title: "Chats involving a contact", Annotations: readOnly(),
		Description: "List every chat involving a contact: the direct chat and the groups they wrote in, most recently active first. Paginated by cursor."},
		withStore(s, s.getContactChats))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_last_interaction", Title: "Last interaction", Annotations: readOnly(),
		Description: "Get the most recent message exchanged with a contact (sent by them anywhere, or in their direct chat)."},
		withStore(s, s.getLastInteraction))
	mcp.AddTool(srv, &mcp.Tool{Name: "get_message_context", Title: "Message context", Annotations: readOnly(),
		Description: "Get the messages before and after a given message in its chat."},
		withStore(s, s.getMessageContext))
	mcp.AddTool(srv, &mcp.Tool{Name: "send_message", Title: "Send a text message", Annotations: sending(),
		Description: "Send a WhatsApp text message to a person or a group.\n\nSafety: with confirm=false (the default) NOTHING is sent; the tool returns a preview (resolved recipient, their name, whether it is a group, the text). Show it to the user and call again with confirm=true only after they approved that exact content. Never set confirm=true on the first call. Recipient: phone number with country code and no plus sign, a JID (123@s.whatsapp.net, 123@g.us for a group) or a contact name."},
		withStore(s, s.sendMessage))
	mcp.AddTool(srv, &mcp.Tool{Name: "send_file", Title: "Send a file", Annotations: sending(),
		Description: "Send a file (image, video, document, or an audio file as a plain attachment) with an optional caption. The file must be under the daemon's send root directory.\n\nSafety: with confirm=false (the default) NOTHING is sent; the tool returns a preview. Show it to the user and call again with confirm=true only after they approved that exact file and recipient. Never set confirm=true on the first call."},
		withStore(s, s.sendFile))
	mcp.AddTool(srv, &mcp.Tool{Name: "send_audio_message", Title: "Send a voice note", Annotations: sending(),
		Description: "Send an audio file as a WhatsApp voice note (converted to Opus with ffmpeg when needed; if ffmpeg is missing use send_file instead). The file must be under the daemon's send root directory.\n\nSafety: with confirm=false (the default) NOTHING is sent; the tool returns a preview. Show it to the user and call again with confirm=true only after they approved. Never set confirm=true on the first call."},
		withStore(s, s.sendAudio))
	mcp.AddTool(srv, &mcp.Tool{Name: "download_media", Title: "Download media", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, IdempotentHint: true, DestructiveHint: new(bool), OpenWorldHint: ptr(true)},
		Description: "Download the media (image, video, audio, document, sticker) attached to a message and return its local path. Use the id and chat_jid of a list_messages result whose media_type is set."},
		withStore(s, s.downloadMedia))
}

func ptr[T any](v T) *T { return &v }

// ---- inputs and outputs ----

// SearchContactsInput is the input of search_contacts.
type SearchContactsInput struct {
	Query  string `json:"query" jsonschema:"part of a name, or digits of a phone number"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of results (default 20, max 200)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

// ContactsOutput is a page of contacts.
type ContactsOutput struct {
	Contacts []store.Contact `json:"contacts"`
	store.Page
}

func (s *Server) searchContacts(ctx context.Context, _ *mcp.CallToolRequest, in SearchContactsInput) (*mcp.CallToolResult, ContactsOutput, error) {
	contacts, page, err := s.store.SearchContacts(ctx, in.Query, in.Limit, in.Cursor)
	if err != nil {
		return nil, ContactsOutput{}, err
	}
	if contacts == nil {
		contacts = []store.Contact{}
	}
	return nil, ContactsOutput{Contacts: contacts, Page: page}, nil
}

// ListMessagesInput is the input of list_messages.
type ListMessagesInput struct {
	ChatJID        string `json:"chat_jid,omitempty" jsonschema:"only messages of this chat (JID from list_chats or search_contacts)"`
	Sender         string `json:"sender,omitempty" jsonschema:"only messages from this person: phone number, JID or contact name"`
	Query          string `json:"query,omitempty" jsonschema:"full-text search; words are matched by prefix, accent- and case-insensitively"`
	After          string `json:"after,omitempty" jsonschema:"only messages after this ISO-8601 date or datetime"`
	Before         string `json:"before,omitempty" jsonschema:"only messages before this ISO-8601 date or datetime"`
	Limit          int    `json:"limit,omitempty" jsonschema:"page size (default 20, max 200)"`
	Cursor         string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
	IncludeContext *bool  `json:"include_context,omitempty" jsonschema:"attach neighbouring messages to each result (default false)"`
	ContextBefore  int    `json:"context_before,omitempty" jsonschema:"messages before each result when include_context is true (default 1)"`
	ContextAfter   int    `json:"context_after,omitempty" jsonschema:"messages after each result when include_context is true (default 1)"`
}

// MessagesOutput is a page of messages.
type MessagesOutput struct {
	Messages []store.Hit `json:"messages"`
	store.Page
}

func (s *Server) listMessages(ctx context.Context, _ *mcp.CallToolRequest, in ListMessagesInput) (*mcp.CallToolResult, MessagesOutput, error) {
	q := store.MessageQuery{ChatJID: strings.TrimSpace(in.ChatJID), Query: in.Query, Limit: in.Limit, Cursor: in.Cursor}
	var err error
	if q.After, err = store.ParseUserTime(in.After, "after"); err != nil {
		return nil, MessagesOutput{}, err
	}
	if q.Before, err = store.ParseUserTime(in.Before, "before"); err != nil {
		return nil, MessagesOutput{}, err
	}
	if strings.TrimSpace(in.Sender) != "" {
		people, err := s.store.ResolveIdentities(ctx, in.Sender)
		if err != nil {
			return nil, MessagesOutput{}, err
		}
		if len(people) == 0 {
			return nil, MessagesOutput{}, fmt.Errorf("sender %q is a group JID: pass it as chat_jid instead", in.Sender)
		}
		q.Senders = people
	}
	if in.IncludeContext != nil && *in.IncludeContext {
		q.ContextBefore, q.ContextAfter = orDefault(in.ContextBefore, 1), orDefault(in.ContextAfter, 1)
	}
	msgs, page, err := s.store.ListMessages(ctx, q)
	if err != nil {
		return nil, MessagesOutput{}, err
	}
	if msgs == nil {
		msgs = []store.Hit{}
	}
	return nil, MessagesOutput{Messages: msgs, Page: page}, nil
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// ListChatsInput is the input of list_chats.
type ListChatsInput struct {
	Query              string `json:"query,omitempty" jsonschema:"filter by chat name, JID, or contact name/number"`
	Limit              int    `json:"limit,omitempty" jsonschema:"page size (default 20, max 200)"`
	Cursor             string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
	IncludeLastMessage *bool  `json:"include_last_message,omitempty" jsonschema:"include the latest message of each chat (default true)"`
	SortBy             string `json:"sort_by,omitempty" jsonschema:"last_active (default) or name"`
}

// ChatsOutput is a page of chats.
type ChatsOutput struct {
	Chats []store.Chat `json:"chats"`
	store.Page
}

func (s *Server) listChats(ctx context.Context, _ *mcp.CallToolRequest, in ListChatsInput) (*mcp.CallToolResult, ChatsOutput, error) {
	chats, page, err := s.store.ListChats(ctx, store.ChatQuery{Query: in.Query, Limit: in.Limit, Cursor: in.Cursor,
		IncludeLastMessage: in.IncludeLastMessage == nil || *in.IncludeLastMessage, SortBy: in.SortBy})
	if err != nil {
		return nil, ChatsOutput{}, err
	}
	if chats == nil {
		chats = []store.Chat{}
	}
	return nil, ChatsOutput{Chats: chats, Page: page}, nil
}

// GetChatInput is the input of get_chat.
type GetChatInput struct {
	ChatJID            string `json:"chat_jid" jsonschema:"JID of the chat"`
	IncludeLastMessage *bool  `json:"include_last_message,omitempty" jsonschema:"include the latest message (default true)"`
}

func (s *Server) getChat(ctx context.Context, _ *mcp.CallToolRequest, in GetChatInput) (*mcp.CallToolResult, store.Chat, error) {
	jid := strings.TrimSpace(in.ChatJID)
	if jid == "" {
		return nil, store.Chat{}, errors.New("chat_jid is required: take it from list_chats or search_contacts")
	}
	ch, ok, err := s.store.GetChat(ctx, jid, in.IncludeLastMessage == nil || *in.IncludeLastMessage)
	if err != nil {
		return nil, store.Chat{}, err
	}
	if !ok {
		if !strings.Contains(jid, "@") {
			if people, err := s.store.ResolveIdentities(ctx, jid); err == nil && len(people) > 0 {
				if ch, ok, _ := s.store.DirectChat(ctx, people, true); ok {
					return nil, ch, nil
				}
			}
		}
		return nil, store.Chat{}, fmt.Errorf("no chat with JID %q: use list_chats or get_direct_chat_by_contact to find the right JID", jid)
	}
	return nil, ch, nil
}

// ContactInput names one person.
type ContactInput struct {
	Contact string `json:"contact" jsonschema:"phone number, JID or contact name"`
}

func (s *Server) getDirectChat(ctx context.Context, _ *mcp.CallToolRequest, in ContactInput) (*mcp.CallToolResult, store.Chat, error) {
	people, err := s.resolvePeople(ctx, in.Contact)
	if err != nil {
		return nil, store.Chat{}, err
	}
	ch, ok, err := s.store.DirectChat(ctx, people, true)
	if err != nil {
		return nil, store.Chat{}, err
	}
	if !ok {
		return nil, store.Chat{}, fmt.Errorf("no direct chat stored with %q yet; the contact exists but no one-to-one message has been archived", in.Contact)
	}
	return nil, ch, nil
}

// ContactChatsInput is the input of get_contact_chats.
type ContactChatsInput struct {
	Contact string `json:"contact" jsonschema:"phone number, JID or contact name"`
	Limit   int    `json:"limit,omitempty" jsonschema:"page size (default 20, max 200)"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (s *Server) getContactChats(ctx context.Context, _ *mcp.CallToolRequest, in ContactChatsInput) (*mcp.CallToolResult, ChatsOutput, error) {
	people, err := s.resolvePeople(ctx, in.Contact)
	if err != nil {
		return nil, ChatsOutput{}, err
	}
	chats, page, err := s.store.ContactChats(ctx, people, in.Limit, in.Cursor)
	if err != nil {
		return nil, ChatsOutput{}, err
	}
	if chats == nil {
		chats = []store.Chat{}
	}
	return nil, ChatsOutput{Chats: chats, Page: page}, nil
}

// LastInteractionOutput is the output of get_last_interaction.
type LastInteractionOutput struct {
	Found   bool           `json:"found"`
	Message *store.Message `json:"message,omitempty"`
}

func (s *Server) getLastInteraction(ctx context.Context, _ *mcp.CallToolRequest, in ContactInput) (*mcp.CallToolResult, LastInteractionOutput, error) {
	people, err := s.resolvePeople(ctx, in.Contact)
	if err != nil {
		return nil, LastInteractionOutput{}, err
	}
	msg, ok, err := s.store.LastInteraction(ctx, people)
	if err != nil {
		return nil, LastInteractionOutput{}, err
	}
	if !ok {
		return nil, LastInteractionOutput{Found: false}, nil
	}
	return nil, LastInteractionOutput{Found: true, Message: &msg}, nil
}

// MessageContextInput is the input of get_message_context.
type MessageContextInput struct {
	MessageID string `json:"message_id" jsonschema:"id of the message (from list_messages)"`
	ChatJID   string `json:"chat_jid,omitempty" jsonschema:"chat of the message; recommended, ids are only unique per chat"`
	Before    int    `json:"before,omitempty" jsonschema:"messages to include before (default 5)"`
	After     int    `json:"after,omitempty" jsonschema:"messages to include after (default 5)"`
}

func (s *Server) getMessageContext(ctx context.Context, _ *mcp.CallToolRequest, in MessageContextInput) (*mcp.CallToolResult, store.MessageContext, error) {
	if strings.TrimSpace(in.MessageID) == "" {
		return nil, store.MessageContext{}, errors.New("message_id is required: take it from a list_messages result")
	}
	mc, err := s.store.GetMessageContext(ctx, in.MessageID, strings.TrimSpace(in.ChatJID), orDefault(in.Before, 5), orDefault(in.After, 5))
	if err != nil {
		return nil, store.MessageContext{}, err
	}
	return nil, mc, nil
}

func (s *Server) resolvePeople(ctx context.Context, contact string) ([]store.Identity, error) {
	people, err := s.store.ResolveIdentities(ctx, contact)
	if err != nil {
		return nil, err
	}
	if len(people) == 0 {
		return nil, fmt.Errorf("%q is a group JID, not a contact: use list_messages with chat_jid for groups", contact)
	}
	return people, nil
}

// ---- sending ----

// Preview describes what a send would do.
type Preview struct {
	Recipient     string `json:"recipient" jsonschema:"the recipient as given"`
	RecipientJID  string `json:"recipient_jid" jsonschema:"the JID the message would be sent to"`
	RecipientName string `json:"recipient_name" jsonschema:"resolved name of the person or group, or a note when unknown"`
	IsGroup       bool   `json:"is_group"`
	Message       string `json:"message,omitempty" jsonschema:"text that would be sent"`
	FilePath      string `json:"file_path,omitempty" jsonschema:"file that would be sent"`
	FileSize      int64  `json:"file_size,omitempty"`
	Caption       string `json:"caption,omitempty"`
	Warning       string `json:"warning,omitempty" jsonschema:"anything the user should know before confirming"`
}

// SendOutput is the output of every send tool.
type SendOutput struct {
	Sent              bool       `json:"sent" jsonschema:"true only when the message left the account"`
	NeedsConfirmation bool       `json:"needs_confirmation" jsonschema:"true when this was a preview: repeat the call with confirm=true after the user approved"`
	Preview           *Preview   `json:"preview,omitempty"`
	MessageID         string     `json:"message_id,omitempty"`
	Timestamp         *time.Time `json:"timestamp,omitempty" jsonschema:"server timestamp of the sent message"`
	Message           string     `json:"message" jsonschema:"human-readable status"`
}

// SendMessageInput is the input of send_message.
type SendMessageInput struct {
	Recipient string `json:"recipient" jsonschema:"phone number with country code (no +), a JID (x@s.whatsapp.net or x@g.us) or a contact name"`
	Message   string `json:"message" jsonschema:"text to send"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"false (default) only previews; true sends, after the user approved the preview"`
}

// SendFileInput is the input of send_file.
type SendFileInput struct {
	Recipient string `json:"recipient" jsonschema:"phone number with country code (no +), a JID or a contact name"`
	MediaPath string `json:"media_path" jsonschema:"absolute path of the file, under the send root directory"`
	Caption   string `json:"caption,omitempty" jsonschema:"optional caption (images, videos, documents)"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"false (default) only previews; true sends, after the user approved the preview"`
}

// SendAudioInput is the input of send_audio_message.
type SendAudioInput struct {
	Recipient string `json:"recipient" jsonschema:"phone number with country code (no +), a JID or a contact name"`
	MediaPath string `json:"media_path" jsonschema:"absolute path of the audio file, under the send root directory"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"false (default) only previews; true sends, after the user approved the preview"`
}

const previewNote = "Nothing was sent. Show this preview to the user and, only once they approve this exact recipient and content, call again with confirm=true."

// resolveRecipient maps a phone number, JID or name to the JID to send to.
func (s *Server) resolveRecipient(ctx context.Context, recipient string) (Preview, error) {
	r := strings.TrimSpace(recipient)
	if r == "" {
		return Preview{}, errors.New("recipient is empty: pass a phone number, a JID or a contact name")
	}
	p := Preview{Recipient: recipient}
	if strings.Contains(r, "@") {
		user, server := store.SplitJID(r)
		switch server {
		case "g.us":
			p.RecipientJID, p.IsGroup = user+"@g.us", true
			name, _ := s.store.ChatName(ctx, p.RecipientJID)
			if name == "" {
				p.RecipientName = "unknown group (no message from it is archived)"
				p.Warning = "this group JID is not in the archive; double-check it"
			} else {
				p.RecipientName = name
			}
			return p, nil
		case "s.whatsapp.net", "c.us", "lid":
			people, err := s.store.ResolveIdentities(ctx, r)
			if err != nil {
				return Preview{}, err
			}
			return s.previewPerson(ctx, p, people[0])
		}
		return Preview{}, fmt.Errorf("unsupported recipient JID %q: use x@s.whatsapp.net, x@lid or x@g.us", r)
	}
	people, err := s.store.ResolveIdentities(ctx, r)
	if err != nil {
		return Preview{}, err
	}
	if len(people) > 1 {
		var names []string
		for _, id := range people {
			c, _, _ := s.store.LookupContact(ctx, id.Phone, id.LID)
			names = append(names, fmt.Sprintf("%s (%s)", c.Name, firstJID(id)))
		}
		return Preview{}, fmt.Errorf("%q matches several contacts: %s. Pass the phone number or JID of the right one", r, strings.Join(names, "; "))
	}
	return s.previewPerson(ctx, p, people[0])
}

func firstJID(id store.Identity) string {
	if j := id.JIDs(); len(j) > 0 {
		return j[0]
	}
	return ""
}

func (s *Server) previewPerson(ctx context.Context, p Preview, id store.Identity) (Preview, error) {
	c, known, err := s.store.LookupContact(ctx, id.Phone, id.LID)
	if err != nil {
		return Preview{}, err
	}
	if ch, ok, _ := s.store.DirectChat(ctx, []store.Identity{id}, false); ok {
		p.RecipientJID = ch.JID
	} else {
		p.RecipientJID = firstJID(id)
		p.Warning = "no previous conversation with this recipient is archived"
	}
	switch {
	case known && c.Name != "":
		p.RecipientName = c.Name
	case known:
		p.RecipientName = "unknown name (contact without a name)"
	default:
		p.RecipientName = "unknown (not in contacts)"
		if p.Warning == "" {
			p.Warning = "this number is not in the contacts; double-check it"
		}
	}
	return p, nil
}

func (s *Server) sendMessage(ctx context.Context, _ *mcp.CallToolRequest, in SendMessageInput) (*mcp.CallToolResult, SendOutput, error) {
	if strings.TrimSpace(in.Message) == "" {
		return nil, SendOutput{}, errors.New("message is empty")
	}
	p, err := s.resolveRecipient(ctx, in.Recipient)
	if err != nil {
		return nil, SendOutput{}, err
	}
	p.Message = in.Message
	if !in.Confirm {
		return nil, SendOutput{NeedsConfirmation: true, Preview: &p, Message: previewNote}, nil
	}
	res, err := s.sender.SendMessage(ctx, ipc.SendMessageParams{To: p.RecipientJID, Text: in.Message})
	if err != nil {
		return nil, SendOutput{}, err
	}
	return nil, SendOutput{Sent: true, MessageID: res.MessageID, Timestamp: &res.Timestamp, Message: "Message sent to " + p.RecipientName + " (" + res.To + ")"}, nil
}

func (s *Server) checkFile(ctx context.Context, path string) (string, int64, error) {
	resolved, err := security.ValidateSendPath(path, s.sendRoot(ctx))
	if err != nil {
		return "", 0, err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", 0, err
	}
	return resolved, st.Size(), nil
}

func (s *Server) sendFile(ctx context.Context, _ *mcp.CallToolRequest, in SendFileInput) (*mcp.CallToolResult, SendOutput, error) {
	path, size, err := s.checkFile(ctx, in.MediaPath)
	if err != nil {
		return nil, SendOutput{}, err
	}
	p, err := s.resolveRecipient(ctx, in.Recipient)
	if err != nil {
		return nil, SendOutput{}, err
	}
	p.FilePath, p.FileSize, p.Caption = path, size, in.Caption
	if !in.Confirm {
		return nil, SendOutput{NeedsConfirmation: true, Preview: &p, Message: previewNote}, nil
	}
	res, err := s.sender.SendFile(ctx, ipc.SendFileParams{To: p.RecipientJID, Path: path, Caption: in.Caption})
	if err != nil {
		return nil, SendOutput{}, err
	}
	return nil, SendOutput{Sent: true, MessageID: res.MessageID, Timestamp: &res.Timestamp, Message: fmt.Sprintf("File %s sent to %s (%s)", filepath.Base(path), p.RecipientName, res.To)}, nil
}

func (s *Server) sendAudio(ctx context.Context, _ *mcp.CallToolRequest, in SendAudioInput) (*mcp.CallToolResult, SendOutput, error) {
	path, size, err := s.checkFile(ctx, in.MediaPath)
	if err != nil {
		return nil, SendOutput{}, err
	}
	p, err := s.resolveRecipient(ctx, in.Recipient)
	if err != nil {
		return nil, SendOutput{}, err
	}
	p.FilePath, p.FileSize = path, size
	if !in.Confirm {
		return nil, SendOutput{NeedsConfirmation: true, Preview: &p, Message: previewNote + " The audio will be sent as a voice note."}, nil
	}
	res, err := s.sender.SendAudio(ctx, ipc.SendAudioParams{To: p.RecipientJID, Path: path})
	if err != nil {
		return nil, SendOutput{}, err
	}
	return nil, SendOutput{Sent: true, MessageID: res.MessageID, Timestamp: &res.Timestamp, Message: fmt.Sprintf("Voice note %s sent to %s (%s)", filepath.Base(path), p.RecipientName, res.To)}, nil
}

// DownloadInput is the input of download_media.
type DownloadInput struct {
	MessageID string `json:"message_id" jsonschema:"id of the media message (from list_messages)"`
	ChatJID   string `json:"chat_jid" jsonschema:"chat_jid of that message"`
}

// DownloadOutput is the output of download_media.
type DownloadOutput struct {
	Path      string `json:"path" jsonschema:"absolute path of the downloaded file"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	Cached    bool   `json:"cached" jsonschema:"true when the file had already been downloaded"`
}

func (s *Server) downloadMedia(ctx context.Context, _ *mcp.CallToolRequest, in DownloadInput) (*mcp.CallToolResult, DownloadOutput, error) {
	if strings.TrimSpace(in.MessageID) == "" || strings.TrimSpace(in.ChatJID) == "" {
		return nil, DownloadOutput{}, errors.New("message_id and chat_jid are both required: take them from a list_messages result whose media_type is set")
	}
	if _, err := s.store.GetMediaInfo(ctx, in.MessageID, in.ChatJID); err != nil {
		return nil, DownloadOutput{}, err
	}
	res, err := s.sender.DownloadMedia(ctx, ipc.DownloadParams{MessageID: in.MessageID, ChatJID: in.ChatJID})
	if err != nil {
		return nil, DownloadOutput{}, err
	}
	return nil, DownloadOutput{Path: res.Path, MediaType: res.MediaType, Filename: res.Filename, Size: res.Size, Cached: res.Cached}, nil
}
