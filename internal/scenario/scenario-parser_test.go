// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/Veitangie/sinq/internal/config"
)

type errorReader struct{}

func (errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("simulated read error")
}

func TestParseRequestBlueprint(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantContent    string
		wantPre        string
		wantPost       string
		wantAssert     string
		wantRetry      string
		wantErr        bool
		checkValidHTTP bool
	}{
		{
			name: "Clean HTTP Start (Strip Pre-script whitespace)",
			input: `$PRE{
										local x = 1
		}
						GET https://api.example.com/users`,
			wantPre: `
										local x = 1
		`,
			wantContent:    "GET https://api.example.com/users",
			checkValidHTTP: true,
		},
		{
			name: "Environment Variable in Path",
			input: `$PRE{ setup() }
						GET /api/v1/users/${env.USER_ID}/details`,
			wantPre:        " setup() ",
			wantContent:    "GET /api/v1/users/$Unnamed_0{env.USER_ID}/details",
			checkValidHTTP: true,
		},
		{
			name: "Environment Variable in Headers and Body",
			input: `POST /login
						Content-Type: application/json
						X-API-Key: ${env.API_KEY}

						{
							"username": "${env.TEST_USER}",
							"password": "password123"
						}`,
			wantContent: `POST /login
						Content-Type: application/json
						X-API-Key: $Unnamed_0{env.API_KEY}

						{
							"username": "$Unnamed_1{env.TEST_USER}",
							"password": "password123"
						}`,
			checkValidHTTP: true,
		},
		{
			name: "Multiple Scripts with spacing",
			input: `$PRE{ x=1 }

						$ASSERT{ assert(200) }

						DELETE /resource/1`,
			wantPre:        " x=1 ",
			wantAssert:     " assert(200) ",
			wantContent:    "DELETE /resource/1",
			checkValidHTTP: true,
		},
		{
			name: "Ambiguous Dollar Signs (Not Scripts)",
			input: `POST /calculate
						Body: \$100.00 vs \$200.00`,
			wantContent: `POST /calculate
						Body: $100.00 vs $200.00`,
			checkValidHTTP: true,
		},
		{
			name: "Dollar followed by braces (Not a keyword)",
			input: `GET /tags
						X-Tag: \$UNKNOWN{value}`,
			wantContent: `GET /tags
						X-Tag: $UNKNOWN{value}`,
			checkValidHTTP: true,
		},
		{
			name: "Complex Lua Trap in PRE",
			input: `$PRE{ print("}}}}") }
				GET /`,
			wantPre:        ` print("}}}}") `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		// --- LUA PARSER STRESS TESTS ---
		{
			name:           "Lua Trap: Long Brackets",
			input:          `$PRE{ x = [[ } ]] }GET /`,
			wantPre:        ` x = [[ } ]] `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:           "Lua Trap: Long Brackets with Levels",
			input:          `$PRE{ code = [=[ if x[i] then return "]]" end ]=] }GET /`,
			wantPre:        ` code = [=[ if x[i] then return "]]" end ]=] `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:           "Lua Trap: Escaped Quotes",
			input:          `$PRE{ msg = "Hello \"World\"" }GET /`,
			wantPre:        ` msg = "Hello \"World\"" `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:           "Lua Trap: Nested Braces",
			input:          `$PRE{ if x then { return } end }GET /`,
			wantPre:        ` if x then { return } end `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		// --- LOGIC EDGE CASES ---
		{
			name:           "Edge Case: Semantic Skip (Partial Long String)",
			input:          `$PRE{ t = [{a=1}] }GET /`,
			wantPre:        ` t = [{a=1}] `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:    "Error: Duplicate Script",
			input:   "$PRE{ x=1 } $PRE{ x=2 }",
			wantErr: true,
		},
		{
			name:           "Lua Trap: Mixed Level Long Brackets",
			input:          `$PRE{ code = [==[ ]===] }}}}} ]==] }GET /`,
			wantPre:        ` code = [==[ ]===] }}}}} ]==] `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		// --- COMMENT PARSING & TRAPS ---
		{
			name: "Comment: Short comment with brace",
			input: `$PRE{
			x = 1 -- This comment contains a closing brace }
		}GET /`,
			wantPre: `
			x = 1 -- This comment contains a closing brace }
		`,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:           "Comment: Long Comment (Level 0)",
			input:          `$PRE{ --[[ } ]] }GET /`,
			wantPre:        ` --[[ } ]] `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:           "Comment: Long Comment (Level 2)",
			input:          `$PRE{ --[=[ This ignores ]] and } because level matches ]=] }GET /`,
			wantPre:        ` --[=[ This ignores ]] and } because level matches ]=] `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name: "Comment: False Long Comment (The 'Equals' Trap)",
			input: `$PRE{
			--[= This is just a short comment with a } inside
		}GET /`,
			wantPre: `
			--[= This is just a short comment with a } inside
		`,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name: "Comment: String inside Comment",
			input: `$PRE{
			-- "This string is commented out } "
		}GET /`,
			wantPre: `
			-- "This string is commented out } "
		`,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:           "Comment: Comment inside String",
			input:          `$PRE{ msg = "-- This is NOT a comment, it is a string with }" }GET /`,
			wantPre:        ` msg = "-- This is NOT a comment, it is a string with }" `,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name: "Comment: Long Bracket inside Short Comment",
			input: `$PRE{
			-- We can write [[ brackets }]] here without consequence }
		}GET /`,
			wantPre: `
			-- We can write [[ brackets }]] here without consequence }
		`,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name: "Bug Reproduction: Unary Minus Swallowing",
			input: `$PRE{
			local t = -{ a = 1 }
		}GET /`,
			wantPre: `
			local t = -{ a = 1 }
		`,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name: "Bug Reproduction: String Swallowing Closer",
			input: `$PRE{
			x = [[data]]}GET /`,
			wantPre: `
			x = [[data]]`,
			wantContent:    "GET /",
			checkValidHTTP: true,
		},
		{
			name:    "Error: Unexpected EOF after escape character",
			input:   "GET /api/v1/resource\\",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			got, err := ParseRequestBlueprint(r, "test")

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRequestBlueprint() error = %v, wantErr %t", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}

			if string(got.ExtractPayload(got.Pre)) != tt.wantPre {
				t.Errorf("Pre script mismatch.\nGot:  %q\nWant: %q", string(got.ExtractPayload(got.Pre)), tt.wantPre)
			}
			if string(got.ExtractPayload(got.Post)) != tt.wantPost {
				t.Errorf("Post script mismatch.\nGot:  %q\nWant: %q", string(got.ExtractPayload(got.Post)), tt.wantPost)
			}
			if string(got.ExtractPayload(got.Assert)) != tt.wantAssert {
				t.Errorf("Assert script mismatch.\nGot:  %q\nWant: %q", string(got.ExtractPayload(got.Assert)), tt.wantAssert)
			}
			if string(got.ExtractPayload(got.Retry)) != tt.wantRetry {
				t.Errorf("Retry script mismatch.\nGot:  %q\nWant: %q", string(got.ExtractPayload(got.Retry)), tt.wantRetry)
			}

			result := strings.Builder{}
			for _, token := range got.Content {
				switch token.Type {
				case Text:
					result.Write(got.ExtractPayload(token))
				case Script:
					result.WriteByte('$')
					result.WriteString(token.Name)
					result.WriteByte('{')
					result.Write(got.ExtractPayload(token))
					result.WriteByte('}')
				}
			}
			if result.String() != tt.wantContent {
				t.Errorf("Content mismatch.\nGot:\n%s\nWant:\n%s", result.String(), tt.wantContent)
			}

			if tt.checkValidHTTP && len(got.Content) > 0 {
				firstChar := rune(result.String()[0])
				if unicode.IsSpace(firstChar) {
					t.Errorf("Invalid HTTP: Content starts with whitespace (char code %d). Expected Method (GET/POST/etc). content: %q", firstChar, result.String())
				}
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAmount uint64
		wantUnit   config.DataUnit
		wantErr    bool
	}{
		{"Bytes", "500 B", 500, config.Byte, false},
		{"Bytes Without Space", "1024Byte", 1024, config.Byte, false},
		{"Kilobytes", "2 K", 2048, config.KiByte, false},
		{"Megabytes Decimals", "1.5 MiB", 1572864, config.MiByte, false},
		{"Gigabytes", "10 G", 10737418240, config.GiByte, false},
		{"Missing Unit Defaults to B", "256", 256, config.Byte, false},
		{"Whitespace Heavy", "   42   KiByte  ", 43008, config.KiByte, false},
		{"Empty String", "", 0, config.Byte, true},
		{"Invalid Number", "abc MiB", 0, config.Byte, true},
		{"Negative Number", "-10 M", 0, config.Byte, true},
		{"Unknown Unit", "100 ZB", 0, config.Byte, true},
		{"Multiple Decimals", "10.5.5 MiB", 0, config.Byte, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.ParseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.ByteAmount != tt.wantAmount {
					t.Errorf("parseSize(%q) byte amount = %d, want %d", tt.input, got.ByteAmount, tt.wantAmount)
				}
				if got.Unit != tt.wantUnit {
					t.Errorf("parseSize(%q) unit = %v, want %v", tt.input, got.Unit, tt.wantUnit)
				}
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	t.Run("Valid JSON Parsing", func(t *testing.T) {
		jsonStr := `{
			"name": "Test Config",
			"max_body": "5 MiB",
			"fail_fast": true
		}`
		cfg := SaneDefaultConfig()
		err := ParseConfig(&cfg, strings.NewReader(jsonStr))
		if err != nil {
			t.Fatalf("ParseConfig failed: %v", err)
		}

		if cfg.Name != "Test Config" {
			t.Errorf("Expected Name 'Test Config', got %q", cfg.Name)
		}
		if cfg.FailFast != true {
			t.Errorf("Expected FailFast true")
		}
		if cfg.MaxBodySize.ByteAmount != 5*uint64(config.MiByte) {
			t.Errorf("Expected MaxBodySize to be parsed to 5 MiB, got %d bytes", cfg.MaxBodySize.ByteAmount)
		}
	})

	t.Run("Invalid JSON structure", func(t *testing.T) {
		jsonStr := `{ "name": "bad", "max_body": "10 M"`
		cfg := SaneDefaultConfig()
		err := ParseConfig(&cfg, strings.NewReader(jsonStr))
		if err == nil {
			t.Error("Expected error on invalid JSON, got nil")
		}
	})

	t.Run("Invalid MaxBody format", func(t *testing.T) {
		jsonStr := `{ "max_body": "invalid_size" }`
		cfg := SaneDefaultConfig()
		err := ParseConfig(&cfg, strings.NewReader(jsonStr))
		if err == nil {
			t.Error("Expected error on invalid max_body string, got nil")
		}
	})

	t.Run("Reader Error", func(t *testing.T) {
		cfg := SaneDefaultConfig()
		err := ParseConfig(&cfg, errorReader{})
		if err == nil {
			t.Error("Expected error on reader failure, got nil")
		}
	})
}

func TestDataSize_StringAndUnit(t *testing.T) {
	tests := []struct {
		size     config.DataSize
		expected string
	}{
		{config.DataSize{ByteAmount: 512, Unit: config.Byte}, "512.000000B"},
		{config.DataSize{ByteAmount: 2048, Unit: config.KiByte}, "2.000000KiB"},
		{config.DataSize{ByteAmount: 1572864, Unit: config.MiByte}, "1.500000MiB"},
		{config.DataSize{ByteAmount: 10737418240, Unit: config.GiByte}, "10.000000GiB"},
	}

	for _, tt := range tests {
		got := tt.size.String()
		if got != tt.expected {
			t.Errorf("DataSize.String() = %q, want %q", got, tt.expected)
		}
	}

	unmappedUnit := config.DataUnit(999)
	if unmappedUnit.String() != "" {
		t.Errorf("Expected empty string for unknown unit, got %q", unmappedUnit.String())
	}
}

func FuzzParseRequestBlueprint(f *testing.F) {
	seedCorpus := []string{
		`$PRE{ local x = 1 } GET /`,
		`GET /api/v1/users/${env.USER_ID}/details`,
		"POST /login\nContent-Type: application/json\n\n{\"user\": \"${env.USER}\"}",
		`$ASSERT{ assert(200) } DELETE /resource/1`,
		`Body: \$100.00 vs \$200.00`,
		`$PRE{ print("}}}}") } GET /`,
		`$PRE{ x = [[ } ]] } GET /`,
		`$PRE{ code = [=[ if x[i] then return "]]" end ]=] } GET /`,
		`$PRE{ msg = "Hello \"World\"" } GET /`,
		`$PRE{ if x then { return } end } GET /`,
		`$PRE{ t = [{a=1}] } GET /`,
		`$PRE{ --[[ } ]] } GET /`,
		`$PRE{ --[=[ This ignores ]] and } ]=] } GET /`,
		`$PRE{ -- "This string is commented out } " } GET /`,
		`$PRE{ local t = -{ a = 1 } } GET /`,
		`$PRE{ x = [[data]]} GET /`,
		`GET /api/v1/resource\\`,
		`$RETRY{ return 1 } GET /`,
		`$POST{ sinq.test.fail("boom") } GET /`,
		`$PRE{ 
	_G.TOKEN = "abc" 
}
POST /api/v1/jobs
Authorization: Bearer ${ _G.TOKEN }
X-Correlation-ID: ${ generate_uuid() }

{
	"job_name": "export",
	"dynamic_val": "${ env.VAL }"
}

$POST{
	_G.JOB_ID = sinq.requests[1].body.id
}
$ASSERT{
	if sinq.requests[1].code ~= 202 then sinq.test.fail("bad code") end
}`,
		`GET /jobs/${ _G.JOB_ID }/status

$RETRY{
	if sinq.requests[2].body.status == "pending" then return 500 end
	return -1
}

$ASSERT{
	if sinq.requests[2].body.status ~= "complete" then error("failed") end
}`,
		`PUT /upload?token=${token}&id=${id}&force=${true}
X-Custom: ${header1}${header2}

$ASSERT{ assert(200) }`,
		`$PRE{ missing_closer = 1  
GET /
$POST{ print("will fail but shouldn't panic") }`,
		`POST /upload HTTP/1.1
X-Token: ${token}
X-Giant-Header: ` + strings.Repeat("a", 5000) + `
Content-Type: application/json

{"data": "value"}`,
		`GET /api/v1/bad-format HTTP/1.1$PRE{ local a = 1 }`,
		`POST /data
Body: [=[ This is a string [[ that never ends ]=]
$ASSERT{ assert(false) }`,
		"$PRE{ x=1 }\r\n\r\n\t\t\n   $POST{ y=2 }\nGET / HTTP/1.1\r\n\r\n",
		`GET /
Header: $NOT_A_SCRIPT { but it looks like one }
Header2: $PRE { space before brace }
Header3: $PRE{no_space_but_valid()}`,
		`GET /test HTTP/1.1
X-Escape: \`,
		`$PRE{
	-- This comment ends without a newline
}GET /`,
	}

	for _, seed := range seedCorpus {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		r := strings.NewReader(data)

		bp, err := ParseRequestBlueprint(r, "fuzz.sinq")

		if err != nil {
			return
		}

		verifyBounds := func(tok Token) {
			if tok.Type != IncompleteToken {
				_ = bp.ExtractPayload(tok)
			}
		}

		verifyBounds(bp.Pre)
		verifyBounds(bp.Post)
		verifyBounds(bp.Assert)
		verifyBounds(bp.Retry)

		for _, tok := range bp.Content {
			verifyBounds(tok)
		}
	})
}
