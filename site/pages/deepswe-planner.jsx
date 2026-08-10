import React from "react";
import Layout from "@theme/Layout";
import useBaseUrl from "@docusaurus/useBaseUrl";

const frameStyle = {
  display: "block",
  width: "100%",
  minHeight: "calc(100vh - 60px)",
  border: 0,
  background: "#f3f5f2",
};

/**
 * Render the self-contained DeepSWE task evidence table inside the website
 * shell. The iframe keeps the validated evidence layout isolated from
 * documentation theme styles while preserving same-origin clipboard access.
 *
 * @returns {React.JSX.Element} website task-evidence page
 */
export default function DeepSWEPlannerPage() {
  const plannerUrl = useBaseUrl("/deepswe-planner/app/");

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
