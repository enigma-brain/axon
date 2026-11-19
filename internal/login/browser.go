package login

import "time"

// loginWait is how long login waits for the browser login to complete. It
// matches the window in which sky api login polls the server for the result.
const loginWait = 5 * time.Minute

// notCompletedHint is the next action login gives when the wait ends.
const notCompletedHint = "Run `axon login` again."
