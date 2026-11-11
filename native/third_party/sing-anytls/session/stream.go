package session

import (
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/anytls/sing-anytls/pipe"
)

// Stream implements net.Conn
type Stream struct {
	id uint32

	sess *Session

	pipeR         *pipe.PipeReader
	pipeW         *pipe.PipeWriter
	writeDeadline pipe.PipeDeadline

	// Local patch: synchronize terminal state with concurrent I/O and hook registration.
	stateMu sync.Mutex
	dieOnce sync.Once
	dieHook func()
	dieErr  error

	reportOnce sync.Once
}

// newStream initiates a Stream struct
func newStream(id uint32, sess *Session) *Stream {
	s := new(Stream)
	s.id = id
	s.sess = sess
	s.pipeR, s.pipeW = pipe.Pipe()
	s.writeDeadline = pipe.MakePipeDeadline()
	return s
}

// Read implements net.Conn
func (s *Stream) Read(b []byte) (n int, err error) {
	n, err = s.pipeR.Read(b)
	if n == 0 {
		if terminal := s.terminalError(); terminal != nil {
			err = terminal
		}
	}
	return
}

// Write implements net.Conn
func (s *Stream) Write(b []byte) (n int, err error) {
	select {
	case <-s.writeDeadline.Wait():
		return 0, os.ErrDeadlineExceeded
	default:
	}
	if terminal := s.terminalError(); terminal != nil {
		return 0, terminal
	}
	n, err = s.sess.writeDataFrame(s.id, b)
	return
}

// Close implements net.Conn
func (s *Stream) Close() error {
	return s.closeWithError(io.ErrClosedPipe)
}

// closeLocally only closes Stream and don't notify remote peer
func (s *Stream) closeLocally() {
	var once bool
	s.dieOnce.Do(func() {
		s.stateMu.Lock()
		s.dieErr = net.ErrClosed
		s.stateMu.Unlock()
		s.pipeR.Close()
		once = true
	})
	if once {
		s.runCloseHook()
	}
}

func (s *Stream) closeWithError(err error) error {
	var once bool
	s.dieOnce.Do(func() {
		s.stateMu.Lock()
		s.dieErr = err
		s.stateMu.Unlock()
		s.pipeR.Close()
		once = true
	})
	if once {
		s.runCloseHook()
		return s.sess.streamClosed(s.id)
	} else {
		return s.terminalError()
	}
}

func (s *Stream) SetReadDeadline(t time.Time) error {
	return s.pipeR.SetReadDeadline(t)
}

func (s *Stream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline.Set(t)
	return nil
}

func (s *Stream) SetDeadline(t time.Time) error {
	s.SetWriteDeadline(t)
	return s.SetReadDeadline(t)
}

// LocalAddr satisfies net.Conn interface
func (s *Stream) LocalAddr() net.Addr {
	if ts, ok := s.sess.conn.(interface {
		LocalAddr() net.Addr
	}); ok {
		return ts.LocalAddr()
	}
	return nil
}

// RemoteAddr satisfies net.Conn interface
func (s *Stream) RemoteAddr() net.Addr {
	if ts, ok := s.sess.conn.(interface {
		RemoteAddr() net.Addr
	}); ok {
		return ts.RemoteAddr()
	}
	return nil
}

// HandshakeFailure should be called when Server fail to create outbound proxy
func (s *Stream) HandshakeFailure(err error) error {
	var once bool
	s.reportOnce.Do(func() {
		once = true
	})
	if once && err != nil && s.sess.peerVersion >= 2 {
		f := newFrame(cmdSYNACK, s.id)
		f.data = []byte(err.Error())
		if _, err := s.sess.writeControlFrame(f); err != nil {
			return err
		}
	}
	return nil
}

// HandshakeSuccess should be called when Server success to create outbound proxy
func (s *Stream) HandshakeSuccess() error {
	var once bool
	s.reportOnce.Do(func() {
		once = true
	})
	if once && s.sess.peerVersion >= 2 {
		if _, err := s.sess.writeControlFrame(newFrame(cmdSYNACK, s.id)); err != nil {
			return err
		}
	}
	return nil
}

// terminalError protects error publication without holding a mutex during I/O.
func (s *Stream) terminalError() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.dieErr
}

// setCloseHook also handles a peer closing before CreateStream installs its hook.
func (s *Stream) setCloseHook(hook func()) {
	s.stateMu.Lock()
	if s.dieErr != nil {
		s.stateMu.Unlock()
		hook()
		return
	}
	s.dieHook = hook
	s.stateMu.Unlock()
}
func (s *Stream) runCloseHook() {
	s.stateMu.Lock()
	hook := s.dieHook
	s.dieHook = nil
	s.stateMu.Unlock()
	if hook != nil {
		hook()
	}
}
