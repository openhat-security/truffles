package disclose

import (
	"fmt"
	"sort"
	"strings"
)

const (
	DefaultContact = "openhat@devrecated.com"

	TrufflesRepoURL = "https://github.com/openhat-security/truffles"
	TrufflesSiteURL = "https://truffles.devrecated.com"
	OpenHatOrgURL   = "https://github.com/openhat-security"
)

// Finding is one CSV row used for disclosure.
type Finding struct {
	Detector  string
	Verified  bool
	Timestamp string
	Secret    string
	RepoURL   string
	FileLine  string
	Commit    string
	Author    string
	Decoder   string
	VerifyErr string
	Owner     string
	Repo      string
}

// RepoReport aggregates findings for one GitHub repository.
type RepoReport struct {
	Owner    string
	Repo     string
	RepoURL  string
	Findings []Finding
}

func (r RepoReport) Key() string {
	return r.Owner + "/" + r.Repo
}

func (r RepoReport) HasVerified() bool {
	for _, f := range r.Findings {
		if f.Verified {
			return true
		}
	}
	return false
}

func (r RepoReport) Severity() string {
	if r.HasVerified() {
		return "high"
	}
	return "medium"
}

func (r RepoReport) primaryDetector() string {
	for _, f := range r.Findings {
		if f.Verified && f.Detector != "" {
			return f.Detector
		}
	}
	if len(r.Findings) > 0 {
		return r.Findings[0].Detector
	}
	return "credential"
}

// PrivateSummary is the GitHub private advisory summary.
func PrivateSummary(r RepoReport) string {
	d := r.primaryDetector()
	n := len(r.Findings)
	if n == 1 {
		if r.HasVerified() {
			return fmt.Sprintf("Active %s secret exposed", d)
		}
		return fmt.Sprintf("%s secret exposed", d)
	}
	if r.HasVerified() {
		return fmt.Sprintf("Active secrets exposed (%d findings, incl. %s)", n, d)
	}
	return fmt.Sprintf("Secrets exposed (%d findings)", n)
}

// creditBlock is shared OpenHat / truffles attribution + star/follow CTA.
func creditBlock() string {
	return fmt.Sprintf(`If this report helps you remediate a leaked secret, please **star** [truffles](%s) and **follow** the [OpenHat](%s) organization on GitHub — and consider installing truffles for future scans at [%s](%s).`,
		TrufflesRepoURL, OpenHatOrgURL, TrufflesSiteURL, TrufflesSiteURL)
}

