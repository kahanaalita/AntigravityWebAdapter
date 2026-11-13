package cdp

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// WSConn is a minimal RFC 6455 WebSocket client connection.
type WSConn struct {
	conn       net.Conn
	reader     *bufio.Reader
	writeMu    sync.Mutex
	closed     bool
	closeMu    sync.Mutex
	onMessage  func([]byte)
	onError    func(error)
	closeCh    chan struct{}
}

// DialWS connects to a WebSocket URL (ws://...).
func DialWS(rawURL string) (*WSConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket url: %w", err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("websocket dial tcp failed: %w", err)
	}

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	// Generate Sec-WebSocket-Key
	rawKey := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, rawKey); err != nil {
		conn.Close()
		return nil, err
	}
	secKey := base64.StdEncoding.EncodeToString(rawKey)

	// Calculate expected accept header
	h := sha1.New()
	h.Write([]byte(secKey + wsGUID))
	expectedAccept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	req := fmt.Sprintf("GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", path, u.Host, secKey)

	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake write error: %w", err)
	}

	reader := bufio.NewReader(conn)
	// Read HTTP response status
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake status read error: %w", err)
	}

	if !strings.Contains(statusLine, "101") {
		conn.Close()
		return nil, fmt.Errorf("websocket unexpected handshake response: %s", strings.TrimSpace(statusLine))
	}

	// Read headers
	acceptOk := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("websocket handshake header read error: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Sec-WebSocket-Accept") {
			if strings.TrimSpace(parts[1]) == expectedAccept {
				acceptOk = true
			}
		}
	}

	if !acceptOk {
		conn.Close()
		return nil, errors.New("websocket handshake missing or invalid Sec-WebSocket-Accept header")
	}

	ws := &WSConn{
		conn:    conn,
		reader:  reader,
		closeCh: make(chan struct{}),
	}

	return ws, nil
}

// SetHandlers sets callbacks for incoming text messages and errors.
func (ws *WSConn) SetHandlers(onMsg func([]byte), onErr func(error)) {
	ws.onMessage = onMsg
	ws.onError = onErr
}

// StartReadLoop starts reading incoming frames in a background goroutine.
func (ws *WSConn) StartReadLoop() {
	go ws.readLoop()
}

func (ws *WSConn) readLoop() {
	var fragBuf []byte

	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(ws.reader, header); err != nil {
			ws.handleError(err)
			return
		}

		fin := (header[0] & 0x80) != 0
		opcode := header[0] & 0x0F
		masked := (header[1] & 0x80) != 0
		payloadLen := uint64(header[1] & 0x7F)

		if payloadLen == 126 {
			ext := make([]byte, 2)
			if _, err := io.ReadFull(ws.reader, ext); err != nil {
				ws.handleError(err)
				return
			}
			payloadLen = uint64(binary.BigEndian.Uint16(ext))
		} else if payloadLen == 127 {
			ext := make([]byte, 8)
			if _, err := io.ReadFull(ws.reader, ext); err != nil {
				ws.handleError(err)
				return
			}
			payloadLen = binary.BigEndian.Uint64(ext)
		}

		var maskKey []byte
		if masked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(ws.reader, maskKey); err != nil {
				ws.handleError(err)
				return
			}
		}

		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(ws.reader, payload); err != nil {
			ws.handleError(err)
			return
		}

		if masked {
			for i := uint64(0); i < payloadLen; i++ {
				payload[i] ^= maskKey[i%4]
			}
		}

		switch opcode {
		case opClose:
			ws.Close()
			return
		case opPing:
			_ = ws.writeFrame(opPong, payload)
		case opPong:
			// no-op
		case opContinuation:
			fragBuf = append(fragBuf, payload...)
			if fin {
				complete := fragBuf
				fragBuf = nil
				if ws.onMessage != nil {
					ws.onMessage(complete)
				}
			}
		case opText, opBinary:
			if fin {
				if ws.onMessage != nil {
					ws.onMessage(payload)
				}
			} else {
				fragBuf = append(fragBuf, payload...)
			}
		}
	}
}

// SendText sends a text frame to the server.
func (ws *WSConn) SendText(text string) error {
	return ws.writeFrame(opText, []byte(text))
}

func (ws *WSConn) writeFrame(opcode byte, data []byte) error {
	ws.writeMu.Lock()
	defer ws.writeMu.Unlock()

	ws.closeMu.Lock()
	if ws.closed {
		ws.closeMu.Unlock()
		return errors.New("websocket connection is closed")
	}
	ws.closeMu.Unlock()

	length := len(data)
	var header []byte

	b0 := byte(0x80 | (opcode & 0x0F)) // FIN = 1
	// Client to server MUST be masked
	if length <= 125 {
		header = []byte{b0, byte(0x80 | length)}
	} else if length <= 65535 {
		header = make([]byte, 4)
		header[0] = b0
		header[1] = byte(0x80 | 126)
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	} else {
		header = make([]byte, 10)
		header[0] = b0
		header[1] = byte(0x80 | 127)
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}

	mask := make([]byte, 4)
	if _, err := io.ReadFull(rand.Reader, mask); err != nil {
		return err
	}
	header = append(header, mask...)

	maskedData := make([]byte, length)
	for i := 0; i < length; i++ {
		maskedData[i] = data[i] ^ mask[i%4]
	}

	if _, err := ws.conn.Write(header); err != nil {
		return err
	}
	if _, err := ws.conn.Write(maskedData); err != nil {
		return err
	}

	return nil
}

func (ws *WSConn) handleError(err error) {
	ws.closeMu.Lock()
	defer ws.closeMu.Unlock()
	if !ws.closed {
		ws.closed = true
		close(ws.closeCh)
		if ws.onError != nil {
			ws.onError(err)
		}
	}
}

// Close closes the WebSocket connection.
func (ws *WSConn) Close() error {
	ws.closeMu.Lock()
	if ws.closed {
		ws.closeMu.Unlock()
		return nil
	}
	ws.closed = true
	close(ws.closeCh)
	ws.closeMu.Unlock()

	// Send close frame
	_ = ws.writeFrame(opClose, []byte{})
	return ws.conn.Close()
}
