// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type ScenarioBlueprint struct {
	Config   *ScenarioConfig
	Requests []*RequestBlueprint
	Secrets  map[string]any
}

func (s ScenarioBlueprint) String() string {
	sb := strings.Builder{}

	for idx, req := range s.Requests {
		fmt.Fprintf(&sb, "request[%d]:\n%v\n", idx, req)
		if idx != len(s.Requests)-1 {
			sb.WriteString(" and then ")
		}
	}
	sb.WriteByte('\n')
	fmt.Fprintf(&sb, "Config: %v\n", s.Config)

	return sb.String()
}

type RequestBlueprint struct {
	Source   []byte
	Content  []Token
	Pre      Token
	Assert   Token
	Retry    Token
	Post     Token
	Filename string
}

func (bp RequestBlueprint) String() string {
	sb := strings.Builder{}
	for _, token := range bp.Content {
		switch token.Type {
		case Text:
			sb.Write(bp.ExtractPayload(token))
		case Script:
			sb.WriteByte('$')
			sb.WriteString(token.Name)
			sb.WriteByte('{')
			sb.Write(bp.ExtractPayload(token))
			sb.WriteByte('}')
		case IncompleteToken:
		case EOF:
		}
	}
	return fmt.Sprintf(
		"Content: %s\nPre: {%s}\nAssert: {%s}\nRetry: {%s}\nPost: {%s}\n",
		sb.String(),
		bp.ExtractPayload(bp.Pre),
		bp.ExtractPayload(bp.Assert),
		bp.ExtractPayload(bp.Retry),
		bp.ExtractPayload(bp.Post),
	)
}

type ScenarioConfig struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	Env map[string]any `json:"env"`

	ReqTimeout    Duration `json:"req_timeout"`
	ScriptTimeout Duration `json:"script_timeout"`
	Timeout       Duration `json:"timeout"`
	FailFast      bool     `json:"fail_fast"`
	MaxRedirects  int      `json:"max_redirects"`
	MaxRetries    int      `json:"max_retries"`
	MaxBody       string   `json:"max_body"`
	MaxBodySize   DataSize
	EnvMatrix     []map[string]map[string]any
}

type DataSize struct {
	ByteAmount uint64
	Unit       DataUnit
}

func (d DataSize) String() string {
	unitAmount := float64(d.ByteAmount) / float64(d.Unit)
	return fmt.Sprintf("%f%v", unitAmount, d.Unit)
}

type DataUnit int

const (
	Byte   DataUnit = 1
	KiByte DataUnit = 1 << 10
	MiByte DataUnit = 1 << 20
	GiByte DataUnit = 1 << 30
)

func (d DataUnit) String() string {
	switch d {
	case Byte:
		return "B"
	case KiByte:
		return "KiB"
	case MiByte:
		return "MiB"
	case GiByte:
		return "GiB"
	}
	return ""
}

func (sc ScenarioConfig) String() string {
	return fmt.Sprintf(
		"  Name: %s\n  Description: %s\n  Env: %v\n  Timeout: %v\n  Fail fast: %t\n  Max redirects: %d\n  Max retries: %d\n  Max body: %s\n",
		sc.Name,
		sc.Description,
		sc.Env,
		sc.ReqTimeout,
		sc.FailFast,
		sc.MaxRedirects,
		sc.MaxRetries,
		sc.MaxBody,
	)
}

type Duration struct {
	time.Duration
}

func SaneDefaultConfig() ScenarioConfig {
	return ScenarioConfig{
		ReqTimeout:    Duration{5 * time.Second},
		ScriptTimeout: Duration{5 * time.Second},
		Timeout:       Duration{10 * time.Minute},
		FailFast:      true,
		MaxRedirects:  10,
		MaxRetries:    5,
		MaxBodySize: DataSize{
			ByteAmount: 1 << 20,
			Unit:       MiByte,
		},
	}
}

var specialScripts map[string]bool = map[string]bool{"PRE": true, "RETRY": true, "ASSERT": true, "POST": true}

func (d *Duration) UnmarshalJSON(source []byte) error {
	value, err := time.ParseDuration(strings.Trim(string(source), "\""))
	if err != nil {
		return err
	}
	d.Duration = value
	return nil
}

func ParseRequestBlueprint(r io.Reader, filename string) (*RequestBlueprint, error) {
	source, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("Failed to read request source: %w", err)
	}
	res := RequestBlueprint{Source: source, Filename: filename}
	parser := parser{source: source, lineNumber: 1, offsetNumber: 1}

	parser.consumeWhitespace()

ParsingLoop:
	for {
		t, err := parser.lexToken()
		if err != nil {
			return nil, err
		}

		switch t.Type {
		case Text:
			res.Content = append(res.Content, t)
		case Script:
			err := res.setScript(t)
			if err != nil {
				return nil, err
			}
			if specialScripts[t.Name] {
				parser.consumeWhitespace()
			}
		case IncompleteToken:
			return nil, errors.New("Incomplete token in parser output")
		case EOF:
			break ParsingLoop
		}
	}

	return &res, nil
}

func (bp *RequestBlueprint) ExtractPayload(t Token) []byte {
	check := func(start, end int) bool {
		return start <= end && start < len(bp.Source) && start >= 0 && end <= len(bp.Source) && end >= 0
	}
	if check(t.PayloadStart, t.PayloadEnd) {
		return bp.escapePayload(t)
	}
	return []byte{}
}

func (bp *RequestBlueprint) escapePayload(t Token) []byte {
	if t.Type != Text || !t.HasEscapes {
		return bp.Source[t.PayloadStart:t.PayloadEnd]
	}

	content := make([]byte, 0, t.PayloadEnd-t.PayloadStart-1)

	for idx := t.PayloadStart; idx < t.PayloadEnd; idx++ {
		if bp.Source[idx] == '\\' {
			idx++
		}
		content = append(content, bp.Source[idx])
	}
	return content
}
