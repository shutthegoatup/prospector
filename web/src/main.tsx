import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router";
import { AddPage } from "@/pages/Add";
import { CampaignsPage } from "@/pages/Campaigns";
import { EnquiriesPage } from "@/pages/Enquiries";
import { OnboardingPage } from "@/pages/Onboarding";
import { ResearchPage } from "@/pages/Research";
import { TaskPage } from "@/pages/Task";
import { TasksPage } from "@/pages/Tasks";
import { SubscriptionsPage } from "@/pages/Subscriptions";
import { SessionContext } from "@/session";

const mount = document.getElementById("app");
if (mount) {
  const session = { org: mount.dataset.org ?? "", sales: mount.dataset.sales === "true", support: mount.dataset.support === "true", owner: mount.dataset.owner === "true" };
  const queries = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false } } });
  createRoot(mount).render(
    <StrictMode>
      <SessionContext.Provider value={session}>
        <QueryClientProvider client={queries}>
          <BrowserRouter basename={`/o/${session.org}/app`}>
            <Routes>
              <Route path="research" element={<ResearchPage />} />
              <Route path="campaigns" element={<CampaignsPage />} />
              <Route path="onboarding" element={<OnboardingPage />} />
              <Route path="add" element={<AddPage />} />
              <Route path="tasks" element={<TasksPage />} />
              <Route path="tasks/:id" element={<TaskPage />} />
              <Route path="enquiries" element={<EnquiriesPage />} />
              <Route path="subscriptions" element={<SubscriptionsPage />} />
            </Routes>
          </BrowserRouter>
        </QueryClientProvider>
      </SessionContext.Provider>
    </StrictMode>,
  );
}
