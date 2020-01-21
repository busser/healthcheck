package healthcheck_test

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/busser/healthcheck"
	"github.com/busser/healthcheck/testserver"
)

type server struct {
	name           string
	options        []func(*testserver.Server)
	expectedStatus bool
}

func http200(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func http404(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
}

func httpSlow(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-w.(http.CloseNotifier).CloseNotify():
			// Client has closed connection.
			return
		}
		time.Sleep(delay)
	}
}

func httpPanic(w http.ResponseWriter, r *http.Request) {
	panic("the request caused the server to panic")
}

func tcpHTTP200(c net.Conn) {
	c.Write([]byte("HTTP/1.1 200 OK\nContent-Length: 0\n\n")) // Valid HTTP 200 OK response.
	c.Close()
}

func closedConn(c net.Conn) {
	c.Close()
}

func TestRun(t *testing.T) {
	tests := map[string]struct {
		timeout time.Duration
		servers []server
	}{
		"no-servers": {
			timeout: time.Millisecond,
			servers: nil,
		},
		"one-healthy": {
			timeout: 10 * time.Millisecond,
			servers: []server{
				{
					name: "http-200",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", http200),
					},
					expectedStatus: true,
				},
			},
		},
		"multiple-healthy": {
			timeout: 10 * time.Millisecond,
			servers: []server{
				{
					name: "http-200-a",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", http200),
					},
					expectedStatus: true,
				},
				{
					name: "http-200-b",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", http200),
					},
					expectedStatus: true,
				},
			},
		},
		"one-unhealthy": {
			timeout: 10 * time.Millisecond,
			servers: []server{
				{
					name: "http-404",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", http404),
					},
					expectedStatus: false,
				},
			},
		},
		"multiple-unhealthy": {
			timeout: 10 * time.Millisecond,
			servers: []server{
				{
					name: "http-404",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", http404),
					},
					expectedStatus: false,
				},
				{
					name: "not-http",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.TCPServer,
					},
					expectedStatus: false,
				},
				{
					name: "closed-connection",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.TCPServer,
						testserver.ConnectionHandler(closedConn),
					},
					expectedStatus: false,
				},
				{
					name: "not-listening",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.NotListening,
					},
					expectedStatus: false,
				},
			},
		},
		"one-slow": {
			timeout: 10 * time.Millisecond,
			servers: []server{
				{
					name: "http-slow",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", httpSlow(100*time.Millisecond)),
					},
					expectedStatus: false,
				},
			},
		},
		"multiple-slow-one-fast": {
			timeout: 10 * time.Millisecond,
			servers: []server{
				{
					name: "http-slow-a",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", httpSlow(100*time.Millisecond)),
					},
					expectedStatus: false,
				},
				{
					name: "http-slow-b",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", httpSlow(1000*time.Millisecond)),
					},
					expectedStatus: false,
				},
				{
					name: "http-fast",
					options: []func(*testserver.Server){
						testserver.Quiet,
						testserver.HTTPServer,
						testserver.HTTPHandlerFunc("/status", httpSlow(1*time.Millisecond)),
					},
					expectedStatus: true,
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var services []healthcheck.Service
			for _, s := range tc.servers {
				srv := testserver.New(t, s.options...)
				defer srv.Stop()
				services = append(services, srv)
			}

			statusByAddr := healthcheck.Run(services, tc.timeout)

			for i := range tc.servers {
				srvName := tc.servers[i].name
				expected := tc.servers[i].expectedStatus
				actual := statusByAddr[services[i].Addr()]
				if actual != expected {
					t.Errorf("server %s: expected status %t, got %t", srvName, expected, actual)
				}
			}
		})
	}
}
