# Security policy

## Reporting a vulnerability

Please report security problems privately, not in a public issue.

Use GitHub's private reporting form: <https://github.com/Gitguy268/plain/security/advisories/new>

Say what you found, how to reproduce it, and your OS. You should get a reply within a week. Please give the fix time to ship before you publish details.

## Scope

- Plain source that makes the compiler write or run something outside the project folder;
- generated JavaScript that behaves differently from what the checker promised (for example a null leaking through `extern`);
- the GitHub Actions workflows in this repository.

## Supported versions

Only the `main` branch is supported.
