# Releasing

A release is a GitHub release with three files: one tarball per Mac architecture and the Claude Desktop extension (`.mcpb`). Every binary is signed with a Developer ID certificate and notarized by Apple, so macOS opens it without a warning.

Pushing a tag does everything:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The `release` workflow (`.github/workflows/release.yml`) runs the tests, builds, signs and notarizes, then publishes the release. It refuses to publish binaries that are not notarized.

## One-time setup

This needs the Apple Developer account holder, about 15 minutes, and five repository secrets.

### 1. Create a Developer ID Application certificate

An "Apple Development" certificate is not enough: only "Developer ID Application" can be notarized and run on other people's Macs.

1. Open Xcode > Settings > Accounts, select the team, click **Manage Certificates…**
2. Click **+** and choose **Developer ID Application**. Only the account holder can create it.
3. Open Keychain Access > login > My Certificates. Right-click **Developer ID Application: …** > **Export…**, save as `developer-id.p12` and choose a password.

### 2. Create an App Store Connect API key for notarization

1. Go to [App Store Connect](https://appstoreconnect.apple.com) > Users and Access > Integrations > App Store Connect API > **Team Keys**.
2. Click **+**, name it `whatsapp-mcp notarization`, give it the **Developer** role, and generate it.
3. Download `AuthKey_XXXXXXXXXX.p8`. Apple lets you download it only once.
4. Note the **Key ID** shown next to the key and the **Issuer ID** shown above the list.

### 3. Add the repository secrets

Run these from a terminal, in the folder holding the two files. `gh` prompts for the values that are not piped in.

```bash
base64 -i developer-id.p12 | gh secret set MACOS_SIGN_P12_BASE64 --repo abonur/whatsapp-mcp
gh secret set MACOS_SIGN_P12_PASSWORD --repo abonur/whatsapp-mcp
base64 -i AuthKey_XXXXXXXXXX.p8 | gh secret set APPLE_API_KEY_P8_BASE64 --repo abonur/whatsapp-mcp
gh secret set APPLE_API_KEY_ID --repo abonur/whatsapp-mcp
gh secret set APPLE_API_ISSUER_ID --repo abonur/whatsapp-mcp
```

Then delete `developer-id.p12` and keep the `.p8` file somewhere safe, such as a password manager.

| Secret | Value |
|---|---|
| `MACOS_SIGN_P12_BASE64` | The exported certificate and its private key, base64-encoded |
| `MACOS_SIGN_P12_PASSWORD` | The password chosen when exporting |
| `APPLE_API_KEY_P8_BASE64` | The `.p8` API key, base64-encoded |
| `APPLE_API_KEY_ID` | The key ID, 10 characters |
| `APPLE_API_ISSUER_ID` | The issuer ID, a UUID |

## Building a release locally

`scripts/build-release.sh VERSION` writes the same files to `dist/`. Without any environment variables it signs ad hoc, which is enough to test on your own Mac. To sign and notarize like CI:

```bash
export SIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)"
export NOTARY_KEY_PATH=~/keys/AuthKey_XXXXXXXXXX.p8
export NOTARY_KEY_ID=XXXXXXXXXX
export NOTARY_ISSUER_ID=00000000-0000-0000-0000-000000000000
./scripts/build-release.sh 0.1.0
```

`security find-identity -v -p codesigning` lists the identities in your keychain.

## What the script does

1. Builds `darwin/arm64` and `darwin/amd64` with the version baked in.
2. Signs each binary with the hardened runtime and a secure timestamp, which notarization requires.
3. Joins them into a universal binary for the extension and signs it again.
4. Sends all three binaries to Apple's notary service and waits for `Accepted`. Bare executables cannot be stapled, so macOS checks the notarization online the first time the binary runs.
5. Builds the Linux and Windows archives, packs the macOS tarballs and the `.mcpb`, validated by `@anthropic-ai/mcpb`, and writes `checksums.txt`.
6. Adds the installer (`install.sh`) and the Homebrew formula (`whatsapp-mcp.rb`) generated from `checksums.txt`.

## Homebrew

[abonur/homebrew-tap](https://github.com/abonur/homebrew-tap) holds `Formula/whatsapp-mcp.rb`. A workflow in that repository checks every hour for a new release of this one and commits the release's `whatsapp-mcp.rb`, so no token is shared between the two repositories. To publish a formula right away, run that workflow by hand from its Actions tab. GitHub pauses scheduled workflows after 60 days without activity in a repository: if a release does not reach Homebrew, re-enable the workflow there.
