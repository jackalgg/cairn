package parser

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strconv"

	yamlv3 "go.yaml.in/yaml/v3"
)

// ValidateYAML reports whether data is parseable as one or more YAML documents.
// It is intentionally permissive about shape: mappings, sequences, and scalars
// are all valid. A non-nil error means the bytes are not valid YAML at all.
func ValidateYAML(data []byte) error {
	dec := yamlv3.NewDecoder(bytes.NewReader(data))
	for {
		var node interface{}
		err := dec.Decode(&node)
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

// ParseErrorLine extracts the 1-based line number embedded in a yaml.v3 parse
// error message (for example "yaml: line 6: ..."). It returns false when no
// line number can be recovered.
func ParseErrorLine(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	m := lineErrorRe.FindStringSubmatch(err.Error())
	if m == nil {
		return 0, false
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil || n < 1 {
		return 0, false
	}
	return n, true
}

var lineErrorRe = regexp.MustCompile(`line (\d+)`)
