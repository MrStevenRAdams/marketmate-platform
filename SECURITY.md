# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest (main) | ✅ |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

If you discover a security vulnerability in MarketMate, please report it by emailing:

**security@e-lister.co.uk**

Include as much of the following as possible:

- Type of issue (e.g. SQL injection, XSS, authentication bypass, credential exposure)
- Full path of the source file(s) related to the issue
- Location of the affected source code (tag/branch/commit or direct URL)
- Any special configuration required to reproduce the issue
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue, including how an attacker might exploit it

We aim to respond within **48 hours** and will keep you informed of progress.

## Security Measures

- All API credentials are encrypted at rest using AES-256-GCM
- Multi-tenant data isolation enforced at every query layer
- All secrets stored in GCP Secret Manager (never in environment variables or code)
- Automated compliance scans run on every deploy (Trivy, gosec, Semgrep, OWASP ZAP, Prowler)
- Authentication via Firebase Auth with per-tenant middleware
- SP-API and marketplace webhooks verified by signature on every inbound request

## Disclosure Policy

Once a vulnerability is confirmed, we commit to:

1. Confirming receipt within 48 hours
2. Providing a fix timeline within 7 days
3. Notifying you when the fix is deployed
4. Crediting you in the release notes (unless you prefer anonymity)
