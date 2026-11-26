import { StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";
import { FrontPage } from "./FrontPage";

const mount = document.getElementById("front");
if (mount) {
  const { version = "", intake, origin } = mount.dataset;
  hydrateRoot(
    mount,
    <StrictMode>
      <FrontPage version={version} intake={intake} origin={origin} />
    </StrictMode>,
  );
}
