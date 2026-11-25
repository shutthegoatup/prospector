// Built from web/src/app.ts by `npm run build` in web/. Don't edit by hand.
"use strict";
(() => {
  // node_modules/openapi-fetch/dist/index.mjs
  var PATH_PARAM_RE = /\{[^{}]+\}/g;
  var supportsRequestInitExt = () => {
    return typeof process === "object" && Number.parseInt(process?.versions?.node?.substring(0, 2)) >= 18 && process.versions.undici;
  };
  function randomID() {
    return Math.random().toString(36).slice(2, 11);
  }
  function createClient(clientOptions) {
    let {
      baseUrl = "",
      Request: CustomRequest = globalThis.Request,
      fetch: baseFetch = globalThis.fetch,
      querySerializer: globalQuerySerializer,
      bodySerializer: globalBodySerializer,
      pathSerializer: globalPathSerializer,
      headers: baseHeaders,
      requestInitExt = void 0,
      ...baseOptions
    } = { ...clientOptions };
    requestInitExt = supportsRequestInitExt() ? requestInitExt : void 0;
    baseUrl = removeTrailingSlash(baseUrl);
    const globalMiddlewares = [];
    async function coreFetch(schemaPath, fetchOptions) {
      const {
        baseUrl: localBaseUrl,
        fetch: fetch2 = baseFetch,
        Request = CustomRequest,
        headers,
        params = {},
        parseAs = "json",
        querySerializer: requestQuerySerializer,
        bodySerializer = globalBodySerializer ?? defaultBodySerializer,
        pathSerializer: requestPathSerializer,
        body,
        middleware: requestMiddlewares = [],
        ...init
      } = fetchOptions || {};
      let finalBaseUrl = baseUrl;
      if (localBaseUrl) {
        finalBaseUrl = removeTrailingSlash(localBaseUrl) ?? baseUrl;
      }
      let querySerializer = typeof globalQuerySerializer === "function" ? globalQuerySerializer : createQuerySerializer(globalQuerySerializer);
      if (requestQuerySerializer) {
        querySerializer = typeof requestQuerySerializer === "function" ? requestQuerySerializer : createQuerySerializer({
          ...typeof globalQuerySerializer === "object" ? globalQuerySerializer : {},
          ...requestQuerySerializer
        });
      }
      const pathSerializer = requestPathSerializer || globalPathSerializer || defaultPathSerializer;
      const serializedBody = body === void 0 ? void 0 : bodySerializer(
        body,
        // Note: we declare mergeHeaders() both here and below because it’s a bit of a chicken-or-egg situation:
        // bodySerializer() needs all headers so we aren’t dropping ones set by the user, however,
        // the result of this ALSO sets the lowest-priority content-type header. So we re-merge below,
        // setting the content-type at the very beginning to be overwritten.
        // Lastly, based on the way headers work, it’s not a simple “present-or-not” check becauase null intentionally un-sets headers.
        mergeHeaders(baseHeaders, headers, params.header)
      );
      const finalHeaders = mergeHeaders(
        // with no body, we should not to set Content-Type
        serializedBody === void 0 || // if serialized body is FormData; browser will correctly set Content-Type & boundary expression
        serializedBody instanceof FormData ? {} : {
          "Content-Type": "application/json"
        },
        baseHeaders,
        headers,
        params.header
      );
      const finalMiddlewares = [...globalMiddlewares, ...requestMiddlewares];
      const requestInit = {
        redirect: "follow",
        ...baseOptions,
        ...init,
        body: serializedBody,
        headers: finalHeaders
      };
      let id;
      let options;
      let request = new Request(
        createFinalURL(schemaPath, { baseUrl: finalBaseUrl, params, querySerializer, pathSerializer }),
        requestInit
      );
      let response;
      for (const key in init) {
        if (!(key in request)) {
          request[key] = init[key];
        }
      }
      if (finalMiddlewares.length) {
        id = randomID();
        options = Object.freeze({
          baseUrl: finalBaseUrl,
          fetch: fetch2,
          parseAs,
          querySerializer,
          bodySerializer,
          pathSerializer
        });
        for (const m of finalMiddlewares) {
          if (m && typeof m === "object" && typeof m.onRequest === "function") {
            const result = await m.onRequest({
              request,
              schemaPath,
              params,
              options,
              id
            });
            if (result) {
              if (result instanceof Request) {
                request = result;
              } else if (result instanceof Response) {
                response = result;
                break;
              } else {
                throw new Error("onRequest: must return new Request() or Response() when modifying the request");
              }
            }
          }
        }
      }
      if (!response) {
        try {
          response = await fetch2(request, requestInitExt);
        } catch (error2) {
          let errorAfterMiddleware = error2;
          if (finalMiddlewares.length) {
            for (let i = finalMiddlewares.length - 1; i >= 0; i--) {
              const m = finalMiddlewares[i];
              if (m && typeof m === "object" && typeof m.onError === "function") {
                const result = await m.onError({
                  request,
                  error: errorAfterMiddleware,
                  schemaPath,
                  params,
                  options,
                  id
                });
                if (result) {
                  if (result instanceof Response) {
                    errorAfterMiddleware = void 0;
                    response = result;
                    break;
                  }
                  if (result instanceof Error) {
                    errorAfterMiddleware = result;
                    continue;
                  }
                  throw new Error("onError: must return new Response() or instance of Error");
                }
              }
            }
          }
          if (errorAfterMiddleware) {
            throw errorAfterMiddleware;
          }
        }
        if (finalMiddlewares.length) {
          for (let i = finalMiddlewares.length - 1; i >= 0; i--) {
            const m = finalMiddlewares[i];
            if (m && typeof m === "object" && typeof m.onResponse === "function") {
              const result = await m.onResponse({
                request,
                response,
                schemaPath,
                params,
                options,
                id
              });
              if (result) {
                if (!(result instanceof Response)) {
                  throw new Error("onResponse: must return new Response() when modifying the response");
                }
                response = result;
              }
            }
          }
        }
      }
      const contentLength = response.headers.get("Content-Length");
      if (response.status === 204 || request.method === "HEAD" || contentLength === "0" && !response.headers.get("Transfer-Encoding")?.includes("chunked")) {
        return response.ok ? { data: void 0, response } : { error: void 0, response };
      }
      if (response.ok) {
        const getResponseData = async () => {
          if (parseAs === "stream") {
            return response.body;
          }
          if (parseAs === "json" && !contentLength) {
            const raw = await response.text();
            return raw ? JSON.parse(raw) : void 0;
          }
          return await response[parseAs]();
        };
        return { data: await getResponseData(), response };
      }
      let error = await response.text();
      try {
        error = JSON.parse(error);
      } catch {
      }
      return { error, response };
    }
    return {
      request(method, url2, init) {
        return coreFetch(url2, { ...init, method: method.toUpperCase() });
      },
      /** Call a GET endpoint */
      GET(url2, init) {
        return coreFetch(url2, { ...init, method: "GET" });
      },
      /** Call a PUT endpoint */
      PUT(url2, init) {
        return coreFetch(url2, { ...init, method: "PUT" });
      },
      /** Call a POST endpoint */
      POST(url2, init) {
        return coreFetch(url2, { ...init, method: "POST" });
      },
      /** Call a DELETE endpoint */
      DELETE(url2, init) {
        return coreFetch(url2, { ...init, method: "DELETE" });
      },
      /** Call a OPTIONS endpoint */
      OPTIONS(url2, init) {
        return coreFetch(url2, { ...init, method: "OPTIONS" });
      },
      /** Call a HEAD endpoint */
      HEAD(url2, init) {
        return coreFetch(url2, { ...init, method: "HEAD" });
      },
      /** Call a PATCH endpoint */
      PATCH(url2, init) {
        return coreFetch(url2, { ...init, method: "PATCH" });
      },
      /** Call a TRACE endpoint */
      TRACE(url2, init) {
        return coreFetch(url2, { ...init, method: "TRACE" });
      },
      /** Register middleware */
      use(...middleware) {
        for (const m of middleware) {
          if (!m) {
            continue;
          }
          if (typeof m !== "object" || !("onRequest" in m || "onResponse" in m || "onError" in m)) {
            throw new Error("Middleware must be an object with one of `onRequest()`, `onResponse() or `onError()`");
          }
          globalMiddlewares.push(m);
        }
      },
      /** Unregister middleware */
      eject(...middleware) {
        for (const m of middleware) {
          const i = globalMiddlewares.indexOf(m);
          if (i !== -1) {
            globalMiddlewares.splice(i, 1);
          }
        }
      }
    };
  }
  function serializePrimitiveParam(name, value, options) {
    if (value === void 0 || value === null) {
      return "";
    }
    if (typeof value === "object") {
      throw new Error(
        "Deeply-nested arrays/objects aren\u2019t supported. Provide your own `querySerializer()` to handle these."
      );
    }
    return `${name}=${options?.allowReserved === true ? value : encodeURIComponent(value)}`;
  }
  function serializeObjectParam(name, value, options) {
    if (!value || typeof value !== "object") {
      return "";
    }
    const values = [];
    const joiner = {
      simple: ",",
      label: ".",
      matrix: ";"
    }[options.style] || "&";
    if (options.style !== "deepObject" && options.explode === false) {
      for (const k in value) {
        values.push(k, options.allowReserved === true ? value[k] : encodeURIComponent(value[k]));
      }
      const final2 = values.join(",");
      switch (options.style) {
        case "form": {
          return `${name}=${final2}`;
        }
        case "label": {
          return `.${final2}`;
        }
        case "matrix": {
          return `;${name}=${final2}`;
        }
        default: {
          return final2;
        }
      }
    }
    for (const k in value) {
      const finalName = options.style === "deepObject" ? `${name}[${k}]` : k;
      values.push(serializePrimitiveParam(finalName, value[k], options));
    }
    const final = values.join(joiner);
    return options.style === "label" || options.style === "matrix" ? `${joiner}${final}` : final;
  }
  function serializeArrayParam(name, value, options) {
    if (!Array.isArray(value)) {
      return "";
    }
    if (options.explode === false) {
      const joiner2 = { form: ",", spaceDelimited: "%20", pipeDelimited: "|" }[options.style] || ",";
      const final = (options.allowReserved === true ? value : value.map((v) => encodeURIComponent(v))).join(joiner2);
      switch (options.style) {
        case "simple": {
          return final;
        }
        case "label": {
          return `.${final}`;
        }
        case "matrix": {
          return `;${name}=${final}`;
        }
        // case "spaceDelimited":
        // case "pipeDelimited":
        default: {
          return `${name}=${final}`;
        }
      }
    }
    const joiner = { simple: ",", label: ".", matrix: ";" }[options.style] || "&";
    const values = [];
    for (const v of value) {
      if (options.style === "simple" || options.style === "label") {
        values.push(options.allowReserved === true ? v : encodeURIComponent(v));
      } else {
        values.push(serializePrimitiveParam(name, v, options));
      }
    }
    return options.style === "label" || options.style === "matrix" ? `${joiner}${values.join(joiner)}` : values.join(joiner);
  }
  function createQuerySerializer(options) {
    return function querySerializer(queryParams) {
      const search = [];
      if (queryParams && typeof queryParams === "object") {
        for (const name in queryParams) {
          const value = queryParams[name];
          if (value === void 0 || value === null) {
            continue;
          }
          if (Array.isArray(value)) {
            if (value.length === 0) {
              continue;
            }
            search.push(
              serializeArrayParam(name, value, {
                style: "form",
                explode: true,
                ...options?.array,
                allowReserved: options?.allowReserved || false
              })
            );
            continue;
          }
          if (typeof value === "object") {
            search.push(
              serializeObjectParam(name, value, {
                style: "deepObject",
                explode: true,
                ...options?.object,
                allowReserved: options?.allowReserved || false
              })
            );
            continue;
          }
          search.push(serializePrimitiveParam(name, value, options));
        }
      }
      return search.join("&");
    };
  }
  function defaultPathSerializer(pathname, pathParams) {
    let nextURL = pathname;
    for (const match of pathname.match(PATH_PARAM_RE) ?? []) {
      let name = match.substring(1, match.length - 1);
      let explode = false;
      let style = "simple";
      if (name.endsWith("*")) {
        explode = true;
        name = name.substring(0, name.length - 1);
      }
      if (name.startsWith(".")) {
        style = "label";
        name = name.substring(1);
      } else if (name.startsWith(";")) {
        style = "matrix";
        name = name.substring(1);
      }
      if (!pathParams || pathParams[name] === void 0 || pathParams[name] === null) {
        continue;
      }
      const value = pathParams[name];
      if (Array.isArray(value)) {
        nextURL = nextURL.replace(match, serializeArrayParam(name, value, { style, explode }));
        continue;
      }
      if (typeof value === "object") {
        nextURL = nextURL.replace(match, serializeObjectParam(name, value, { style, explode }));
        continue;
      }
      if (style === "matrix") {
        nextURL = nextURL.replace(match, `;${serializePrimitiveParam(name, value)}`);
        continue;
      }
      nextURL = nextURL.replace(match, style === "label" ? `.${encodeURIComponent(value)}` : encodeURIComponent(value));
    }
    return nextURL;
  }
  function defaultBodySerializer(body, headers) {
    if (body instanceof FormData) {
      return body;
    }
    if (headers) {
      const contentType = headers.get instanceof Function ? headers.get("Content-Type") ?? headers.get("content-type") : headers["Content-Type"] ?? headers["content-type"];
      if (contentType === "application/x-www-form-urlencoded") {
        return new URLSearchParams(body).toString();
      }
    }
    return JSON.stringify(body);
  }
  function createFinalURL(pathname, options) {
    let finalURL = `${options.baseUrl}${pathname}`;
    if (options.params?.path) {
      finalURL = options.pathSerializer(finalURL, options.params.path);
    }
    let search = options.querySerializer(options.params.query ?? {});
    if (search.startsWith("?")) {
      search = search.substring(1);
    }
    if (search) {
      finalURL += `?${search}`;
    }
    return finalURL;
  }
  function mergeHeaders(...allHeaders) {
    const finalHeaders = new Headers();
    for (const h of allHeaders) {
      if (!h || typeof h !== "object") {
        continue;
      }
      const iterator = h instanceof Headers ? h.entries() : Object.entries(h);
      for (const [k, v] of iterator) {
        if (v === null) {
          finalHeaders.delete(k);
        } else if (Array.isArray(v)) {
          for (const v2 of v) {
            finalHeaders.append(k, v2);
          }
        } else if (v !== void 0) {
          finalHeaders.set(k, v);
        }
      }
    }
    return finalHeaders;
  }
  function removeTrailingSlash(url2) {
    if (url2.endsWith("/")) {
      return url2.substring(0, url2.length - 1);
    }
    return url2;
  }

  // src/problem.ts
  function problemText(body, statusText, status) {
    if (typeof body === "string" && body.trim() !== "") return body.trim();
    if (body && typeof body === "object" && "detail" in body && typeof body.detail === "string" && body.detail !== "") return body.detail;
    return statusText || `The server answered ${status}.`;
  }

  // src/app.ts
  var base = document.body.dataset.base || "/";
  var content = document.getElementById("content");
  var org = base.match(/^\/o\/([^/]+)\//)?.[1] ?? "";
  var api = createClient({ baseUrl: location.origin });
  function url(path) {
    return path.startsWith("/") ? path : base + path;
  }
  function signIn() {
    location.href = "/login?next=" + encodeURIComponent(location.pathname + location.search);
  }
  async function send(path, data, method = "POST") {
    const init = { method, headers: { "content-type": "application/json" } };
    if (method !== "DELETE") init.body = JSON.stringify(data);
    const res = await fetch(url(path), init);
    if (res.status === 401) signIn();
    const type = res.headers.get("content-type") || "";
    if (!res.ok) throw new Error(type.includes("problem+json") ? (await res.json()).detail || res.statusText : (await res.text()).trim());
    return type.includes("json") ? await res.json() : {};
  }
  function answered(r) {
    if (r.response.status === 401) signIn();
    if (r.error !== void 0 || !r.response.ok) throw new Error(problemText(r.error, r.response.statusText, r.response.status));
    return r.data;
  }
  function save(data, name) {
    const a = document.createElement("a");
    a.href = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: "application/json" }));
    a.download = name;
    a.click();
    URL.revokeObjectURL(a.href);
  }
  function report(err) {
    alert(err instanceof Error ? err.message : String(err));
  }
  async function refresh(focus) {
    if (!content) return;
    const res = await fetch(location.href, { headers: { "X-Fragment": "1" } });
    if (!res.ok) return;
    const doc = new DOMParser().parseFromString(await res.text(), "text/html");
    content.classList.add("settled");
    content.replaceChildren(...doc.body.childNodes);
    const card = focus && content.querySelector(`[data-card="${focus}"]`);
    if (card) {
      card.classList.add("ring-2", "ring-primary");
      card.scrollIntoView({ block: "start", behavior: "smooth" });
    }
  }
  function writeClipboard(text) {
    if (navigator.clipboard && window.isSecureContext) {
      navigator.clipboard.writeText(text).catch(() => {
      });
      return;
    }
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.append(area);
    area.select();
    document.execCommand("copy");
    area.remove();
  }
  function copy(button, text, link) {
    writeClipboard(text);
    if (/^https?:\/\//.test(link || "")) window.open(link, "linkedin");
    button.textContent = "Copied \u2713";
  }
  document.addEventListener("click", async (e) => {
    const target = e.target;
    if (target.closest("[data-bookmarklet]")) return void e.preventDefault();
    const b = target.closest("button");
    if (!b) return;
    const d = b.dataset;
    try {
      if (d.mark) {
        await send("ui/mark", { id: +(d.id || 0), step: d.mark });
        refresh(d.next !== "0" && d.next);
      } else if ("copy" in d) {
        copy(b, d.copy || "", d.open);
      } else if (d.copyFrom) {
        const from = document.getElementById(d.copyFrom);
        copy(b, from?.value || "", d.open);
      } else if (d.download) {
        save(await send(d.download, JSON.parse(d.body || "{}")), d.file || "download.json");
        refresh();
      } else if (d.post) {
        if (d.confirm && !confirm(d.confirm)) return;
        const body = JSON.parse(d.body || "{}");
        if (d.withFit) body.fit = document.getElementById(d.withFit)?.value || "";
        const res = await send(d.post, body, d.method);
        if (typeof res.go === "string") return void (location.href = res.go);
        refresh();
      }
    } catch (err) {
      report(err);
    }
  });
  document.addEventListener("input", (e) => {
    const input = e.target;
    if (input.name !== "linkedin" || !input.form) return;
    const name = input.form.elements.namedItem("name");
    const slug = (input.value.match(/linkedin\.com\/in\/([^/?#]+)/) || [])[1];
    if (!name || name.value || !slug) return;
    const words = decodeURIComponent(slug).split("-").filter((w) => w && !/\d/.test(w));
    name.value = words.map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
  });
  var subscriptionFields = ["name", "amount", "per", "start", "term_months", "then", "notice_days", "notice_on", "ended_on"];
  var dealFields = ["kind", "amount", "close_date", "owner", "stage", "next_step", "next_date", "lost_reason"];
  var enquiryFields = ["state", "owner", "note"];
  var campaignFields = ["name", "audience", "subject", "body"];
  function oneOf(list, f, what) {
    if (!list.includes(f)) throw new Error(`${what} has no ${f}.`);
    return f;
  }
  document.addEventListener("change", async (e) => {
    const input = e.target.closest("input[type=file][data-upload]");
    const file = input?.files?.[0];
    if (!input || !file) return;
    try {
      answered(
        await api.PUT("/o/{org}/api/v1/agreements/{id}/document", {
          params: { path: { org, id: +(input.dataset.upload || 0) }, query: { name: file.name } },
          // The spec's binary body is typed as a string; the File is sent as it is.
          body: file,
          bodySerializer: (b) => b,
          headers: { "content-type": "application/pdf" }
        })
      );
      refresh();
    } catch (err) {
      input.value = "";
      report(err);
    }
  });
  document.addEventListener("change", async (e) => {
    const f = e.target.closest("[data-field]");
    if (!f) return;
    const field = f.dataset.field || "";
    try {
      if (f.dataset.deal) {
        const change = { [oneOf(dealFields, field, "A deal")]: f.value };
        answered(await api.PATCH("/o/{org}/api/v1/deals/{id}", { params: { path: { org, id: +f.dataset.deal } }, body: change }));
      } else if (f.dataset.sub) {
        const key = oneOf(subscriptionFields, field, "A subscription");
        const number = f.type === "number" || !!f.dataset.number;
        if (number && f.value.trim() === "") throw new Error("Enter a number, or 0 for none.");
        const change = { [key]: number ? +f.value : f.value };
        answered(await api.PATCH("/o/{org}/api/v1/subscriptions/{id}", { params: { path: { org, id: +f.dataset.sub } }, body: change }));
      } else if (f.dataset.enquiry) {
        const change = { [oneOf(enquiryFields, field, "An enquiry")]: f.value };
        answered(await api.PATCH("/o/{org}/api/v1/enquiries/{id}", { params: { path: { org, id: +f.dataset.enquiry } }, body: change }));
      } else if (f.dataset.campaign) {
        const change = { [oneOf(campaignFields, field, "A campaign")]: f.value };
        answered(await api.PATCH("/o/{org}/api/v1/campaigns/{id}", { params: { path: { org, id: +f.dataset.campaign } }, body: change }));
      } else if (f.dataset.step) {
        if (field !== "due") throw new Error(`A step's ${field} is changed another way.`);
        answered(await api.PATCH("/o/{org}/api/v1/onboarding-steps/{id}", { params: { path: { org, id: +f.dataset.step } }, body: { due: f.value } }));
      } else await send("ui/update", { id: +(f.dataset.id || 0), field, value: f.value });
      if (f.dataset.refresh || field === "phone") refresh();
    } catch (err) {
      report(err);
    }
  });
  function formJSON(form) {
    const out = {};
    for (const el of Array.from(form.elements)) {
      if (!el.name || el.disabled) continue;
      if (el instanceof HTMLInputElement && el.type === "checkbox" && el.dataset.multi) {
        const list = out[el.name] || [];
        if (el.checked) list.push(el.value);
        out[el.name] = list;
      } else if (el instanceof HTMLInputElement && el.type === "checkbox") {
        out[el.name] = el.checked;
      } else if (el.type === "number" || el.dataset.number) {
        out[el.name] = el.value === "" ? 0 : +el.value;
      } else {
        out[el.name] = el.value;
      }
    }
    return out;
  }
  document.addEventListener("submit", async (e) => {
    const form = e.target.closest("form[data-form]");
    if (!form) return;
    e.preventDefault();
    const result = form.querySelector("[data-result]");
    const button = form.querySelector("[type=submit]");
    if (button) button.disabled = true;
    try {
      const res = await send(form.dataset.form || "", formJSON(form), form.dataset.method);
      if (typeof res.go === "string") return void (location.href = res.go);
      if (form.dataset.go && (typeof res.id === "number" || typeof res.id === "string")) return void (location.href = url(form.dataset.go.replace("{id}", String(res.id))));
      if (result) result.textContent = typeof res.message === "string" ? res.message : "Saved \u2713";
      if (form.dataset.reset) {
        for (const el of Array.from(form.elements)) {
          if (el.type !== "checkbox" && el.type !== "submit" && el.tagName !== "SELECT" && !el.hidden && !el.closest("fieldset[data-keep]")) el.value = "";
        }
        form.querySelector("input:not([type=hidden])")?.focus();
      }
      if (form.dataset.close) setTimeout(() => window.close(), 1200);
      if (form.dataset.refresh) setTimeout(() => refresh(), 600);
    } catch (err) {
      if (result) {
        result.textContent = err instanceof Error ? err.message : String(err);
        result.style.color = "var(--stop)";
        setTimeout(() => result.style.color = "", 4e3);
      } else report(err);
    } finally {
      if (button) button.disabled = false;
    }
  });
  var typing = () => /INPUT|TEXTAREA|SELECT/.test(document.activeElement?.tagName || "");
  document.addEventListener("keydown", (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey || typing()) return;
    const first = content?.querySelector("[data-card]");
    const button = first?.querySelector(`[data-key="${e.key}"]`);
    if (button) {
      e.preventDefault();
      button.click();
    }
  });
  setInterval(() => {
    if (!typing() && !document.querySelector("details[open] form")) refresh();
  }, 6e4);
})();
