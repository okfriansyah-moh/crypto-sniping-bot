// Package rpc provides network clients for chain data access.
// ws.go — minimal RFC 6455 WebSocket client (wss:// and ws://).
//
// No external dependencies: uses crypto/tls + bufio for the upgrade,
// encoding/binary for frame parsing, and crypto/rand for masking keys.
//
// Only used internally by solana_rpc.go; not exported.
package rpc

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // SHA-1 required by RFC 6455 §4.2.2
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const wsHandshakeGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// wsConn is a minimal RFC 6455 WebSocket connection.
//
// Supports text and binary frames; automatically responds to ping frames
// with pong; returns io.EOF on close frames.
//
// Concurrent writes are serialised via wmu. Reads are not concurrent-safe
// (each goroutine should own its own wsConn for reading).
//
// If readDeadline > 0, the connection deadline is reset to now+readDeadline
// after every successfully received frame — including pong frames that are
// consumed transparently inside ReadJSON. This prevents an i/o timeout from
// firing when the connection is kept alive exclusively by ping/pong exchanges
// with no data frames arriving.
type wsConn struct {
	conn         net.Conn
	br           *bufio.Reader
	wmu          sync.Mutex
	readDeadline time.Duration // if >0, reset deadline after every received frame
}

// dialWS establishes a WebSocket connection to rawURL.
// Scheme must be wss:// (TLS) or ws:// (plain TCP).
// connectTimeout applies only to the TCP+TLS+HTTP handshake; subsequent I/O
// is governed by per-call deadlines set via setDeadline.
func dialWS(rawURL string, connectTimeout time.Duration) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ws: parse url: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("ws: unsupported scheme %q (want ws:// or wss://)", u.Scheme)
	}

	deadline := time.Now().Add(connectTimeout)
	if connectTimeout == 0 {
		deadline = time.Time{} // no deadline
	}

	host := u.Host

	var conn net.Conn
	switch u.Scheme {
	case "wss":
		if !strings.Contains(host, ":") {
			host = host + ":443"
		}
		d := tls.Dialer{Config: &tls.Config{ServerName: u.Hostname()}} //nolint:gosec
		c, dialErr := d.Dial("tcp", host)
		if dialErr != nil {
			return nil, fmt.Errorf("ws: tls dial %s: %w", host, dialErr)
		}
		conn = c
	case "ws":
		if !strings.Contains(host, ":") {
			host = host + ":80"
		}
		d := &net.Dialer{Timeout: connectTimeout}
		c, dialErr := d.Dial("tcp", host)
		if dialErr != nil {
			return nil, fmt.Errorf("ws: tcp dial %s: %w", host, dialErr)
		}
		conn = c
	}

	// Set a deadline that covers the entire HTTP upgrade exchange.
	if !deadline.IsZero() {
		if err := conn.SetDeadline(deadline); err != nil {
			conn.Close()
			return nil, fmt.Errorf("ws: set deadline: %w", err)
		}
	}

	// Generate a random 16-byte nonce for the Sec-WebSocket-Key header.
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws: generate nonce: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(nonce)

	// Send the HTTP/1.1 upgrade request.
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	upgradeReq := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(upgradeReq)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws: send upgrade request: %w", err)
	}

	// Parse the HTTP response using net/http so header folding etc. is handled.
	br := bufio.NewReaderSize(conn, 8192)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws: read upgrade response: %w", err)
	}
	resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("ws: server rejected upgrade: HTTP %d", resp.StatusCode)
	}

	// Validate Sec-WebSocket-Accept per RFC 6455 §4.2.2.
	got := resp.Header.Get("Sec-WebSocket-Accept")
	want := wsComputeAccept(key)
	if got != want {
		conn.Close()
		return nil, fmt.Errorf("ws: invalid Sec-WebSocket-Accept (got %q, want %q)", got, want)
	}

	// Clear deadline; callers set per-operation deadlines afterwards.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ws: clear deadline: %w", err)
	}

	return &wsConn{conn: conn, br: br}, nil
}

