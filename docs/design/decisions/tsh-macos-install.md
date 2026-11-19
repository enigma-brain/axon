# axon installs tsh on macOS from Teleport's archive

## Context

On macOS, `axon login` installs `tsh` when it is missing on a teleport
pathway. At the proxy's major version, Teleport publishes for macOS a plain
archive for Apple silicon, one for Intel and one universal, each with a
published checksum, and a full installer package of about 400 MB. It publishes
no installer package for `tsh` alone.

The `tsh` in each archive is a signed and notarized application bundle,
`tsh.app`, with a keychain access group and an embedded provisioning profile.
It runs from inside the bundle. A binary copied out of the bundle is killed by
macOS on launch.

The method was exercised on an Apple silicon Mac. The archive's `tsh.app` was
run from a directory outside `/Applications`, after a logout, and
`tsh login --proxy=<proxy>` against an organisation's proxy completed
through the browser single sign-on and produced a profile valid for the full session length. The
binary copied out of the bundle was killed at once.

Not tested: an account without administrator rights, an Intel Mac, and which
second-factor method the login prompted. The login goes through the browser's
single sign-on, so the second factor observed is the identity provider's, in
the browser, and a second factor that `tsh` itself performs, such as a
security key or Touch ID, was not exercised.

## Rationale

`axon` downloads the archive for the laptop's architecture, verifies its
checksum, and keeps `tsh.app` intact in `~/.axon/tools/`, running the binary
inside the bundle.

- Nothing in the method needs elevated rights, a password, or a system
  directory, so an account without administrator rights needs nothing
  extra, and removing `~/.axon` removes the install.
- It matches the Linux install: an archive, a verified checksum, and a private
  directory.
- The Apple silicon archive is about half the size of the full package.
- The bundle stays whole, which keeps its signature, notarization,
  entitlements and provisioning profile.

## Alternatives considered

- **Run Teleport's full installer package.** It asks for an administrator
  password in the middle of `axon login`, installs Teleport's other tools into
  a system directory, and fails for a user without administrator rights.
- **Copy the `tsh` binary out of the bundle.** macOS kills it on launch.
- **No install on macOS.** Every macOS user would install `tsh` by hand
  before the command whose purpose is that they do not have to.
