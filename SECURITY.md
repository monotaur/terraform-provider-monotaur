# Security Policy

## Supported Versions

The Monotaur Terraform provider is under active development. Security fixes are
released against the latest published version on the
[Terraform Registry](https://registry.terraform.io/providers/monotaur/monotaur).
We recommend always running the most recent release.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately by email to **security@shaftware.com**.

Please include:

- A description of the vulnerability and its potential impact
- Steps to reproduce, or a proof of concept
- The provider version and Terraform version affected
- Any relevant configuration (with secrets redacted)

You can expect an initial acknowledgement within **3 business days**. We will
keep you informed of our progress toward a fix and may ask for additional
detail. Please give us a reasonable opportunity to remediate the issue before
any public disclosure.

## Scope

This policy covers the provider code in this repository. Vulnerabilities in the
Monotaur platform/API itself should also be reported to security@shaftware.com.

## A Note on Credentials

This provider handles API keys and secrets. Never commit credentials, API keys
(`mtr_live_*` / `mtr_test_*`), `*.tfstate` files, or GPG private keys to this
repository — they are excluded via `.gitignore`. If you discover leaked
credentials in the repo or its history, treat it as a security report and
contact us at the address above.
