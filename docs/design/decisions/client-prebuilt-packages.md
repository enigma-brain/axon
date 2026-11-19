# A command group's client is installed from pre-built packages only

## Context

A command group whose backend needs a client on the laptop has `axon`
install that client, with its dependencies, from a package index. A package
index offers a dependency as pre-built packages for some platforms, and as
source that the installer compiles on any other.

A dependency publishes builds for the platforms it chooses, and the set
changes between its versions: a dependency with no build for a platform in
its newest version often has one in an earlier version. An installer that
takes the newest version of each dependency and compiles what has no build
needs the dependency's compiler and build tools on the laptop. A user's
laptop has, in the general case, none of them, and `axon` installs nothing
outside its own directory to supply them.

`axon` is released for more than one operating system and CPU.

## Rationale

The installer takes pre-built packages only and never compiles one. For each
dependency it takes the newest version that has a build for the laptop's
platform.

- The install needs no compiler or build tools on the laptop, on any
  platform, so it works on the laptops `axon` is meant for.
- It works on every platform for which a build of each dependency exists,
  with no choice made per platform or per dependency, so nothing names a
  dependency or holds one at a version that later goes stale.
- A platform with no build of something the client needs fails at once, with
  the installer's reason, instead of after a long compile or with a
  compiler's error.

The cost is that on a platform a dependency has stopped building for, the
client gets the dependency's last version with a build there, and a later
client that needs a newer version cannot be installed on that platform.

## Alternatives considered

- **Hold the dependency with no build at its last version with one, on the
  platform affected.** It names one dependency and one version, which has to
  be found and maintained for each such gap, and it fixes only the gap
  already seen.
- **Build from source, with a compiler and build tools installed for it.**
  It installs toolchains outside the client's own needs, adds minutes and a
  new class of failure to the install, and differs by dependency.
- **Stop releasing `axon` for a platform its backend's dependencies do not
  build for.** It ties the platforms `axon` runs on to one backend's
  dependencies, and removes `axon` itself from a platform where it runs.
