# Security Policy

Avrea takes the security of its products and services seriously. This policy
explains how to report a vulnerability and what to expect from us in return.

## Reporting a vulnerability

Email **security@avrea.com** with the details. If you wish to encrypt your
report, request our PGP key in an initial (unencrypted) message and we will
respond with it.

Please include, where possible:

- The product or component affected (e.g. the `avr` CLI) and version / commit
- A description of the vulnerability and its potential impact
- Steps to reproduce, proof-of-concept, or affected URLs/endpoints
- Any suggested remediation

Please do **not** report security vulnerabilities through public GitHub issues,
discussions, or pull requests.

## Our commitment

We follow a coordinated vulnerability disclosure model:

| Stage | Target |
|-------|--------|
| Acknowledge receipt | within 3 business days |
| Initial triage & severity assessment | within 10 business days |
| Progress updates | at least every 14 days until resolution |
| Coordinated public disclosure | by mutual agreement, once a fix is available |

We will keep you informed of remediation progress and credit reporters who
wish to be acknowledged.

## Safe harbour

We will not pursue or support legal action against researchers who, in good
faith:

- Make a reasonable effort to avoid privacy violations, data destruction, and
  service degradation;
- Only interact with accounts they own or have explicit permission to access;
- Report vulnerabilities promptly and do not exploit them beyond what is
  necessary to demonstrate the issue;
- Do not disclose the issue publicly before a coordinated resolution.

## Scope

This policy covers Avrea products with digital elements as placed on the
market, including the `avr` CLI, the Avrea control plane and backend services,
and the web console.

## Machine-readable contact

A machine-readable contact record will be published per
[RFC 9116](https://www.rfc-editor.org/rfc/rfc9116) at
`https://avrea.com/.well-known/security.txt`.
