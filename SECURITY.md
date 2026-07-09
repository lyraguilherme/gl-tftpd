# Security Policy

## Supported versions

This is a small project maintained on a best-effort basis. Only the latest
release receives security fixes.

| Version | Supported |
| ------- | --------- |
| latest  | ✅        |
| older   | ❌        |

## Reporting a vulnerability

Please report security issues **privately** — do not open a public issue.

Use GitHub's [Private Vulnerability Reporting][pvr] (the **"Report a
vulnerability"** button on the repository's *Security* tab). I aim to
acknowledge reports within 7 days and to ship a fix as soon as practical,
crediting you unless you'd rather stay anonymous.

[pvr]: https://github.com/lyraguilherme/gl-tftpd/security/advisories/new

## Scope

TFTP has **no authentication or transport encryption by design** (see *Security
notes* in the README). Reports about that inherent property are not treated as
vulnerabilities.

Issues in *this implementation* are in scope, for example:

- path traversal or sandbox escape (reading/writing outside `-root`),
- crashes, panics, or hangs triggered by malformed packets,
- memory-safety or resource-exhaustion problems,
- anything that lets a client exceed its documented privileges.