// wsComputeAccept returns the expected Sec-WebSocket-Accept header value.
func wsComputeAccept(key string) string {
	h := sha1.New() //nolint:gosec // required by RFC 6455
	h.Write([]byte(key + wsHandshakeGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// WriteJSON marshals v to JSON and sends it as a masked text frame (opcode=0x1).
// Client-to-server frames MUST be masked per RFC 6455 §5.3.
func (c *wsConn) WriteJSON(v interface{}) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("ws: marshal: %w", err)
	}
	return c.writeFrame(0x1, payload)
}

// writeFrame sends a single complete (FIN=1) frame with a fresh masking key.
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("ws: generate mask: %w", err)
	}

	ln := len(payload)
	// Header byte 0: FIN=1, RSV=0, opcode.
	// Header byte 1: MASK=1, payload length (7-bit or extended).
	var hdr []byte
	hdr = append(hdr, 0x80|opcode)
	switch {
	case ln < 126:
		hdr = append(hdr, byte(0x80|ln))
	case ln <= 65535:
		hdr = append(hdr, 0x80|126, byte(ln>>8), byte(ln))
	default:
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(ln))
		hdr = append(hdr, 0x80|127)
		hdr = append(hdr, ext[:]...)
	}
	hdr = append(hdr, mask[:]...)

	// Apply mask to payload copy (never mutate the caller's slice).
	masked := make([]byte, ln)
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}

	frame := append(hdr, masked...)
	_, err := c.conn.Write(frame)
	return err
}

// ReadJSON reads the next data frame and unmarshals its JSON payload into v.
// Control frames (ping/pong/close) are handled transparently:
//   - ping  → sends a pong and loops
//   - pong  → ignored, loops
//   - close → returns io.EOF
func (c *wsConn) ReadJSON(v interface{}) error {
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return err
		}
		switch opcode {
		case 0x8: // close
			return io.EOF
		case 0x9: // ping → reply with pong
			_ = c.writeFrame(0xA, payload)
			continue
		case 0xA: // pong — ignore
			continue
		case 0x1, 0x2: // text or binary data frame
			return json.Unmarshal(payload, v)
		default:
			// Unknown opcode — skip.
			continue
		}
	}
}

// readFrame reads one complete WebSocket frame from the connection.
// Returns opcode and the (already-unmasked) payload bytes.
func (c *wsConn) readFrame() (opcode byte, payload []byte, err error) {
	// Read 2-byte fixed header.
	var hdr [2]byte
	if _, err = io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, fmt.Errorf("ws: read frame header: %w", err)
	}

	opcode = hdr[0] & 0x0F
	isMasked := hdr[1]&0x80 != 0
	rawLen := int(hdr[1] & 0x7F)

	var payloadLen int
	switch rawLen {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, fmt.Errorf("ws: read 16-bit length: %w", err)
		}
		payloadLen = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, fmt.Errorf("ws: read 64-bit length: %w", err)
		}
		payloadLen = int(binary.BigEndian.Uint64(ext[:]))
	default:
		payloadLen = rawLen
	}

	var maskKey [4]byte
	if isMasked {
		if _, err = io.ReadFull(c.br, maskKey[:]); err != nil {
			return 0, nil, fmt.Errorf("ws: read mask key: %w", err)
		}
	}

	payload = make([]byte, payloadLen)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return 0, nil, fmt.Errorf("ws: read payload (%d bytes): %w", payloadLen, err)
	}

	if isMasked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	// Refresh the read deadline on every successfully received frame.
	// This is critical: pong frames are consumed inside ReadJSON without
	// returning to the caller, so the deadline would not be renewed by
	// the outer setDeadline call in solana_rpc.go. Without this, a
	// connection kept alive only by ping/pong times out after 90 s.
	if c.readDeadline > 0 {
		_ = c.conn.SetDeadline(time.Now().Add(c.readDeadline))
	}

	return opcode, payload, nil
}

// writePing sends a WebSocket ping control frame (opcode 0x9).
// The remote end is expected to reply with a pong (opcode 0xA), which
// ReadJSON handles transparently by looping.
func (c *wsConn) writePing() error {
	return c.writeFrame(0x9, nil)
}

// Close sends a WebSocket close frame and closes the underlying connection.
func (c *wsConn) Close() error {
	_ = c.writeFrame(0x8, nil) // best-effort close frame
	return c.conn.Close()
}

// setDeadline sets a read/write deadline on the underlying connection.
func (c *wsConn) setDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
