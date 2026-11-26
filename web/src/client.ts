import createFetchClient from "openapi-fetch";
import createClient from "openapi-react-query";
import type { paths } from "./api";

// Same-origin calls with the session cookie: every page call goes through the spec.
export const fetchClient = createFetchClient<paths>({ baseUrl: "" });
export const $api = createClient(fetchClient);
