// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type zeroSource struct{}

func (zeroSource) Uint64() uint64 { return 0 }

type fixedSeqSource struct {
	vals []uint64
	idx  int
}

func (s *fixedSeqSource) Uint64() uint64 {
	v := s.vals[s.idx%len(s.vals)]
	s.idx++
	return v
}

func newLuaFakerContext(t *testing.T) *LuaContext {
	t.Helper()
	lc := NewLuaContext(nil, false, nil, nil)
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)
	return lc
}

func callFakeFn(t *testing.T, lc *LuaContext, fn lua.LGFunction) string {
	t.Helper()
	lc.Push(lc.NewFunction(fn))
	if err := lc.PCall(0, 1, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}
	v := lc.Get(-1)
	lc.Pop(1)
	return v.String()
}

func TestLuaFaker(t *testing.T) {
	lc := NewLuaContext(nil, false, nil, nil)
	defer lc.Close()

	lc.SetupScenarioEnvironment(
		func(ls *lua.LState) int { return 0 },
		func(ls *lua.LState) int { return 0 },
		func(ls *lua.LState) int { return 0 },
		nil,
		nil,
	)

	script := `
		sinq.fake.setSeed(1337)
		local u1 = sinq.fake.uuid()
		local e1 = sinq.fake.email()
		local t1 = sinq.fake.trace()
		
		sinq.fake.setSeed(1337)
		local u2 = sinq.fake.uuid()
		local e2 = sinq.fake.email()
		local t2 = sinq.fake.trace()
		
		assert(u1 == u2, "Seeding is not deterministic for uuid")
		assert(e1 == e2, "Seeding is not deterministic for email")
		assert(t1 == t2, "Seeding is not deterministic for trace")

		assert(type(sinq.fake.uuid()) == "string")
		assert(type(sinq.fake.email()) == "string")
		assert(type(sinq.fake.phone()) == "string")
		assert(type(sinq.fake.name()) == "string")
		assert(type(sinq.fake.firstName()) == "string")
		assert(type(sinq.fake.lastName()) == "string")
		assert(type(sinq.fake.username()) == "string")
		assert(type(sinq.fake.password()) == "string")
		assert(type(sinq.fake.word()) == "string")
		assert(type(sinq.fake.address()) == "string")
		assert(type(sinq.fake.company()) == "string")
		assert(type(sinq.fake.ipv4()) == "string")
		assert(type(sinq.fake.ipv6()) == "string")
		assert(type(sinq.fake.url()) == "string")
		assert(type(sinq.fake.userAgent()) == "string")
		
		local trace = sinq.fake.trace()
		assert(type(trace) == "string")
		assert(string.match(trace, "^00%-[a-f0-9]+%-[a-f0-9]+%-01$"), "Trace format invalid")
		
		local ts = sinq.fake.timestamp(1000)
		assert(type(ts) == "number")
		
		local intVal = sinq.fake.int(1, 10)
		assert(type(intVal) == "number" and intVal >= 1 and intVal <= 10)
		
		local floatVal = sinq.fake.float(2, 1, 10)
		assert(type(floatVal) == "number" and floatVal >= 1 and floatVal <= 10)
		
		local sh = sinq.fake.shakespeare()
		assert(type(sh) == "boolean")
		
		local picked = sinq.fake.oneOf({"a", "b", "c"})
		assert(picked == "a" or picked == "b" or picked == "c")
	`

	fn, err := lc.LoadString(script)
	if err != nil {
		t.Fatalf("Failed to compile lua test script: %v", err)
	}

	err = lc.RunSandboxed(fn.Proto, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to execute lua test script: %v", err)
	}
}

func TestFakePhone_FirstDigitInRange(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	for seed := 0; seed < 50; seed++ {
		lc.generator = *rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
		phone := callFakeFn(t, lc, lc.FakePhone)

		var code string
		for _, c := range phoneCodes {
			if strings.HasPrefix(phone, c) {
				code = c
				break
			}
		}
		if code == "" {
			t.Fatalf("seed %d: phone %q did not start with any known country code", seed, phone)
		}
		rest := phone[len(code):]
		if len(rest) == 0 {
			t.Fatalf("seed %d: phone %q has no digits after country code", seed, phone)
		}
		first := rest[0]
		if first < '1' || first > '9' {
			t.Errorf("seed %d: expected first phone digit to be an ASCII '1'-'9' character, got %d (%q) (phone=%q)", seed, first, first, phone)
		}
	}
}

