# Releasing `rtz`

## One-time setup

These must exist before the first tag, or the release fails at the publish step.

1. **Package-manager repos** — goreleaser writes formulas into repos that must already exist:

   ```bash
   gh repo create runtimez-com/homebrew-tap --public \
     --description "Homebrew formulas for runtimez tools"
   gh repo create runtimez-com/scoop-bucket --public \
     --description "Scoop manifests for runtimez tools"
   ```

2. **A token that can write to them.** The default `GITHUB_TOKEN` in Actions is scoped to
   *this* repo only, so publishing a formula into another repo needs its own credential.
   Create a fine-grained PAT with `contents: write` on both repos and add it here:

   ```bash
   gh secret set TAP_GITHUB_TOKEN --repo runtimez-com/runtimez-cli
   ```

3. **Make this repo public**, so `brew install`, Scoop and `install.sh` work without a token:

   ```bash
   gh repo edit runtimez-com/runtimez-cli --visibility public --accept-visibility-change-consequences
   ```

   Check the history first — going public exposes every commit, not just the current tree.

## Cutting a release

```bash
git tag -a v0.1.0 -m "rtz v0.1.0"
git push origin v0.1.0
```

The tag triggers `.github/workflows/release.yml`, which builds six binaries
(darwin/linux/windows × amd64/arm64), signs the checksums with cosign, and publishes the
GitHub Release, the Homebrew formula, the Scoop manifest, and deb/rpm/apk packages.

Dry-run the whole thing without publishing:

```bash
goreleaser release --snapshot --clean
```

## What a customer runs

```bash
brew install runtimez-com/tap/rtz                  # macOS, Linux
scoop bucket add runtimez https://github.com/runtimez-com/scoop-bucket && scoop install rtz
curl -fsSL https://raw.githubusercontent.com/runtimez-com/runtimez-cli/main/install.sh | sh
```

Then:

```bash
rtz login                    # hosted backend (app.runtimez.io) by default
rtz login --api https://runtimez.internal.example.com   # self-hosted
```

## Verifying an artifact

Checksums are signed keylessly with cosign, so a customer can confirm a download came from
this repo's release workflow and not from a mirror:

```bash
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/runtimez-com/runtimez-cli/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

## Version numbering

The version is stamped from the git tag at build time. `rtz version` on an untagged build
reports `dev`, which is how you tell a local build from a released one.
