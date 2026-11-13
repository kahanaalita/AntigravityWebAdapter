package cdp

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebSocketHandshakeAndEcho(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()

	var wg sync.WaitGroup
	wg.Add(1)

	// Mock server that handles RFC 6455 upgrade and echoes one masked text frame back unmasked
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		var secKey string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Sec-WebSocket-Key") {
				secKey = strings.TrimSpace(parts[1])
			}
		}

		h := sha1.New()
		h.Write([]byte(secKey + wsGUID))
		acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

		resp := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: %s\r\n\r\n", acceptKey)
		conn.Write([]byte(resp))

		// Read client frame (must be masked)
		header := make([]byte, 2)
		reader.Read(header)
		length := int(header[1] & 0x7F)
		mask := make([]byte, 4)
		reader.Read(mask)
		payload := make([]byte, length)
		reader.Read(payload)

		// Unmask
		for i := 0; i < length; i++ {
			payload[i] ^= mask[i%4]
		}

		// Echo back unmasked frame
		echoHeader := []byte{0x81, byte(length)}
		conn.Write(echoHeader)
		conn.Write(payload)
	}()

	client, err := DialWS("ws://" + addr + "/devtools/test")
	if err != nil {
		t.Fatalf("DialWS failed: %v", err)
	}
	defer client.Close()

	received := make(chan string, 1)
	client.SetHandlers(func(msg []byte) {
		received <- string(msg)
	}, func(err error) {
		t.Logf("client error: %v", err)
	})
	client.StartReadLoop()

	testMsg := "Hello Chrome DevTools Protocol!"
	if err := client.SendText(testMsg); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	select {
	case msg := <-received:
		if msg != testMsg {
			t.Fatalf("expected %q, got %q", testMsg, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for echo response")
	}

	wg.Wait()
}