func TestFakePhone_DigitsArePrintableASCII(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	lc.generator = *rand.New(rand.NewPCG(42, 42))
	phone := callFakeFn(t, lc, lc.FakePhone)

	var code string
	for _, c := range phoneCodes {
		if strings.HasPrefix(phone, c) {
			code = c
			break
		}
	}
	if code == "" {
		t.Fatalf("phone %q did not start with any known country code", phone)
	}
	rest := phone[len(code):]
	if len(rest) != 10 {
		t.Fatalf("expected 10 digit characters after country code, got %d (phone=%q)", len(rest), phone)
	}

	if rest[0] < '1' || rest[0] > '9' {
		t.Errorf("digit[0] = %d (%q), want an ASCII '1'-'9' character (phone=%q, bytes=%v)", rest[0], rest[0], phone, []byte(rest))
	}
	for i := 1; i < 10; i++ {
		if rest[i] < '0' || rest[i] > '9' {
			t.Errorf("digit[%d] = %d (%q), want an ASCII '0'-'9' character (phone=%q, bytes=%v)", i, rest[i], rest[i], phone, []byte(rest))
		}
	}
}

func TestFakeURL_SchemeDistribution(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	const total = 60
	httpsCount := 0
	for seed := 0; seed < total; seed++ {
		lc.generator = *rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
		url := callFakeFn(t, lc, lc.FakeURL)
		if strings.HasPrefix(url, "https://") {
			httpsCount++
		} else if !strings.HasPrefix(url, "http://") {
			t.Fatalf("seed %d: unexpected URL scheme: %q", seed, url)
		}
	}

	if httpsCount <= total/4 || httpsCount >= total {
		t.Errorf("expected a majority-but-not-all https ratio (~5/6), got %d/%d", httpsCount, total)
	}
}

func TestFakeURL_SizeBoundary(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	extractHost := func(u string) string {
		_, after, ok := strings.Cut(u, "://")
		if !ok {
			t.Fatalf("URL missing scheme separator: %q", u)
		}
		host, _, _ := strings.Cut(after, "/")
		return host
	}
	isBareDomain := func(host string) bool {
		for _, d := range domains {
			if host == d {
				return true
			}
		}
		return false
	}

	lc.generator = *rand.New(rand.NewPCG(4, 4))
	url3 := callFakeFn(t, lc, lc.FakeURL)
	if host := extractHost(url3); !isBareDomain(host) {
		t.Errorf("size==3: expected bare domain host (no noun prefix), got %q (url=%q)", host, url3)
	}

	lc.generator = *rand.New(rand.NewPCG(1, 1))
	url4 := callFakeFn(t, lc, lc.FakeURL)
	if host := extractHost(url4); isBareDomain(host) {
		t.Errorf("size==4: expected noun-prefixed host, got bare domain %q (url=%q)", host, url4)
	}
}

func TestFakeFloat_DefaultBoundsAreNotEqual(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	lc.generator = *rand.New(rand.NewPCG(1, 1))
	lc.Push(lc.NewFunction(lc.FakeFloat))
	if err := lc.PCall(0, 1, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}
	v := lc.Get(-1)
	lc.Pop(1)

	got, ok := v.(lua.LNumber)
	if !ok {
		t.Fatalf("expected number, got %v", v.Type())
	}
	f := float64(got)
	if f == math.MaxFloat64 || f == -math.MaxFloat64 {
		t.Errorf("expected fake.float() with no args to pick a random value strictly between the default bounds, got exactly %v (default mi/ma sign likely inverted)", f)
	}
}

func TestFakeFloat_ExactFormula(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	const seed = 999
	probe := rand.New(rand.NewPCG(seed, seed))
	drawn := probe.Float64()

	mi, ma := 2.0, 10.0
	expected := mi + drawn*(ma-mi)

	lc.generator = *rand.New(rand.NewPCG(seed, seed))
	lc.Push(lc.NewFunction(lc.FakeFloat))
	lc.Push(lua.LNumber(mi))
	lc.Push(lua.LNumber(ma))
	if err := lc.PCall(2, 1, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}
	v := lc.Get(-1)
	lc.Pop(1)

	got, ok := v.(lua.LNumber)
	if !ok {
		t.Fatalf("expected number, got %v", v.Type())
	}
	if math.Abs(float64(got)-expected) > 1e-9 {
		t.Errorf("FakeFloat(%v,%v) = %v, want %v (mi + gen.Float64()*(ma-mi))", mi, ma, float64(got), expected)
	}
	if float64(got) < mi || float64(got) > ma {
		t.Errorf("FakeFloat(%v,%v) = %v out of bounds", mi, ma, float64(got))
	}
}

