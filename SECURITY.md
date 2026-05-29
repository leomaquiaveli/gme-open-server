# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Send a description to the maintainers via the contact in the repository's GitHub profile. Include:

- A description of the vulnerability and its potential impact
- Steps to reproduce
- Any suggested fix, if you have one

You will receive a response within 72 hours. We will work with you to understand and address the issue before any public disclosure.

## Scope

This project is a self-hosted server. Security considerations that apply to your deployment:

- **`API_KEY`**: treat it as a secret. Rotate it if you suspect it was exposed. Pass it via environment variable, never hardcode it.
- **`SERVICE_ACCOUNT_CREDENTIALS`**: the GCS service account JSON contains a private key. Never commit it to version control. Pass it via environment variable or Secret Manager.
- **Network exposure**: the server does not enforce HTTPS. Run behind a load balancer or reverse proxy that terminates TLS in production.
- **Input URLs**: the server fetches URLs you provide. Ensure your deployment network policy prevents access to internal endpoints if that is a concern for your environment.

## Container image vulnerabilities

The container image bundles FFmpeg and its system dependencies (Debian/Ubuntu packages such as `gnutls`, `mbedtls`, `glibc`, and the FFmpeg codec libraries). Automated scanners (e.g. GCP Artifact Registry, Trivy) will report CVEs against these OS packages.

Our posture on these:

- **Application layer:** the Go binary has **zero third-party dependencies** (standard library only), so the application code carries no transitive dependency CVEs. The Go toolchain is kept current to absorb standard-library advisories.
- **OS layer:** the majority of reported CVEs are in FFmpeg and its codec/TLS libraries and **have no fixed version available upstream** at the time of build, regardless of base image (Ubuntu, Debian-slim, etc.). We rebuild periodically to pick up patches as they are published.
- **Effective severity:** many high-CVSS entries are downgraded to *medium effective severity* by scanners because they are not exploitable in this deployment context (they require crafted media from untrusted sources, which the threat model above already addresses).

If you operate in a higher-risk environment (public endpoint, untrusted input), consider: enabling a network egress policy, restricting the `/v1/media/pipeline` route, and keeping the image rebuilt on a schedule.
