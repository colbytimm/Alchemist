// Package canonical spells a JSON document one way, so that two documents a
// reader would call equal have equal bytes: object keys sorted, no
// insignificant whitespace, and every number written as the value it is.
package canonical

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

// Marshal returns the canonical form of document. Strings are compared as
// the code points they decode to, never Unicode-normalized; array order is
// kept; of two members with one key, the last wins.
func Marshal(document []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("canonical: decode: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("canonical: decode: more than one value")
	}
	return Encode(Numbers(value))
}

// Encode writes a value decoded with UseNumber, and already passed through
// Numbers, in canonical form. encoding/json sorts map keys itself.
func Encode(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("canonical: encode: %w", err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// Numbers respells every number in a value decoded with UseNumber, so 1,
// 1.0 and 1e0 are one number, while an integer too large for a float64
// keeps every digit. It rewrites maps and slices in place.
func Numbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		return number(v)
	case map[string]any:
		for name, member := range v {
			v[name] = Numbers(member)
		}
	case []any:
		for i, element := range v {
			v[i] = Numbers(element)
		}
	}
	return value
}

// PartitionKeyValue is value, a scalar, in canonical form, but for a
// negative zero, which stays -0: the service hashes a partition key number
// by its bits, so -0 and 0 may name two partitions.
func PartitionKeyValue(value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("canonical: decode: %w", err)
	}
	if n, ok := decoded.(json.Number); ok {
		if float, err := n.Float64(); err == nil && float == 0 && math.Signbit(float) {
			return []byte("-0"), nil
		}
	}
	return Marshal(value)
}

func number(n json.Number) json.Number {
	if integer, err := n.Int64(); err == nil {
		return json.Number(strconv.FormatInt(integer, 10))
	}
	float, err := n.Float64()
	switch {
	case err != nil:
		return n
	case float == 0:
		return "0" // -0.0 would otherwise respell as -0, which reads back as the integer 0
	}
	return json.Number(strconv.FormatFloat(float, 'g', -1, 64))
}
