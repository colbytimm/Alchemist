package cosmos

import (
	"errors"
	"fmt"
	"strconv"
)

const defaultPageSize = int32(100)

// Settings errors, matchable with errors.Is.
var (
	ErrMissingCredentials = errors.New("settings require connection_string, or endpoint and key")
	ErrInvalidPageSize    = errors.New("page_size must be a positive integer")
)

// Settings is the validated connection configuration for one Cosmos account.
type Settings struct {
	Endpoint           string
	Key                string
	ConnectionString   string
	InsecureSkipVerify bool
	PageSize           int32
}

func ParseSettings(raw map[string]string) (Settings, error) {
	s := Settings{
		Endpoint:         raw["endpoint"],
		Key:              raw["key"],
		ConnectionString: raw["connection_string"],
		PageSize:         defaultPageSize,
	}
	if s.ConnectionString == "" && (s.Endpoint == "" || s.Key == "") {
		return Settings{}, fmt.Errorf("cosmos: parse settings: %w", ErrMissingCredentials)
	}
	s.InsecureSkipVerify = raw["insecure_skip_verify"] == "true"
	if v, ok := raw["page_size"]; ok {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return Settings{}, fmt.Errorf("cosmos: parse settings: page_size %q: %w", v, ErrInvalidPageSize)
		}
		s.PageSize = int32(n)
	}
	return s, nil
}
