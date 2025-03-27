package cosmos

import (
	"strings"
	"testing"
)

func TestExtractCosmosCredentials(t *testing.T) {
	tests := []struct {
		name              string
		connectionString  string
		wantEndpoint      string
		wantKey           string
		wantError         bool
		wantErrorContains string
	}{
		{
			name:             "Valid Connection String",
			connectionString: "AccountEndpoint=https://example.documents.azure.com:443/;AccountKey=dGVzdEtleQ==;",
			wantEndpoint:     "https://example.documents.azure.com:443/",
			wantKey:          "dGVzdEtleQ==",
			wantError:        false,
		},
		{
			name:              "Missing AccountEndpoint",
			connectionString:  "AccountKey=dGVzdEtleQ==;",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint",
		},
		{
			name:              "Missing AccountKey",
			connectionString:  "AccountEndpoint=https://example.documents.azure.com:443/;",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint or AccountKey",
		},
		{
			name:              "Empty Connection String",
			connectionString:  "",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint",
		},
		{
			name:              "Malformed Connection String",
			connectionString:  "InvalidFormat;WithNoProperSeparators",
			wantEndpoint:      "",
			wantKey:           "",
			wantError:         true,
			wantErrorContains: "missing AccountEndpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint, key, err := ExtractCosmosCredentials(tt.connectionString)

			if (err != nil) != tt.wantError {
				t.Errorf("ExtractCosmosCredentials() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrorContains) {
					t.Errorf("Expected error containing %q, got %v", tt.wantErrorContains, err)
				}
				return
			}

			if endpoint != tt.wantEndpoint {
				t.Errorf("ExtractCosmosCredentials() endpoint = %v, want %v", endpoint, tt.wantEndpoint)
			}

			if key != tt.wantKey {
				t.Errorf("ExtractCosmosCredentials() key = %v, want %v", key, tt.wantKey)
			}
		})
	}
}
