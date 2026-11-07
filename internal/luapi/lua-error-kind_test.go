// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/sinqerror"
)

func asClientErr(err error) error {
	return sinqerror.WrapNetworkError(&url.Error{Op: "Get", URL: "http://example.test/", Err: err})
}

func dialErr(err error) error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: err}}
}

func TestGetErrorKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want errorKind
	}{
		{"request timeout", sinqerror.ErrRequestTimeout, "timeout"},
		{"too many redirects", asClientErr(sinqerror.ErrTooManyRedirects), "redirects"},
		{"dns not found", asClientErr(&net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "x.invalid", IsNotFound: true}}), "dns"},
		{"dns timeout is still dns", asClientErr(&net.DNSError{Err: "i/o timeout", Name: "x.test", IsTimeout: true}), "dns"},
		{"refused", asClientErr(dialErr(syscall.ECONNREFUSED)), "refused"},
		{"refused on windows", asClientErr(dialErr(wsaECONNREFUSED)), "refused"},
		{"reset", asClientErr(&net.OpError{Op: "read", Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}}), "reset"},
		{"reset on windows", asClientErr(&net.OpError{Op: "read", Err: &os.SyscallError{Syscall: "wsarecv", Err: wsaECONNRESET}}), "reset"},
		{"broken pipe", asClientErr(&net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}}), "reset"},
		{"server closed before responding", asClientErr(io.EOF), "reset"},
		{"connection cut mid-response", asClientErr(io.ErrUnexpectedEOF), "reset"},
		{"server closed idle connection", asClientErr(errors.New(httpServerClosedIdle)), "reset"},
		{"http response to https client", asClientErr(errors.New(httpResponseToHTTPS)), "tls"},
		{"net/http text outside a url.Error", errors.New(httpServerClosedIdle), "other"},
		{"unknown authority", asClientErr(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), "tls"},
		{"bare x509 hostname mismatch", asClientErr(x509.HostnameError{Certificate: &x509.Certificate{}, Host: "x.test"}), "tls"},
		{"bare x509 invalid certificate", asClientErr(x509.CertificateInvalidError{Reason: x509.Expired}), "tls"},
		{"not a tls server", asClientErr(tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}), "tls"},
		{"tls alert from server", asClientErr(tls.AlertError(42)), "tls"},
		{"transport dial timeout", asClientErr(&net.OpError{Op: "dial", Err: os.ErrDeadlineExceeded}), "timeout"},
		{"anything else", asClientErr(errors.New("boom")), "other"},
		{"unwrapped anything else", errors.New("boom"), "other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := getErrorKind(tc.err); got != tc.want {
				t.Errorf("Expected kind %q for %v, got %q", tc.want, tc.err, got)
			}
		})
	}
}

func failedGet(t *testing.T, client *http.Client, url string) error {
	t.Helper()
	resp, err := client.Get(url)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("Expected the request to %s to fail", url)
	}
	return err
}

func TestGetErrorKind_RealNetworkErrors(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("refused", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to listen: %v", err)
		}
		addr := ln.Addr().String()
		ln.Close()

		err = failedGet(t, client, fmt.Sprintf("http://%s/", addr))
		if got := getErrorKind(sinqerror.WrapNetworkError(err)); got != "refused" {
			t.Errorf("Expected kind \"refused\" for %v, got %q", err, got)
		}
	})

	t.Run("untrusted certificate", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer srv.Close()
		srv.Config.ErrorLog = nil

		err := failedGet(t, client, srv.URL)
		if got := getErrorKind(sinqerror.WrapNetworkError(err)); got != "tls" {
			t.Errorf("Expected kind \"tls\" for %v, got %q", err, got)
		}
	})

	t.Run("not a tls server", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to listen: %v", err)
		}
		defer ln.Close()
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				conn.Write([]byte("definitely not tls\n"))
				conn.Close()
			}
		}()

		err = failedGet(t, client, fmt.Sprintf("https://%s/", ln.Addr()))
		if got := getErrorKind(sinqerror.WrapNetworkError(err)); got != "tls" {
			t.Errorf("Expected kind \"tls\" for %v, got %q", err, got)
		}
	})

	t.Run("https to a plain http server", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer srv.Close()

		err := failedGet(t, client, "https://"+srv.Listener.Addr().String())
		if got := getErrorKind(sinqerror.WrapNetworkError(err)); got != "tls" {
			t.Errorf("Expected kind \"tls\" for %v, got %q", err, got)
		}
	})

	t.Run("server hangs up", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("Failed to listen: %v", err)
		}
		defer ln.Close()
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				conn.Close()
			}
		}()

		err = failedGet(t, client, fmt.Sprintf("http://%s/", ln.Addr()))
		if got := getErrorKind(sinqerror.WrapNetworkError(err)); got != "reset" {
			t.Errorf("Expected kind \"reset\" for %v, got %q", err, got)
		}
	})

	t.Run("unresolvable host", func(t *testing.T) {
		err := failedGet(t, client, "http://sinq-error-kind.invalid/")
		if got := getErrorKind(sinqerror.WrapNetworkError(err)); got != "dns" {
			t.Errorf("Expected kind \"dns\" for %v, got %q", err, got)
		}
	})
}
