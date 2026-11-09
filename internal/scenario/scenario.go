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

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/scanner"
)

type ScenarioBlueprint struct {
	SeqId   int
	Config  *ScenarioConfig
	Stages  []*StageBlueprint
	Secrets map[string]any
}

func (s ScenarioBlueprint) CountVariants() int {
	mod := 1
	for _, mat := range s.Config.EnvMatrix {
		if len(mat) > 0 {
			mod *= len(mat)
		}
	}
	return mod
}

func (s ScenarioBlueprint) String() string {
	sb := strings.Builder{}

	for idx, req := range s.Stages {
		fmt.Fprintf(&sb, "stage[%d]:\n%v\n", idx, req)
		if idx != len(s.Stages)-1 {
			sb.WriteString(" and then ")
		}
	}
	sb.WriteByte('\n')
	fmt.Fprintf(&sb, "Config: %v\n", s.Config)

	return sb.String()
}

type StageBlueprint struct {
	Source   []byte
	Content  []Token
	Pre      Token
	Assert   Token
	Retry    Token
	Post     Token
	Filename string
	Rootname string
	Name     string
}

func (bp StageBlueprint) IsEmpty() bool {
	return len(bp.Content) == 0 && bp.Pre.IsEmpty() && bp.Retry.IsEmpty() && bp.Assert.IsEmpty() && bp.Post.IsEmpty()
}

func (bp StageBlueprint) Validate() error {
	if len(bp.Content) == 0 && !bp.Retry.IsEmpty() {
		return errors.New("Invalid stage: $RETRY needs a request to retry")
	}
	return nil
}

func (bp StageBlueprint) IsNoRequest() bool {
	return len(bp.Content) == 0 && (!bp.Pre.IsEmpty() || !bp.Assert.IsEmpty() || !bp.Post.IsEmpty())
}

func (bp StageBlueprint) String() string {
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
		case Delimiter:
			sb.WriteString("\n### ")
			sb.Write(bp.ExtractPayload(token))
			sb.WriteByte('\n')
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
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Tags        map[string]struct{} `json:"-"`
	TagsList    []string            `json:"-"`

	Env map[string]any `json:"-"`

	ReqTimeout    Duration                    `json:"req_timeout"`
	ScriptTimeout Duration                    `json:"script_timeout"`
	Timeout       Duration                    `json:"timeout"`
	FailFast      bool                        `json:"fail_fast"`
	MaxRedirects  int                         `json:"max_redirects"`
	MaxRetries    int                         `json:"max_retries"`
	MaxBody       string                      `json:"max_body"`
	MaxBodySize   config.DataSize             `json:"-"`
	EnvMatrix     []map[string]map[string]any `json:"-"`
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
		Tags:          make(map[string]struct{}),
		TagsList:      make([]string, 0),
		Env:           make(map[string]any),
		ReqTimeout:    Duration{5 * time.Second},
		ScriptTimeout: Duration{5 * time.Second},
		Timeout:       Duration{10 * time.Minute},
		FailFast:      true,
		MaxRedirects:  5,
		MaxRetries:    10,
		MaxBodySize: config.DataSize{
			ByteAmount: 1 << 20,
			Unit:       config.MiByte,
		},
		EnvMatrix: make([]map[string]map[string]any, 0),
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

func ParseStageBlueprints(r io.Reader, filename, rootName string) ([]*StageBlueprint, error) {
	source, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("Failed to read request source: %w", err)
	}
	currentRes := &StageBlueprint{Source: source, Filename: filename, Rootname: rootName}
	res := []*StageBlueprint{currentRes}
	parser := parser{ByteScanner: scanner.NewByteScanner(source)}

	parser.consumeWhitespace()

ParsingLoop:
	for {
		t, err := parser.lexToken()
		if err != nil {
			return nil, err
		}

		switch t.Type {
		case Text:
			currentRes.Content = append(currentRes.Content, t)
		case Script:
			err := currentRes.setScript(t)
			if err != nil {
				return nil, err
			}
			if specialScripts[t.Name] {
				parser.consumeWhitespace()
			}
		case IncompleteToken:
			return nil, errors.New("Incomplete token in parser output")
		case Delimiter:
			if !currentRes.IsEmpty() {
				if err := currentRes.Validate(); err != nil {
					return res, err
				}
				currentRes = &StageBlueprint{Source: source, Filename: filename, Rootname: rootName}
				res = append(res, currentRes)
			}
			currentRes.Name = string(currentRes.ExtractPayload(t))
			parser.consumeWhitespace()
		case EOF:
			break ParsingLoop
		}
	}

	if currentRes.IsEmpty() {
		return res, errors.New("Empty stage (did you add ### at the end, denoting the start of a new stage?)")
	}

	return res, currentRes.Validate()
}

func (bp *StageBlueprint) ExtractPayload(t Token) []byte {
	check := func(start, end int) bool {
		return start <= end && start < len(bp.Source) && start >= 0 && end <= len(bp.Source) && end >= 0
	}
	if check(t.PayloadStart, t.PayloadEnd) {
		return bp.escapePayload(t)
	}
	return []byte{}
}

func (bp *StageBlueprint) escapePayload(t Token) []byte {
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
