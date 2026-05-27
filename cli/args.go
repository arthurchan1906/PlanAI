package cli

import (
	"strconv"
	"strings"
)

type Args struct {
	data map[string]string
	list map[string][]string
}

func ParseArgs(raw []string) *Args {
	p := &Args{data: map[string]string{}, list: map[string][]string{}}
	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		key = strings.ReplaceAll(key, "-", "_")
		if strings.HasPrefix(key, "no_") {
			p.data[key] = "false"
			continue
		}
		if i+1 < len(raw) && !strings.HasPrefix(raw[i+1], "--") {
			p.data[key] = raw[i+1]
			i++
		} else {
			p.data[key] = "true"
		}
	}
	return p
}

func (p *Args) Get(k string) string { return p.data[k] }
func (p *Args) Str(k, def string) string {
	if v, ok := p.data[k]; ok {
		return v
	}
	return def
}
func (p *Args) Int(k string, def int) int {
	if v, ok := p.data[k]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
func (p *Args) Bool(k string) bool { return p.data[k] == "true" || p.data[k] == "1" }
