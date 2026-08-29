package cosmos

import (
	"fmt"
	"strconv"
)

// defaultPageSize is the page-size hint used when none is configured.
const defaultPageSize = int32(100)

// Settings is the validated connection configuration for one Cosmos account.
type Settings struct {
	Endpoint           string
	Key                string
	ConnectionString   string
	InsecureSkipVerify bool
	PageSize           int32
}

// ParseSettings validates the raw settings map. Either "connection_string"
// or both "endpoint" and "key" must be present; "insecure_skip_verify" is
// honored only when it is exactly "true" (emulator use only); "page_size"
// must be a positive integer when set.
func ParseSettings(raw map[string]string) (Settings, error) {
	s := Settings{
		Endpoint:         raw["endpoint"],
		Key:              raw["key"],
		ConnectionString: raw["connection_string"],
		PageSize:         defaultPageSize,
	}
	if s.ConnectionString == "" && (s.Endpoint == "" || s.Key == "") {
		return Settings{}, fmt.Errorf("cosmos: settings require connection_string, or endpoint and key")
	}
	s.InsecureSkipVerify = raw["insecure_skip_verify"] == "true"
	if v, ok := raw["page_size"]; ok {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return Settings{}, fmt.Errorf("cosmos: page_size %q must be a positive integer", v)
		}
		s.PageSize = int32(n)
	}
	return s, nil
}
