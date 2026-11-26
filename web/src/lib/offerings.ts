import type { components } from "@/api";
import { $api } from "@/client";
import { useSession } from "@/session";

export type Offering = components["schemas"]["Offering"];

// What the organisation sells besides its own offering, which is never listed. An
// organisation with none sees no offering bar or "For" picker anywhere.
export function useOfferings(): Offering[] {
  const { org } = useSession();
  return $api.useQuery("get", "/o/{org}/api/v1/offerings", { params: { path: { org }, query: { limit: 200 } } }).data?.items ?? [];
}
