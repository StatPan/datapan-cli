# Security reporting

Report a vulnerability privately through GitHub's [private vulnerability
reporting form](https://github.com/StatPan/datapan-cli/security/advisories/new).
This form is the only supported channel for sensitive reports in this
repository. Do not put credentials, browser profiles, provider URLs, API
responses, exploit details, or other sensitive material in a public GitHub
issue.

Security support covers the maintained `main` branch. Public issues remain
appropriate for non-sensitive bugs and documentation problems only; remove
security-sensitive material before posting.

Maintainers can verify that GitHub still exposes the private route with:

```bash
gh api repos/StatPan/datapan-cli/private-vulnerability-reporting
```

The command must return `{"enabled":true}`. It only reads the repository
setting and does not create a report.
