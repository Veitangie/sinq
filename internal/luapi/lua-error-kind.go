// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/url"
	"syscall"

	"veitangie.dev/sinq/internal/sinqerror"
)

const (
	wsaECONNRESET   syscall.Errno = 10054
	wsaECONNREFUSED syscall.Errno = 10061
)

type errorKind string

const (
	timeoutKind   errorKind = "timeout"
	redirectsKind errorKind = "redirects"
	dnsKind       errorKind = "dns"
	refusedKind   errorKind = "refused"
	resetKind     errorKind = "reset"
	tlsKind       errorKind = "tls"
	otherKind     errorKind = "other"
)

func getErrorKind(err error) errorKind {
	if errors.Is(err, sinqerror.ErrTooManyRedirects) {
		return redirectsKind
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsKind
	}
	if errors.Is(err, sinqerror.ErrRequestTimeout) {
		return timeoutKind
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return timeoutKind
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, wsaECONNREFUSED) {
		return refusedKind
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, wsaECONNRESET) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		isHTTPClientError(err, httpServerClosedIdle) {
		return resetKind
	}
	if isTLSError(err) || isHTTPClientError(err, httpResponseToHTTPS) {
		return tlsKind
	}

	return otherKind
}

const (
	httpServerClosedIdle = "http: server closed idle connection"
	httpResponseToHTTPS  = "http: server gave HTTP response to HTTPS client"
)

func isHTTPClientError(err error, message string) bool {
	urlErr, ok := errors.AsType[*url.Error](err)
	return ok && urlErr.Err != nil && urlErr.Err.Error() == message
}

func isTLSError(err error) bool {
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return true
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return true
	}
	if _, ok := errors.AsType[tls.AlertError](err); ok {
		return true
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return true
	}
	if _, ok := errors.AsType[x509.HostnameError](err); ok {
		return true
	}
	if _, ok := errors.AsType[x509.CertificateInvalidError](err); ok {
		return true
	}
	return false
}