func TestFakeFloat_BoundaryEquality_NoRNGConsumed(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	const seed = 55
	lc.generator = *rand.New(rand.NewPCG(seed, seed))

	lc.Push(lc.NewFunction(lc.FakeFloat))
	lc.Push(lua.LNumber(7))
	lc.Push(lua.LNumber(7))
	if err := lc.PCall(2, 1, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}
	v := lc.Get(-1)
	lc.Pop(1)
	if v != lua.LNumber(7) {
		t.Fatalf("FakeFloat(7,7) = %v, want exactly 7", v)
	}

	gotNext := lc.generator.Uint64()
	wantNext := rand.New(rand.NewPCG(seed, seed)).Uint64()
	if gotNext != wantNext {
		t.Errorf("FakeFloat(7,7) unexpectedly consumed RNG state: next draw = %d, want %d (fresh, unconsumed)", gotNext, wantNext)
	}
}

func TestFakeFloat_MinGreaterThanMax_ReturnsMinExactly(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	for _, seed := range []uint64{1, 2, 3, 4, 5} {
		lc.generator = *rand.New(rand.NewPCG(seed, seed))
		lc.Push(lc.NewFunction(lc.FakeFloat))
		lc.Push(lua.LNumber(10))
		lc.Push(lua.LNumber(3))
		if err := lc.PCall(2, 1, nil); err != nil {
			t.Fatalf("unexpected lua error: %v", err)
		}
		v := lc.Get(-1)
		lc.Pop(1)
		if v != lua.LNumber(10) {
			t.Errorf("seed %d: FakeFloat(10,3) = %v, want exactly 10 (mi>=ma should short-circuit)", seed, v)
		}
	}
}

func TestFakeShakespeare_MatchesRawModulo(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	seq := []uint64{0, 1, 2, 3}
	lc.generator = *rand.New(&fixedSeqSource{vals: seq})

	for i, raw := range seq {
		want := int64(raw)%2 == 0
		lc.Push(lc.NewFunction(lc.FakeShakespeare))
		if err := lc.PCall(0, 1, nil); err != nil {
			t.Fatalf("unexpected lua error: %v", err)
		}
		v := lc.Get(-1)
		lc.Pop(1)
		got, ok := v.(lua.LBool)
		if !ok {
			t.Fatalf("draw %d: expected bool, got %v", i, v.Type())
		}
		if bool(got) != want {
			t.Errorf("draw %d (raw=%d): FakeShakespeare() = %v, want %v", i, raw, bool(got), want)
		}
	}
}

func TestFakeTrace_AllZeroSourceForcesNonZero(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	lc.generator = *rand.New(zeroSource{})
	trace := callFakeFn(t, lc, lc.FakeTrace)

	parts := strings.Split(trace, "-")
	if len(parts) != 4 {
		t.Fatalf("unexpected trace format: %q", trace)
	}
	traceID, parentID := parts[1], parts[2]

	if traceID != "01"+strings.Repeat("00", 15) {
		t.Errorf("expected all-zero trace ID to be corrected to start with 0x01, got %q", traceID)
	}
	if parentID != "01"+strings.Repeat("00", 7) {
		t.Errorf("expected all-zero parent ID to be corrected to start with 0x01, got %q", parentID)
	}
}

func TestRandomEntryHelpers(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	if got := lc.randomStringEntry(nil); got != "" {
		t.Errorf("randomStringEntry(nil) = %q, want empty string", got)
	}
	if got := lc.randomStringEntry([]string{"only"}); got != "only" {
		t.Errorf("randomStringEntry(single) = %q, want %q", got, "only")
	}
	if got := lc.randomStringEntry([]string{"a", "b"}); got != "a" && got != "b" {
		t.Errorf("randomStringEntry(multi) = %q, want one of a/b", got)
	}

	if got := lc.randomSliceEntry(nil); len(got) != 0 {
		t.Errorf("randomSliceEntry(nil) = %v, want empty slice", got)
	}
	single := []string{"x", "y"}
	if got := lc.randomSliceEntry([][]string{single}); !reflect.DeepEqual(got, single) {
		t.Errorf("randomSliceEntry(single) = %v, want %v", got, single)
	}
	a, b := []string{"a"}, []string{"b"}
	if got := lc.randomSliceEntry([][]string{a, b}); !reflect.DeepEqual(got, a) && !reflect.DeepEqual(got, b) {
		t.Errorf("randomSliceEntry(multi) = %v, want %v or %v", got, a, b)
	}

	if got := lc.randomByteEntry(nil); got != 0 {
		t.Errorf("randomByteEntry(nil) = %d, want 0", got)
	}
	if got := lc.randomByteEntry([]byte{7}); got != 7 {
		t.Errorf("randomByteEntry(single) = %d, want 7", got)
	}
	if got := lc.randomByteEntry([]byte{1, 2}); got != 1 && got != 2 {
		t.Errorf("randomByteEntry(multi) = %d, want 1 or 2", got)
	}
}

