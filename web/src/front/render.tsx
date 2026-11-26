import { StrictMode } from "react";
import { renderToString } from "react-dom/server";
import { FrontPage } from "./FrontPage";

// The server swaps this for its asset version, and stamps the enquiry form's address
// on the mount, as it serves the page.
export const versionMark = "__ASSET_VERSION__";

const description =
  "A prospecting and customer workbench for small teams selling to named accounts: research, deals, agreements, subscriptions and onboarding on one company record. Agents research and draft; you decide what they may send. Free to self-host, or hosted for £5 a user a month.";

export function frontDocument(): string {
  const asset = (path: string) => `/assets/${path}?v=${versionMark}`;
  const page = renderToString(
    <StrictMode>
      <FrontPage version={versionMark} />
    </StrictMode>,
  );
  return `<!DOCTYPE html>
<!-- Built from web/src/front by \`npm run build\` in web/. Don't edit by hand. -->
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<meta name="theme-color" content="#ffc233">
<title>Prospector · A few hundred accounts, one queue a day</title>
<meta name="description" content="${description}">
<link rel="icon" type="image/svg+xml" href="${asset("img/mark.svg")}">
<link rel="stylesheet" href="${asset("css/fonts.css")}">
<link rel="stylesheet" href="${asset("css/landing.css")}">
<script src="${asset("front/front.js")}" defer></script>
</head>
<body class="lp">
<div id="front" data-version="${versionMark}">${page}</div>
</body>
</html>
`;
}
