# A direct pathway's login uses the default browser and waits five minutes

## Context

On a direct pathway, `sky api login` opens the server's login page in a
browser and waits for the session. The client has three ways to obtain it,
tried in order: a polling exchange, used when the server's API version is 30
or higher, in which the client polls the server for the result and the page
makes no request to `localhost`; a callback exchange, in which the page sends
the session to a listener on `localhost`, a request that Chromium browsers
deny; and a manual paste of the token. The client polls for 300 seconds
before it falls back from the first to the second.

A server at an API version of 30 or higher completes login through the
polling exchange. On such a server, a login after a logout that left no
session completed this way on November 18, 2026, once in Firefox and once
in Chrome: the client opened the authorize page, the user clicked
"Authorize", and the client logged in.

Not tested: Edge, Brave and Arc, and the client's fallback from the polling
exchange to the callback exchange.

## Rationale

`axon` lets `sky api login` open the page in the user's default browser,
chooses no browser of its own and prints no warning about any browser. It
waits up to five minutes and then says that the login was not completed in
the browser and to run `axon login` again.

- The polling exchange makes no request to `localhost`, so the browser
  denial that the callback exchange meets does not arise on such a server, and
  a login completed in both Firefox and Chrome.
- Five minutes is the client's own polling window, so `axon` never ends a
  login that the client is still waiting on.

## Alternatives considered

- **Prefer Firefox, and warn users of Chromium browsers.** It overrides the
  user's browser for a failure that does not occur on such a server.
- **A three-minute wait.** It ends before the client's own polling window,
  cutting off logins the client would still complete.
