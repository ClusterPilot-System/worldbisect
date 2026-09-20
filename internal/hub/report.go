package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Submission is a caller-reported summary, not a hub-verified certificate.
// No workspace identifier, source files, logs or executable command is accepted.
type Submission struct {
	Repository  string `json:"repository"`
	CheckName   string `json:"check_name"`
	CommitSHA   string `json:"commit_sha,omitempty"`
	RunURL      string `json:"run_url,omitempty"`
	Status      string `json:"status"`
	Finding     string `json:"finding"`
	Tested      string `json:"tested"`
	NextStep    string `json:"next_step"`
	Experiments int    `json:"experiments"`
	AnalysisID  string `json:"analysis_id,omitempty"`
}

type Report struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Submission
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var commitPattern = regexp.MustCompile(`^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var runIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

func (s Submission) Validate() error {
	if len(s.Repository) > 200 || !repositoryPattern.MatchString(s.Repository) {
		return errors.New("repository must be an owner/name identifier (maximum 200 bytes)")
	}
	for _, field := range []struct {
		name, value string
		max         int
		required    bool
	}{
		{"check_name", s.CheckName, 120, true}, {"finding", s.Finding, 2000, true},
		{"tested", s.Tested, 2000, true}, {"next_step", s.NextStep, 2000, true}, {"analysis_id", s.AnalysisID, 128, false},
	} {
		if len(field.value) > field.max || !utf8.ValidString(field.value) || (field.required && strings.TrimSpace(field.value) == "") {
			return fmt.Errorf("%s must contain valid text within %d bytes", field.name, field.max)
		}
		for _, r := range field.value {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				return fmt.Errorf("%s contains a control character", field.name)
			}
		}
	}
	if s.CommitSHA != "" && !commitPattern.MatchString(s.CommitSHA) {
		return errors.New("commit_sha must contain 40 or 64 hexadecimal characters")
	}
	if s.Experiments < 0 || s.Experiments > 1000000 {
		return errors.New("experiments must be between 0 and 1000000")
	}
	switch s.Status {
	case "PROVEN", "SUPPORTED", "CORRELATED", "UNPROVEN":
	default:
		return errors.New("status must be PROVEN, SUPPORTED, CORRELATED or UNPROVEN")
	}
	if s.RunURL != "" {
		u, err := url.Parse(s.RunURL)
		prefix := "/" + s.Repository + "/actions/runs/"
		if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, prefix) || !runIDPattern.MatchString(strings.TrimPrefix(u.Path, prefix)) {
			return errors.New("run_url must be an HTTPS github.com Actions run URL for the same repository, without query or fragment")
		}
	}
	return nil
}

func decodeStrict(b []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected exactly one JSON object")
	}
	return nil
}
