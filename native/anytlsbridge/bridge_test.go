package anytlsbridge

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	anytls "github.com/anytls/sing-anytls"
	"github.com/anytls/sing-anytls/padding"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const validConfig = `{"outbounds":[{"tag":"proxy","protocol":"anytls","settings":{"address":"example.invalid","port":443,"password":"test-only"},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"example.invalid"}}}],"routing":{"rules":[]}}`

var testDial DialContextFunc = (&net.Dialer{Timeout: time.Second}).DialContext

func TestOrdinaryConfigPreserved(t *testing.T) {
	input := `{ "outbounds": [{"protocol":"freedom"}] }`
	p, e := Prepare(input, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if p.Config() != input {
		t.Fatal("ordinary config changed")
	}
}
func TestPrepareLifecycleAndCredentials(t *testing.T) {
	p, e := Prepare(validConfig, testDial)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(p.Config(), "test-only") || strings.Contains(p.Config(), "example.invalid") {
		t.Fatal("remote credentials leaked into prepared JSON")
	}
	if len(p.bridges) != 1 {
		t.Fatal("missing bridge")
	}
	b := p.bridges[0]
	var parsed map[string]any
	if json.Unmarshal([]byte(p.Config()), &parsed) != nil {
		t.Fatal("invalid output")
	}
	out := parsed["outbounds"].([]any)[0].(map[string]any)
	if out["tag"] != "proxy" || out["protocol"] != "socks" {
		t.Fatal("tag or protocol incorrect")
	}
	addr := b.listener.Addr().String()
	c, e := net.Dial("tcp", addr)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if e = p.Close(); e != nil {
		t.Fatal(e)
	}
	p.Close()
	if c2, e := net.DialTimeout("tcp", addr, time.Second); e == nil {
		c2.Close()
		t.Fatal("listener survived close")
	}
	q, e := Prepare(validConfig, testDial)
	if e != nil {
		t.Fatal(e)
	}
	defer q.Close()
	if b.password == q.bridges[0].password {
		t.Fatal("credentials reused")
	}
}
func TestRejectUnsupportedSettings(t *testing.T) {
	cases := []string{
		strings.Replace(validConfig, `"password":"test-only"`, `"password":""`, 1),
		strings.Replace(validConfig, `"port":443`, `"port":0`, 1),
		strings.Replace(validConfig, `"security":"tls"`, `"security":"none"`, 1),
		strings.Replace(validConfig, `"network":"tcp"`, `"network":"ws"`, 1),
		strings.Replace(validConfig, `"serverName":"example.invalid"`, `"serverName":"example.invalid","fingerprint":"chrome"`, 1),
		strings.Replace(validConfig, `"tag":"proxy"`, `"tag":"proxy","sendThrough":"192.0.2.1"`, 1),
		strings.Replace(validConfig, `"tag":"proxy"`, `"tag":"proxy","mux":{"enabled":true}`, 1),
		strings.Replace(validConfig, `"network":"tcp"`, `"network":"tcp","sockopt":{"dialerProxy":"other"}`, 1),
	}
	for i, c := range cases {
		p, e := Prepare(c, testDial)
		if e == nil {
			p.Close()
			t.Fatalf("case %d accepted", i)
		}
	}
	if p, e := Prepare(validConfig, nil); e == nil {
		p.Close()
		t.Fatal("unprotected dialer accepted")
	}
}
func TestInvalidSecondOutboundRejected(t *testing.T) {
	var root map[string]json.RawMessage
	json.Unmarshal([]byte(validConfig), &root)
	var outs []map[string]json.RawMessage
	json.Unmarshal(root["outbounds"], &outs)
	bad := map[string]json.RawMessage{"protocol": json.RawMessage(`"anytls"`), "settings": json.RawMessage(`{}`)}
	outs = append(outs, bad)
	root["outbounds"], _ = json.Marshal(outs)
	data, _ := json.Marshal(root)
	var started []*bridge
	starter := func(s settings, tc *tls.Config, dial DialContextFunc) (*bridge, error) {
		b, e := start(s, tc, dial)
		if e == nil {
			started = append(started, b)
		}
		return b, e
	}
	if p, e := prepare(string(data), testDial, starter); e == nil {
		p.Close()
		t.Fatal("invalid second outbound accepted")
	}
	if len(started) != 1 {
		t.Fatal("expected first bridge to start")
	}
	for _, b := range started {
		b.mu.Lock()
		closed := b.closed
		b.mu.Unlock()
		if !closed {
			b.close()
			t.Fatal("rollback failed to close bridge")
		}
		c, e := net.DialTimeout("tcp", b.listener.Addr().String(), time.Second)
		if e == nil {
			c.Close()
			t.Fatal("rollback listener still reachable")
		}
	}
}
func authSOCKS(t *testing.T, b *bridge, password string) net.Conn {
	t.Helper()
	c, e := net.Dial("tcp", b.listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	c.SetDeadline(time.Now().Add(4 * time.Second))
	c.Write([]byte{5, 1, 2})
	v, e := readBytes(c, 2)
	if e != nil || v[1] != 2 {
		c.Close()
		t.Fatalf("method auth: %v", e)
	}
	packet := append([]byte{1, byte(len(b.user))}, []byte(b.user)...)
	packet = append(packet, byte(len(password)))
	packet = append(packet, []byte(password)...)
	c.Write(packet)
	v, e = readBytes(c, 2)
	if e != nil {
		c.Close()
		t.Fatal(e)
	}
	if password == b.password && v[1] != 0 {
		c.Close()
		t.Fatal("auth failed")
	}
	if password != b.password && v[1] == 0 {
		c.Close()
		t.Fatal("bad auth accepted")
	}
	return c
}
func connectSOCKS(c net.Conn, command byte) (byte, error) {
	_, e := c.Write([]byte{5, command, 0, 3, 4, 'e', 'c', 'h', 'o', 0, 80})
	if e != nil {
		return 0, e
	}
	v, e := readBytes(c, 10)
	if e != nil {
		return 0, e
	}
	return v[1], nil
}
func TestAuthenticationAndUDPFailClosed(t *testing.T) {
	p, e := Prepare(validConfig, testDial)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	b := p.bridges[0]
	bad := authSOCKS(t, b, "wrong")
	bad.Close()
	c := authSOCKS(t, b, b.password)
	defer c.Close()
	code, e := connectSOCKS(c, 3)
	if e != nil || code != 7 {
		t.Fatalf("UDP not refused: %d %v", code, e)
	}
	noauth, e := net.Dial("tcp", b.listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer noauth.Close()
	noauth.SetDeadline(time.Now().Add(time.Second))
	noauth.Write([]byte{5, 1, 0})
	v, e := readBytes(noauth, 2)
	if e != nil || v[1] != 255 {
		t.Fatal("anonymous connection accepted")
	}
}

type echoHandler struct{}

func (echoHandler) NewConnectionEx(ctx context.Context, c net.Conn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() { defer c.Close(); io.Copy(c, c) }()
}
func tlsAnyTLSServer(t *testing.T) (settings, *tls.Config) {
	t.Helper()
	hs := httptest.NewTLSServer(nil)
	cert := hs.TLS.Certificates[0]
	hs.Close()
	ln, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if e != nil {
		t.Fatal(e)
	}
	service, e := anytls.NewService(anytls.ServiceConfig{Users: []anytls.User{{Name: "test", Password: "server-test"}}, Handler: echoHandler{}, Logger: logger.NOP(), PaddingScheme: padding.DefaultPaddingScheme})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var mu sync.Mutex
	conns := map[net.Conn]bool{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			mu.Lock()
			conns[c] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				defer func() { mu.Lock(); delete(conns, c); mu.Unlock() }()
				service.NewConnection(ctx, c, M.Socksaddr{}, nil)
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		ln.Close()
		mu.Lock()
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	pool := x509.NewCertPool()
	parsed, e := x509.ParseCertificate(cert.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	pool.AddCert(parsed)
	return settings{Address: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Password: "server-test"}, &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12}
}
func TestTLSAnyTLSEchoAndProtectedDialer(t *testing.T) {
	s, tc := tlsAnyTLSServer(t)
	var calls atomic.Int32
	b, e := start(s, tc, func(ctx context.Context, network, address string) (net.Conn, error) {
		calls.Add(1)
		return testDial(ctx, network, address)
	})
	if e != nil {
		t.Fatal(e)
	}
	defer b.close()
	c := authSOCKS(t, b, b.password)
	defer c.Close()
	code, e := connectSOCKS(c, 1)
	if e != nil || code != 0 {
		t.Fatalf("connect: %d %v", code, e)
	}
	data := "AnyTLS loopback echo"
	c.Write([]byte(data))
	v, e := readBytes(c, len(data))
	if e != nil || string(v) != data {
		t.Fatalf("echo: %q %v", v, e)
	}
	if calls.Load() == 0 {
		t.Fatal("protected dialer was bypassed")
	}
}
func TestTLSVerificationDefault(t *testing.T) {
	s, _ := tlsAnyTLSServer(t)
	b, e := start(s, &tls.Config{ServerName: "example.com", MinVersion: tls.VersionTLS12}, testDial)
	if e != nil {
		t.Fatal(e)
	}
	defer b.close()
	c := authSOCKS(t, b, b.password)
	defer c.Close()
	code, e := connectSOCKS(c, 1)
	if e != nil {
		t.Fatal(e)
	}
	if code == 0 {
		t.Fatal("untrusted certificate accepted")
	}
}
func TestWrongAnyTLSPasswordFailsData(t *testing.T) {
	s, tc := tlsAnyTLSServer(t)
	s.Password = "wrong"
	b, e := start(s, tc, testDial)
	if e != nil {
		t.Fatal(e)
	}
	defer b.close()
	c := authSOCKS(t, b, b.password)
	defer c.Close()
	code, e := connectSOCKS(c, 1)
	if e != nil || code != 0 {
		return
	}
	c.Write([]byte("must-not-echo"))
	v, e := readBytes(c, 13)
	if e == nil && string(v) == "must-not-echo" {
		t.Fatal("wrong server password succeeded")
	}
}

func TestCloseTerminatesActiveSession(t *testing.T) {
	s, tc := tlsAnyTLSServer(t)
	b, e := start(s, tc, testDial)
	if e != nil {
		t.Fatal(e)
	}
	c := authSOCKS(t, b, b.password)
	defer c.Close()
	code, e := connectSOCKS(c, 1)
	if e != nil || code != 0 {
		b.close()
		t.Fatalf("connect: %d %v", code, e)
	}
	c.Write([]byte("live"))
	if _, e = readBytes(c, 4); e != nil {
		b.close()
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() { b.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not terminate session")
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, e = readBytes(c, 1); e == nil {
		t.Fatal("connection remains open")
	}
	b.close()
}
func TestWrongTLSNameRejected(t *testing.T) {
	s, tc := tlsAnyTLSServer(t)
	tc.ServerName = "wrong.invalid"
	b, e := start(s, tc, testDial)
	if e != nil {
		t.Fatal(e)
	}
	defer b.close()
	c := authSOCKS(t, b, b.password)
	defer c.Close()
	code, e := connectSOCKS(c, 1)
	if e != nil {
		t.Fatal(e)
	}
	if code == 0 {
		t.Fatal("wrong TLS hostname accepted")
	}
}

func TestOrdinaryJSONCPreserved(t *testing.T) {
	input := `{ // Xray JSONC
 "outbounds":[{"protocol":"freedom"}] }`
	p, e := Prepare(input, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if p.Config() != input {
		t.Fatal("legacy JSONC changed")
	}
}
