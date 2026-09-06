package transition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

// cloneAngularProject returns angular.json with the project named to added as a copy of
// the project named from: every path and target reference to the source project is
// rewritten, the dev server takes the port and serves under /to, and the build gets the
// matching base href. The edit is textual so the file keeps its layout.
func cloneAngularProject(data []byte, from, to string, port int) ([]byte, error) {
	start, end, indent, err := projectRange(data, from)
	if err != nil {
		return nil, err
	}
	if _, _, _, err := projectRange(data, to); err == nil {
		return nil, errors.Newf("project %q already exists", to)
	}

	clone := string(data[start:end])
	bounded := regexp.MustCompile(`(["/:])` + regexp.QuoteMeta(from) + `(["/:])`)
	clone = bounded.ReplaceAllString(clone, "${1}"+to+"${2}")
	if port > 0 {
		clone = regexp.MustCompile(`"port":\s*\d+`).ReplaceAllString(clone, fmt.Sprintf(`"port": %d`, port))
	}
	clone = setOrInsert(clone, "servePath", fmt.Sprintf("%q", "/"+to), "proxyConfig")
	clone = setOrInsert(clone, "baseHref", fmt.Sprintf("%q", "/"+to+"/"), "outputPath")

	var b bytes.Buffer
	b.Write(data[:end])
	fmt.Fprintf(&b, ",\n%s%q: %s", indent, to, clone)
	b.Write(data[end:])

	return b.Bytes(), nil
}

// setOrInsert sets the string value of key where it appears, or inserts the key before
// the line holding anchor with the same indentation. Missing both, the text is returned
// unchanged.
func setOrInsert(text, key, value, anchor string) string {
	keyRE := regexp.MustCompile(`"` + key + `":\s*"[^"]*"`)
	if keyRE.MatchString(text) {
		return keyRE.ReplaceAllString(text, fmt.Sprintf("%q: %s", key, value))
	}
	anchorRE := regexp.MustCompile(`(?m)^([ \t]*)"` + anchor + `":`)
	m := anchorRE.FindStringSubmatchIndex(text)
	if m == nil {
		return text
	}
	indent := text[m[2]:m[3]]

	return text[:m[0]] + fmt.Sprintf("%s%q: %s,\n", indent, key, value) + text[m[0]:]
}

// projectRange finds the byte range of the named project's object under "projects", and
// the indentation of its key.
func projectRange(data []byte, name string) (start, end int, indent string, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	depth := 0
	inProjects := false
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, "", errors.Newf("project %q not found in angular.json", name)
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 1 {
					inProjects = false
				}
			}
		case string:
			if depth == 1 && !inProjects {
				if t == "projects" {
					inProjects = true
					// Enter the projects object.
					if _, err := dec.Token(); err != nil {
						return 0, 0, "", errors.Wrap(err, "json.Decoder.Token()")
					}
					depth++
				} else {
					skipValue(dec)
				}

				continue
			}
			if depth == 2 && inProjects {
				lastKey = t
				keyEnd := int(dec.InputOffset())
				if lastKey != name {
					skipValue(dec)

					continue
				}
				brace := bytes.IndexByte(data[keyEnd:], '{')
				if brace < 0 {
					return 0, 0, "", errors.Newf("project %q is not an object", name)
				}
				start = keyEnd + brace
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return 0, 0, "", errors.Wrap(err, "json.Decoder.Decode()")
				}
				end = int(dec.InputOffset())
				keyStart := keyEnd - len(name) - 2
				lineStart := bytes.LastIndexByte(data[:keyStart], '\n') + 1
				indent = strings.TrimRight(string(data[lineStart:keyStart]), "\"")

				return start, end, indent, nil
			}
		default:
		}
	}
}

// skipValue consumes the value following a key.
func skipValue(dec *json.Decoder) {
	var raw json.RawMessage
	_ = dec.Decode(&raw)
}

// removeAngularProject returns angular.json without the named project's entry, the
// comma that joined it to its neighbor included. The edit is textual so the file keeps
// its layout.
func removeAngularProject(data []byte, name string) ([]byte, error) {
	start, end, _, err := projectRange(data, name)
	if err != nil {
		return nil, err
	}
	key := bytes.LastIndex(data[:start], []byte(`"`+name+`"`))
	if key < 0 {
		return nil, errors.Newf("project %q: its key was not found before its object", name)
	}
	from := bytes.LastIndexByte(data[:key], '\n') + 1
	to := end
	if rest := bytes.TrimLeft(data[end:], " \t\r\n"); len(rest) > 0 && rest[0] == ',' {
		// Not the last project: the entry and its comma go, up to the next key's line.
		to = len(data) - len(rest) + 1
		if rest := bytes.TrimLeft(data[to:], " \t\r"); len(rest) > 0 && rest[0] == '\n' {
			to = len(data) - len(rest) + 1
		}
	} else {
		// The last project: the comma that joined it to the one before goes too.
		head := bytes.TrimRight(data[:from], " \t\r\n")
		if len(head) > 0 && head[len(head)-1] == ',' {
			from = len(head) - 1
		}
	}

	var b bytes.Buffer
	b.Write(data[:from])
	b.Write(data[to:])

	return b.Bytes(), nil
}
