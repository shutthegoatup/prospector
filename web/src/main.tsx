import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router";
import { ResearchPage } from "@/pages/Research";
import { SessionContext } from "@/session";

const mount = document.getElementById("app");
if (mount) {
  const session = { org: mount.dataset.org ?? "", sales: mount.dataset.sales === "true" };
  const queries = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false } } });
  createRoot(mount).render(
    <StrictMode>
      <SessionContext.Provider value={session}>
        <QueryClientProvider client={queries}>
          <BrowserRouter basename={`/o/${session.org}/app`}>
            <Routes>
              <Route path="research" element={<ResearchPage />} />
            </Routes>
          </BrowserRouter>
        </QueryClientProvider>
      </SessionContext.Provider>
    </StrictMode>,
  );
}
