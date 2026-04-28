# Release Process

This document describes the steps required to publish a new version of the
Monotaur Terraform provider to the [Terraform Registry](https://registry.terraform.io).

## Prerequisites

- Repository admin access to `monotaur/monotaur-terraform-provider` on GitHub
- A Terraform Registry account linked to the `monotaur` namespace at
  `registry.terraform.io`
- GPG key pair for signing release artifacts

## One-time Setup

### 1. Generate a GPG signing key

Run the following on a trusted machine:

```sh
gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: Monotaur Release
Name-Email: releases@monotaur.io
Expire-Date: 0
%no-protection
%commit
EOF
```

Or generate interactively:

```sh
gpg --full-gen-key
```

Export the private key and note the fingerprint:

```sh
gpg --list-secret-keys --keyid-format LONG
# Copy the fingerprint (40-char hex string after "rsa4096/")

gpg --armor --export-secret-keys <FINGERPRINT> > monotaur-release.gpg
```

### 2. Add secrets to GitHub

In the GitHub repository go to **Settings > Secrets and variables > Actions**
and add:

| Secret name | Value |
|---|---|
| `GPG_PRIVATE_KEY` | Contents of `monotaur-release.gpg` (the `--armor` export) |
| `PASSPHRASE` | Passphrase used when generating the key (leave empty if none) |

### 3. Upload the public key to the Terraform Registry

1. Export the public key:

   ```sh
   gpg --armor --export <FINGERPRINT> > monotaur-release-public.gpg
   ```

2. Log in to [registry.terraform.io](https://registry.terraform.io).
3. Navigate to **User Settings > GPG Keys**.
4. Click **Add a GPG key**, paste the contents of `monotaur-release-public.gpg`,
   and save.

### 4. Connect the GitHub repository to the Terraform Registry

1. In the Terraform Registry, click **Publish > Provider**.
2. Select the `monotaur/monotaur-terraform-provider` repository.
3. The Registry will install a webhook on the GitHub repository that triggers
   on new version tags.

## Publishing a Release

Once the one-time setup is complete, publishing a new version is:

### 1. Merge all milestone PRs

Ensure all pull requests targeting the milestone branch are merged and the
milestone branch has been merged into `main`.

### 2. Verify the build locally (optional)

```sh
go build ./...
go test ./...
```

### 3. Create and push the version tag

```sh
git tag v0.1.0
git push origin v0.1.0
```

Pushing the tag triggers the `.github/workflows/release.yml` workflow which:

1. Builds provider binaries for `linux/darwin/windows` x `amd64/arm64`.
2. Packages each binary into a `.zip` archive.
3. Generates a `SHA256SUMS` file.
4. Signs `SHA256SUMS` with the configured GPG key.
5. Creates a **draft** GitHub Release containing all artifacts.

### 4. Review and publish the GitHub Release

- Open the draft release created by GoReleaser in the GitHub UI.
- Verify the artifacts are present and the checksums look correct.
- Click **Publish release**.

### 5. Terraform Registry publishes automatically

The webhook installed in step 4 of the one-time setup fires when the GitHub
Release is published. The Terraform Registry ingests the release artifacts and
makes the new version available at:

```
registry.terraform.io/providers/monotaur/monotaur
```

## Version Numbering

This provider follows [Semantic Versioning](https://semver.org):

- **Patch** (`v0.1.x`): bug fixes and non-breaking dependency updates
- **Minor** (`v0.x.0`): new resources or data sources, backwards-compatible changes
- **Major** (`vx.0.0`): breaking changes to existing resource schemas
