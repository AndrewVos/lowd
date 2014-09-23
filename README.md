# lowd

## Setup

If you're planning on load testing a server with https then you're going to need to do this:

- Import `ca.pem` to your browser.
- Set the http proxy to `localhost:8090`.

## Recording

- Run `lowd record`.
- Do some stuff in the browser.
- Kill off lowd.

## Testing

`lowd test`
