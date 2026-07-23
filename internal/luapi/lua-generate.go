// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

var domains []string = []string{
	"google.com", "yahoo.com", "hotmail.com", "outlook.com", "protonmail.com",
	"tutanota.com", "fastmail.com", "example.com", "example.net", "example.org",
	"gmail.com", "icloud.com", "aol.com", "mail.com", "yandex.com", "gmx.com", "zoho.com",
}

var usernames []string = []string{"shadow", "hunter", "coolguy", "ninja", "sniper", "wizard", "gamer", "pro", "master", "king", "queen", "lord", "lady", "knight", "dragon", "slayer", "phantom", "ghost", "spirit", "soul", "legend", "hero", "villain", "boss", "champ", "star", "fire", "ice", "storm", "thunder", "wolf", "tiger", "lion", "bear", "hawk", "eagle", "falcon", "raven", "crow", "snake"}
var firstNames []string = []string{"James", "Mary", "Robert", "Patricia", "John", "Jennifer", "Michael", "Linda", "David", "Elizabeth", "William", "Barbara", "Richard", "Susan", "Joseph", "Jessica", "Thomas", "Sarah", "Charles", "Karen", "Christopher", "Lisa", "Daniel", "Nancy", "Matthew", "Betty", "Anthony", "Margaret", "Mark", "Sandra", "Donald", "Ashley", "Steven", "Kimberly", "Paul", "Emily", "Andrew", "Donna", "Joshua", "Michelle", "Kenneth", "Carol", "Kevin", "Amanda", "Brian", "Melissa", "George", "Deborah", "Timothy", "Stephanie"}
var lastNames []string = []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Rodriguez", "Martinez", "Hernandez", "Lopez", "Gonzales", "Wilson", "Anderson", "Thomas", "Taylor", "Moore", "Jackson", "Martin", "Lee", "Perez", "Thompson", "White", "Harris", "Sanchez", "Clark", "Ramirez", "Lewis", "Robinson", "Walker", "Young", "Allen", "King", "Wright", "Scott", "Torres", "Nguyen", "Hill", "Flores", "Green", "Adams", "Nelson", "Baker", "Hall", "Rivera", "Campbell", "Mitchell", "Carter", "Roberts"}
var adjectives []string = []string{"abandoned", "able", "absolute", "adorable", "adventurous", "academic", "acceptable", "acclaimed", "accomplished", "accurate", "aching", "acidic", "acrobatic", "active", "actual", "adept", "admirable", "admired", "adolescent", "adorable", "adored", "advanced", "afraid", "affectionate", "aged", "aggravating", "aggressive", "agile", "agitated", "agonizing", "agreeable", "ajar", "alarmed", "alarming", "alert", "alienated", "alive", "all", "altruistic", "amazing", "ambitious", "ample", "amused", "amusing", "anchored", "ancient", "angelic", "angry", "anguished", "animated"}
var nouns []string = []string{"time", "year", "people", "way", "day", "man", "thing", "woman", "life", "child", "world", "school", "state", "family", "student", "group", "country", "problem", "hand", "part", "place", "case", "week", "company", "system", "program", "question", "work", "government", "number", "night", "point", "home", "water", "room", "mother", "area", "money", "story", "fact", "month", "lot", "right", "study", "book", "eye", "job", "word", "business", "issue"}
var delims []byte = []byte{'.', '_', '-', '+'}
var phoneCodes []string = []string{"+1", "+44", "+33", "+49", "+61", "+81", "+86", "+91", "+55", "+52"}
var passwordChars []byte = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+-=[]{}|;:,.<>?")
var hexadecimal []byte = []byte("0123456789abcdef")
var uuidv4variant []byte = []byte("89ab")
var streetTypes []string = []string{"Street", "Avenue", "Boulevard", "Drive", "Lane", "Road", "Court", "Way", "Terrace", "Place", "Circle", "Square", "Plaza", "Highway", "Parkway"}
var cities []string = []string{"New York", "London", "Paris", "Tokyo", "Berlin", "Sydney", "Toronto", "Madrid", "Rome", "Dubai", "Singapore", "Hong Kong", "Amsterdam", "Seoul", "Moscow", "Mumbai", "Sao Paulo", "Los Angeles", "Chicago", "Miami", "Boston", "San Francisco", "Seattle", "Austin", "Denver", "Vancouver", "Montreal", "Munich", "Frankfurt", "Vienna", "Zurich", "Geneva", "Stockholm", "Oslo", "Copenhagen", "Helsinki", "Warsaw", "Prague", "Budapest", "Athens", "Istanbul", "Cape Town", "Johannesburg", "Cairo", "Tel Aviv"}
var countries []string = []string{"United States", "United Kingdom", "France", "Japan", "Germany", "Australia", "Canada", "Spain", "Italy", "United Arab Emirates", "Singapore", "China", "Netherlands", "South Korea", "Russia", "India", "Brazil", "Mexico", "Argentina", "Chile", "South Africa", "Egypt", "Nigeria", "Kenya", "Morocco", "Turkey", "Greece", "Poland", "Sweden", "Norway", "Denmark", "Finland", "Switzerland", "Austria", "Belgium", "Ireland", "Portugal", "New Zealand", "Thailand", "Vietnam", "Malaysia", "Indonesia", "Philippines", "Israel", "Saudi Arabia", "Iran", "Iraq", "Pakistan", "Bangladesh", "Colombia"}
var companySuffix []string = []string{"Engineering", "& Co", "Solutions", "AI", "Decisions", "Inc.", "LLC", "Corp.", "Group", "Holdings", "Technologies", "Services", "Partners", "Logistics", "Enterprises", "Systems", "Studios", "Media"}
var userAgents []string = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (iPad; CPU OS 17_2_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
	"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
}

