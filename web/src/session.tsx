import { createContext, useContext } from "react";

// What the server says about the page's viewer, from the mount node's data attributes.
export type Session = {
  org: string;
  sales: boolean;
  support: boolean;
};

export const SessionContext = createContext<Session>({ org: "", sales: false, support: false });

export function useSession() {
  return useContext(SessionContext);
}
