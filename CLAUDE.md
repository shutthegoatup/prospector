# Working in this repository

This repository is public. Keep real prospect data, hostnames and credentials out of code, tests, commits and PRs; fixtures use invented people on `.example` domains.

## Commands that touch shared state

Several sessions may be working on this machine at once. These are the commands that can collide:

- **`prospector serve`** listens on `127.0.0.1:8766`, or `:$PORT` when `PORT` is set. Check the port is free first, pass an explicit address (`prospector serve 127.0.0.1:<port>`), and stop the server when done.
- **`DATABASE_URL`** defaults to `prospector.sqlite` in the current directory. For a throwaway run, point it at a file outside the checkout.
- **`PROSPECTOR_TEST_PG=postgres://… go test -p 1 ./...` drops every table** in that database before each test. Use it only on a disposable database that nothing else is using, and never run two such test runs against the same one. CI does this run; locally it is rarely needed.

`go test ./...` without that variable gives each test its own SQLite file in a temporary directory, so it is safe to run alongside other sessions.

## Before a change is done

- Only the pages outside an organisation's app are templ: sign-in, password reset and choosing a first password, single sign-on, starting a first organisation, error pages (`layout.templ`), the front page (`landing.templ`), a customer's onboarding page (`customer.templ`), unsubscribing (`suppression.templ`) and the `/api` reference (`apidoc.templ`). After editing a `*.templ` file run `go tool templ generate` and commit the regenerated `*_templ.go`. CI fails if they differ. A new page inside an organisation is a React page, never a templ one.
- `web/src/app.ts` is the little script of those templ pages: it posts a `data-form` as JSON. After changing it, or the spec, run `npm ci && npm run build` in `web/` and commit `web/src/api.d.ts`, the built `assets/js/app.js` and `assets/app/`; CI fails if they differ.
- The app inside an organisation is a React front end in `web/`; Go serves its shell and the API. Every page at `/o/{org}/<page>` (and `/o/{org}/companies/{id}`, `/o/{org}/tasks/{id}`) gets the same `assets/app/index.html`, built by Vite from `web/index.html`, under the usual sign-in, policy and headers; the pages are listed in `appPages` in `app_ui.go`, with the agreements prepare page at `/o/{org}/agreements/{id}/prepare`. The old `/o/{org}/app/…` addresses redirect to the real ones. The front end:
  - routes with TanStack Router in `web/src/router.tsx`: one route constant per page (inline in `addChildren` they lose their types), path ids parsed to numbers, and every search param a zod schema in `web/src/search.ts`, with real types (numbers as numbers, enums for filters) and a bad value falling back to the default through `.catch`. Pages read them with `useSearch`/`useParams({ from: "/o/$org/…" })` and link with typed `Link`s; a page the server serves must be routed there too (a test checks);
  - draws the sidebar itself (`web/src/components/app/layout.tsx`) from `GET /o/{org}/api/v1/nav`, which also gives the pages `useSession()`;
  - reads and writes only through the typed client (`$api` in `web/src/client.ts`, openapi-react-query over the spec). Anything it needs that the API lacks goes into `api/openapi.yaml` first;
  - builds from shadcn/ui. `web/src/components/ui/` is exactly what `npx shadcn@latest add <name>` writes (run it in `web/`): never edit those files. Prospector's look goes in wrappers in `web/src/components/app/`;
  - takes its styles from the shared `assets/css/output.css`, so rebuild the CSS after adding classes;
  - is built with `npm run build` in `web/`, which writes `assets/app/index.html` and `assets/app/main.js`: commit them.

  Go tests of what a page shows assert on the API, since the shell is the same at every address.
- The API is spec-first. Change `api/openapi.yaml`, run `go generate ./api`, and commit the regenerated `api/api.gen.go`; CI fails if they differ. A page or form does what it does through the API, never through a route of its own, and each operation names the key scope it needs. Each operation also lists every error status it can answer with, as the shared `components/responses` (no `default`), and every error is a Problem (RFC 9457). A refusal a client acts on without reading `detail` gets a `ProblemType` (`/api/problems/<name>`, with a line in its `x-enum-descriptions`), and is written with `typed{…}` or `writeTyped`. The test binary checks every answer the suite sees against the spec and fails if an operation answered with a status it doesn't declare. The server's only other routes are the signed-out ones (sign-in, reset, `/c/…` and `/u/…`), `POST /password`, and the enquiry intake.
- `assets/css/output.css` is a committed Tailwind build. A utility class that isn't already in it renders unstyled until the CSS is rebuilt (`node build-css.mjs <node_modules with tailwindcss v4>`), so prefer classes already in use.
- Pages are served under a Content-Security-Policy that allows only the app's own scripts: no inline `<script>`, no `on…=` attributes. `TestContentPolicy` checks every page.
- A behaviour change ships with a test in the top-level `*_test.go` files.
- A new table that holds an organisation's data, or points at a contact, has to be carried by the exports, erasure and restore. `tables_test.go` fails until it is, or until it says why not.
- A column added to a model reaches existing databases through auto-migration on start-up. Give it a not-null default and test it against a table that already has rows.

## Merging is releasing

`main` only takes pull requests, and the `test` and `vulncheck` checks must pass. Every green push to `main` is tagged as the next patch version and published as a release, which `deploy.yml` then installs on the host its `DEPLOY_*` secrets name. Treat a merge as a deploy. To go back, run the `deploy` workflow by hand with an earlier version.
