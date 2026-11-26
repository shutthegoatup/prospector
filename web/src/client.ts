import createFetchClient from "openapi-fetch";
import createClient from "openapi-react-query";
import type { paths } from "./api";

// Same-origin calls with the session cookie: every page call goes through the spec.
// Signed-in POSTs must say they're JSON, bodiless ones included: the server refuses anything
// else from a browser, which is what stops another site posting a form with the cookie.
export const fetchClient = createFetchClient<paths>({ baseUrl: "", headers: { "Content-Type": "application/json" } });
export const $api = createClient(fetchClient);
