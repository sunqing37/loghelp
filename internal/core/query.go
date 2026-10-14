package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Predicate func(*Entry, time.Time) bool
type token struct {
	text    string
	pos     int
	op      bool
	literal bool
}
type Query struct {
	Match         Predicate
	Text          string
	TimeDependent bool
}

func lex(input string) ([]token, error) {
	var out []token
	for i := 0; i < len(input); {
		if input[i] == ' ' || input[i] == '\t' || input[i] == '\n' {
			i++
			continue
		}
		start := i
		if strings.ContainsRune("&|()", rune(input[i])) {
			out = append(out, token{string(input[i]), i, true, false})
			i++
			continue
		}
		var b strings.Builder
		literal := input[start] == '"' || input[start] == '\''
		quoted := byte(0)
		for i < len(input) {
			c := input[i]
			if quoted != 0 {
				if c == quoted {
					quoted = 0
					i++
					continue
				}
				if c == '\\' && i+1 < len(input) && (input[i+1] == quoted || input[i+1] == '\\') {
					i++
					c = input[i]
				}
				b.WriteByte(c)
				i++
				continue
			}
			if c == '"' || c == '\'' {
				quoted = c
				i++
				continue
			}
			if c == ' ' || c == '\t' || c == '\n' || strings.ContainsRune("&|()", rune(c)) {
				break
			}
			b.WriteByte(c)
			i++
		}
		if quoted != 0 {
			return nil, fmt.Errorf("位置 %d：引号未闭合", start+1)
		}
		text := b.String()
		// AS permits whitespace between a field colon and its value.
		if len(out) > 0 && !out[len(out)-1].op && !out[len(out)-1].literal && strings.HasSuffix(out[len(out)-1].text, ":") {
			out[len(out)-1].text += text
		} else {
			out = append(out, token{text, start, false, literal})
		}
	}
	return out, nil
}

type parser struct {
	tokens    []token
	i         int
	sensitive bool
}