func (lc *LuaContext) SetSeed(ls *lua.LState) int {
	number := ls.CheckInt64(1)
	lc.generator = *rand.New(rand.NewPCG(uint64(number), uint64(number)))
	return 0
}

func (lc *LuaContext) FakeEmail(ls *lua.LState) int {
	ls.Push(lua.LString(fmt.Sprintf("%s%c%s@%s",
		lc.randomMatrixEntry(lastNames, firstNames, usernames, adjectives),
		lc.randomByteEntry(delims),
		lc.randomMatrixEntry(lastNames, firstNames, usernames, nouns),
		lc.randomStringEntry(domains),
	)))
	return 1
}

func (lc *LuaContext) FakePhone(ls *lua.LState) int {
	digits := make([]byte, 10)
	digits[0] = byte(lc.generator.Int64()%9) + 1
	ls.Push(lua.LString(fmt.Sprintf("%s%s", lc.randomStringEntry(phoneCodes), digits)))
	return 1
}

func (lc *LuaContext) FakeName(ls *lua.LState) int {
	ls.Push(lua.LString(fmt.Sprintf("%s %s",
		capitalizeFirst(lc.randomStringEntry(firstNames)),
		capitalizeFirst(lc.randomStringEntry(lastNames)))))
	return 1
}

func (lc *LuaContext) FakeFirstName(ls *lua.LState) int {
	ls.Push(lua.LString(capitalizeFirst(lc.randomStringEntry(firstNames))))
	return 1
}

func (lc *LuaContext) FakeLastName(ls *lua.LState) int {
	ls.Push(lua.LString(capitalizeFirst(lc.randomStringEntry(lastNames))))
	return 1
}

func (lc *LuaContext) FakeUsername(ls *lua.LState) int {
	ls.Push(lua.LString(lc.capitalizeAtRandom(lc.randomStringEntry(usernames))))
	return 1
}

func (lc *LuaContext) FakePassword(ls *lua.LState) int {
	size := lc.randomInt(8, 24)
	pwd := make([]byte, size)
	for idx := range pwd {
		pwd[idx] = lc.randomByteEntry(passwordChars)
	}
	ls.Push(lua.LString(pwd))
	return 1
}

