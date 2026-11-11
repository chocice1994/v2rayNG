// Package anytlsbridge adapts explicitly marked AnyTLS outbounds to local SOCKS.
// It is linked into the same gomobile library as Xray, not a second Go AAR.
package anytlsbridge

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	anytls "github.com/anytls/sing-anytls"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
)

// Prepared owns all listeners and sessions for one core config. Close it after
// the core stops, and on every failed start. Never log Config: it has credentials.
type Prepared struct {
	config  string
	bridges []*bridge
	once    sync.Once
}

func (p *Prepared) Config() string { return p.config }
func (p *Prepared) Close() error {
	p.once.Do(func() {
		for _, b := range p.bridges {
			b.close()
		}
	})
	return nil
}

type settings struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}
type stream struct {
	Network  string `json:"network"`
	Security string `json:"security"`
	TLS      *struct {
		ServerName    string `json:"serverName"`
		AllowInsecure bool   `json:"allowInsecure"`
	} `json:"tlsSettings"`
}

// Prepare preserves ordinary configs byte-for-byte. For AnyTLS, TLS and a
// nonempty password are mandatory. This preview supports TCP only: UDP requests
// are rejected, never sent directly. It does not implement Xray transport/Mux.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

func Prepare(config string, dial DialContextFunc) (*Prepared, error) {
	return prepare(config, dial, start)
}

func prepare(config string, dial DialContextFunc, starter func(settings, *tls.Config, DialContextFunc) (*bridge, error)) (_ *Prepared, err error) {
	p := &Prepared{config: config}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	var root map[string]json.RawMessage
	if err = json.Unmarshal([]byte(config), &root); err != nil {
		// Xray owns JSONC and legacy syntax. AnyTLS JSONC remains unsupported:
		// returning it unchanged makes Xray reject its unknown protocol, not bypass it.
		return p, nil
	}
	var outs []map[string]json.RawMessage
	if raw, ok := root["outbounds"]; ok {
		if err = json.Unmarshal(raw, &outs); err != nil {
			return nil, errors.New("invalid outbounds")
		}
	}
	changed := false
	for _, out := range outs {
		var protocol string
		if err = json.Unmarshal(out["protocol"], &protocol); err != nil {
			continue
		}
		if !strings.EqualFold(protocol, "anytls") {
			continue
		}
		for key, raw := range out {
			switch key {
			case "tag", "protocol", "settings", "streamSettings", "mux":
			case "targetStrategy":
				var value string
				if json.Unmarshal(raw, &value) != nil || (value != "" && !strings.EqualFold(value, "AsIs")) {
					return nil, errors.New("AnyTLS target strategy is unsupported")
				}
			default:
				return nil, errors.New("unsupported AnyTLS outbound field")
			}
		}
		var s settings
		if err = strictDecode(out["settings"], &s); err != nil {
			return nil, errors.New("invalid AnyTLS settings")
		}
		if s.Address == "" || strings.ContainsAny(s.Address, "\x00\r\n") || s.Port < 1 || s.Port > 65535 || s.Password == "" {
			return nil, errors.New("invalid AnyTLS server, port or password")
		}
		var st stream
		if err = strictDecode(out["streamSettings"], &st); err != nil {
			return nil, errors.New("unsupported AnyTLS transport settings")
		}
		if st.Security != "tls" || st.TLS == nil || (st.Network != "" && st.Network != "tcp" && st.Network != "raw") {
			return nil, errors.New("AnyTLS requires plain TLS transport")
		}
		if raw := out["mux"]; len(raw) > 0 && string(raw) != "null" {
			var m struct {
				Enabled bool `json:"enabled"`
			}
			if json.Unmarshal(raw, &m) != nil || m.Enabled {
				return nil, errors.New("Xray mux is unsupported for AnyTLS")
			}
		}
		if len(out["proxySettings"]) > 0 || len(out["sendThrough"]) > 0 {
			return nil, errors.New("AnyTLS outbound chaining and source binding are unsupported")
		}
		name := st.TLS.ServerName
		if name == "" {
			name = s.Address
		}
		if dial == nil {
			return nil, errors.New("AnyTLS requires a protected dialer")
		}
		b, startErr := starter(s, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name, InsecureSkipVerify: st.TLS.AllowInsecure}, dial)
		if startErr != nil {
			return nil, errors.New("cannot start AnyTLS bridge")
		}
		p.bridges = append(p.bridges, b)
		out["protocol"] = json.RawMessage(`"socks"`)
		port := b.listener.Addr().(*net.TCPAddr).Port
		out["settings"], err = json.Marshal(map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]string{"user": b.user, "pass": b.password}}}}})
		if err != nil {
			return nil, errors.New("cannot encode bridge settings")
		}
		delete(out, "streamSettings")
		delete(out, "mux")
		changed = true
	}
	if changed {
		root["outbounds"], err = json.Marshal(outs)
		if err != nil {
			return nil, err
		}
		var data []byte
		data, err = json.Marshal(root)
		if err != nil {
			return nil, err
		}
		p.config = string(data)
	}
	return p, nil
}
func strictDecode(raw []byte, target any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(target)
}

