package disclose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const apiRoot = "https://api.github.com"
const userAgent = "truffles-disclose"

// Client talks to GitHub for PVR checks, private reports, and issues.
type Client struct {
	HTTP  *http.Client
	Token string
}

func NewClient(token string) *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: 45 * time.Second},
		Token: token,
	}
}

// ViewerLogin returns the @handle for the token (GET /user).
func (c *Client) ViewerLogin() (string, error) {
	if c.Token == "" {
		return "", fmt.Errorf("no token")
	}
	code, body, err := c.do(http.MethodGet, apiRoot+"/user", nil)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("GET /user: HTTP %d: %s", code, trimBody(body))
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", err
	}
	if u.Login == "" {
		return "", fmt.Errorf("GET /user: empty login")
	}
	return u.Login, nil
}

type pvrStatus struct {
	Enabled bool `json:"enabled"`
}

// CheckPVR reports whether private vulnerability reporting is enabled.
func (c *Client) CheckPVR(owner, repo string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/private-vulnerability-reporting", apiRoot, owner, repo)
	code, body, err := c.do(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	switch code {
	case 200:
		var st pvrStatus
		if err := json.Unmarshal(body, &st); err != nil {
			return false, err
		}
		return st.Enabled, nil
	case 404:
		return false, nil
	default:
		return false, fmt.Errorf("PVR check %s/%s: HTTP %d: %s", owner, repo, code, trimBody(body))
	}
}

type privateReportReq struct {
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Severity    string   `json:"severity"`
	CWEs        []string `json:"cwe_ids"`
}

type privateReportResp struct {
	GHSAID  string `json:"ghsa_id"`
	HTMLURL string `json:"html_url"`
}

// SubmitPrivateReport files a private vulnerability report.
func (c *Client) SubmitPrivateReport(r RepoReport, includeSecret bool) (htmlURL string, err error) {
	url := fmt.Sprintf("%s/repos/%s/%s/security-advisories/reports", apiRoot, r.Owner, r.Repo)
	payload := privateReportReq{
		Summary:     PrivateSummary(r),
		Description: PrivateDescription(r, includeSecret),
		Severity:    r.Severity(),
		CWEs:        []string{"CWE-798"},
	}
	code, body, err := c.do(http.MethodPost, url, payload)
	if err != nil {
		return "", err
	}
	if code != 201 && code != 200 {
		return "", fmt.Errorf("private report %s: HTTP %d: %s", r.Key(), code, trimBody(body))
	}
	var resp privateReportResp
	_ = json.Unmarshal(body, &resp)
	if resp.HTMLURL != "" {
		return resp.HTMLURL, nil
	}
	return string(body), nil
}

type issueReq struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type issueResp struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
}

// CreateIssue opens a public issue with the safe contact template.
func (c *Client) CreateIssue(owner, repo, contact string) (htmlURL string, err error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues", apiRoot, owner, repo)
	payload := issueReq{
		Title: IssueTitle(),
		Body:  IssueBody(contact),
	}
	code, body, err := c.do(http.MethodPost, url, payload)
	if err != nil {
		return "", err
	}
	if code == 410 {
		return "", fmt.Errorf("issues disabled on %s/%s", owner, repo)
	}
	if code != 201 {
		return "", fmt.Errorf("create issue %s/%s: HTTP %d: %s", owner, repo, code, trimBody(body))
	}
	var resp issueResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.HTMLURL, nil
}

func (c *Client) do(method, url string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode == 403 || resp.StatusCode == 429 {
		// One short backoff then single retry.
		time.Sleep(2 * time.Second)
		req2, _ := http.NewRequest(method, url, nil)
		if payload != nil {
			b, _ := json.Marshal(payload)
			req2, _ = http.NewRequest(method, url, bytes.NewReader(b))
			req2.Header.Set("Content-Type", "application/json")
		}
		req2.Header.Set("Accept", "application/vnd.github+json")
		req2.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req2.Header.Set("User-Agent", userAgent)
		if c.Token != "" {
			req2.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp2, err := c.HTTP.Do(req2)
		if err != nil {
			return 0, nil, err
		}
		defer resp2.Body.Close()
		data2, _ := io.ReadAll(io.LimitReader(resp2.Body, 1<<20))
		return resp2.StatusCode, data2, nil
	}
	return resp.StatusCode, data, nil
}

func trimBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
