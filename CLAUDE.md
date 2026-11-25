# Working in this repository

This repository is public. Keep real prospect data, hostnames and credentials out of code, tests, commits and PRs; fixtures use invented people on `.example` domains.

## Commands that touch shared state

Several sessions may be working on this machine at once. These are the commands that can collide:

- **`prospector serve`** listens on `127.0.0.1:8766`, or `:$PORT` when `PORT` is set. Check the port is free first, pass an explicit address (`prospector serve 127.0.0.1:<port>`), and stop the server when done.
- **`DATABASE_URL`** defaults to `prospector.sqlite` in the current directory. For a throwaway run, point it at a file outside the checkout.
- **`PROSPECTOR_TEST_PG=postgres://… go test -p 1 ./...` drops every table** in that database before each test. Use it only on a disposable database that nothing else is using, and never run two such test runs against the same one. CI does this run; locally it is rarely needed.

`go test ./...` without that variable gives each test its own SQLite file in a temporary directory, so it is safe to run alongside other sessions.

## Before a change is done

- After editing a `*.templ` file run `go tool templ generate` and commit the regenerated `*_templ.go`. CI fails if they differ.
- The API is spec-first. Change `api/openapi.yaml`, run `go generate ./api`, and commit the regenerated `api/api.gen.go`; CI fails if they differ. A page or form does what it does through the API, never through a route of its own, and each operation names the key scope it needs.
- `assets/css/output.css` is a committed Tailwind build. A utility class that isn't already in it renders unstyled until the CSS is rebuilt (`node build-css.mjs <node_modules with tailwindcss v4>`), so prefer classes already in use.
- Pages are served under a Content-Security-Policy that allows only the app's own scripts: no inline `<script>`, no `on…=` attributes. `TestContentPolicy` checks every page.
- A behaviour change ships with a test in the top-level `*_test.go` files.
- A new table that holds an organisation's data, or points at a contact, has to be carried by the exports, erasure and restore. `tables_test.go` fails until it is, or until it says why not.
- A column added to a model reaches existing databases through auto-migration on start-up. Give it a not-null default and test it against a table that already has rows.

## Merging is releasing

`main` only takes pull requests, and the `test` and `vulncheck` checks must pass. Every green push to `main` is tagged as the next patch version and published as a release, which `deploy.yml` then installs on the host its `DEPLOY_*` secrets name. Treat a merge as a deploy. To go back, run the `deploy` workflow by hand with an earlier version.