type bridge struct {
	listener       net.Listener
	client         *anytls.Client
	user, password string
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	closed         bool
	connections    map[net.Conn]struct{}
	wg             sync.WaitGroup
}

func start(s settings, tc *tls.Config, dial DialContextFunc) (*bridge, error) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &bridge{ctx: ctx, cancel: cancel, connections: make(map[net.Conn]struct{})}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		cancel()
		return nil, err
	}
	b.user = hex.EncodeToString(token[:16])
	b.password = hex.EncodeToString(token[16:])

	var err error
	b.client, err = anytls.NewClient(ctx, anytls.ClientConfig{Password: s.Password, IdleSessionCheckInterval: 30 * time.Second, IdleSessionTimeout: 30 * time.Second, MinIdleSession: 0, Logger: logger.NOP(), DialOut: func(c context.Context) (net.Conn, error) {
		raw, err := dial(c, "tcp", net.JoinHostPort(s.Address, strconv.Itoa(s.Port)))
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, tc)
		if err = conn.HandshakeContext(c); err != nil {
			raw.Close()
			return nil, err
		}
		return conn, nil
	}})
	if err != nil {
		cancel()
		return nil, err
	}
	b.listener, err = net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		cancel()
		b.client.Close()
		return nil, err
	}
	b.wg.Add(1)
	go b.accept()
	return b, nil
}
func (b *bridge) accept() {
	defer b.wg.Done()
	for {
		c, err := b.listener.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			c.Close()
			return
		}
		b.connections[c] = struct{}{}
		b.wg.Add(1)
		b.mu.Unlock()
		go func() {
			defer b.wg.Done()
			defer func() { c.Close(); b.mu.Lock(); delete(b.connections, c); b.mu.Unlock() }()
			b.serve(c)
		}()
	}
}
func (b *bridge) close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.cancel()
	b.listener.Close()
	for c := range b.connections {
		c.Close()
	}
	b.mu.Unlock()
	b.client.Close()
	b.wg.Wait()
}
func readBytes(c net.Conn, n int) ([]byte, error) {
	v := make([]byte, n)
	_, err := io.ReadFull(c, v)
	return v, err
}
func reply(c net.Conn, code byte) { c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
func (b *bridge) serve(c net.Conn) {
	c.SetDeadline(time.Now().Add(15 * time.Second))
	h, err := readBytes(c, 2)
	if err != nil || h[0] != 5 || h[1] == 0 {
		return
	}
	methods, err := readBytes(c, int(h[1]))
	if err != nil {
		return
	}
	auth := false
	for _, m := range methods {
		if m == 2 {
			auth = true
		}
	}
	if !auth {
		c.Write([]byte{5, 255})
		return
	}
	if _, err = c.Write([]byte{5, 2}); err != nil {
		return
	}
	h, err = readBytes(c, 2)
	if err != nil || h[0] != 1 || h[1] == 0 {
		return
	}
	user, err := readBytes(c, int(h[1]))
	if err != nil {
		return
	}
	size, err := readBytes(c, 1)
	if err != nil || size[0] == 0 {
		return
	}
	password, err := readBytes(c, int(size[0]))
	if err != nil {
		return
	}
	if subtle.ConstantTimeCompare(user, []byte(b.user)) != 1 || subtle.ConstantTimeCompare(password, []byte(b.password)) != 1 {
		c.Write([]byte{1, 1})
		return
	}
	if _, err = c.Write([]byte{1, 0}); err != nil {
		return
	}
	h, err = readBytes(c, 4)
	if err != nil || h[0] != 5 || h[2] != 0 {
		return
	}
	if h[1] != 1 {
		reply(c, 7)
		return
	}
	var host string
	switch h[3] {
	case 1:
		v, e := readBytes(c, 4)
		if e != nil {
			return
		}
		host = net.IP(v).String()
	case 4:
		v, e := readBytes(c, 16)
		if e != nil {
			return
		}
		host = net.IP(v).String()
	case 3:
		v, e := readBytes(c, 1)
		if e != nil || v[0] == 0 {
			return
		}
		v, e = readBytes(c, int(v[0]))
		if e != nil {
			return
		}
		host = string(v)
	default:
		reply(c, 8)
		return
	}
	port, err := readBytes(c, 2)
	if err != nil {
		return
	}
	p := uint16(port[0])<<8 | uint16(port[1])
	if p == 0 {
		reply(c, 1)
		return
	}
	ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
	remote, err := b.client.CreateProxy(ctx, M.ParseSocksaddrHostPort(host, p))
	cancel()
	if err != nil {
		reply(c, 1)
		return
	}
	defer remote.Close()
	reply(c, 0)
	c.SetDeadline(time.Time{})
	// The pinned AnyTLS Stream has no CloseWrite. Until an upstream API adds
	// half-close, EOF closes the complete stream; see preview limitations.
	done := make(chan struct{})
	go func() { io.Copy(remote, c); remote.Close(); close(done) }()
	io.Copy(c, remote)
	c.Close()
	<-done
}

// Version identifies the pinned wire implementation, not Android readiness.
func Version() string { return fmt.Sprintf("AnyTLS bridge TCP preview (sing-anytls %s)", "v0.0.11") }
