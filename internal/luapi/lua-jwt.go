// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	lua "github.com/yuin/gopher-lua"
)

func DecodeJWT(ls *lua.LState) int {
	source := ls.CheckString(1)
	token, _, err := jwt.NewParser(jwt.WithoutClaimsValidation()).ParseUnverified(source, jwt.MapClaims{})
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		res := ls.NewTable()
		res.RawSetString("header", ToLuaValue(token.Header, ls))
		res.RawSetString("signature", lua.LString(token.Signature))
		if token.Method != nil {
			res.RawSetString("method", lua.LString(token.Method.Alg()))
		}

		if mapClaims, ok := token.Claims.(jwt.MapClaims); ok {
			res.RawSetString("claims", ToLuaValue(map[string]any(mapClaims), ls))
		}

		ls.Push(res)
		ls.Push(lua.LNil)
	}

	return 2
}

func publicKeyFromMethod(key, method string) (any, error) {
	keyBytes := []byte(key)

	if strings.HasPrefix(method, "hs") {
		return keyBytes, nil
	}

	if strings.HasPrefix(method, "rs") || strings.HasPrefix(method, "ps") {
		return jwt.ParseRSAPublicKeyFromPEM(keyBytes)
	}

	if strings.HasPrefix(method, "es") {
		return jwt.ParseECPublicKeyFromPEM(keyBytes)
	}

	if strings.HasPrefix(method, "ed") {
		return jwt.ParseEdPublicKeyFromPEM(keyBytes)
	}

	return nil, fmt.Errorf("Unsupported encryption method: %v", method)
}

func privateKeyFromMethod(key, method string) (any, error) {
	keyBytes := []byte(key)

	if strings.HasPrefix(method, "hs") {
		return keyBytes, nil
	}

	if strings.HasPrefix(method, "rs") || strings.HasPrefix(method, "ps") {
		return jwt.ParseRSAPrivateKeyFromPEM(keyBytes)
	}

	if strings.HasPrefix(method, "es") {
		return jwt.ParseECPrivateKeyFromPEM(keyBytes)
	}

	if strings.HasPrefix(method, "ed") {
		return jwt.ParseEdPrivateKeyFromPEM(keyBytes)
	}

	return nil, fmt.Errorf("Unsupported encryption method: %v", method)
}

func VerifyJWT(ls *lua.LState) int {
	source := ls.CheckString(1)
	key := ls.CheckString(2)
	maybeAlgo := ls.OptString(3, "")
	maybeAlgo = strings.ToLower(maybeAlgo)
	token, err := jwt.NewParser().Parse(source, func(t *jwt.Token) (any, error) {
		if t.Method == nil {
			return nil, errors.New("Expected method to be defined, got nil")
		}

		if maybeAlgo != "" && strings.ToLower(t.Method.Alg()) != maybeAlgo {
			return nil, fmt.Errorf("Expected method to be %v, got %v", maybeAlgo, t.Method.Alg())
		}

		return publicKeyFromMethod(key, strings.ToLower(t.Method.Alg()))
	})

	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		res := ls.NewTable()
		res.RawSetString("header", ToLuaValue(token.Header, ls))
		res.RawSetString("signature", lua.LString(token.Signature))
		if token.Method != nil {
			res.RawSetString("method", lua.LString(token.Method.Alg()))
		}

		if mapClaims, ok := token.Claims.(jwt.MapClaims); ok {
			res.RawSetString("claims", ToLuaValue(map[string]any(mapClaims), ls))
		}

		ls.Push(res)
		ls.Push(lua.LNil)
	}

	return 2
}

var normalizedMethodName map[string]string = map[string]string{
	"hs256": "HS256",
	"hs384": "HS384",
	"hs512": "HS512",
	"rs256": "RS256",
	"rs384": "RS384",
	"rs512": "RS512",
	"ps256": "PS256",
	"ps384": "PS384",
	"ps512": "PS512",
	"es256": "ES256",
	"es384": "ES384",
	"es512": "ES512",
	"eddsa": "EdDSA",
}

func SignJWT(ls *lua.LState) int {
	source := ls.CheckAny(1)
	key := ls.CheckString(2)
	maybeMethod := strings.ToLower(ls.OptString(3, "hs256"))
	method, ok := normalizedMethodName[maybeMethod]
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown signing method: %s", maybeMethod)))
		return 2
	}

	claimsAny, err := FromLuaValue(source, make(map[*lua.LTable]bool))
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Failed to serialize value: %s", err.Error())))
		return 2
	}

	mapClaims, ok := claimsAny.(map[string]any)
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("Failed to parse claims, make sure you use only string keys for all tables, and do not mix lists with tables"))
		return 2
	}

	token := jwt.NewWithClaims(jwt.GetSigningMethod(method), jwt.MapClaims(mapClaims))
	if token == nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("Failed to construct token"))
		return 2
	}

	keyAny, err := privateKeyFromMethod(key, maybeMethod)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	res, err := token.SignedString(keyAny)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LString(res))
		ls.Push(lua.LNil)
	}

	return 2
}
