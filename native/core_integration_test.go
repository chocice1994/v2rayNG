package libv2ray

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	anytls "github.com/anytls/sing-anytls"
	"github.com/anytls/sing-anytls/padding"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type previewCallback struct{}

func (previewCallback) Startup() int                 { return 0 }
func (previewCallback) Shutdown() int                { return 0 }
func (previewCallback) OnEmitStatus(int, string) int { return 0 }

type previewHTTPHandler struct{}

func (previewHTTPHandler) NewConnectionEx(_ context.Context, conn net.Conn, _, _ M.Socksaddr, _ N.CloseHandlerFunc) {
	go func() {
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		request.Body.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	}()
}

func previewConfig(t *testing.T) string {
	t.Helper()
	fixture := httptest.NewTLSServer(nil)
	certificate := fixture.TLS.Certificates[0]
	fixture.Close()
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	service, err := anytls.NewService(anytls.ServiceConfig{Users: []anytls.User{{Name: "fixture", Password: "test-only"}}, Handler: previewHTTPHandler{}, Logger: logger.NOP(), PaddingScheme: padding.DefaultPaddingScheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	var mu sync.Mutex
	conns := map[net.Conn]bool{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns[conn] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				defer func() { mu.Lock(); delete(conns, conn); mu.Unlock() }()
				_ = service.NewConnection(ctx, conn, M.Socksaddr{}, nil)
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		mu.Lock()
		for conn := range conns {
			conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	// The local self-signed fixture opts out explicitly. Production defaults are
	// verified separately by bridge tests for untrusted CA and wrong hostname.
	return fmt.Sprintf(`{"log":{"loglevel":"none"},"stats":{},"inbounds":[],"outbounds":[{"tag":"proxy","protocol":"anytls","settings":{"address":"127.0.0.1","port":%d,"password":"test-only"},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"example.com","allowInsecure":true}}}],"routing":{"domainStrategy":"AsIs","rules":[]}}`, listener.Addr().(*net.TCPAddr).Port)
}

func TestAnyTLSControllerRepeatedStartStopAndRestart(t *testing.T) {
	config := previewConfig(t)
	controller := NewCoreController(previewCallback{})
	if err := controller.StartLoop(config, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { controller.StopLoop() })
	prepared := controller.anyTLSPrepared
	if !controller.IsRunning || prepared == nil {
		t.Fatal("missing active core/bridge")
	}
	if err := controller.StartLoop(config, 0); err != nil {
		t.Fatal(err)
	}
	if controller.anyTLSPrepared != prepared {
		t.Fatal("duplicate bridge on repeated start")
	}
	if err := controller.StopLoop(); err != nil {
		t.Fatal(err)
	}
	if controller.IsRunning || controller.anyTLSPrepared != nil {
		t.Fatal("bridge retained after stop")
	}
	if err := controller.StopLoop(); err != nil {
		t.Fatal(err)
	}
	if err := controller.StartLoop(config, 0); err != nil {
		t.Fatal(err)
	}
	if controller.anyTLSPrepared == prepared {
		t.Fatal("restart reused closed bridge")
	}
}

func TestAnyTLSMeasureOutboundHTTP(t *testing.T) {
	elapsed, err := MeasureOutboundDelay(previewConfig(t), "http://example.test/generate_204")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed < 0 {
		t.Fatal("negative latency on successful Xray -> SOCKS -> TLS -> AnyTLS -> HTTP path")
	}
}

func TestAnyTLSControllerFailedCoreStart(t *testing.T) {
	config := previewConfig(t)
	// Preparation creates a bridge before the Xray JSON loader rejects this.
	config = config[:len(config)-1] + `,"api":{"services":["not-a-service"]}}`
	controller := NewCoreController(previewCallback{})
	err := controller.StartLoop(config, 0)
	if err == nil {
		controller.StopLoop()
		t.Fatal("invalid core config accepted")
	}
	if controller.IsRunning || controller.anyTLSPrepared != nil {
		t.Fatal("failed core retained active ownership")
	}
}

func TestAnyTLSControllerCoreInitFailure(t *testing.T) {
	var config map[string]any
	if err := json.Unmarshal([]byte(previewConfig(t)), &config); err != nil {
		t.Fatal(err)
	}
	config["outbounds"] = append(config["outbounds"].([]any), map[string]any{"tag": "proxy", "protocol": "blackhole"})
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	controller := NewCoreController(previewCallback{})
	err = controller.StartLoop(string(data), 0)
	if err == nil || !strings.Contains(err.Error(), "core init failed") {
		controller.StopLoop()
		t.Fatalf("expected core.New failure, got %v", err)
	}
	if controller.IsRunning || controller.anyTLSPrepared != nil {
		t.Fatal("failed init retained ownership")
	}
}

func TestAnyTLSControllerCoreStartFailure(t *testing.T) {
	var config map[string]any
	if err := json.Unmarshal([]byte(previewConfig(t)), &config); err != nil {
		t.Fatal(err)
	}
	config["api"] = map[string]any{"tag": "api", "listen": "not-an-address", "services": []string{}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	controller := NewCoreController(previewCallback{})
	err = controller.StartLoop(string(data), 0)
	if err == nil || !strings.Contains(err.Error(), "startup failed") {
		controller.StopLoop()
		t.Fatalf("expected core.Start failure, got %v", err)
	}
	if controller.IsRunning || controller.anyTLSPrepared != nil || controller.coreInstance != nil {
		t.Fatal("failed start retained ownership")
	}
}
