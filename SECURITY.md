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
