import { createContext, useContext } from "react";

// Who is looking and what they may change here, from the nav API; the layout provides it
// once it has been read, so a page never sees it empty.
export type Session = {
  org: string;
  sales: boolean;
  support: boolean;
  owner: boolean;
};

export const SessionContext = createContext<Session>({ org: "", sales: false, support: false, owner: false });

export function useSession() {
  return useContext(SessionContext);
}