func CompileQuery(text string, sensitive bool) (Query, error) {
	tokens, err := lex(text)
	if err != nil {
		return Query{}, err
	}
	if len(tokens) == 0 {
		return Query{Match: func(*Entry, time.Time) bool { return true }, Text: text}, nil
	}
	p := parser{tokens: tokens, sensitive: sensitive}
	pred, err := p.or()
	if err == nil && p.i < len(tokens) {
		err = fmt.Errorf("位置 %d：多余的 %s", tokens[p.i].pos+1, tokens[p.i].text)
	}
	timeDependent := false
	for _, t := range tokens {
		field, _, hasField := strings.Cut(t.text, ":")
		if !t.literal && hasField && strings.EqualFold(strings.TrimPrefix(field, "-"), "age") {
			timeDependent = true
		}
	}
	return Query{Match: pred, Text: text, TimeDependent: timeDependent}, err
}
func combine(a, b Predicate, or bool) Predicate {
	if or {
		return func(e *Entry, t time.Time) bool { return a(e, t) || b(e, t) }
	}
	return func(e *Entry, t time.Time) bool { return a(e, t) && b(e, t) }
}
func (p *parser) or() (Predicate, error) {
	a, err := p.and()
	if err != nil {
		return nil, err
	}
	for p.i < len(p.tokens) && p.tokens[p.i].text == "|" && p.tokens[p.i].op {
		p.i++
		b, err := p.and()
		if err != nil {
			return nil, err
		}
		a = combine(a, b, true)
	}
	return a, nil
}
func (p *parser) and() (Predicate, error) {
	a, err := p.implicit()
	if err != nil {
		return nil, err
	}
	for p.i < len(p.tokens) && p.tokens[p.i].text == "&" && p.tokens[p.i].op {
		p.i++
		b, err := p.implicit()
		if err != nil {
			return nil, err
		}
		a = combine(a, b, false)
	}
	return a, nil
}
func (p *parser) implicit() (Predicate, error) {
	var groups []Predicate
	keys := map[string]int{}
	for p.i < len(p.tokens) && (!p.tokens[p.i].op || p.tokens[p.i].text == "(") {
		t := p.tokens[p.i]
		var a Predicate
		var err error
		key := ""
		if t.op {
			p.i++
			a, err = p.or()
			if err == nil {
				if p.i >= len(p.tokens) || p.tokens[p.i].text != ")" || !p.tokens[p.i].op {
					err = fmt.Errorf("位置 %d：缺少右括号", t.pos+1)
				} else {
					p.i++
				}
			}
		} else {
			p.i++
			a, key, err = term(t, p.sensitive)
		}
		if err != nil {
			return nil, err
		}
		if index, ok := keys[key]; key != "" && ok {
			groups[index] = combine(groups[index], a, true)
		} else {
			if key != "" {
				keys[key] = len(groups)
			}
			groups = append(groups, a)
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("位置 %d：需要查询条件", p.position())
	}
	a := groups[0]
	for _, b := range groups[1:] {
		a = combine(a, b, false)
	}
	return a, nil
}
func (p *parser) position() int {
	if p.i < len(p.tokens) {
		return p.tokens[p.i].pos + 1
	}
	return 1
}

func term(t token, sensitive bool) (Predicate, string, error) {
	field, value, has := strings.Cut(t.text, ":")
	if t.literal {
		has = false
	}
	negative := false
	regex := false
	if !has {
		field, value = "message", t.text
	} else {
		negative = strings.HasPrefix(field, "-")
		field = strings.TrimPrefix(field, "-")
		regex = strings.HasSuffix(field, "~")
		field = strings.TrimSuffix(field, "~")
	}
	field = strings.ToLower(field)
	fail := func(err error) (Predicate, string, error) { return nil, "", fmt.Errorf("位置 %d：%w", t.pos+1, err) }
	if value == "" {
		return fail(fmt.Errorf("%s 的值不能为空", field))
	}
	var pred Predicate
	switch field {
	case "level":
		n := LevelValue(value)
		if n == 0 || regex {
			return fail(fmt.Errorf("无效日志级别 %s", value))
		}
		pred = func(e *Entry, _ time.Time) bool { return LevelValue(e.Level) >= n }
	case "pid", "tid":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || regex {
			return fail(fmt.Errorf("%s 必须是非负整数", field))
		}
		pred = func(e *Entry, _ time.Time) bool {
			if field == "pid" {
				return e.PID == n
			}
			return e.TID == n
		}
	case "age":
		v := value
		if strings.HasSuffix(v, "d") {
			days, err := strconv.ParseInt(strings.TrimSuffix(v, "d"), 10, 32)
			if err != nil || days < 0 || days > 100000 {
				return fail(fmt.Errorf("无效 age %s", value))
			}
			v = fmt.Sprintf("%dh", days*24)
		}
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 || regex {
			return fail(fmt.Errorf("age 使用如 30s、5m、2h、7d"))
		}
		pred = func(e *Entry, now time.Time) bool {
			return !e.Time.IsZero() && !e.Time.Before(now.Add(-d)) && !e.Time.After(now)
		}
	case "after", "before":
		date, err := ParseTime(value)
		if err != nil || regex {
			return fail(fmt.Errorf("时间使用 RFC3339 或带引号的 YYYY-MM-DD HH:MM:SS"))
		}
		pred = func(e *Entry, _ time.Time) bool {
			if e.Time.IsZero() {
				return false
			}
			if field == "after" {
				return !e.Time.Before(date)
			}
			return e.Time.Before(date)
		}
	case "tag", "package", "process", "message", "line":
		if field == "package" && value == "mine" {
			return fail(fmt.Errorf("独立工具不支持 package:mine，请选择应用包名"))
		}
		var match func(string) bool
		if regex {
			pattern := value
			if !sensitive {
				pattern = "(?i)" + pattern
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fail(err)
			}
			match = re.MatchString
		} else {
			v := value
			if !sensitive {
				v = strings.ToLower(v)
			}
			match = func(s string) bool {
				if !sensitive {
					s = strings.ToLower(s)
				}
				return strings.Contains(s, v)
			}
		}
		pred = func(e *Entry, _ time.Time) bool {
			switch field {
			case "tag":
				return match(e.Tag)
			case "package":
				return match(e.Package)
			case "process":
				return match(e.Process)
			case "line":
				return match(e.Raw)
			default:
				return match(e.Message)
			}
		}
	default:
		return fail(fmt.Errorf("不支持的字段 %s", field))
	}
	if negative {
		a := pred
		pred = func(e *Entry, now time.Time) bool { return !a(e, now) }
	}
	key := ""
	if has && !negative {
		key = field
	}
	return pred, key, nil
}

func ParseTime(value string) (time.Time, error) {
	for _, format := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(format, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无效时间 %s", value)
}

type Search struct {
	re   *regexp.Regexp
	Text string
}

func CompileSearch(text string, regex, sensitive bool) (Search, error) {
	if text == "" {
		return Search{}, nil
	}
	p := text
	if !regex {
		p = regexp.QuoteMeta(p)
	}
	if !sensitive {
		p = "(?i)" + p
	}
	re, err := regexp.Compile(p)
	return Search{re: re, Text: text}, err
}
func (s Search) Matches(text string) [][]int {
	if s.re == nil {
		return nil
	}
	return s.re.FindAllStringIndex(text, 1000)
}
