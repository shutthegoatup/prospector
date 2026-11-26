import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppLayout, NotFound } from "@/components/app/layout";
import { AddPage } from "@/pages/Add";
import { AgentsPage } from "@/pages/Agents";
import { CampaignsPage } from "@/pages/Campaigns";
import { CompaniesPage } from "@/pages/Companies";
import { CompanyPage } from "@/pages/Company";
import { ConversationsPage } from "@/pages/Conversations";
import { EnquiriesPage } from "@/pages/Enquiries";
import { OnboardingPage } from "@/pages/Onboarding";
import { PreparePage } from "@/pages/Prepare";
import { ResearchPage } from "@/pages/Research";
import { SignPage } from "@/pages/Sign";
import { SettingsPage } from "@/pages/Settings";
import { SubscriptionsPage } from "@/pages/Subscriptions";
import { TaskPage } from "@/pages/Task";
import { TasksPage } from "@/pages/Tasks";
import { TodayPage } from "@/pages/Today";
import { addSearch, companiesSearch, enquiriesSearch, parseSearch, pathId, researchSearch, stringifySearch, tasksSearch, todaySearch } from "@/search";

// Each route is its own constant: written inline in addChildren, a route's path would be
// typed as any string and its params and search would lose their types.

const root = createRootRoute({ component: Outlet, notFoundComponent: NotFound });
const org = createRoute({ getParentRoute: () => root, path: "o/$org", component: AppLayout, notFoundComponent: NotFound });

// An id in the path is a number to the page; anything else is no such page.
const idParams = {
  parse: (p: { id: string }) => ({ id: pathId.parse(p.id) }),
  stringify: (p: { id: number }) => ({ id: String(p.id) }),
};

const today = createRoute({ getParentRoute: () => org, path: "today", validateSearch: todaySearch, component: TodayPage });
const research = createRoute({ getParentRoute: () => org, path: "research", validateSearch: researchSearch, component: ResearchPage });
const conversations = createRoute({ getParentRoute: () => org, path: "conversations", component: ConversationsPage });
const enquiries = createRoute({ getParentRoute: () => org, path: "enquiries", validateSearch: enquiriesSearch, component: EnquiriesPage });
const onboarding = createRoute({ getParentRoute: () => org, path: "onboarding", component: OnboardingPage });
const subscriptions = createRoute({ getParentRoute: () => org, path: "subscriptions", component: SubscriptionsPage });
const campaigns = createRoute({ getParentRoute: () => org, path: "campaigns", component: CampaignsPage });
const tasks = createRoute({ getParentRoute: () => org, path: "tasks", validateSearch: tasksSearch, component: TasksPage });
const task = createRoute({ getParentRoute: () => org, path: "tasks/$id", params: idParams, errorComponent: NotFound, component: TaskPage });
const companies = createRoute({ getParentRoute: () => org, path: "companies", validateSearch: companiesSearch, component: CompaniesPage });
const company = createRoute({ getParentRoute: () => org, path: "companies/$id", params: idParams, errorComponent: NotFound, component: CompanyPage });
const settings = createRoute({ getParentRoute: () => org, path: "settings", component: SettingsPage });
const agents = createRoute({ getParentRoute: () => org, path: "agents", component: AgentsPage });
const prepare = createRoute({ getParentRoute: () => org, path: "agreements/$id/prepare", params: idParams, errorComponent: NotFound, component: PreparePage });
const sign = createRoute({ getParentRoute: () => org, path: "agreements/$id/sign", params: idParams, errorComponent: NotFound, component: SignPage });
const add = createRoute({ getParentRoute: () => org, path: "add", validateSearch: addSearch, component: AddPage });

const routeTree = root.addChildren([
  org.addChildren([today, research, conversations, enquiries, onboarding, subscriptions, campaigns, tasks, task, companies, company, prepare, sign, settings, agents, add]),
]);

export const router = createRouter({ routeTree, scrollRestoration: true, parseSearch, stringifySearch });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
