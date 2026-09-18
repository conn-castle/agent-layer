import React from "react";
import { Redirect } from "@docusaurus/router";
import useBaseUrl from "@docusaurus/useBaseUrl";

export default function DeepSWEPlannerRedirect() {
  return <Redirect to={useBaseUrl("/deltaselect-tool")} />;
}
