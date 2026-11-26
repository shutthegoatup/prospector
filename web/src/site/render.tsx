import { StrictMode } from "react";
import { renderToString } from "react-dom/server";
import { ApiReference, type Doc } from "./ApiReference";
import doc from "./apidoc.json";
import { Landing } from "./Landing";

// What prerender.mjs writes into the built pages, rendered once when the app is built.
export function landing(): string {
  return renderToString(
    <StrictMode>
      <Landing />
    </StrictMode>,
  );
}

export function apiReference(): string {
  return renderToString(<ApiReference doc={doc as Doc} />);
}