func (lc *LuaContext) FakeUUIDv4(ls *lua.LState) int {
	p1 := make([]byte, 8)
	for idx := range p1 {
		p1[idx] = lc.randomByteEntry(hexadecimal)
	}
	p2 := make([]byte, 4)
	for idx := range p2 {
		p2[idx] = lc.randomByteEntry(hexadecimal)
	}
	p3 := make([]byte, 3)
	for idx := range p3 {
		p3[idx] = lc.randomByteEntry(hexadecimal)
	}
	p4 := make([]byte, 3)
	for idx := range p4 {
		p4[idx] = lc.randomByteEntry(hexadecimal)
	}
	p5 := make([]byte, 12)
	for idx := range p5 {
		p5[idx] = lc.randomByteEntry(hexadecimal)
	}
	ls.Push(lua.LString(fmt.Sprintf("%s-%s-4%s-%c%s-%s",
		p1, p2, p3, lc.randomByteEntry(uuidv4variant), p4, p5,
	)))
	return 1
}

func (lc *LuaContext) FakeWord(ls *lua.LState) int {
	ls.Push(lua.LString(lc.randomMatrixEntry(nouns, adjectives)))
	return 1
}

func (lc *LuaContext) FakeAddress(ls *lua.LState) int {
	ls.Push(lua.LString(fmt.Sprintf("%d %s %s Apt %d, %s, %s",
		lc.randomInt(1, 256),
		capitalizeFirst(lc.randomMatrixEntry(nouns, adjectives)),
		capitalizeFirst(lc.randomStringEntry(streetTypes)),
		lc.randomInt(1, 500),
		capitalizeFirst(lc.randomStringEntry(cities)),
		capitalizeFirst(lc.randomStringEntry(countries)))))
	return 1
}

func (lc *LuaContext) FakeCompany(ls *lua.LState) int {
	ls.Push(lua.LString(fmt.Sprintf("%s %s, LLC",
		capitalizeFirst(lc.randomMatrixEntry(lastNames, firstNames, nouns, adjectives, cities)), capitalizeFirst(lc.randomMatrixEntry(companySuffix, nouns, adjectives)),
	)))
	return 1
}

func (lc *LuaContext) FakeIPv4(ls *lua.LState) int {
	ls.Push(lua.LString(fmt.Sprintf("%d.%d.%d.%d", lc.randomInt(0, 255), lc.randomInt(0, 255), lc.randomInt(0, 255), lc.randomInt(0, 255))))
	return 1
}

func (lc *LuaContext) FakeIPv6(ls *lua.LState) int {
	ls.Push(lua.LString(fmt.Sprintf("%x:%x:%x:%x:%x:%x:%x:%x",
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff,
		lc.generator.Uint32()&0xffff)))
	return 1
}

func (lc *LuaContext) FakeURL(ls *lua.LState) int {
	size := lc.randomInt(0, 5)
	sb := strings.Builder{}
	sb.WriteString("http")
	if lc.generator.Int64()%6 != 0 {
		sb.WriteByte('s')
	}
	sb.WriteString("://")
	if size > 3 {
		size -= 1
		sb.WriteString(lc.randomStringEntry(nouns))
		sb.WriteByte('.')
	}
	sb.WriteString(lc.randomStringEntry(domains))
	sb.WriteByte('/')
	for range size {
		sb.WriteString(lc.randomStringEntry(nouns))
		sb.WriteByte('/')
	}

	ls.Push(lua.LString(sb.String()))
	return 1
}

func (lc *LuaContext) FakeUserAgent(ls *lua.LState) int {
	ls.Push(lua.LString(lc.randomStringEntry(userAgents)))
	return 1
}

func (lc *LuaContext) FakeTime(ls *lua.LState) int {
	from := ls.CheckInt64(1)
	to := ls.OptInt64(2, lc.clock.Now().UnixMilli())
	ls.Push(lua.LNumber(float64(lc.randomInt64(from, to))))
	return 1
}

