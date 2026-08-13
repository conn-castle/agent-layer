import React from "react";
import Layout from "@theme/Layout";
import useBaseUrl from "@docusaurus/useBaseUrl";
import { useColorMode } from "@docusaurus/theme-common";

const frameStyle = {
  display: "block",
  width: "100%",
  minHeight: "calc(100vh - 60px)",
  border: 0,
  background: "transparent",
};

/**
 * Render the self-contained DeepSWE task evidence table inside the website
 * shell. The iframe keeps the validated evidence layout isolated from
 * documentation theme styles while preserving same-origin clipboard access.
 *
 * @returns {React.JSX.Element} website task-evidence page
 */
export default function DeepSWEPlannerPage() {
  const plannerBaseUrl = useBaseUrl("/deepswe-planner/app/");
  const { colorMode } = useColorMode();
  const plannerUrl = `${plannerBaseUrl}?theme=${colorMode}`;

  return (
    <Layout
      title="DeepSWE task evidence"
      description="Compare DeepSWE task correlation, calibrated composite weight, and estimated published score and price."
      noFooter
    >
      <iframe
        title="DeepSWE task correlation and cost"
        src={plannerUrl}
        style={frameStyle}
        allow="clipboard-write"
      />
    </Layout>
  );
}
