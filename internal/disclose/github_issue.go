package disclose

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ParseIssueURL extracts owner, repo, and issue number from a github.com issue URL.
func ParseIssueURL(raw string) (owner, repo string, number int, ok bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, "github.com") {
		return "", "", 0, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "issues" {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil || n <= 0 {
		return "", "", 0, false
	}
	return parts[0], parts[1], n, true
}

// IssueOpen reports whether an issue exists and is open. Missing issues return (false, nil).
func (c *Client) IssueOpen(owner, repo string, number int) (bool, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d", apiRoot, owner, repo, number)
	code, body, err := c.do("GET", apiURL, nil)
	if err != nil {
		return false, err
	}
	switch code {
	case 200:
		var st struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(body, &st); err != nil {
			return false, err
		}
		return strings.EqualFold(st.State, "open"), nil
	case 404:
		return false, nil
	default:
		return false, fmt.Errorf("issue %s/%s#%d: HTTP %d: %s", owner, repo, number, code, trimBody(body))
	}
}