// PrivateDescription is the full private advisory body.
func PrivateDescription(r RepoReport, includeSecret bool) string {
	var b strings.Builder
	findings := append([]Finding(nil), r.Findings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].FileLine != findings[j].FileLine {
			return findings[i].FileLine < findings[j].FileLine
		}
		return findings[i].Detector < findings[j].Detector
	})

	b.WriteString("### Summary\n\n")
	b.WriteString(PrivateSummary(r))
	b.WriteString(". ")
	if r.HasVerified() {
		b.WriteString("At least one secret was **verified live** by our scanner.\n\n")
	} else {
		b.WriteString("\n\n")
	}
	b.WriteString(creditBlock())
	b.WriteString("\n\n")

	b.WriteString("### Details\n\n")
	b.WriteString(fmt.Sprintf("Repository: %s\n\n", r.RepoURL))
	for i, f := range findings {
		b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, f.Detector))
		b.WriteString(fmt.Sprintf("   - verified: %v\n", f.Verified))
		if f.Timestamp != "" {
			b.WriteString(fmt.Sprintf("   - timestamp: %s\n", f.Timestamp))
		}
		if includeSecret && f.Secret != "" {
			b.WriteString(fmt.Sprintf("   - secret: `%s`\n", f.Secret))
		}
		if f.FileLine != "" {
			b.WriteString(fmt.Sprintf("   - location: `%s`\n", f.FileLine))
		}
		if f.Commit != "" {
			short := f.Commit
			if len(short) > 8 {
				short = short[:8]
			}
			b.WriteString(fmt.Sprintf("   - commit: `%s`\n", short))
		}
		if f.Author != "" {
			b.WriteString(fmt.Sprintf("   - author: %s\n", f.Author))
		}
		b.WriteString("\n")
	}
	if !includeSecret {
		b.WriteString("_Secret values omitted here. Reply if you need identification help beyond file/commit locations._\n\n")
	}

	b.WriteString("### Proof of concept\n\n")
	if r.HasVerified() {
		b.WriteString("One or more secrets above are **active** — verified by our program, ")
		b.WriteString(fmt.Sprintf("[truffles](%s), while testing. ", TrufflesRepoURL))
	} else {
		b.WriteString("Secrets were detected in public git history by ")
		b.WriteString(fmt.Sprintf("[truffles](%s). ", TrufflesRepoURL))
	}
	b.WriteString("Exposed credentials are a security vulnerability. ")
	b.WriteString("We are reporting this (and other active keys found while testing our product) ")
	b.WriteString("to repository owners in good faith.\n\n")
	b.WriteString(creditBlock())
	b.WriteString("\n\n")

	b.WriteString("### Impact\n\n")
	b.WriteString("Anyone with access to this public repository (or its forks/mirrors/history) ")
	b.WriteString("may be able to use the exposed credential(s) until they are rotated and revoked. ")
	b.WriteString("**Please rotate/revoke affected credentials immediately** and scrub them from git history ")
	b.WriteString("where practical.\n\n")

	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("Reported by [OpenHat](%s) using [truffles](%s) — [%s](%s).\n",
		OpenHatOrgURL, TrufflesRepoURL, TrufflesSiteURL, TrufflesSiteURL))

	return b.String()
}

// IssueTitle is the public issue title when PVR is unavailable.
func IssueTitle() string {
	return "Security: exposed credentials reported via OpenHat"
}

// IssueBody is a public-safe issue: no secrets, paths, commits, or detectors.
func IssueBody(contact string) string {
	if strings.TrimSpace(contact) == "" {
		contact = DefaultContact
	}
	return fmt.Sprintf(`## Private security report

We believe this repository may contain **exposed credentials** in git history. It appears to be an active secret or credential, verified by our tool, `+"`truffles`"+`.

**Private vulnerability reporting is not enabled** on this repository, so we cannot attach the advisory details here without putting secrets in a public issue.

Please email **%s** to receive the private advisory (detector, file/commit locations, and remediation guidance).

If this helps you remediate, please **star** [truffles](%s) and **follow** [OpenHat](%s) on GitHub, and visit [%s](%s).

If you enable [private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability),future reports can be filed directly through GitHub.

— [OpenHat](%s) / [truffles](%s) / [%s](%s)
`, contact, TrufflesRepoURL, OpenHatOrgURL, TrufflesSiteURL, TrufflesSiteURL, OpenHatOrgURL, TrufflesRepoURL, TrufflesSiteURL, TrufflesSiteURL)
}

// LocalDraft is the operator-facing markdown (full detail).
func LocalDraft(r RepoReport, mode, contact string, includeSecret bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Disclosure draft: %s\n\n", r.Key()))
	b.WriteString(fmt.Sprintf("- mode: `%s`\n", mode))
	b.WriteString(fmt.Sprintf("- contact: %s\n", contact))
	b.WriteString(fmt.Sprintf("- repo: %s\n\n", r.RepoURL))
	b.WriteString("---\n\n")
	b.WriteString("## Private advisory body\n\n")
	b.WriteString(PrivateDescription(r, includeSecret))
	b.WriteString("\n---\n\n")
	b.WriteString("## Public issue body (if used)\n\n")
	b.WriteString(IssueBody(contact))
	return b.String()
}
