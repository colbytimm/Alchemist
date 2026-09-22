package cosmos_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/colbytimm/alchemist/internal/adapter/cosmos"
)

// capturedUsage is the header as the emulator sends it: keys this adapter
// does not use, a trailing separator, and the three figures out of order.
const capturedUsage = "functions=0;storedProcedures=0;triggers=0;documentSize=0;documentsSize=4300;documentsCount=1284;collectionSize=4400;"

func TestParseResourceUsage(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   cosmos.ResourceUsage
		wantOK bool
	}{
		{
			name:   "captured header",
			header: capturedUsage,
			want:   cosmos.ResourceUsage{Documents: 1284, DocumentsKB: 4300, CollectionKB: 4400},
			wantOK: true,
		},
		{
			name:   "spaces around the pairs",
			header: "documentsCount = 3; documentsSize = 1 ; collectionSize=2",
			want:   cosmos.ResourceUsage{Documents: 3, DocumentsKB: 1, CollectionKB: 2},
			wantOK: true,
		},
		{
			name:   "truncated header",
			header: "documentsCount=1284;documentsSize",
		},
		{
			name:   "unknown keys only",
			header: "functions=0;triggers=0",
		},
		{
			name:   "a figure that is not a number",
			header: "documentsCount=many;documentsSize=1;collectionSize=2",
		},
		{
			name:   "empty header",
			header: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cosmos.ParseResourceUsage(tt.header)

			require.Equal(t, tt.wantOK, ok, "ParseResourceUsage(%q)", tt.header)
			assert.Equal(t, tt.want, got)
		})
	}
}
