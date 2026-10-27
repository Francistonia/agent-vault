# Code signing policy

## Current status

Agent Vault submitted an application for free open-source code signing from
[SignPath Foundation](https://signpath.org/) on 2026-10-26. The application form
confirmed submission. The maintainer subsequently reported that the application
was declined because the project does not yet have sufficient community
recognition. No approval or signing certificate has been received. Installer and
portable releases remain unsigned; packaging does not create publisher trust.

The project does not claim sponsorship or signing coverage before approval.
If accepted and activated, the signing attribution will be:
"Free code signing provided by SignPath.io, certificate by SignPath Foundation."

## Responsibilities

- Author and repository maintainer: [Francistonia](https://github.com/Francistonia).
- Reviewer for external contributions: Francistonia.
- Proposed signing approver: Francistonia; activation is subject to SignPath's
  review and approval of this role arrangement.

Before signing is enabled, the maintainer must verify multi-factor authentication
for GitHub and SignPath accounts. Signing requests must receive explicit manual
approval from the designated approver. No signed-release workflow is enabled yet.

## Build provenance and signing scope

The [Windows build workflow](https://github.com/Francistonia/agent-vault/blob/main/.github/workflows/windows-build.yml) checks out the
repository, installs the pinned browser component, builds the Windows executable,
runs Go checks and uploads the unsigned distribution for review. GitHub Actions
dependencies are pinned to commit hashes. This build workflow does not request
signing or publish a release.

The initial Windows build, Go checks and tests completed successfully in
[this GitHub Actions run](https://github.com/Francistonia/agent-vault/actions/runs/40264572986).
The opt-in full browser/UI tests are not enabled in that workflow.

The proposed signing scope covers binaries built from source maintained in this
repository. SignPath Foundation's project signing must not be used to re-sign
upstream agent-browser binaries. Those binaries retain their upstream identity
and license; they are currently unsigned. Main-executable signing alone is not a
claim that Smart App Control will accept all components.

Before the first signed release, the signing configuration must verify product
name and version metadata, restrict artifact selection and bind requests to
trusted automated builds. These signing-specific controls have not yet been
configured, because no SignPath organization or project has been provisioned.

## Privacy and supported behavior

The [privacy policy](privacy.md) describes local storage, user-requested network
connections and third-party components. Vault contents, user passwords, private
keys and browser profiles must never be included in build artifacts or signing
requests.

The application assists authentication to user-configured servers, databases and
websites. It does not provide vulnerability scanning, exploitation or a facility
to bypass Windows application-control policy. Its protection boundaries are
documented in the [README](../README.md#边界).

## Downloads and verification

Official releases are available at
<https://github.com/Francistonia/agent-vault/releases> and include SHA-256 checksums.
A checksum checks artifact integrity against the published value; it does not
replace publisher signing or guarantee Windows application-control acceptance.
Users should not disable security controls as a routine installation step.
