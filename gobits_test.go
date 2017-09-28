package gobits

import (
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"testing"
)

func TestNewHandler(t *testing.T) {

	testcases := []struct {
		name       string
		input      *Config
		output     *Config
		errorMatch string
	}{
		{
			name:       "default config",
			input:      &Config{},
			output:     &Config{TempDir: path.Join(os.TempDir(), "gobits"), AllowedMethod: "BITS_POST", Protocol: "{7df0354d-249b-430f-820d-3d2a9bef4931}", MaxSize: 0, Allowed: []string{".*"}, Disallowed: []string{}},
			errorMatch: "",
		},
		{
			name:       "specified config",
			input:      &Config{TempDir: "/tmp", AllowedMethod: "FOO_BAR", Protocol: "{11111111-2222-3333-4444-555555555555}", MaxSize: 10, Allowed: []string{"foo"}, Disallowed: []string{"bar"}},
			output:     &Config{TempDir: "/tmp", AllowedMethod: "FOO_BAR", Protocol: "{11111111-2222-3333-4444-555555555555}", MaxSize: 10, Allowed: []string{"foo"}, Disallowed: []string{"bar"}},
			errorMatch: "",
		},
		{
			name:       "invalid_allowed",
			input:      &Config{Allowed: []string{"?"}},
			output:     &Config{},
			errorMatch: "^failed to compile regexp .*",
		},
		{
			name:       "invalid_disallowed",
			input:      &Config{Disallowed: []string{"?"}},
			output:     &Config{},
			errorMatch: "^failed to compile regexp .*",
		},
	}

	for _, tc := range testcases {

		t.Run(tc.name, func(t *testing.T) {
			h, err := NewHandler(*tc.input, nil)
			if err != nil {
				if tc.errorMatch == "" {
					t.Error(err)
					return
				}
				if b, _ := regexp.Match(tc.errorMatch, []byte(err.Error())); !b {
					t.Errorf("unexpected error: %v, expected %v", err, tc.errorMatch)
					return
				}
				return
			}
			if h.cfg.TempDir != tc.output.TempDir {
				t.Errorf("invalid default tempdir: %v, expected %v", h.cfg.TempDir, tc.output.TempDir)
			}
			if h.cfg.AllowedMethod != tc.output.AllowedMethod {
				t.Errorf("invalid default method: %v, expected %v", h.cfg.AllowedMethod, tc.output.AllowedMethod)
			}
			if h.cfg.Protocol != tc.output.Protocol {
				t.Errorf("invalid default protocol: %v, expected %v", h.cfg.Protocol, tc.output.Protocol)
			}
			if h.cfg.MaxSize != tc.output.MaxSize {
				t.Errorf("invalid default max size: %d, expected %d", h.cfg.MaxSize, tc.output.MaxSize)
			}
			if len(h.cfg.Allowed) != len(tc.output.Allowed) {
				t.Errorf("invalid default allowed: %v, expected %v", h.cfg.Allowed, tc.output.Allowed)
			}
			for i, a := range h.cfg.Allowed {
				if a != tc.output.Allowed[i] {
					t.Errorf("invalid default allowed: %v, expected %v", h.cfg.Allowed, tc.output.Allowed)
					break
				}
			}

			if len(h.cfg.Disallowed) != len(tc.output.Disallowed) {
				t.Errorf("invalid default disallowed: %v, expected %v", h.cfg.Disallowed, tc.output.Disallowed)
			}
			for i, d := range h.cfg.Disallowed {
				if d != tc.output.Disallowed[i] {
					t.Errorf("invalid default disallowed: %v, expected %v", h.cfg.Disallowed, tc.output.Disallowed)
					break
				}
			}

		})
	}

}

func TestBitsError(t *testing.T) {

	testcases := []struct {
		name    string
		guid    string
		status  int
		code    int
		context ErrorContext
		headers map[string]string
	}{
		{
			name:    "without session",
			guid:    "",
			status:  200,
			code:    255,
			context: ErrorContextUnknown,
			headers: map[string]string{
				"BITS-Packet-Type":   "Ack",
				"BITS-Error-Code":    "ff",
				"BITS-Error-Context": "1",
			},
		},
		{
			name:    "with session",
			guid:    "123",
			status:  200,
			code:    255,
			context: ErrorContextUnknown,
			headers: map[string]string{
				"BITS-Packet-Type":   "Ack",
				"BITS-Session-Id":    "123",
				"BITS-Error-Code":    "ff",
				"BITS-Error-Context": "1",
			},
		},
	}

	for _, tc := range testcases {

		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			bitsError(rec, tc.guid, tc.status, tc.code, tc.context)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tc.status {
				t.Errorf("expected status %v, got %v", tc.status, res.StatusCode)
			}

			if res.ContentLength > 0 {
				t.Errorf("expected empty body, got %v", res.ContentLength)
			}

			for hk, hv := range tc.headers {
				if res.Header.Get(hk) != hv {
					t.Errorf("expected %v = %v, got %v", hk, hv, res.Header.Get(hk))
				}
			}

		})

	}

}

func TestNewUUID(t *testing.T) {

	n, err := newUUID()
	if err != nil {
		t.Error(err)
		return
	}

	const uuid = "[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}"

	if b, _ := regexp.Match(uuid, []byte(n)); !b {
		t.Errorf("invalid uuid! got %v", n)
	}

}

func TestExists(t *testing.T) {

	b, err := exists(os.Args[0])
	if err != nil {
		t.Error(err)
	} else if !b {
		t.Errorf("file should exist: %v", os.Args[0])
	}

	b, err = exists(os.Args[0] + "randOMdSTRING")
	if err != nil {
		t.Error(err)
	} else if b {
		t.Errorf("file should not exist: %v", os.Args[0])
	}

}

func TestParseRange(t *testing.T) {

	testcases := []struct {
		name       string
		input      string
		rangeStart uint64
		rangeEnd   uint64
		fileLength uint64
		errorMatch string
	}{
		{
			name:       "no bytes prefix",
			input:      "a",
			errorMatch: "invalid range syntax",
		},
		{
			name:       "no slash",
			input:      "bytes a",
			errorMatch: "invalid range syntax",
		},
		{
			name:       "invalid length",
			input:      "bytes a/a",
			errorMatch: "strconv.ParseUint: parsing",
		},
		{
			name:       "invalid range",
			input:      "bytes a/100",
			errorMatch: "invalid range syntax",
		},
		{
			name:       "invalid range start",
			input:      "bytes a-20/100",
			errorMatch: "strconv.ParseUint: parsing",
		},
		{
			name:       "invalid range end",
			input:      "bytes 10-a/100",
			errorMatch: "strconv.ParseUint: parsing",
		},
		{
			name:       "invalid range end",
			input:      "bytes 10-20/100",
			rangeStart: 10,
			rangeEnd:   20,
			fileLength: 100,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			rangeStart, rangeEnd, fileLength, err := parseRange(tc.input)

			if err != nil {
				if b, _ := regexp.Match(tc.errorMatch, []byte(err.Error())); !b {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}

			if rangeStart != tc.rangeStart {
				t.Errorf("invalid rangeStart %v, expected %v", rangeStart, tc.rangeStart)
			}

			if rangeEnd != tc.rangeEnd {
				t.Errorf("invalid rangeEnd %v, expected %v", rangeEnd, tc.rangeEnd)
			}

			if fileLength != tc.fileLength {
				t.Errorf("invalid fileLength %v, expected %v", fileLength, tc.fileLength)
			}

		})
	}

}
