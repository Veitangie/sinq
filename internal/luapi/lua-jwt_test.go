// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	lua "github.com/yuin/gopher-lua"
)

// Header: {"alg":"HS256","typ":"JWT"}
// Payload: {"sub":"1234567890","name":"John Doe","iat":1516239022}
// Signature: SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c
const testJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

func TestDecodeJWT(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	L.Push(L.NewFunction(DecodeJWT))
	L.Push(lua.LString(testJWT))

	if err := L.PCall(1, 2, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}

	errRes := L.Get(-1)
	valRes := L.Get(-2)
	L.Pop(2)

	if errRes != lua.LNil {
		t.Fatalf("expected nil error, got %v", errRes)
	}

	tbl, ok := valRes.(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %v", valRes.Type())
	}

	headerRaw := tbl.RawGetString("header")
	headerTbl, ok := headerRaw.(*lua.LTable)
	if !ok {
		t.Fatalf("expected header to be table")
	}
	if headerTbl.RawGetString("alg").String() != "HS256" {
		t.Errorf("expected alg=HS256, got %v", headerTbl.RawGetString("alg"))
	}

	claimsRaw := tbl.RawGetString("claims")
	claimsTbl, ok := claimsRaw.(*lua.LTable)
	if !ok {
		t.Fatalf("expected claims to be table")
	}
	if claimsTbl.RawGetString("name").String() != "John Doe" {
		t.Errorf("expected name=John Doe, got %v", claimsTbl.RawGetString("name"))
	}
	if claimsTbl.RawGetString("sub").String() != "1234567890" {
		t.Errorf("expected sub=1234567890, got %v", claimsTbl.RawGetString("sub"))
	}

	methodRaw := tbl.RawGetString("method")
	if methodRaw.String() != "HS256" {
		t.Errorf("expected method=HS256, got %v", methodRaw.String())
	}

	sigRaw := tbl.RawGetString("signature")
	token, _, _ := jwt.NewParser().ParseUnverified(testJWT, jwt.MapClaims{})
	if sigRaw.String() != string(token.Signature) {
		t.Errorf("expected signature bytes to match token signature bytes")
	}
}

func TestDecodeJWT_Failure(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	L.Push(L.NewFunction(DecodeJWT))
	L.Push(lua.LString("garbage.token.string"))

	if err := L.PCall(1, 2, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}

	errRes := L.Get(-1)
	valRes := L.Get(-2)
	L.Pop(2)

	if errRes == lua.LNil {
		t.Fatalf("expected error, got nil")
	}
	if valRes != lua.LNil {
		t.Errorf("expected value to be nil on error, got %v", valRes)
	}
}

func TestVerifyJWT(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	key := "your-256-bit-secret"

	L.Push(L.NewFunction(VerifyJWT))
	L.Push(lua.LString(testJWT))
	L.Push(lua.LString(key))

	if err := L.PCall(2, 2, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}

	errRes := L.Get(-1)
	valRes := L.Get(-2)
	L.Pop(2)

	if errRes != lua.LNil {
		t.Fatalf("expected nil error, got %v", errRes)
	}

	tbl, ok := valRes.(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %v", valRes.Type())
	}

	methodRaw := tbl.RawGetString("method")
	if methodRaw.String() != "HS256" {
		t.Errorf("expected method=HS256, got %v", methodRaw.String())
	}
}

func TestVerifyJWT_Failure(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	L.Push(L.NewFunction(VerifyJWT))
	L.Push(lua.LString(testJWT))
	L.Push(lua.LString("wrong-secret"))

	if err := L.PCall(2, 2, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}

	errRes := L.Get(-1)
	valRes := L.Get(-2)
	L.Pop(2)

	if errRes == lua.LNil {
		t.Fatalf("expected error, got nil")
	}
	if valRes != lua.LNil {
		t.Errorf("expected value to be nil on error, got %v", valRes)
	}
}

func TestSignJWT(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	key := "your-256-bit-secret"
	claims := L.NewTable()
	claims.RawSetString("sub", lua.LString("1234567890"))
	claims.RawSetString("name", lua.LString("John Doe"))
	claims.RawSetString("iat", lua.LNumber(1516239022))

	L.Push(L.NewFunction(SignJWT))
	L.Push(claims)
	L.Push(lua.LString(key))
	L.Push(lua.LString("hs256"))

	if err := L.PCall(3, 2, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}

	errRes := L.Get(-1)
	valRes := L.Get(-2)
	L.Pop(2)

	if errRes != lua.LNil {
		t.Fatalf("expected nil error, got %v", errRes)
	}

	tokenStr := valRes.String()

	L.Push(L.NewFunction(VerifyJWT))
	L.Push(lua.LString(tokenStr))
	L.Push(lua.LString(key))
	if err := L.PCall(2, 2, nil); err != nil {
		t.Fatalf("unexpected lua error during verify: %v", err)
	}
	verErrRes := L.Get(-1)
	L.Pop(2)
	if verErrRes != lua.LNil {
		t.Errorf("expected signed token to be verifiable, but got error: %v", verErrRes)
	}
}

func TestSignJWT_Failures(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	L.Push(L.NewFunction(SignJWT))
	L.Push(L.NewTable())
	L.Push(lua.LString("secret"))
	L.Push(lua.LString("invalid-algo"))
	L.PCall(3, 2, nil)
	errRes := L.Get(-1)
	L.Pop(2)
	if errRes == lua.LNil {
		t.Errorf("expected error on invalid algo")
	}

	L.Push(L.NewFunction(SignJWT))
	L.Push(lua.LString("not-a-table"))
	L.Push(lua.LString("secret"))
	L.PCall(2, 2, nil)
	errRes2 := L.Get(-1)
	L.Pop(2)
	if errRes2 == lua.LNil {
		t.Errorf("expected error on invalid claims type")
	}
}

func TestJWT_MethodResolvers(t *testing.T) {
	invalidKey := "not-a-pem"
	methods := []string{"rs256", "ps256", "es256", "ed"}
	for _, m := range methods {
		_, err := publicKeyFromMethod(invalidKey, m)
		if err == nil {
			t.Errorf("expected error for public key method %s with invalid pem", m)
		}
		_, err = privateKeyFromMethod(invalidKey, m)
		if err == nil {
			t.Errorf("expected error for private key method %s with invalid pem", m)
		}
	}

	_, errPub := publicKeyFromMethod(invalidKey, "unknown")
	if errPub == nil {
		t.Errorf("expected error for unknown public key method")
	}
	_, errPriv := privateKeyFromMethod(invalidKey, "unknown")
	if errPriv == nil {
		t.Errorf("expected error for unknown private key method")
	}
}
