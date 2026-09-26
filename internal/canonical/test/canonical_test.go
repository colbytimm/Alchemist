package canonical_test

import (
	"crypto/sha256"
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/canonical"
)

func hash(t *testing.T, document string) [sha256.Size]byte {
	t.Helper()
	encoded, err := canonical.Marshal([]byte(document))
	require.NoError(t, err, "Marshal(%s)", document)
	return sha256.Sum256(encoded)
}

func TestDocumentsAReaderCallsEqualHashEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{name: "key order at the top", a: `{"a":1,"b":2}`, b: `{"b":2,"a":1}`},
		{name: "key order nested", a: `{"x":{"a":[{"p":1,"q":2}],"b":2}}`, b: `{"x":{"b":2,"a":[{"q":2,"p":1}]}}`},
		{name: "whitespace", a: `{"a":[1,2]}`, b: "{ \"a\" :\n\t[ 1 , 2 ] }"},
		{name: "one and one point oh", a: `{"n":1}`, b: `{"n":1.0}`},
		{name: "one and one e zero", a: `{"n":1}`, b: `{"n":1e0}`},
		{name: "negative zero and zero", a: `{"n":-0.0}`, b: `{"n":0}`},
		{name: "negative integer zero and zero", a: `{"n":-0}`, b: `{"n":0}`},
		{name: "escaped and literal forms of one string", a: `{"s":"Aé"}`, b: `{"s":"Aé"}`},
		{name: "escaped slash", a: `{"s":"a/b"}`, b: `{"s":"a\/b"}`},
		{name: "the last of two equal keys wins", a: `{"a":1,"a":2}`, b: `{"a":2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, hash(t, tt.a), hash(t, tt.b))
		})
	}
}

func TestDocumentsThatDifferHashApart(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{name: "array order", a: `{"a":[1,2]}`, b: `{"a":[2,1]}`},
		{name: "null against absent", a: `{"a":null}`, b: `{}`},
		{name: "string against number", a: `{"a":"1"}`, b: `{"a":1}`},
		{name: "past two to the fifty-third", a: `{"a":9007199254740993}`, b: `{"a":9007199254740992}`},
		{name: "case of a key", a: `{"a":1}`, b: `{"A":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEqual(t, hash(t, tt.a), hash(t, tt.b))
		})
	}
}

func TestMarshalWritesSortedKeysWithoutEscapingHTML(t *testing.T) {
	encoded, err := canonical.Marshal([]byte(`{ "b": "<a&b>", "a": [1.50, 2e2] }`))

	require.NoError(t, err)
	assert.Equal(t, `{"a":[1.5,200],"b":"<a&b>"}`, string(encoded))
}

func TestMarshalRefusesWhatIsNotOneDocument(t *testing.T) {
	for _, document := range []string{``, `{"a":`, `{} {}`, `nope`} {
		_, err := canonical.Marshal([]byte(document))
		assert.Error(t, err, "Marshal(%q)", document)
	}
}

func FuzzMarshalIsIdempotent(f *testing.F) {
	for _, seed := range []string{`{"b":1,"a":[1.0,{"d":null,"c":true}]}`, `[]`, `"x"`, `1e300`, `{"s":" <>"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, document string) {
		once, err := canonical.Marshal([]byte(document))
		if err != nil {
			return
		}
		twice, err := canonical.Marshal(once)
		require.NoError(t, err)
		assert.Equal(t, string(once), string(twice))
	})
}

func FuzzMarshalIgnoresTheOrderOfKeys(f *testing.F) {
	f.Add(`{"b":1,"a":{"y":[1,2],"x":"s"},"c":null}`, uint64(1))
	f.Add(`{"k":{"j":{"i":1.0}}}`, uint64(7))
	f.Fuzz(func(t *testing.T, document string, seed uint64) {
		decoder := json.NewDecoder(strings.NewReader(document))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil {
			return
		}
		want, err := canonical.Marshal([]byte(document))
		if err != nil {
			return
		}
		shuffled := writeShuffled(value, rand.New(rand.NewPCG(seed, seed)))
		got, err := canonical.Marshal([]byte(shuffled))
		require.NoError(t, err)
		assert.Equal(t, string(want), string(got))
	})
}

// writeShuffled writes value back out as JSON with every object's members
// in a random order.
func writeShuffled(value any, random *rand.Rand) string {
	switch v := value.(type) {
	case map[string]any:
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		random.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
		members := make([]string, 0, len(names))
		for _, name := range names {
			label, _ := json.Marshal(name)
			members = append(members, string(label)+":"+writeShuffled(v[name], random))
		}
		return "{" + strings.Join(members, ",") + "}"
	case []any:
		elements := make([]string, 0, len(v))
		for _, element := range v {
			elements = append(elements, writeShuffled(element, random))
		}
		return "[" + strings.Join(elements, ",") + "]"
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
