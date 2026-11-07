# Releasing

A release is a GitHub release with an archive per system and architecture (macOS, Linux, Raspberry Pi, Windows), the Claude Desktop extension (`.mcpb`), the installer (`install.sh`), the Homebrew formula and `checksums.txt`. The macOS binaries are signed with a Developer ID certificate and notarized by Apple, so macOS opens them without a warning. The Windows programs are code-signed with Azure Artifact Signing once it is set up ([below](#windows-code-signing)); until then they ship unsigned and SmartScreen may warn.

Write the notes in `release-notes/X.Y.Z.md`, then push a tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The `release` workflow (`.github/workflows/release.yml`) runs the tests, builds, signs and notarizes, then publishes the release with those notes (GitHub's generated notes when the file is missing). It refuses to publish binaries that are not notarized.

## Pre-releases

A tag with a suffix, such as `v0.1.0-rc1`, publishes a pre-release to test the whole chain before the real one: the macOS binaries are signed but not notarized, and the release is marked as a pre-release, so `/releases/latest`, the installer and the Homebrew tap ignore it. Test it with `install.sh --version 0.1.0-rc1`, then delete the pre-release and its tag.

## One-time setup

This needs the Apple Developer account holder, about 15 minutes, and five repository secrets.

### 1. Create a Developer ID Application certificate

An "Apple Development" certificate is not enough: only "Developer ID Application" can be notarized and run on other people's Macs.

1. Open Xcode > Settings > Accounts, select the team, click **Manage Certificates…**
2. Click **+** and choose **Developer ID Application**. Only the account holder can create it.
3. Open Keychain Access > login > My Certificates. Right-click **Developer ID Application: …** > **Export…**, save as `developer-id.p12` and choose a password.

### 2. Create an App Store Connect API key for notarization

1. Go to [App Store Connect](https://appstoreconnect.apple.com) > Users and Access > Integrations > App Store Connect API > **Team Keys**.
2. Click **+**, name it `askmywa notarization`, give it the **Developer** role, and generate it.
3. Download `AuthKey_XXXXXXXXXX.p8`. Apple lets you download it only once.
4. Note the **Key ID** shown next to the key and the **Issuer ID** shown above the list.

### 3. Add the repository secrets

Run these from a terminal, in the folder holding the two files. `gh` prompts for the values that are not piped in.

```bash
base64 -i developer-id.p12 | gh secret set MACOS_SIGN_P12_BASE64 --repo abonur/askmywa
gh secret set MACOS_SIGN_P12_PASSWORD --repo abonur/askmywa
base64 -i AuthKey_XXXXXXXXXX.p8 | gh secret set APPLE_API_KEY_P8_BASE64 --repo abonur/askmywa
gh secret set APPLE_API_KEY_ID --repo abonur/askmywa
gh secret set APPLE_API_ISSUER_ID --repo abonur/askmywa
```

Then delete `developer-id.p12` and keep the `.p8` file somewhere safe, such as a password manager.

| Secret | Value |
|---|---|
| `MACOS_SIGN_P12_BASE64` | The exported certificate and its private key, base64-encoded |
| `MACOS_SIGN_P12_PASSWORD` | The password chosen when exporting |
| `APPLE_API_KEY_P8_BASE64` | The `.p8` API key, base64-encoded |
| `APPLE_API_KEY_ID` | The key ID, 10 characters |
| `APPLE_API_ISSUER_ID` | The issuer ID, a UUID |

## Windows code signing

Optional: without it the release ships unsigned Windows programs, as before. Once the repository variable `AZURE_SIGNING_ACCOUNT` exists, the `windows` job of the release workflow builds `askmywa.exe` for x64 and ARM on a Windows runner, signs both with [Azure Artifact Signing](https://learn.microsoft.com/azure/artifact-signing/quickstart) and checks the signatures. The release job then packs those signed files into the zip archives and the `.mcpb`. If signing fails, nothing is published.

Windows shows the certificate's name as the publisher, so a signed `askmywa.exe` says who made it instead of "Unknown publisher". SmartScreen still learns from downloads: a signed program earns its reputation faster, but the first downloads of a new certificate can still be warned about.

### Before you start

- **Who can sign.** Public certificates go to organizations in the EU, the UK, the US, Canada and a few other countries. Individual developers must be in the US or Canada. From France, sign as the company (RUWAD), which becomes the publisher name Windows shows.
- **A paid Azure subscription.** Pay-as-you-go works; free, trial and sponsored subscriptions are refused. The Basic plan costs about 10 USD a month for 5,000 signatures, far more than a release needs (2 per release).
- **Identity validation** by Microsoft takes from a few hours to several working days. Microsoft may ask for a company registration document (such as a Kbis) less than 12 months old. Use email addresses on the company's domain.

### 1. Create the signing account

In the [Azure portal](https://portal.azure.com):

1. **Subscriptions** > your subscription > **Resource providers**: register `Microsoft.CodeSigning`.
2. Search for **Artifact Signing Accounts** > **Create**: resource group `askmywa`, name `askmywa`, region **West Europe**, plan **Basic**.
3. In the new account, **Access control (IAM)** > **Add role assignment**: give yourself **Artifact Signing Identity Verifier**.

### 2. Validate the company

1. In the account, **Identity validations** > **Organization** > **New identity** > **Public**.
2. Enter the company's legal name and address exactly as registered, and two email addresses on the company's domain.
3. When the status reads **Action Required**, follow the link to verify the representative's ID, then wait for **Completed**.

### 3. Create the certificate profile

In the account, **Certificate profiles** > **Create** > **Public Trust**: name `askmywa`, and choose the validated identity.

### 4. Let GitHub sign, without a password

1. **Microsoft Entra ID** > **App registrations** > **New registration**: name `askmywa-release-signing`, single tenant. Note the **Application (client) ID** and the **Directory (tenant) ID**.
2. In the app, **Certificates & secrets** > **Federated credentials** > **Add credential** > **GitHub Actions deploying Azure resources**: organization `abonur`, repository `askmywa`, entity **Environment**, environment `release`.
3. Back in the signing account, **Access control (IAM)** > **Add role assignment**: give the app `askmywa-release-signing` the **Artifact Signing Certificate Profile Signer** role.

GitHub proves to Azure that the job runs in this repository's `release` environment, so no Azure secret is stored in GitHub. The environment is created by the first run that uses it.

### 5. Add the repository variables

These are identifiers, not secrets. The endpoint matches the account's region (`https://weu.codesigning.azure.net` for West Europe, `https://neu.codesigning.azure.net` for North Europe).

```bash
gh variable set AZURE_CLIENT_ID --repo abonur/askmywa --body "<application (client) ID>"
gh variable set AZURE_TENANT_ID --repo abonur/askmywa --body "<directory (tenant) ID>"
gh variable set AZURE_SIGNING_ENDPOINT --repo abonur/askmywa --body https://weu.codesigning.azure.net
gh variable set AZURE_SIGNING_PROFILE --repo abonur/askmywa --body askmywa
gh variable set AZURE_SIGNING_ACCOUNT --repo abonur/askmywa --body askmywa
```

Set `AZURE_SIGNING_ACCOUNT` last: it is the switch that turns signing on. Set them as repository variables, not environment variables: the workflow reads the switch before the job enters the environment.

Then test with a pre-release tag (see [Pre-releases](#pre-releases)): the `windows` job's log prints the publisher of each signed file. On Windows, right-click `askmywa.exe` > **Properties** > **Digital Signatures** shows the same.

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

The Windows programs are built unsigned unless `WINDOWS_BIN_DIR` points to a folder holding `askmywa-windows-amd64.exe` and `askmywa-windows-arm64.exe`, built for the same version by `scripts/build-windows.sh` and signed elsewhere.

## What the script does

1. Builds `darwin/arm64` and `darwin/amd64` with the version baked in.
2. Wraps each one in `askmywa.app` (`scripts/macos-app.sh`: icon, `LSUIElement`, bundle identifier `com.askmywa.app`) and signs the app with the hardened runtime and a secure timestamp, which notarization requires. The app is what gives the background service its name and logo in System Settings.
3. Joins them into a universal binary, wrapped and signed the same way, for the extension.
4. Sends the three apps to Apple's notary service, waits for `Accepted`, then staples the ticket to each app so macOS can check it offline.
5. Builds the Linux and Windows archives (with the signed Windows programs from `WINDOWS_BIN_DIR` when given), packs the macOS tarballs and the `.mcpb`, validated by `@anthropic-ai/mcpb`, and writes `checksums.txt`.
6. Adds the installer (`install.sh`) and the Homebrew formula (`askmywa.rb`) generated from `checksums.txt`.

## Homebrew

[abonur/homebrew-tap](https://github.com/abonur/homebrew-tap) holds `Formula/askmywa.rb`. A workflow in that repository checks every hour for a new release of this one and commits the release's `askmywa.rb`, so no token is shared between the two repositories. To publish a formula right away, run that workflow by hand from its Actions tab. GitHub pauses scheduled workflows after 60 days without activity in a repository: if a release does not reach Homebrew, re-enable the workflow there.

## Icons

`assets/logo.svg` (flat) and `assets/logo-macos.svg` (with the macOS margin and shadow) are the sources. `scripts/make-icons.sh` regenerates the macOS `.icns`, the extension icon, the site icons and `assets/icon-256.png`; `scripts/make-windows-resources.sh` turns that PNG into the `rsrc_windows_*.syso` files that give `askmywa.exe` its icon. The outputs are committed.
