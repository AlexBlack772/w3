// Package rpctest provides utilities for testing RPC methods.
package rpctest

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

const (
	logBodyLimit = 1024
)

// Server is a fake RPC endpoint that responds only to a single requests that
// is defined in a golden-file.
//
// Request golden-files have the following format to define a single request
// and the corresponding response:
//
//	// Comments and empty lines will be ignored.
//	// Request starts with ">".
//	> {"jsonrpc":"2.0","id":1,"method":"eth_chainId"}
//	// Response starts with "<".
//	< {"jsonrpc":"2.0","id":1,"result":"0x1"}
type Server struct {
	t *testing.T

	reader   io.Reader
	readOnce sync.Once
	in       []byte
	out      []byte

	httptestSrv *httptest.Server
}

// NewServer returns a new instance of Server that serves the golden-file from
// Reader r.
func NewServer(t *testing.T, r io.Reader) *Server {
	srv := &Server{t: t, reader: r}
	httptestSrv := httptest.NewServer(srv)

	srv.httptestSrv = httptestSrv
	return srv
}

// NewFileServer returns a new instance of Server that serves the golden-file
// from the given filename.
func NewFileServer(t *testing.T, filename string) *Server {
	f, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	return NewServer(t, f)
}

func (srv *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv.readOnce.Do(srv.readGolden)
	defer r.Body.Close()

	// read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		srv.t.Fatalf("Failed to read body: %v", err)
	}

	// log request
	path := r.URL.Path
	if q := r.URL.RawQuery; q != "" {
		path += "?" + q
	}
	srv.t.Logf("request: method=%s path=%s body_len=%d body=%q", r.Method, path, len(body), preview(body))

	// check body
	if !bytes.Equal(srv.in, body) {
		srv.t.Logf("request mismatch: want_len=%d got_len=%d", len(srv.in), len(body))
		srv.t.Logf("want: %q", preview(srv.in))
		srv.t.Logf("got:  %q", preview(body))
		srv.t.Fatalf("Invalid request body (-want, +got)\n-%s\n+%s", srv.in, body)
	}

	// respond
	w.Header().Set("Content-Type", "application/json")
	srv.t.Logf("response: %q", preview(srv.out))
	w.Write(srv.out)
}

// URL returns the servers RPC endpoint url.
func (srv *Server) URL() string {
	return srv.httptestSrv.URL
}

// Close shuts down the server.
func (srv *Server) Close() {
	srv.httptestSrv.Close()
}

func (srv *Server) readGolden() {
	if rc, ok := srv.reader.(io.ReadCloser); ok {
		defer rc.Close()
	}

	scan := bufio.NewScanner(srv.reader)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) <= 0 {
			continue // skip empty lines
		}

		switch line[0] {
		case '>':
			trimedLine := bytes.Trim(line, "> ")
			srv.in = make([]byte, len(trimedLine))
			copy(srv.in, trimedLine)
		case '<':
			trimedLine := bytes.Trim(line, "< ")
			srv.out = make([]byte, len(trimedLine))
			copy(srv.out, trimedLine)
		case '/': // ignore lines starting with "/"
		default:
			srv.t.Fatalf("Invalid line %q", scan.Text())
		}
	}
	if err := scan.Err(); err != nil {
		srv.t.Fatalf("Failed to scan file: %v", err)
	}
	srv.t.Logf("golden in: %q", preview(srv.in))
	srv.t.Logf("golden out: %q", preview(srv.out))
}

func preview(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) <= logBodyLimit {
		return string(b)
	}
	return string(b[:logBodyLimit]) + "... (truncated)"
}
