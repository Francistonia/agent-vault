# Privacy policy

Effective date: 2026-10-26

This policy describes the code maintained in the Agent Vault repository at
<https://github.com/Francistonia/agent-vault>.

## Local data

Agent Vault stores connection settings, encrypted credentials and local usage
records in the current Windows user's application-data directory, by default
`%LOCALAPPDATA%\AgentVault`. Browser profiles and website sessions may also be
stored locally by the browsers it starts. Usage records describe operations and
results; they are not intended to contain passwords or private keys.

Vault storage uses Windows DPAPI and encrypted application data. This does not
provide strong isolation against other programs running as the same Windows
user or against an administrator. See the [documented boundaries](../README.md#边界).

## Network connections and authentication

Agent Vault does not implement advertising, analytics, usage telemetry or
automatic crash-report uploads, and does not upload the vault to a service
operated by the project maintainer.

When the user or an authorized agent requests an operation, the local components
connect to the configured SSH server, database or website. Database and website
authentication necessarily provides credentials to the selected authentication
endpoint. SSH authentication provides the public key and a signature, rather
than exporting the private key. Websites may contact other services while being
loaded, and the browser and those websites may store cookies or other data.

Agent Vault aims to keep passwords and private keys out of model context on its
supported authentication paths. This is not a guarantee covering every external
tool, every page output, transformed secrets or malicious local software. Data
returned to an agent, such as query results or page snapshots, is also subject
to that agent provider's own handling and settings.

## Third-party software and distribution services

Agent Vault uses agent-browser and an installed Chrome/Edge browser. Their own
network behavior, settings and privacy policies apply independently. The browser
component is pinned in `runtime/package-lock.json` and its license accompanies
the distribution. Upstream project: <https://github.com/vercel-labs/agent-browser>.
Browser privacy information: [Chrome](https://www.google.com/chrome/privacy/)
and [Microsoft Edge](https://www.microsoft.com/edge/privacy).

Source code, release downloads and automated builds are hosted on GitHub. Those
services process repository and download activity under their own policies.
If code signing is approved by an external service, the signing service receives
build artifacts and build provenance, not users' vault contents or credentials.
Code-signing application and account data are separate from application runtime
data. See the [code signing policy](code-signing.md) for the current status.

## User control and removal

Users can lock the vault, disable SSH client access, disable individual
connections, close browser sessions or remove saved connections. Locking does
not revoke an already established remote session or credentials already given
to a trusted authentication component.

To uninstall an installed copy, close Agent Vault and use Windows Installed Apps.
For a portable copy, close it and delete its extracted program directory.
Users may separately delete the application-data directory and installed Agent
Vault skill directories if they no longer need them. Removing a connection or
the application does not remove an original private-key file, a server-side
public key or remote website sessions.

Questions can be raised through the repository's GitHub issues. Do not include
passwords, private keys, vault files or private connection details in public
issues.
