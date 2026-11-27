import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RawIntlProvider } from "react-intl";
import { catalogue } from "@/catalogs";
import { intl, speak } from "@/i18n";
import { ServedPage } from "@/pages/Served";
import { router } from "@/router";
import { retryQuery } from "@/problem";
import { parseServed } from "@/served";

const mount = document.getElementById("app");
if (mount) {
  // The server has already chosen the language (locale.go) and said it in <html lang>.
  const lang = document.documentElement.lang;
  // A catalogue that can't be fetched leaves the page in the source language, not blank.
  const messages = await catalogue(lang).catch(() => undefined);
  if (messages) speak(lang, messages);
  else document.documentElement.lang = intl.locale;
  const queries = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, retry: retryQuery } } });
  // A page the server decided, such as an error at an app address, isn't routed: the
  // router would draw the app around it and ask the API for the sidebar.
  const served = parseServed(mount.dataset.page);
  createRoot(mount).render(
    <StrictMode>
      <RawIntlProvider value={intl}>
        <QueryClientProvider client={queries}>{served ? <ServedPage page={served} /> : <RouterProvider router={router} />}</QueryClientProvider>
      </RawIntlProvider>
    </StrictMode>,
  );
}
