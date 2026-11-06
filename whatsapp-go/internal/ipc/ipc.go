// Package ipc is the line-delimited JSON protocol spoken over the daemon's
// Unix socket. The MCP server is the only client. One request per
// connection: the client writes a Request line, reads a Response line and
// closes. The socket lives in the private data directory with mode 0600, so
// only the user's own processes can reach it; it is never a TCP port.
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"whatsapp-mcp/internal/store"
)

// Methods.
const (
	MethodPing          = "ping"
	MethodStatus        = "status"
	MethodSendMessage   = "send_message"
	MethodSendFile      = "send_file"
	MethodSendAudio     = "send_audio"
	MethodDownloadMedia = "download_media"
	MethodPair          = "pair_phone"
)

// Request is one call.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the reply; exactly one of Error and Result is meaningful.
type Response struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// SendMessageParams sends a text message.
type SendMessageParams struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// SendFileParams sends a file (image, video, document, raw audio) with an optional caption.
type SendFileParams struct {
	To      string `json:"to"`
	Path    string `json:"path"`
	Caption string `json:"caption,omitempty"`
}

// SendAudioParams sends a voice note (converted to Opus when needed).
type SendAudioParams struct {
	To   string `json:"to"`
	Path string `json:"path"`
}

// SendResult reports a sent message.
type SendResult struct {
	MessageID string    `json:"message_id"`
	To        string    `json:"to"`
	Timestamp time.Time `json:"timestamp"`
}

// DownloadParams identifies a media message.
type DownloadParams struct {
	MessageID string `json:"message_id"`
	ChatJID   string `json:"chat_jid"`
}

// DownloadResult is where the media was written.
type DownloadResult struct {
	Path      string `json:"path"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	// Cached is true when the file was already on disk.
	Cached bool `json:"cached"`
}

// StatusResult describes the daemon.
type StatusResult struct {
	Connected bool         `json:"connected"`
	LoggedIn  bool         `json:"logged_in"`
	JID       string       `json:"jid,omitempty"`
	LID       string       `json:"lid,omitempty"`
	SendRoot  string       `json:"send_root"`
	MediaDir  string       `json:"media_dir"`
	Counts    store.Counts `json:"counts"`
	Uptime    string       `json:"uptime"`
	// LoggedOut is true when WhatsApp revoked this device; restart the daemon to pair again.
	LoggedOut bool `json:"logged_out,omitempty"`
	// Pairing is true while a phone-pairing code is waiting to be entered.
	Pairing   bool   `json:"pairing,omitempty"`
	PairError string `json:"pair_error,omitempty"`
}

// PairParams asks for a phone-pairing code.
type PairParams struct {
	Phone string `json:"phone"`
}

// PairResult carries the code to type in WhatsApp.
type PairResult struct {
	Code          string    `json:"code,omitempty"`
	ExpiresAt     time.Time `json:"expires_at,omitempty"`
	AlreadyPaired bool      `json:"already_paired,omitempty"`
	JID           string    `json:"jid,omitempty"`
}

// Handler serves requests; it returns the result value or an error whose
// text is sent to the client verbatim.
type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (any, error)
}

// Serve listens on the Unix socket at path until ctx is cancelled.
func Serve(ctx context.Context, path string, h Handler, log *slog.Logger) error {
	// macOS limits Unix socket paths to 104 bytes (sun_path), Linux to 108.
	if len(path) >= 104 {
		return fmt.Errorf("socket path %s is %d bytes long, over the 103-byte limit for Unix sockets: choose a shorter --data-dir", path, len(path))
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("chmod socket %s: %w", path, err)
	}
	go func() {
		<-ctx.Done()
		ln.Close()
		os.Remove(path)
	}()
	log.Info("socket listening", "path", path)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warn("socket accept failed", "error", err)
			continue
		}
		go serveConn(ctx, conn, h, log)
	}
}

func serveConn(ctx context.Context, conn net.Conn, h Handler, log *slog.Logger) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Minute))
	reader := bufio.NewReaderSize(conn, 1<<20)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req Request
	resp := Response{}
	if err := json.Unmarshal(line, &req); err != nil {
		resp.Error = "malformed request: " + err.Error()
	} else {
		result, err := h.Handle(ctx, req.Method, req.Params)
		if err != nil {
			log.Warn("request failed", "method", req.Method, "error", err)
			resp.Error = err.Error()
		} else {
			raw, err := json.Marshal(result)
			if err != nil {
				resp.Error = "encode result: " + err.Error()
			} else {
				resp.OK, resp.Result = true, raw
			}
		}
	}
	out, _ := json.Marshal(resp)
	_, _ = conn.Write(append(out, '\n'))
}

// Client calls the daemon.
type Client struct {
	Path string
	// Timeout bounds one call (uploads of large files can take a while).
	Timeout time.Duration
}

// NewClient returns a client for the socket at path.
func NewClient(path string) *Client {
	return &Client{Path: path, Timeout: 5 * time.Minute}
}

// Call sends method with params and decodes the result into out (may be nil).
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", c.Path)
	if err != nil {
		return fmt.Errorf("the whatsapp-mcp daemon is not reachable at %s: start it with `whatsapp-mcp daemon` or `whatsapp-mcp install-service` (%w)", c.Path, err)
	}
	defer conn.Close()
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	line, _ := json.Marshal(Request{Method: method, Params: raw})
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("send request to daemon: %w", err)
	}
	respLine, err := bufio.NewReaderSize(conn, 1<<20).ReadBytes('\n')
	if err != nil && len(respLine) == 0 {
		return fmt.Errorf("read daemon response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return fmt.Errorf("decode daemon response: %w", err)
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode daemon result: %w", err)
		}
	}
	return nil
}

// Sender is the subset of the client the MCP server depends on (mocked in tests).
type Sender interface {
	SendMessage(ctx context.Context, p SendMessageParams) (SendResult, error)
	SendFile(ctx context.Context, p SendFileParams) (SendResult, error)
	SendAudio(ctx context.Context, p SendAudioParams) (SendResult, error)
	DownloadMedia(ctx context.Context, p DownloadParams) (DownloadResult, error)
	Status(ctx context.Context) (StatusResult, error)
	Pair(ctx context.Context, p PairParams) (PairResult, error)
}

// Pair implements Sender.
func (c *Client) Pair(ctx context.Context, p PairParams) (PairResult, error) {
	var r PairResult
	return r, c.Call(ctx, MethodPair, p, &r)
}

// SendMessage implements Sender.
func (c *Client) SendMessage(ctx context.Context, p SendMessageParams) (SendResult, error) {
	var r SendResult
	return r, c.Call(ctx, MethodSendMessage, p, &r)
}

// SendFile implements Sender.
func (c *Client) SendFile(ctx context.Context, p SendFileParams) (SendResult, error) {
	var r SendResult
	return r, c.Call(ctx, MethodSendFile, p, &r)
}

// SendAudio implements Sender.
func (c *Client) SendAudio(ctx context.Context, p SendAudioParams) (SendResult, error) {
	var r SendResult
	return r, c.Call(ctx, MethodSendAudio, p, &r)
}

// DownloadMedia implements Sender.
func (c *Client) DownloadMedia(ctx context.Context, p DownloadParams) (DownloadResult, error) {
	var r DownloadResult
	return r, c.Call(ctx, MethodDownloadMedia, p, &r)
}

// Status implements Sender.
func (c *Client) Status(ctx context.Context) (StatusResult, error) {
	var r StatusResult
	return r, c.Call(ctx, MethodStatus, struct{}{}, &r)
}
