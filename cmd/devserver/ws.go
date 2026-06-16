package main

// WebSocket mínimo (RFC 6455), só o necessário para o canal de foreground:
// handshake + envio de frames de texto (servidor→cliente) + leitura de
// close/ping do cliente. Sem dependências externas. Para produção/Godot, a
// troca por uma lib (ex.: coder/websocket) é direta — o protocolo é o mesmo.

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const wsMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	rw  *bufio.ReadWriter
	c   io.Closer
	wmu sync.Mutex // serializa escritas (loop principal + pong)
}

// wsAccept calcula o Sec-WebSocket-Accept a partir da chave do cliente.
func wsAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsMagic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// wsUpgrade faz o handshake e assume a conexão (hijack).
func wsUpgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("requisição não é um upgrade websocket")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("sem Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("ResponseWriter não suporta hijack")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + wsAccept(key) + "\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		conn.Close()
		return nil, err
	}
	return &wsConn{rw: rw, c: conn}, nil
}

// writeFrame envia um frame não-mascarado (servidor→cliente).
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	n := len(payload)
	var hdr []byte
	switch {
	case n < 126:
		hdr = []byte{0x80 | opcode, byte(n)}
	case n < 65536:
		hdr = []byte{0x80 | opcode, 126, 0, 0}
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = make([]byte, 10)
		hdr[0] = 0x80 | opcode
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	if _, err := c.rw.Write(hdr); err != nil {
		return err
	}
	if _, err := c.rw.Write(payload); err != nil {
		return err
	}
	return c.rw.Flush()
}

func (c *wsConn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, b) // 0x1 = text
}

// readLoop lê frames do cliente até a conexão fechar. Trata close/ping;
// ignora frames de dados (o cliente dev não envia comandos).
func (c *wsConn) readLoop() {
	for {
		b0, err := c.rw.ReadByte()
		if err != nil {
			return
		}
		b1, err := c.rw.ReadByte()
		if err != nil {
			return
		}
		opcode := b0 & 0x0F
		masked := b1&0x80 != 0
		n := int(b1 & 0x7F)
		switch n {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(c.rw, ext); err != nil {
				return
			}
			n = int(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(c.rw, ext); err != nil {
				return
			}
			n = int(binary.BigEndian.Uint64(ext))
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(c.rw, mask[:]); err != nil {
				return
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.rw, payload); err != nil {
			return
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch opcode {
		case 0x8: // close
			return
		case 0x9: // ping → pong
			_ = c.writeFrame(0xA, payload)
		}
	}
}

func (c *wsConn) Close() error { return c.c.Close() }
