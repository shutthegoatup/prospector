import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet } from "@tanstack/react-router";
import { AppLayout, NotFound } from "@/components/app/layout";
import {
  addSearch,
  companiesSearch,
  enquiriesSearch,
  parseSearch,
  pathId,
  researchSearch,
  signInSearch,
  singleSignOnSearch,
  stringifySearch,
  tasksSearch,
  todaySearch,
} from "@/search";

// Each page is its own chunk, fetched when it is first opened or a link to it is hovered,
// so a visit loads the shell and the page it is on rather than every page.
const pages = {
  Add: () => import("@/pages/Add"),
  Agents: () => import("@/pages/Agents"),
  Campaigns: () => import("@/pages/Campaigns"),
  Companies: () => import("@/pages/Companies"),
  Company: () => import("@/pages/Company"),
  Conversations: () => import("@/pages/Conversations"),
  Enquiries: () => import("@/pages/Enquiries"),
  Onboarding: () => import("@/pages/Onboarding"),
  Prepare: () => import("@/pages/Prepare"),
  Research: () => import("@/pages/Research"),
  Settings: () => import("@/pages/Settings"),
  Sign: () => import("@/pages/Sign"),
  SignIn: () => import("@/pages/SignIn"),
  Subscriptions: () => import("@/pages/Subscriptions"),
  Task: () => import("@/pages/Task"),
  Tasks: () => import("@/pages/Tasks"),
  Today: () => import("@/pages/Today"),
};

function page<F extends keyof typeof pages>(file: F, name: keyof Awaited<ReturnType<(typeof pages)[F]>> & string) {
  return lazyRouteComponent(pages[file] as () => Promise<Record<string, () => React.JSX.Element>>, name);
}

// Each route is its own constant: written inline in addChildren, a route's path would be
// typed as any string and its params and search would lose their types.

const root = createRootRoute({ component: Outlet, notFoundComponent: NotFound });
const org = createRoute({ getParentRoute: () => root, path: "o/$org", component: AppLayout, notFoundComponent: NotFound });

// An id in the path is a number to the page; anything else is no such page.
const idParams = {
  parse: (p: { id: string }) => ({ id: pathId.parse(p.id) }),
  stringify: (p: { id: number }) => ({ id: String(p.id) }),
};

const today = createRoute({ getParentRoute: () => org, path: "today", validateSearch: todaySearch, component: page("Today", "TodayPage") });
const research = createRoute({ getParentRoute: () => org, path: "research", validateSearch: researchSearch, component: page("Research", "ResearchPage") });
const conversations = createRoute({ getParentRoute: () => org, path: "conversations", component: page("Conversations", "ConversationsPage") });
const enquiries = createRoute({ getParentRoute: () => org, path: "enquiries", validateSearch: enquiriesSearch, component: page("Enquiries", "EnquiriesPage") });
const onboarding = createRoute({ getParentRoute: () => org, path: "onboarding", component: page("Onboarding", "OnboardingPage") });
const subscriptions = createRoute({ getParentRoute: () => org, path: "subscriptions", component: page("Subscriptions", "SubscriptionsPage") });
const campaigns = createRoute({ getParentRoute: () => org, path: "campaigns", component: page("Campaigns", "CampaignsPage") });
const tasks = createRoute({ getParentRoute: () => org, path: "tasks", validateSearch: tasksSearch, component: page("Tasks", "TasksPage") });
const task = createRoute({ getParentRoute: () => org, path: "tasks/$id", params: idParams, errorComponent: NotFound, component: page("Task", "TaskPage") });
const companies = createRoute({ getParentRoute: () => org, path: "companies", validateSearch: companiesSearch, component: page("Companies", "CompaniesPage") });
const company = createRoute({
  getParentRoute: () => org,
  path: "companies/$id",
  params: idParams,
  errorComponent: NotFound,
  component: page("Company", "CompanyPage"),
});
const settings = createRoute({ getParentRoute: () => org, path: "settings", component: page("Settings", "SettingsPage") });
const agents = createRoute({ getParentRoute: () => org, path: "agents", component: page("Agents", "AgentsPage") });
const prepare = createRoute({
  getParentRoute: () => org,
  path: "agreements/$id/prepare",
  params: idParams,
  errorComponent: NotFound,
  component: page("Prepare", "PreparePage"),
});
const sign = createRoute({
  getParentRoute: () => org,
  path: "agreements/$id/sign",
  params: idParams,
  errorComponent: NotFound,
  component: page("Sign", "SignPage"),
});
const add = createRoute({ getParentRoute: () => org, path: "add", validateSearch: addSearch, component: page("Add", "AddPage") });

// Signing in, outside any organisation and its sidebar.
const signIn = createRoute({ getParentRoute: () => root, path: "login", validateSearch: signInSearch, component: page("SignIn", "SignInPage") });
const singleSignOn = createRoute({
  getParentRoute: () => root,
  path: "login/sso",
  validateSearch: singleSignOnSearch,
  component: page("SignIn", "SingleSignOnPage"),
});
const forgot = createRoute({ getParentRoute: () => root, path: "forgot", component: page("SignIn", "ForgotPage") });
const reset = createRoute({ getParentRoute: () => root, path: "reset/$token", component: page("SignIn", "ResetPage") });
const choosePassword = createRoute({ getParentRoute: () => root, path: "password", component: page("SignIn", "ChoosePasswordPage") });

const routeTree = root.addChildren([
  signIn,
  singleSignOn,
  forgot,
  reset,
  choosePassword,
  org.addChildren([
    today,
    research,
    conversations,
    enquiries,
    onboarding,
    subscriptions,
    campaigns,
    tasks,
    task,
    companies,
    company,
    prepare,
    sign,
    settings,
    agents,
    add,
  ]),
]);

export const router = createRouter({ routeTree, scrollRestoration: true, parseSearch, stringifySearch, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
