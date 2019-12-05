// Package testserver provides a server to be used during testing.
// This package's implementation is heavily inspired by the
// github.com/pkg/profile package.
package testserver

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	httpMode = iota
	tcpMode
	noneMode
)

// A Server is a mock server meant for testing.
type Server struct {
	// quiet suppresses informational messages during serving.
	quiet bool

	// mode holds the type of server that will be run.
	mode int

	// addr holds the server's network endpoint.
	addr net.Addr

	// httpHandlers holds URL patters mapped to the server's HTTP handlers.
	httpHandlers map[string]http.Handler

	// connHandler holds the server's connection handler.
	connHandler func(net.Conn)

	// stopper holds a function that stops the server.
	stopper func()

	// stopped records if a call to Server.Stop has been made.
	stopped uint32
}

// Quiet supresses informational messages during serving.
func Quiet(s *Server) { s.quiet = true }

// HTTPServer enables HTTP serving.
// It disables any previous serving settings.
func HTTPServer(s *Server) { s.mode = httpMode }

// HTTPHandler enables handling of requests sent to pattern with h.
func HTTPHandler(pattern string, h http.Handler) func(*Server) {
	return func(s *Server) {
		if s.httpHandlers == nil {
			s.httpHandlers = make(map[string]http.Handler)
		}
		s.httpHandlers[pattern] = h
	}
}

// HTTPHandlerFunc enables handling of requests sent to pattern with f.
func HTTPHandlerFunc(pattern string, f http.HandlerFunc) func(*Server) {
	return func(s *Server) {
		if s.httpHandlers == nil {
			s.httpHandlers = make(map[string]http.Handler)
		}
		s.httpHandlers[pattern] = f
	}
}

// TCPServer enables TCP serving.
// It disables any previous serving settings.
func TCPServer(s *Server) { s.mode = tcpMode }

// ConnectionHandler enables handling of network connections with f.
// It replaces any previous connection handler.
func ConnectionHandler(f func(net.Conn)) func(*Server) {
	return func(s *Server) {
		s.connHandler = f
	}
}

// NotListening disables any previous serving settings.
// A Server with NotListening will provide a network endpoint on which nothing is
// listening.
func NotListening(s *Server) { s.mode = noneMode }

// Stop stops the Server.
func (s *Server) Stop() {
	if !atomic.CompareAndSwapUint32(&s.stopped, 0, 1) {
		// Stop has already been called.
		return
	}
	s.stopper()
}

// Addr returns s's network endpoint.
func (s *Server) Addr() net.Addr {
	return s.addr
}

// New creates a new Server. Any errors that occur during creation or serving
// will result in a call to t.Fatal.
// The caller should call the Stop method on the value returned to
// cleanly stop the server.
func New(t *testing.T, options ...func(*Server)) *Server {
	t.Helper()

	var srv Server
	for _, option := range options {
		option(&srv)
	}

	logf := func(format string, args ...interface{}) {
		if !srv.quiet {
			t.Logf(format, args...)
		}
	}

	switch srv.mode {
	case httpMode:
		httpServer := &http.Server{}
		if len(srv.httpHandlers) > 0 {
			mux := http.NewServeMux()
			for p, h := range srv.httpHandlers {
				mux.Handle(p, h)
			}
			httpServer.Handler = mux
		}

		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("failed to create network listener: %v", err)
		}
		srv.addr = listener.Addr()

		go func() {
			logf("HTTP server listening on %v", listener.Addr())
			if err := httpServer.Serve(listener); err != http.ErrServerClosed {
				t.Fatalf("failed to start HTTP server: %v", err)
			}
		}()

		srv.stopper = func() {
			if err := httpServer.Shutdown(context.Background()); err != nil {
				t.Fatalf("failed to stop HTTP server: %v", err)
			}
			logf("HTTP server successfully shut down")
		}

	case tcpMode:
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("failed to create network listener: %v", err)
		}
		srv.addr = listener.Addr()

		if srv.connHandler == nil {
			logf("no connection handler provided, using default")
			srv.connHandler = func(c net.Conn) {
				// c.Write([]byte("HTTP/1.1 200 OK\nContent-Length: 0\n\n"))  // Valid HTTP 200 OK response.
				c.Close()
			}
		}

		conns := make(chan net.Conn)
		stop := make(chan struct{})
		var wg sync.WaitGroup

		go func() {
			for {
				c, err := listener.Accept()
				if err != nil {
					select {
					case <-stop:
						// Accept failed because server is stopping.
						return
					default:
						t.Fatalf("failed to accept connect: %v", err)
					}
				}
				conns <- c
			}
		}()

		go func() {
			for {
				select {
				case <-stop:
					// Server is stopping, don't handle connections anymore.
					return
				case conn := <-conns:
					wg.Add(1)
					go func(c net.Conn) {
						c.SetDeadline(time.Now().Add(10 * time.Millisecond))
						srv.connHandler(c)
						wg.Done()
					}(conn)
				}
			}
		}()

		srv.stopper = func() {
			close(stop)
			wg.Wait() // Wait for connections being handled to close.
			logf("TCP server successfully shut down")
		}

	case noneMode:
		addr, err := net.ResolveTCPAddr("tcp", ":0")
		if err != nil {
			t.Fatalf("failed to find unused port: %v", err)
		}

		srv.addr = addr

		srv.stopper = func() {}
	}

	return &srv
}
