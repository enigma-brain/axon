---
title: Direct
description: A direct pathway reaches the API server at its own address, and its login is the server's own, in a browser.
---

A direct pathway reaches your organisation's API server at its own address.
The server may answer only from your organisation's network. Its login is
the API server's own, in a browser.

## Logging in

1. **Connect to the network your organisation names first.** When the server
   does not answer, `axon login` says `Can't reach <host>.`, followed by your
   organisation's hint when its file gives one, and stops without opening a
   browser.
2. **The sign-in opens in your default browser.** Sign in with your
   organisation's account and click "Authorize". `axon` waits for the page to
   complete, and prints `devenv: logging in to <address> in your browser`.
   When your session is still good, no browser opens.
3. **The wait ends after five minutes.** If the login is not completed in the
   browser by then, `axon` stops and asks you to run `axon login` again.

What else the login does on this kind of pathway, such as clearing a stale
session, is on the [backend's page](/axon/devenv/skypilot/#on-a-direct-pathway).

## When something fails

The messages are under
[Signing in on a direct pathway](/axon/troubleshooting/#signing-in-on-a-direct-pathway)
on the troubleshooting page.
