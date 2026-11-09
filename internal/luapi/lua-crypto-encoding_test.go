// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import "testing"

func TestLuaCrypto_Base64MatchesTheEncodingHelpers(t *testing.T) {
	tests := []struct {
		name string
		call string
	}{
		{"md5", `sinq.crypto.md5("abc", enc)`},
		{"sha1", `sinq.crypto.sha1("abc", enc)`},
		{"sha256", `sinq.crypto.sha256("abc", enc)`},
		{"sha512", `sinq.crypto.sha512("abc", enc)`},
		{"hmac", `sinq.crypto.hmac("abc", "sha256", "key", enc)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSinqScript(t, `
				local function hash(enc) return `+tt.call+` end
				local raw = hash("raw")
				local got, want = hash("base64"), sinq.encoding.base64Encode(raw)
				assert(got == want, "base64: got " .. tostring(got) .. ", want " .. want)
				got, want = hash("base64url"), sinq.encoding.base64UrlEncode(raw)
				assert(got == want, "base64url: got " .. tostring(got) .. ", want " .. want)
			`)
		})
	}
}
