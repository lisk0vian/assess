# Security Policy

## Supported versions

Assess is pre-alpha. Only the latest commit on `main` receives security fixes.

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Instead, report
privately via GitHub's "Report a vulnerability" button on the repository's
Security tab, or contact the maintainer directly.

Include:

- A description of the issue and its impact.
- Steps to reproduce (or a proof of concept).
- Affected versions / commits.
- Any suggested fix.

You'll get an acknowledgement within a reasonable time, and we'll coordinate
disclosure. Please give us time to fix before public disclosure.

## Scope notes

Assess runs on a trusted classroom LAN with no accounts and no internet. The
main risks are: arbitrary code in exam content, network exposure of the
teacher's machine, and cheating via client tampering. Proctoring is
penalty-based, not lockdown-based. See `docs/anti-cheat.md`.