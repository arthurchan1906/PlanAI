package hook

import "strings"

// BashFileOp is a high-confidence file target extracted from a Bash command.
type BashFileOp struct {
	Op   string // stage | read | edit
	File string
}

// extractBashFileOps extracts high-confidence file targets from structured
// subcommands:
//   - `git add <files>`              → stage (index, not working tree)
//   - `cat/wc/sed -n <files>`        → read
//   - `find <dir>` / `xcodebuild -project <proj>` → read (traversal/build)
//
// Redirection (`>`/`>>`) and `sed -i` stay in parseBashFileOp (modify/append).
// Low-confidence or compound commands return nil — miss beats mis-attribute
// (8/12 consensus). Project-external targets are filtered by the caller via
// ToRelPath; exit_code != 0 downgrades the source at the call site.
func extractBashFileOps(cmd string) []BashFileOp {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	cmd = stripLeadingCD(cmd)
	toks := tokenizeBash(cmd)
	if len(toks) == 0 {
		return nil
	}

	var ops []BashFileOp
	add := func(op, file string) {
		file = strings.TrimSpace(strings.TrimRight(file, ",;"))
		if file == "" || file == "." || file == ".." || strings.HasSuffix(file, "/") {
			return
		}
		if !isValidFileOpPath(file) && !isProjectBundlePath(file) {
			return
		}
		ops = append(ops, BashFileOp{Op: op, File: file})
	}

	for i := 0; i < len(toks); i++ {
		switch toks[i] {
		case "git":
			if i+1 < len(toks) && toks[i+1] == "add" {
				for j := i + 2; j < len(toks); j++ {
					a := toks[j]
					if isCmdBreak(a) {
						break
					}
					if strings.HasPrefix(a, "-") {
						continue
					}
					if a == "." || a == ".." || strings.HasSuffix(a, "/") {
						continue
					}
					add("stage", a)
				}
			}
		case "cat", "wc", "sed":
			isEdit := toks[i] == "sed" && hasFlag(toks[i+1:], "-i")
			if isEdit {
				continue // sed -i stays with parseBashFileOp (modify)
			}
			for j := i + 1; j < len(toks); j++ {
				a := toks[j]
				if isCmdBreak(a) {
					break
				}
				if strings.HasPrefix(a, "-") {
					continue
				}
				add("read", a)
			}
		case "find":
			// First non-flag token after find is the start path (directory).
			for j := i + 1; j < len(toks); j++ {
				a := toks[j]
				if isCmdBreak(a) {
					break
				}
				if strings.HasPrefix(a, "-") {
					// Skip flag and its expression operands; find paths come first.
					continue
				}
				add("read", a)
				break
			}
		case "xcodebuild":
			for j := i + 1; j < len(toks)-1; j++ {
				if toks[j] == "-project" {
					add("read", toks[j+1])
					break
				}
			}
		}
	}
	return ops
}

// isProjectBundlePath accepts Xcode bundle paths (no directory separator),
// which xcodebuild -project legitimately passes as bare names.
func isProjectBundlePath(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, ".xcodeproj") ||
		strings.HasSuffix(lower, ".xcworkspace") ||
		strings.HasSuffix(lower, ".pbxproj")
}

// tokenizeBash splits a command into tokens, honoring single/double quotes.
func tokenizeBash(cmd string) []string {
	var toks []string
	var cur strings.Builder
	inS, inD := false, false
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case inS:
			if c == '\'' {
				inS = false
			} else {
				cur.WriteByte(c)
			}
		case inD:
			if c == '"' {
				inD = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inS = true
		case c == '"':
			inD = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

// stripLeadingCD removes a leading `cd <dir> && ` so subcommand parsing sees
// the actual tool invocation (codex/ED habit: cd repo && ...).
func stripLeadingCD(cmd string) string {
	idx := strings.Index(cmd, "&&")
	if idx < 0 {
		return cmd
	}
	fields := strings.Fields(cmd[:idx])
	if len(fields) > 0 && fields[0] == "cd" {
		return strings.TrimSpace(cmd[idx+2:])
	}
	return cmd
}

// isCmdBreak reports whether a token ends the current subcommand's arguments
// (shell control or redirection).
func isCmdBreak(tok string) bool {
	if tok == "" {
		return false
	}
	switch tok[0] {
	case '>', '<', '|', ';', '&':
		return true
	}
	return false
}

// hasFlag reports whether the token slice contains the exact flag.
func hasFlag(toks []string, flag string) bool {
	for _, t := range toks {
		if t == flag {
			return true
		}
	}
	return false
}
