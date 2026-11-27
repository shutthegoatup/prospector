import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ServedPage } from "@/pages/Served";
import { router } from "@/router";
import { retryQuery } from "@/problem";
import { parseServed } from "@/served";

const mount = document.getElementById("app");
if (mount) {
  const queries = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, retry: retryQuery } } });
  // A page the server decided, such as an error at an app address, isn't routed: the
  // router would draw the app around it and ask the API for the sidebar.
  const served = parseServed(mount.dataset.page);
  createRoot(mount).render(
    <StrictMode>
      <QueryClientProvider client={queries}>{served ? <ServedPage page={served} /> : <RouterProvider router={router} />}</QueryClientProvider>
    </StrictMode>,
  );
}