func TestRandomEntryHelpers_SingleElement_NoRNGConsumed(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	freshDraw := func(seed uint64) uint64 {
		return rand.New(rand.NewPCG(seed, seed)).Uint64()
	}

	t.Run("randomStringEntry", func(t *testing.T) {
		const seed = 111
		lc.generator = *rand.New(rand.NewPCG(seed, seed))
		lc.randomStringEntry([]string{"only"})
		if got, want := lc.generator.Uint64(), freshDraw(seed); got != want {
			t.Errorf("randomStringEntry with a single-element list consumed the RNG: next draw = %d, want %d (unconsumed)", got, want)
		}
	})

	t.Run("randomSliceEntry", func(t *testing.T) {
		const seed = 222
		lc.generator = *rand.New(rand.NewPCG(seed, seed))
		lc.randomSliceEntry([][]string{{"x", "y"}})
		if got, want := lc.generator.Uint64(), freshDraw(seed); got != want {
			t.Errorf("randomSliceEntry with a single-element list consumed the RNG: next draw = %d, want %d (unconsumed)", got, want)
		}
	})

	t.Run("randomByteEntry", func(t *testing.T) {
		const seed = 333
		lc.generator = *rand.New(rand.NewPCG(seed, seed))
		lc.randomByteEntry([]byte{7})
		if got, want := lc.generator.Uint64(), freshDraw(seed); got != want {
			t.Errorf("randomByteEntry with a single-element list consumed the RNG: next draw = %d, want %d (unconsumed)", got, want)
		}
	})
}

func TestCapitalizeAt(t *testing.T) {
	b := []byte("ab")
	capitalizeAt(b, len(b))
	if string(b) != "ab" {
		t.Errorf("capitalizeAt(idx==len): expected no-op, got %q", string(b))
	}

	b = []byte("azy")
	capitalizeAt(b, 0)
	if b[0] != 'A' {
		t.Errorf("capitalizeAt('a') = %q, want 'A'", string(b[0]))
	}

	capitalizeAt(b, 1)
	if b[1] != 'Z' {
		t.Errorf("capitalizeAt('z') = %q, want 'Z'", string(b[1]))
	}

	c := []byte("`")
	capitalizeAt(c, 0)
	if c[0] != '`' {
		t.Errorf("capitalizeAt('`') = %q, want unchanged", string(c[0]))
	}

	d := []byte("{")
	capitalizeAt(d, 0)
	if d[0] != '{' {
		t.Errorf("capitalizeAt('{') = %q, want unchanged", string(d[0]))
	}
}

func TestCapitalizeAtRandom_DeterministicSequence(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	input := "abcdefghijkl"
	seq := make([]uint64, len(input))
	for i := range seq {
		seq[i] = uint64(i)
	}
	lc.generator = *rand.New(&fixedSeqSource{vals: seq})

	expected := []byte(input)
	for i, raw := range seq {
		if raw%4 == 0 {
			capitalizeAt(expected, i)
		}
	}

	got := lc.capitalizeAtRandom(input)
	if got != string(expected) {
		t.Errorf("capitalizeAtRandom(%q) = %q, want %q", input, got, string(expected))
	}
}

func TestRandomIntBoundaryAndRange(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	if got := lc.randomInt(5, 5); got != 5 {
		t.Errorf("randomInt(5,5) = %d, want 5", got)
	}

	const from, to = 3, 9
	for seed := 0; seed < 30; seed++ {
		lc.generator = *rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
		got := lc.randomInt(from, to)
		if got < from || got >= to {
			t.Fatalf("seed %d: randomInt(%d,%d) = %d, out of range [%d,%d)", seed, from, to, got, from, to)
		}
	}
}

func TestRandomInt64BoundaryAndRange(t *testing.T) {
	lc := newLuaFakerContext(t)
	defer lc.Close()

	if got := lc.randomInt64(5, 5); got != 5 {
		t.Errorf("randomInt64(5,5) = %d, want 5", got)
	}

	const from, to = int64(3), int64(9)
	for seed := 0; seed < 30; seed++ {
		lc.generator = *rand.New(rand.NewPCG(uint64(seed), uint64(seed)))
		got := lc.randomInt64(from, to)
		if got < from || got >= to {
			t.Fatalf("seed %d: randomInt64(%d,%d) = %d, out of range [%d,%d)", seed, from, to, got, from, to)
		}
	}
}
