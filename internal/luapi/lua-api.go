package luapi

import (
	"bytes"
	_ "embed"
	"sync"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

//go:embed lua/retry-when.lua
var retryWhenScript []byte

var RetryWhen func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(retryWhenScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})

//go:embed lua/retry-exponential.lua
var retryExponentialScript []byte

var RetryExponential func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(retryExponentialScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})

//go:embed lua/retry-jitter.lua
var retryJitterScript []byte

var RetryJitter func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(retryJitterScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})

//go:embed lua/assert-code.lua
var assertCodeScript []byte

var AssertCode func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(assertCodeScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})

//go:embed lua/assert-equals.lua
var assertEqualsScript []byte

var AssertEquals func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(assertEqualsScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})

//go:embed lua/assert-contains.lua
var assertContainsScript []byte

var AssertContains func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(assertContainsScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})

//go:embed lua/assert-condition.lua
var assertConditionScript []byte

var AssertCondition func() *lua.FunctionProto = sync.OnceValue(func() *lua.FunctionProto {
	ast, err := parse.Parse(bytes.NewReader(assertConditionScript), "luapi")
	if err != nil {
		panic(err)
	}
	compiled, err := lua.Compile(ast, "luapi")
	if err != nil {
		panic(err)
	}
	return compiled
})