func (lc *LuaContext) FakeInt(ls *lua.LState) int {
	mi := ls.OptInt64(1, math.MinInt64)
	ma := ls.OptInt64(2, math.MaxInt64)
	ls.Push(lua.LNumber(float64(lc.randomInt64(mi, ma))))
	return 1
}

func (lc *LuaContext) FakeFloat(ls *lua.LState) int {
	mi := ls.OptNumber(1, -math.MaxFloat64)
	ma := ls.OptNumber(2, +math.MaxFloat64)
	if mi >= ma {
		ls.Push(mi)
		return 1
	}

	ls.Push(lua.LNumber(mi + lua.LNumber(lc.generator.Float64())*(ma-mi)))
	return 1
}

func (lc *LuaContext) FakeShakespeare(ls *lua.LState) int {
	ls.Push(lua.LBool(lc.generator.Int64()%2 == 0))
	return 1
}

func (lc *LuaContext) FakeTakeOne(ls *lua.LState) int {
	elems := ls.CheckTable(1)
	if elems.Len() == 0 {
		ls.Error(lua.LString("sinq.fake.oneOf unable to pick element from key: value table or sparse array"), 1)
		return 0
	}
	idx := lc.randomInt(1, elems.Len())
	ls.Push(elems.RawGetInt(idx))
	return 1
}

func (lc *LuaContext) FakeTrace(ls *lua.LState) int {
	traceId := make([]byte, 16)
	isZero := true
	for idx := range len(traceId) {
		traceId[idx] = byte(lc.generator.Int64() & 0xff)
		if traceId[idx] != 0 {
			isZero = false
		}
	}

	if isZero {
		traceId[0] = 1
	}

	parentId := make([]byte, 8)
	isZero = true
	for idx := range len(parentId) {
		parentId[idx] = byte(lc.generator.Int64() & 0xff)
		if parentId[idx] != 0 {
			isZero = false
		}
	}

	if isZero {
		parentId[0] = 1
	}

	ls.Push(lua.LString(fmt.Sprintf("00-%s-%s-01", hex.EncodeToString(traceId), hex.EncodeToString(parentId))))

	return 1
}

// func (lc *LuaContext) randomEntry[T any](list []T) T oh where are you go 1.27...
func (lc *LuaContext) randomStringEntry(list []string) string {
	if len(list) == 0 {
		return ""
	}
	if len(list) == 1 {
		return list[0]
	}
	idx := lc.generator.Int64() % int64(len(list))
	return list[idx]
}

func (lc *LuaContext) randomSliceEntry(list [][]string) []string {
	if len(list) == 0 {
		return make([]string, 0)
	}
	if len(list) == 1 {
		return list[0]
	}
	idx := lc.generator.Int64() % int64(len(list))
	return list[idx]
}

func (lc *LuaContext) randomMatrixEntry(list ...[]string) string {
	return lc.randomStringEntry(lc.randomSliceEntry(list))
}

func (lc *LuaContext) randomByteEntry(list []byte) byte {
	if len(list) == 0 {
		return 0
	}
	if len(list) == 1 {
		return list[0]
	}
	idx := lc.generator.Int64() % int64(len(list))
	return list[idx]
}

func capitalizeAt(str []byte, idx int) {
	if len(str) <= idx {
		return
	}
	if str[idx] < 'a' || str[idx] > 'z' {
		return
	}
	str[idx] -= 'a' - 'A'
}

func (lc *LuaContext) capitalizeAtRandom(str string) string {
	inter := []byte(str)
	for idx := range inter {
		if lc.generator.Uint64()%4 == 0 {
			capitalizeAt(inter, idx)
		}
	}
	return string(inter)
}

func capitalizeFirst(str string) string {
	inter := []byte(str)
	capitalizeAt(inter, 0)
	return string(inter)
}

func (lc *LuaContext) randomInt(from, to int) int {
	if from >= to {
		return from
	}

	return int(lc.generator.UintN(uint(to-from))) + from
}

func (lc *LuaContext) randomInt64(from, to int64) int64 {
	if from >= to {
		return from
	}
	return int64(lc.generator.Uint64N(uint64(to-from))) + from
}
