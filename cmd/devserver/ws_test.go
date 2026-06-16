package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"fishingheroes/internal/domain"
)

// Exemplo canônico da RFC 6455.
func TestWSAcceptRFC(t *testing.T) {
	if got := wsAccept("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("Sec-WebSocket-Accept incorreto: %s", got)
	}
}

// Handshake + recebimento do snapshot inicial de estado pelo canal WS.
func TestWSStreamsInitialState(t *testing.T) {
	s := &server{engine: domain.NewEngine(domain.DefaultConfig())}
	s.reset()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	req := "GET /ws HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("handshake sem 101: %q", status)
	}
	for { // consome o resto dos headers
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}

	payload, err := readServerTextFrame(br)
	if err != nil {
		t.Fatalf("lendo frame: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("payload não é JSON: %v (%q)", err, payload)
	}
	if msg["type"] != "state" {
		t.Fatalf("esperava type=state, veio %v", msg["type"])
	}
	if _, ok := msg["state"]; !ok {
		t.Fatal("mensagem de estado sem campo 'state'")
	}
}

// readServerTextFrame lê um frame de texto não-mascarado (servidor→cliente).
func readServerTextFrame(br *bufio.Reader) ([]byte, error) {
	if _, err := br.ReadByte(); err != nil { // b0: FIN+opcode
		return nil, err
	}
	b1, err := br.ReadByte()
	if err != nil {
		return nil, err
	}
	n := int(b1 & 0x7F)
	switch n {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(br, ext); err != nil {
			return nil, err
		}
		n = int(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(br, ext); err != nil {
			return nil, err
		}
		n = int(binary.BigEndian.Uint64(ext))
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
